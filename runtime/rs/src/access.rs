use crate::{
    Context, Options, Role,
    wire::{Wire as Envelope, decode},
};
use bitwire::{
    Detach, Message, ProfileFrame, ProfileKind, Receiver, ReturnAddress, SharedWire, Wire,
};
use bitwire::{Payload, PublicError};
use futures_util::FutureExt;
use nightseam_duplex::encode_path;
use std::{
    collections::{BTreeMap, VecDeque},
    future::Future,
    panic::AssertUnwindSafe,
    sync::{
        Arc, Mutex, Weak,
        atomic::{AtomicU64, Ordering},
    },
};
use tokio::sync::{Notify, oneshot};
use tokio_util::sync::CancellationToken;

pub(crate) type Answer = Result<Payload, PublicError>;
type Key = (usize, String);
fn key(message: &Message) -> Key {
    (
        message
            .returning
            .as_ref()
            .map_or(0, |r| Arc::as_ptr(r) as usize),
        message.frame.id.clone(),
    )
}
pub(crate) fn closed() -> PublicError {
    PublicError::new("disconnected", "Wire closed")
}
fn invalid(message: impl Into<String>) -> PublicError {
    PublicError::new("invalid", message)
}

#[derive(Default)]
pub(crate) struct Registry {
    state: Mutex<RegistryState>,
}
#[derive(Default)]
struct RegistryState {
    closed: bool,
    next: u64,
    receiver: Option<(u64, Receiver)>,
}
impl Registry {
    pub(crate) fn receive(self: &Arc<Self>, receiver: Receiver) -> Result<Detach, PublicError> {
        let mut state = self.state.lock().unwrap();
        if state.closed {
            return Err(closed());
        }
        if state.receiver.is_some() {
            return Err(PublicError::new(
                "receiver_exists",
                "endpoint already attached",
            ));
        }
        state.next += 1;
        let id = state.next;
        state.receiver = Some((id, receiver));
        let weak = Arc::downgrade(self);
        Ok(Arc::new(move || {
            if let Some(registry) = weak.upgrade() {
                let mut state = registry.state.lock().unwrap();
                if state.receiver.as_ref().is_some_and(|(held, _)| *held == id) {
                    state.receiver = None;
                }
            }
        }))
    }
    pub(crate) fn find(&self, _: &[String]) -> Option<Receiver> {
        self.state
            .lock()
            .unwrap()
            .receiver
            .as_ref()
            .filter(|(_, receiver)| receiver.message.is_some())
            .map(|(_, receiver)| receiver.clone())
    }
    pub(crate) fn close(&self, code: u16, reason: &str) {
        let receiver = {
            let mut state = self.state.lock().unwrap();
            state.closed = true;
            state.receiver.take()
        };
        if let Some((
            _,
            Receiver {
                closed: Some(callback),
                ..
            },
        )) = receiver
        {
            let reason = reason.to_owned();
            tokio::spawn(async move {
                callback(code, reason);
            });
        }
    }
}

pub(crate) fn validate(
    path: &[String],
    message: &Message,
    limit: usize,
) -> Result<(), PublicError> {
    let f = &message.frame;
    let name = encode_path(path);
    let envelope = Envelope {
        version: 1,
        kind: match f.kind {
            ProfileKind::Request => "request",
            ProfileKind::Response => "response",
            ProfileKind::Event => "event",
            ProfileKind::Cancel => "cancel",
        }
        .into(),
        id: f.id.clone(),
        method: if f.kind == ProfileKind::Request {
            name.clone()
        } else {
            String::new()
        },
        event: if f.kind == ProfileKind::Event {
            name
        } else {
            String::new()
        },
        params: f.params.clone(),
        result: f.result.clone(),
        error: f.error.clone(),
        data: f.data.clone(),
        trace: f.trace.clone(),
        meta: if f.meta.is_empty() {
            None
        } else {
            Some(f.meta.clone())
        },
    };
    let bytes = serde_json::to_vec(&envelope).map_err(|e| invalid(e.to_string()))?;
    if bytes.len() > limit {
        return Err(invalid("wire frame exceeds size limit"));
    }
    let role = match (f.kind == ProfileKind::Response, f.id.starts_with("c:")) {
        (true, true) | (false, false) => Role::Client,
        _ => Role::Server,
    };
    decode(&bytes, role).map_err(invalid)?;
    if matches!(f.kind, ProfileKind::Request | ProfileKind::Cancel) && message.returning.is_none() {
        return Err(invalid(
            "wire request or cancellation requires a return address",
        ));
    }
    Ok(())
}

pub(crate) fn context(message: &Message) -> Context {
    message
        .context
        .as_ref()
        .and_then(|c| c.downcast_ref::<Context>())
        .cloned()
        .unwrap_or_else(|| {
            Context::received(message.frame.trace.clone(), message.frame.meta.clone())
        })
}

pub(crate) async fn dispatch_request(
    ctx: Context,
    path: Vec<String>,
    params: Payload,
    receiver: Receiver,
    limit: usize,
) -> Answer {
    let (sender, mut receive) = oneshot::channel();
    let id = format!("c:{}", NEXT.fetch_add(1, Ordering::Relaxed) + 1);
    let returning = Arc::new(ReturnAddress {
        wire: Arc::new(Reply {
            sender: Mutex::new(Some(sender)),
            id: id.clone(),
            limit,
        }),
    });
    let mut frame = ProfileFrame::new(ProfileKind::Request);
    frame.id = id;
    frame.params = params;
    frame.trace = ctx.trace().clone();
    frame.meta = ctx.meta().clone();
    let request = Message {
        frame,
        returning: Some(returning),
        context: Some(Arc::new(ctx.clone())),
    };
    let callback = receiver.message.unwrap();
    callback(path.clone(), request.clone());
    tokio::select! {
        biased;
        answer=&mut receive=>answer.unwrap_or_else(|_| Err(closed())),
        _=ctx.cancelled()=>{
            let mut cancel = request;
            let mut frame = ProfileFrame::new(ProfileKind::Cancel);
            frame.id = cancel.frame.id.clone();
            frame.trace = cancel.frame.trace.clone();
            cancel.frame = frame;
            callback(path, cancel);
            // Withdrawal signals the captured handler. Its admitted work still
            // owns a slot until the handler answers, even when it ignores that
            // signal. Peer closure separately abandons this entire dispatch.
            receive.await.unwrap_or_else(|_| Err(closed()))
        },
    }
}

pub(crate) fn dispatch_event(ctx: Context, path: Vec<String>, data: Payload, receiver: Receiver) {
    let mut frame = ProfileFrame::new(ProfileKind::Event);
    frame.data = data;
    frame.trace = ctx.trace().clone();
    frame.meta = ctx.meta().clone();
    if let Some(callback) = receiver.message {
        callback(
            path,
            Message {
                frame,
                returning: None,
                context: Some(Arc::new(ctx)),
            },
        );
    }
}
pub(crate) fn response(request: &Message, answer: Answer) -> Result<(), PublicError> {
    let mut frame = ProfileFrame::new(ProfileKind::Response);
    frame.id = request.frame.id.clone();
    frame.trace = request.frame.trace.clone();
    match answer {
        Ok(value) => {
            frame.result = if value.is_absent() {
                Payload::from_json("null").unwrap()
            } else {
                value
            }
        }
        Err(error) => frame.error = Some(error.without_unpublished_proof()),
    }
    let wire = &request.returning.as_ref().ok_or_else(closed)?.wire;
    if let Err(error) = wire.send(&[], Message::new(frame.clone())) {
        frame.result = Payload::Absent;
        frame.error = Some(PublicError::new(
            "internal",
            "Response could not be encoded",
        ));
        wire.send(&[], Message::new(frame)).map_err(|_| error)?;
    }
    Ok(())
}

struct Reply {
    sender: Mutex<Option<oneshot::Sender<Answer>>>,
    id: String,
    limit: usize,
}
impl Wire for Reply {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        if !path.is_empty()
            || message.frame.kind != ProfileKind::Response
            || message.frame.id != self.id
        {
            return Err(invalid("invalid wire response"));
        }
        validate(path, &message, self.limit)?;
        let answer = message
            .frame
            .error
            .map_or(Ok(message.frame.result), |error| {
                Err(error.without_unpublished_proof())
            });
        self.sender
            .lock()
            .unwrap()
            .take()
            .ok_or_else(closed)?
            .send(answer)
            .map_err(|_| closed())
    }
}

static NEXT: AtomicU64 = AtomicU64::new(0);
/// Complete a request using its private return capability and a bounded deadline.
pub async fn call_wire(
    ctx: Context,
    wire: SharedWire,
    path: &[String],
    params: Payload,
    options: Options,
) -> Answer {
    let options = options.normalized();
    let (sender, receive) = oneshot::channel();
    let id = format!("c:{}", NEXT.fetch_add(1, Ordering::Relaxed) + 1);
    let returning = Arc::new(ReturnAddress {
        wire: Arc::new(Reply {
            sender: Mutex::new(Some(sender)),
            id: id.clone(),
            limit: options.max_frame_bytes,
        }),
    });
    let mut frame = ProfileFrame::new(ProfileKind::Request);
    frame.id = id;
    frame.params = if params.is_absent() {
        Payload::from_json("{}").unwrap()
    } else {
        params
    };
    frame.trace = ctx.trace().clone();
    frame.meta = ctx.outgoing_metadata();
    let message = Message {
        frame,
        returning: Some(returning.clone()),
        context: Some(Arc::new(ctx.clone().for_delivery())),
    };
    if ctx.is_cancelled() {
        return Err(PublicError::new("cancelled", "Request cancelled").unpublished());
    }
    wire.send(path, message.clone())
        .map_err(PublicError::unpublished)?;
    let outcome = tokio::select! {
        biased;
        result=receive=>return result.unwrap_or_else(|_| Err(closed())),
        _=ctx.cancelled()=>Err(PublicError::new("cancelled", "Request cancelled")),
        _=tokio::time::sleep(ctx.timeout().unwrap_or(options.request_timeout))=>Err(PublicError::new("request_timeout", "Request deadline exceeded")),
    };
    let mut cancel = message;
    let id = cancel.frame.id.clone();
    let trace = cancel.frame.trace.clone();
    cancel.frame = ProfileFrame::new(ProfileKind::Cancel);
    cancel.frame.id = id;
    cancel.frame.trace = trace;
    let _ = wire.send(path, cancel);
    outcome
}

/// Admit an event without allocating a response waiter.
pub fn emit_wire(
    ctx: Context,
    wire: SharedWire,
    path: &[String],
    data: Payload,
) -> Result<(), PublicError> {
    if ctx.is_cancelled() {
        return Err(PublicError::new("cancelled", "Request cancelled").unpublished());
    }
    let mut frame = ProfileFrame::new(ProfileKind::Event);
    frame.data = if data.is_absent() {
        Payload::from_json("null").unwrap()
    } else {
        data
    };
    frame.trace = ctx.trace().clone();
    frame.meta = ctx.outgoing_metadata();
    wire.send(
        path,
        Message {
            frame,
            returning: None,
            context: Some(Arc::new(ctx.for_delivery())),
        },
    )
    .map_err(PublicError::unpublished)
}

/// The root dispatches this callback asynchronously; handler futures run in
/// their own tasks while their request budget stays held until the response.
pub fn handle_wire<F, Fut>(
    wire: Arc<crate::Dispatcher>,
    path: &[String],
    handler: F,
) -> Result<Detach, PublicError>
where
    F: Fn(Context, Payload) -> Fut + Send + Sync + 'static,
    Fut: Future<Output = Answer> + Send + 'static,
{
    let handler = Arc::new(handler);
    let incoming: Arc<Mutex<BTreeMap<Key, Context>>> = Arc::new(Mutex::new(BTreeMap::new()));
    let ending = incoming.clone();
    wire.register(
        path,
        Receiver {
            closed: Some(Arc::new(move |_, _| {
                for ctx in ending.lock().unwrap().values() {
                    ctx.cancel();
                }
            })),
            ..Receiver::new(move |_, message| {
                let k = key(&message);
                if message.frame.kind == ProfileKind::Cancel {
                    if let Some(ctx) = incoming.lock().unwrap().get(&k) {
                        ctx.cancel();
                    }
                    return;
                }
                if message.frame.kind != ProfileKind::Request {
                    return;
                }
                let ctx = context(&message).child();
                incoming.lock().unwrap().insert(k.clone(), ctx.clone());
                let incoming = incoming.clone();
                let handler = handler.clone();
                tokio::spawn(async move {
                    let answer = AssertUnwindSafe(async {
                        handler(ctx.clone(), message.frame.params.clone()).await
                    })
                    .catch_unwind()
                    .await
                    .unwrap_or_else(|_| Err(PublicError::new("internal", "Internal error")));
                    let answer = if answer.is_ok() && ctx.is_cancelled() {
                        Err(PublicError::new("cancelled", "Request cancelled"))
                    } else {
                        answer
                    };
                    let _ = response(&message, answer);
                    incoming.lock().unwrap().remove(&k);
                    ctx.cancel();
                });
            })
        },
    )
}

pub fn forward_wire(
    left: Arc<dyn bitwire::Endpoint>,
    right: Arc<dyn bitwire::Endpoint>,
) -> Result<Detach, PublicError> {
    let detaches: Arc<Mutex<Option<Vec<Detach>>>> = Arc::new(Mutex::new(Some(Vec::new())));
    let stop: Detach = {
        let detaches = detaches.clone();
        Arc::new(move || {
            let held = detaches.lock().unwrap().take();
            if let Some(held) = held {
                for detach in held {
                    detach();
                }
            }
        })
    };
    for (source, destination) in [(left.clone(), right.clone()), (right, left)] {
        let ending = stop.clone();
        let failure = stop.clone();
        let receiver = Receiver {
            closed: Some(Arc::new(move |_, _| ending())),
            ..Receiver::new(move |path, message| {
                if let Err(error) = destination.send(&path, message.clone()) {
                    failure();
                    if message.frame.kind == ProfileKind::Request {
                        let _ = response(&message, Err(error));
                    }
                }
            })
        };
        match source.receive(receiver) {
            Ok(detach) => {
                let mut held = detaches.lock().unwrap();
                if let Some(held) = &mut *held {
                    held.push(detach);
                } else {
                    drop(held);
                    detach();
                    return Err(closed());
                }
            }
            Err(error) => {
                stop();
                return Err(error);
            }
        }
    }
    Ok(stop)
}

struct Delivery {
    path: Vec<String>,
    message: Message,
    call: Option<Arc<LocalCall>>,
    refusal: Option<PublicError>,
}
struct LocalCall {
    key: Key,
    request: Message,
    path: Vec<String>,
    state: Mutex<LocalCallState>,
}
#[derive(Default)]
struct LocalCallState {
    returning: Option<Weak<ReturnAddress>>,
    receiver: Option<Receiver>,
    cancel: Option<Context>,
    completed: bool,
    responded: bool,
    cancel_queued: bool,
    cancelled: bool,
    active: bool,
}
#[derive(Default)]
struct End {
    queue: VecDeque<Delivery>,
    data: usize,
    active: usize,
    calls: BTreeMap<Key, Arc<LocalCall>>,
}
#[derive(Default)]
struct PairState {
    closed: bool,
    ends: [End; 2],
}
struct Pair {
    state: Mutex<PairState>,
    options: Options,
    registries: [Arc<Registry>; 2],
    wake: [Notify; 2],
    stop: CancellationToken,
}
struct LocalWire {
    pair: Arc<Pair>,
    side: usize,
    _life: Arc<PairLife>,
}
struct PairLife(Weak<Pair>);
impl Drop for PairLife {
    fn drop(&mut self) {
        if let Some(pair) = self.0.upgrade() {
            pair.close(1000, "local wire released");
        }
    }
}

// Keep the upstream endpoint type visible in the public signature.
#[allow(clippy::type_complexity)]
pub fn wire_pair(
    options: Options,
) -> Result<(Arc<dyn bitwire::Endpoint>, Arc<dyn bitwire::Endpoint>), PublicError> {
    if !options.handlers.is_empty() || !options.events.is_empty() {
        return Err(invalid("local wires install receivers through receive"));
    }
    let pair = Arc::new(Pair {
        state: Mutex::new(PairState::default()),
        options: options.normalized(),
        registries: [Arc::default(), Arc::default()],
        wake: [Notify::new(), Notify::new()],
        stop: CancellationToken::new(),
    });
    let life = Arc::new(PairLife(Arc::downgrade(&pair)));
    for side in 0..2 {
        tokio::spawn(pair.clone().run(side));
    }
    Ok((
        Arc::new(LocalWire {
            pair: pair.clone(),
            side: 0,
            _life: life.clone(),
        }),
        Arc::new(LocalWire {
            pair,
            side: 1,
            _life: life,
        }),
    ))
}
impl Wire for LocalWire {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        validate(path, &message, self.pair.options.max_frame_bytes)?;
        if message.frame.kind == ProfileKind::Response {
            return Err(invalid("responses use a return address"));
        }
        self.pair.admit(1 - self.side, path.to_vec(), message)
    }
}
impl bitwire::Endpoint for LocalWire {
    fn receive(&self, receiver: Receiver) -> Result<Detach, PublicError> {
        self.pair.registries[self.side].receive(receiver)
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        self.pair.close(code, reason);
        Ok(())
    }
}
impl Pair {
    fn admit(
        self: &Arc<Self>,
        side: usize,
        path: Vec<String>,
        mut message: Message,
    ) -> Result<(), PublicError> {
        let mut state = self.state.lock().unwrap();
        if state.closed {
            return Err(closed());
        }
        let end = &mut state.ends[side];
        let k = key(&message);
        let mut call = None;
        let mut refusal = None;
        if message.frame.kind == ProfileKind::Cancel {
            let Some(held) = end.calls.get(&k).cloned() else {
                return Ok(());
            };
            let mut status = held.state.lock().unwrap();
            if status.completed || status.cancel_queued || status.cancelled {
                return Ok(());
            }
            status.cancel_queued = true;
            message.returning = status.returning.as_ref().and_then(Weak::upgrade);
            drop(status);
            call = Some(held);
        } else {
            if end.data >= self.options.queue_capacity {
                drop(state);
                self.close(4011, "local wire queue limit reached");
                return Err(PublicError::new(
                    "backpressure",
                    "local wire queue limit reached",
                ));
            }
            end.data += 1;
            if message.frame.kind == ProfileKind::Request {
                if end.calls.contains_key(&k)
                    || end.calls.len() >= self.options.max_pending_requests
                {
                    let code = if end.calls.contains_key(&k) {
                        "invalid_message"
                    } else {
                        "busy"
                    };
                    refusal = Some(PublicError::new(code, "Request admission refused"));
                } else {
                    let held = Arc::new(LocalCall {
                        key: k.clone(),
                        request: message.clone(),
                        path: path.clone(),
                        state: Mutex::new(LocalCallState::default()),
                    });
                    message.returning = Some(Arc::new(ReturnAddress {
                        wire: Arc::new(LocalReturn {
                            pair: Arc::downgrade(self),
                            side,
                            call: held.clone(),
                        }),
                    }));
                    held.state.lock().unwrap().returning =
                        message.returning.as_ref().map(Arc::downgrade);
                    end.calls.insert(k, held.clone());
                    call = Some(held);
                }
            }
        }
        end.queue.push_back(Delivery {
            path,
            message,
            call,
            refusal,
        });
        drop(state);
        self.wake[side].notify_one();
        Ok(())
    }
    fn close(&self, code: u16, reason: &str) {
        let (calls, refusals) = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return;
            }
            state.closed = true;
            let mut refusals = Vec::new();
            let calls = state
                .ends
                .iter_mut()
                .flat_map(|end| {
                    refusals.extend(
                        end.queue
                            .drain(..)
                            .filter_map(|delivery| delivery.refusal.map(|_| delivery.message)),
                    );
                    end.data = 0;
                    std::mem::take(&mut end.calls).into_values()
                })
                .collect::<Vec<_>>();
            (calls, refusals)
        };
        self.stop.cancel();
        for request in refusals {
            tokio::spawn(async move {
                let _ = response(&request, Err(closed()));
            });
        }
        for registry in &self.registries {
            registry.close(code, reason);
        }
        for call in calls {
            let mut status = call.state.lock().unwrap();
            if let Some(ctx) = &status.cancel {
                ctx.cancel();
            }
            if !status.responded {
                status.responded = true;
                let request = call.request.clone();
                tokio::spawn(async move {
                    let _ = response(&request, Err(closed()));
                });
            }
            status.completed = true;
        }
    }
    fn complete(&self, side: usize, call: &Arc<LocalCall>) {
        let mut state = self.state.lock().unwrap();
        let end = &mut state.ends[side];
        let mut status = call.state.lock().unwrap();
        if !status.completed {
            status.completed = true;
            if status.active {
                end.active -= 1;
                status.active = false;
            }
            if let Some(ctx) = &status.cancel {
                ctx.cancel();
            }
        }
        if !status.cancel_queued
            && end
                .calls
                .get(&call.key)
                .is_some_and(|held| Arc::ptr_eq(held, call))
        {
            end.calls.remove(&call.key);
        }
    }
    async fn run(self: Arc<Self>, side: usize) {
        loop {
            let delivery = {
                let mut state = self.state.lock().unwrap();
                if state.closed {
                    return;
                }
                let end = &mut state.ends[side];
                let delivery = end.queue.pop_front();
                if delivery
                    .as_ref()
                    .is_some_and(|d| d.message.frame.kind != ProfileKind::Cancel)
                {
                    end.data -= 1;
                }
                delivery
            };
            let Some(mut delivery) = delivery else {
                tokio::select! { _=self.stop.cancelled()=>return, _=self.wake[side].notified()=>{} }
                continue;
            };
            if let Some(error) = delivery.refusal {
                let _ = response(&delivery.message, Err(error));
                continue;
            }
            if delivery.message.frame.kind == ProfileKind::Cancel {
                let call = delivery.call.unwrap();
                let mut state = self.state.lock().unwrap();
                let mut status = call.state.lock().unwrap();
                status.cancel_queued = false;
                status.cancelled = true;
                if let Some(ctx) = &status.cancel {
                    ctx.cancel();
                }
                let receiver = if status.completed {
                    state.ends[side].calls.remove(&call.key);
                    None
                } else {
                    status.receiver.clone()
                };
                drop(status);
                drop(state);
                if let Some(receiver) = receiver {
                    if let Some(callback) = receiver.message {
                        callback(call.path.clone(), delivery.message);
                    }
                }
                continue;
            }
            let receiver = self.registries[side].find(&delivery.path);
            if let Some(call) = &delivery.call {
                let mut state = self.state.lock().unwrap();
                let end = &mut state.ends[side];
                if receiver.is_none() || end.active >= self.options.max_concurrent_handlers {
                    let code = if receiver.is_none() {
                        "method_not_found"
                    } else {
                        "busy"
                    };
                    drop(state);
                    let _ = response(
                        &delivery.message,
                        Err(PublicError::new(code, "Request dispatch refused")),
                    );
                    continue;
                }
                end.active += 1;
                let ctx = context(&delivery.message).child();
                delivery.message.context = Some(Arc::new(ctx.clone()));
                let mut status = call.state.lock().unwrap();
                status.active = true;
                status.receiver = receiver.clone();
                status.cancel = Some(ctx.clone());
                drop(status);
                drop(state);
                let pair = Arc::downgrade(&self);
                let call = call.clone();
                let within = self.options.request_timeout;
                tokio::spawn(async move {
                    tokio::select! { _=ctx.cancelled()=>return, _=tokio::time::sleep(within)=>{} }
                    if let Some(pair) = pair.upgrade() {
                        pair.expire(side, call);
                    }
                });
            }
            if let Some(receiver) = receiver {
                if let Some(callback) = receiver.message {
                    let pair = Arc::downgrade(&self);
                    let within = self.options.write_timeout;
                    let task = tokio::task::spawn_blocking(move || {
                        std::panic::catch_unwind(AssertUnwindSafe(|| {
                            callback(delivery.path, delivery.message)
                        }))
                    });
                    if !matches!(tokio::time::timeout(within, task).await, Ok(Ok(Ok(())))) {
                        if let Some(pair) = pair.upgrade() {
                            pair.close(4011, "wire receiver failed or stalled");
                        }
                        return;
                    }
                }
            }
        }
    }
    fn expire(self: &Arc<Self>, side: usize, call: Arc<LocalCall>) {
        let mut state = self.state.lock().unwrap();
        if state.closed {
            return;
        }
        let mut status = call.state.lock().unwrap();
        if status.completed {
            return;
        }
        if let Some(ctx) = &status.cancel {
            ctx.cancel();
        }
        if !status.cancelled && !status.cancel_queued {
            status.cancel_queued = true;
            let mut frame = ProfileFrame::new(ProfileKind::Cancel);
            frame.id = call.request.frame.id.clone();
            state.ends[side].queue.push_back(Delivery {
                path: call.path.clone(),
                message: Message {
                    frame,
                    returning: status.returning.as_ref().and_then(Weak::upgrade),
                    context: None,
                },
                call: Some(call.clone()),
                refusal: None,
            });
        }
        let respond = !status.responded;
        status.responded = true;
        drop(status);
        drop(state);
        self.wake[side].notify_one();
        if respond {
            let _ = response(
                &call.request,
                Err(PublicError::new("cancelled", "Request cancelled")),
            );
        }
    }
}
struct LocalReturn {
    pair: Weak<Pair>,
    side: usize,
    call: Arc<LocalCall>,
}
impl Wire for LocalReturn {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        let pair = self.pair.upgrade().ok_or_else(closed)?;
        if !path.is_empty()
            || message.frame.kind != ProfileKind::Response
            || message.frame.id != self.call.request.frame.id
        {
            return Err(invalid("invalid wire response"));
        }
        validate(path, &message, pair.options.max_frame_bytes)?;
        let respond = {
            let mut status = self.call.state.lock().unwrap();
            let respond = !status.responded && !status.completed;
            status.responded = true;
            respond
        };
        pair.complete(self.side, &self.call);
        if !respond {
            return Err(closed());
        }
        self.call
            .request
            .returning
            .as_ref()
            .ok_or_else(closed)?
            .wire
            .send(path, message)
            .map_err(PublicError::without_unpublished_proof)
    }
}
