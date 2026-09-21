use crate::{
    Payload, PublicError, Role, Trace,
    wire::{Wire, decode, valid_traceparent},
};
use futures_util::{FutureExt, future::BoxFuture};
use nightseam_duplex::{
    Close, Detach, Frame, Message, ProfileKind, Receiver, SharedConnection, SharedWire,
    decode_path, encode_path,
};
use std::{
    collections::{BTreeMap, VecDeque},
    future::Future,
    panic::AssertUnwindSafe,
    sync::{
        Arc, Mutex, Weak,
        atomic::{AtomicU64, Ordering},
    },
    time::Duration,
};
use tokio::{
    sync::{Notify, mpsc, watch},
    time::{Instant, timeout},
};
use tokio_util::sync::CancellationToken;

type Answer = Result<Payload, PublicError>;
type Handler = Arc<dyn Fn(Context, Payload) -> BoxFuture<'static, Answer> + Send + Sync>;
type EventHandler = Arc<dyn Fn(Context, Payload) -> BoxFuture<'static, ()> + Send + Sync>;
type Listener = Arc<dyn Fn(Context, String, Payload) -> BoxFuture<'static, ()> + Send + Sync>;

/// Per-connection bounds. Zero selects the profile's documented default.
#[derive(Clone)]
pub struct Options {
    pub max_concurrent_handlers: usize,
    pub max_pending_requests: usize,
    pub queue_capacity: usize,
    pub max_frame_bytes: usize,
    pub request_timeout: Duration,
    pub write_timeout: Duration,
    pub handlers: BTreeMap<String, Handler>,
    pub events: BTreeMap<String, EventHandler>,
}
impl Default for Options {
    fn default() -> Self {
        Self {
            max_concurrent_handlers: 64,
            max_pending_requests: 128,
            queue_capacity: 128,
            max_frame_bytes: 1 << 20,
            request_timeout: Duration::from_secs(30),
            write_timeout: Duration::from_secs(10),
            handlers: BTreeMap::new(),
            events: BTreeMap::new(),
        }
    }
}
impl Options {
    pub(crate) fn normalized(mut self) -> Self {
        let defaults = Self::default();
        if self.max_concurrent_handlers == 0 {
            self.max_concurrent_handlers = defaults.max_concurrent_handlers;
        }
        if self.max_pending_requests == 0 {
            self.max_pending_requests = defaults.max_pending_requests;
        }
        if self.queue_capacity == 0 {
            self.queue_capacity = defaults.queue_capacity;
        }
        if self.max_frame_bytes == 0 {
            self.max_frame_bytes = defaults.max_frame_bytes;
        }
        if self.request_timeout.is_zero() {
            self.request_timeout = defaults.request_timeout;
        }
        if self.write_timeout.is_zero() {
            self.write_timeout = defaults.write_timeout;
        }
        self
    }
}

/// Context separates metadata received from metadata chosen for an outgoing
/// call. Cloning it propagates cancellation and trace, but no credentials.
#[derive(Clone, Default)]
pub struct Context {
    cancel: CancellationToken,
    trace: Trace,
    incoming_meta: BTreeMap<String, String>,
    outgoing_meta: Option<BTreeMap<String, String>>,
    within: Option<Duration>,
    peer: Weak<Inner>,
    life: Weak<Life>,
}
impl Context {
    pub(crate) fn received(trace: Trace, meta: BTreeMap<String, String>) -> Self {
        Self {
            trace,
            incoming_meta: meta,
            ..Self::default()
        }
    }
    pub(crate) fn outgoing_metadata(&self) -> BTreeMap<String, String> {
        self.outgoing_meta.clone().unwrap_or_default()
    }
    pub(crate) fn timeout(&self) -> Option<Duration> {
        self.within
    }
    pub(crate) fn for_delivery(mut self) -> Self {
        self.incoming_meta = self.outgoing_metadata();
        self.outgoing_meta = None;
        self
    }
    pub(crate) fn child(mut self) -> Self {
        self.cancel = self.cancel.child_token();
        self
    }
    pub async fn cancelled(&self) {
        self.cancel.cancelled().await;
    }
    pub fn is_cancelled(&self) -> bool {
        self.cancel.is_cancelled()
    }
    pub fn cancel(&self) {
        self.cancel.cancel();
    }
    pub fn trace(&self) -> &Trace {
        &self.trace
    }
    pub fn meta(&self) -> &BTreeMap<String, String> {
        &self.incoming_meta
    }
    pub fn with_meta(mut self, mut meta: BTreeMap<String, String>) -> Self {
        meta.retain(|key, _| !key.starts_with("nightseam."));
        self.outgoing_meta = if meta.is_empty() { None } else { Some(meta) };
        self
    }
    pub fn with_timeout(mut self, within: Duration) -> Self {
        self.within = Some(within);
        self
    }
    pub fn peer(&self) -> Option<Peer> {
        Some(Peer {
            inner: self.peer.upgrade()?,
            _life: self.life.upgrade()?,
        })
    }
}

/// A request starts immediately; its result and explicit cancellation are
/// independent of when a consumer begins waiting for it.
#[derive(Clone)]
pub struct Call {
    result: watch::Receiver<Option<Answer>>,
    cancel: CancellationToken,
    admitted: watch::Receiver<bool>,
    finished: watch::Receiver<bool>,
}
impl Call {
    async fn admitted(&self) {
        let mut admitted = self.admitted.clone();
        while !*admitted.borrow_and_update() {
            if admitted.changed().await.is_err() {
                return;
            }
        }
    }
    async fn finished(&self) {
        let mut finished = self.finished.clone();
        while !*finished.borrow_and_update() {
            if finished.changed().await.is_err() {
                return;
            }
        }
    }
    pub fn cancel(&self) {
        self.cancel.cancel();
    }
    pub async fn result(&self) -> Answer {
        let mut result = self.result.clone();
        loop {
            if let Some(answer) = result.borrow().clone() {
                return answer;
            }
            if result.changed().await.is_err() {
                return Err(disconnected());
            }
        }
    }
}

#[derive(Clone)]
pub struct Peer {
    inner: Arc<Inner>,
    _life: Arc<Life>,
}
struct Life(Weak<Inner>);
impl Drop for Life {
    fn drop(&mut self) {
        if let Some(inner) = self.0.upgrade() {
            inner.finish(Close::new(1006, ""), false);
        }
    }
}
struct Inner {
    conn: SharedConnection,
    role: Role,
    options: Options,
    next: AtomicU64,
    stop: CancellationToken,
    ended: Mutex<Option<Close>>,
    outgoing: mpsc::Sender<Vec<u8>>,
    pending: Mutex<BTreeMap<String, watch::Sender<Option<Answer>>>>,
    incoming: Mutex<BTreeMap<String, CancellationToken>>,
    handlers: Mutex<BTreeMap<String, Handler>>,
    events: Mutex<BTreeMap<String, EventHandler>>,
    listeners: Mutex<BTreeMap<u64, Listener>>,
    life: Mutex<Weak<Life>>,
    routes: Arc<crate::access::Registry>,
    registrations: Mutex<()>,
    wire_queue: Mutex<WireQueue>,
    wire_wake: Notify,
}

/// Dropping or cancelling this registration stops just this listener.
pub struct Subscription {
    inner: Weak<Inner>,
    id: u64,
}
impl Subscription {
    pub fn cancel(&self) {
        if let Some(inner) = self.inner.upgrade() {
            inner.listeners.lock().unwrap().remove(&self.id);
        }
    }
}
impl Drop for Subscription {
    fn drop(&mut self) {
        self.cancel();
    }
}

impl Peer {
    /// Handlers in Options are installed before the first frame is read.
    pub fn over(conn: SharedConnection, role: Role, options: Options) -> Result<Self, PublicError> {
        let options = options.normalized();
        if options
            .handlers
            .keys()
            .chain(options.events.keys())
            .any(String::is_empty)
        {
            return Err(invalid("handler name is empty"));
        }
        let (outgoing, outputs) = mpsc::channel(options.queue_capacity);
        let (events, event_rx) = mpsc::channel(options.queue_capacity);
        let inner = Arc::new(Inner {
            conn,
            role,
            next: AtomicU64::new(0),
            stop: CancellationToken::new(),
            ended: Mutex::new(None),
            outgoing,
            pending: Mutex::new(BTreeMap::new()),
            incoming: Mutex::new(BTreeMap::new()),
            handlers: Mutex::new(options.handlers.clone()),
            events: Mutex::new(options.events.clone()),
            listeners: Mutex::new(BTreeMap::new()),
            life: Mutex::new(Weak::new()),
            routes: Arc::default(),
            registrations: Mutex::new(()),
            wire_queue: Mutex::new(WireQueue::default()),
            wire_wake: Notify::new(),
            options,
        });
        let life = Arc::new(Life(Arc::downgrade(&inner)));
        *inner.life.lock().unwrap() = Arc::downgrade(&life);
        tokio::spawn(inner.clone().write_loop(outputs));
        tokio::spawn(inner.clone().read_loop(events));
        tokio::spawn(inner.clone().event_loop(event_rx));
        tokio::spawn(inner.clone().wire_loop());
        Ok(Self { inner, _life: life })
    }
    fn wire_exact_registered(&self, name: &str) -> bool {
        decode_path(name)
            .ok()
            .and_then(|path| self.inner.routes.find(&path))
            .is_some_and(|receiver| !receiver.namespace)
    }
    pub fn wire(&self) -> SharedWire {
        Arc::new(PeerWire(self.clone()))
    }
    pub fn close_with(&self, code: u16, reason: &str) {
        self.inner.finish(Close::new(code, reason), true);
    }
    pub fn role(&self) -> Role {
        self.inner.role
    }
    pub fn subprotocol(&self) -> &str {
        self.inner.conn.subprotocol()
    }
    pub fn close(&self) {
        self.inner.finish(Close::new(1000, ""), true);
    }
    pub fn abort(&self) {
        self.inner.finish(Close::new(1006, ""), false);
    }
    pub async fn wait_closed(&self) -> Close {
        self.inner.stop.cancelled().await;
        self.inner.ended.lock().unwrap().clone().unwrap()
    }
    pub fn handle<F, Fut>(&self, name: &str, handler: F) -> Result<(), PublicError>
    where
        F: Fn(Context, Payload) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Answer> + Send + 'static,
    {
        if name.is_empty() {
            return Err(invalid("handler name is empty"));
        }
        if self.inner.stop.is_cancelled() {
            return Err(disconnected());
        }
        let _registration = self.inner.registrations.lock().unwrap();
        if self.wire_exact_registered(name) {
            return Err(invalid("wire path already registered"));
        }
        let mut handlers = self.inner.handlers.lock().unwrap();
        if handlers.contains_key(name) {
            return Err(invalid("method already registered"));
        }
        handlers.insert(
            name.to_owned(),
            Arc::new(move |ctx, value| Box::pin(handler(ctx, value))),
        );
        Ok(())
    }
    pub fn on_event<F, Fut>(&self, name: &str, handler: F) -> Result<(), PublicError>
    where
        F: Fn(Context, Payload) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = ()> + Send + 'static,
    {
        if name.is_empty() {
            return Err(invalid("event name is empty"));
        }
        if self.inner.stop.is_cancelled() {
            return Err(disconnected());
        }
        let _registration = self.inner.registrations.lock().unwrap();
        if self.wire_exact_registered(name) {
            return Err(invalid("wire path already registered"));
        }
        let mut handlers = self.inner.events.lock().unwrap();
        if handlers.contains_key(name) {
            return Err(invalid("event already registered"));
        }
        handlers.insert(
            name.to_owned(),
            Arc::new(move |ctx, value| Box::pin(handler(ctx, value))),
        );
        Ok(())
    }
    pub fn listen_events<F, Fut>(&self, listener: F) -> Subscription
    where
        F: Fn(Context, String, Payload) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = ()> + Send + 'static,
    {
        let id = self.inner.next.fetch_add(1, Ordering::Relaxed);
        self.inner.listeners.lock().unwrap().insert(
            id,
            Arc::new(move |ctx, name, data| Box::pin(listener(ctx, name, data))),
        );
        Subscription {
            inner: Arc::downgrade(&self.inner),
            id,
        }
    }
    pub fn call(&self, method: &str, params: Payload) -> Call {
        self.call_with(Context::default(), method, params)
    }
    pub fn call_with(&self, ctx: Context, method: &str, params: Payload) -> Call {
        self.call_with_trace(ctx, method, params, None)
    }
    fn call_with_trace(
        &self,
        ctx: Context,
        method: &str,
        params: Payload,
        supplied: Option<Trace>,
    ) -> Call {
        let (admission, admitted) = watch::channel(false);
        let (completion, finished) = watch::channel(false);
        let routed = supplied.is_some();
        let (answer, result) = watch::channel(None);
        let cancel = CancellationToken::new();
        let call = Call {
            result,
            cancel: cancel.clone(),
            admitted,
            finished,
        };
        let reject = |error| {
            admission.send_replace(true);
            completion.send_replace(true);
            answer.send_replace(Some(Err(error)));
        };
        if method.is_empty() {
            reject(invalid("method is empty"));
            return call;
        }
        if self.inner.stop.is_cancelled() {
            reject(disconnected());
            return call;
        }
        if ctx.is_cancelled() {
            reject(cancelled());
            return call;
        }
        let trace = match supplied.map(Ok).unwrap_or_else(|| child_trace(&ctx.trace)) {
            Ok(trace) => trace,
            Err(error) => {
                reject(error);
                return call;
            }
        };
        let id = format!(
            "{}{}",
            self.inner.role.prefix(),
            self.inner.next.fetch_add(1, Ordering::Relaxed) + 1
        );
        {
            let mut pending = self.inner.pending.lock().unwrap();
            if pending.len() >= self.inner.options.max_pending_requests {
                reject(PublicError::new("busy", "Outstanding call limit reached"));
                return call;
            }
            pending.insert(id.clone(), answer.clone());
        }
        let inner = self.inner.clone();
        let frame = Wire {
            version: 1,
            kind: "request".into(),
            id: id.clone(),
            method: method.into(),
            params: default_payload(params, "{}"),
            trace: trace.clone(),
            meta: ctx.outgoing_meta.clone(),
            ..Wire::default()
        };
        let deadline = Instant::now()
            + ctx
                .within
                .unwrap_or(inner.options.request_timeout)
                .min(inner.options.request_timeout);
        tokio::spawn(async move {
            let enqueued = tokio::select! {
                result=inner.enqueue(frame,&cancel)=>result,
                _=ctx.cancelled()=>Err(cancelled()),
                _=tokio::time::sleep_until(deadline)=>Err(timed_out()),
            };
            admission.send_replace(true);
            let (outcome, cancel_remote) = match enqueued {
                Err(error) => (Err(error), false),
                Ok(()) => {
                    let mut incoming = answer.subscribe();
                    let reply = async {
                        loop {
                            if let Some(answer) = incoming.borrow().clone() {
                                return answer;
                            }
                            if incoming.changed().await.is_err() {
                                return Err(disconnected());
                            }
                        }
                    };
                    tokio::select! {
                        biased;
                        answer=reply=>(answer,false),
                        _=inner.stop.cancelled()=>(Err(disconnected()),false),
                        _=cancel.cancelled()=>(Err(cancelled()),true),
                        _=ctx.cancelled()=>(Err(cancelled()),true),
                        _=tokio::time::sleep_until(deadline)=>(Err(timed_out()),true),
                    }
                }
            };
            inner.pending.lock().unwrap().remove(&id);
            answer.send_replace(Some(outcome));
            if cancel_remote {
                let frame = Wire {
                    version: 1,
                    kind: "cancel".into(),
                    id,
                    trace,
                    ..Wire::default()
                };
                if routed {
                    let _ = inner.enqueue(frame, &inner.stop).await;
                } else if let Ok(data) = inner.encode(&frame) {
                    let _ = inner.outgoing.try_send(data);
                }
            }
            completion.send_replace(true);
        });
        call
    }
    pub async fn emit(&self, event: &str, data: Payload) -> Result<(), PublicError> {
        self.emit_with(Context::default(), event, data).await
    }
    pub async fn emit_with(
        &self,
        ctx: Context,
        event: &str,
        data: Payload,
    ) -> Result<(), PublicError> {
        self.emit_with_trace(ctx, event, data, None).await
    }
    async fn emit_with_trace(
        &self,
        ctx: Context,
        event: &str,
        data: Payload,
        supplied: Option<Trace>,
    ) -> Result<(), PublicError> {
        if event.is_empty() {
            return Err(invalid("event is empty"));
        }
        let frame = Wire {
            version: 1,
            kind: "event".into(),
            event: event.into(),
            data: default_payload(data, "null"),
            trace: supplied
                .map(Ok)
                .unwrap_or_else(|| child_trace(&ctx.trace))?,
            meta: ctx.outgoing_meta.clone(),
            ..Wire::default()
        };
        self.inner.enqueue(frame, &ctx.cancel).await
    }
}

impl Inner {
    async fn wire_loop(self: Arc<Self>) {
        loop {
            let delivery = {
                let mut queue = self.wire_queue.lock().unwrap();
                if self.stop.is_cancelled() {
                    return;
                }
                let delivery = queue.deliveries.pop_front();
                if delivery
                    .as_ref()
                    .is_some_and(|d| d.message.frame.kind != ProfileKind::Cancel)
                {
                    queue.data -= 1;
                }
                delivery
            };
            let Some(delivery) = delivery else {
                tokio::select! { _=self.stop.cancelled()=>return, _=self.wire_wake.notified()=>{} }
                continue;
            };
            let WireDelivery {
                path,
                message,
                pending,
                refusal,
            } = delivery;
            if let Some(error) = refusal {
                let _ = crate::access::response(&message, Err(error));
                continue;
            }
            if message.frame.kind == ProfileKind::Cancel {
                let pending = pending.unwrap();
                let dispatch = {
                    let mut queue = self.wire_queue.lock().unwrap();
                    let key = wire_key(&message);
                    if let Some(state) = queue
                        .calls
                        .get_mut(&key)
                        .filter(|state| Arc::ptr_eq(&state.pending, &pending))
                    {
                        state.cancel_queued = false;
                        state.cancelled = true;
                        if state.completed {
                            queue.calls.remove(&key);
                            false
                        } else {
                            true
                        }
                    } else {
                        false
                    }
                };
                if dispatch {
                    pending.ctx.cancel();
                    let call = pending.call.lock().unwrap().clone();
                    if let Some(call) = call {
                        call.finished().await;
                    }
                }
                continue;
            }
            let life = self.life.lock().unwrap().upgrade();
            let Some(life) = life else {
                return;
            };
            let peer = Peer {
                inner: self.clone(),
                _life: life,
            };
            let name = encode_path(&path);
            if message.frame.kind == ProfileKind::Event {
                let ctx = Context::received(message.frame.trace.clone(), BTreeMap::new())
                    .with_meta(message.frame.meta.clone());
                if peer
                    .emit_with_trace(ctx, &name, message.frame.data, Some(message.frame.trace))
                    .await
                    .is_err()
                {
                    self.finish(Close::new(1006, "wire event publication failed"), false);
                    return;
                }
            } else {
                let pending = pending.unwrap();
                let call = peer.call_with_trace(
                    pending.ctx.clone(),
                    &name,
                    message.frame.params.clone(),
                    Some(message.frame.trace.clone()),
                );
                *pending.call.lock().unwrap() = Some(call.clone());
                call.admitted().await;
                let inner = self.clone();
                tokio::spawn(async move {
                    let answer = call.result().await;
                    let respond = {
                        let mut queue = inner.wire_queue.lock().unwrap();
                        let key = wire_key(&message);
                        if let Some(state) = queue
                            .calls
                            .get_mut(&key)
                            .filter(|state| Arc::ptr_eq(&state.pending, &pending))
                        {
                            state.completed = true;
                            if !state.cancel_queued {
                                queue.calls.remove(&key);
                            }
                            true
                        } else {
                            false
                        }
                    };
                    if respond {
                        let _ = crate::access::response(&message, answer);
                    }
                });
            }
        }
    }
    fn context(
        self: &Arc<Self>,
        trace: Trace,
        meta: Option<BTreeMap<String, String>>,
        cancel: CancellationToken,
    ) -> Context {
        Context {
            cancel,
            trace,
            incoming_meta: meta.unwrap_or_default(),
            peer: Arc::downgrade(self),
            life: self.life.lock().unwrap().clone(),
            ..Context::default()
        }
    }
    fn finish(self: &Arc<Self>, mut close: Close, send: bool) {
        let mut ended = self.ended.lock().unwrap();
        if ended.is_some() {
            return;
        }
        while close.reason.len() > 123 {
            close.reason.pop();
        }
        *ended = Some(close.clone());
        drop(ended);
        self.stop.cancel();
        self.routes.close(close.code, &close.reason);
        let queued = {
            let mut queue = self.wire_queue.lock().unwrap();
            for delivery in queue.deliveries.drain(..) {
                if delivery.refusal.is_some() {
                    tokio::spawn(async move {
                        let _ = crate::access::response(&delivery.message, Err(disconnected()));
                    });
                }
            }
            queue.data = 0;
            std::mem::take(&mut queue.calls)
        };
        for state in queued.into_values() {
            state.pending.ctx.cancel();
            if !state.completed {
                tokio::spawn(async move {
                    let _ = crate::access::response(&state.pending.request, Err(disconnected()));
                });
            }
        }
        for (_, answer) in std::mem::take(&mut *self.pending.lock().unwrap()) {
            answer.send_replace(Some(Err(disconnected())));
        }
        for cancel in self.incoming.lock().unwrap().values() {
            cancel.cancel();
        }
        if send {
            let conn = self.conn.clone();
            let within = self.options.write_timeout;
            tokio::spawn(async move {
                if timeout(within, conn.close(close.code, &close.reason))
                    .await
                    .is_err()
                {
                    conn.abort();
                }
            });
        } else {
            self.conn.abort();
        }
    }
    fn encode(&self, frame: &Wire) -> Result<Vec<u8>, PublicError> {
        let data = serde_json::to_vec(frame).map_err(|e| invalid(e.to_string()))?;
        if data.len() > self.options.max_frame_bytes {
            return Err(invalid("duplex frame exceeds size limit"));
        }
        Ok(data)
    }
    async fn enqueue(
        self: &Arc<Self>,
        frame: Wire,
        cancel: &CancellationToken,
    ) -> Result<(), PublicError> {
        if cancel.is_cancelled() {
            return Err(cancelled());
        }
        if self.stop.is_cancelled() {
            return Err(disconnected());
        }
        let data = self.encode(&frame)?;
        tokio::select! {
            biased;
            _=self.stop.cancelled()=>Err(disconnected()),
            _=cancel.cancelled()=>Err(cancelled()),
            result=timeout(self.options.write_timeout,self.outgoing.send(data))=>match result {
                Ok(Ok(()))=>Ok(()),Ok(Err(_))=>Err(disconnected()),Err(_)=>{self.finish(Close::new(1006,"consumer is stalled"),false);Err(disconnected())}
            }
        }
    }
    async fn write_loop(self: Arc<Self>, mut outputs: mpsc::Receiver<Vec<u8>>) {
        loop {
            let data = tokio::select! {biased;_=self.stop.cancelled()=>return,data=outputs.recv()=>match data{Some(data)=>data,None=>return}};
            let result = tokio::select! {_=self.stop.cancelled()=>return,result=timeout(self.options.write_timeout,self.conn.send(Frame::Text(data)))=>result};
            if !matches!(result, Ok(Ok(()))) {
                self.finish(Close::new(1006, ""), false);
                return;
            }
        }
    }
    async fn read_loop(self: Arc<Self>, events: mpsc::Sender<Wire>) {
        loop {
            let received = tokio::select! {biased;_=self.stop.cancelled()=>return,result=self.conn.receive()=>result};
            let data = match received {
                Ok(Frame::Text(data)) => data,
                Ok(_) => {
                    self.finish(Close::new(4011, "duplex requires JSON text frames"), true);
                    return;
                }
                Err(close) => {
                    self.finish(close, false);
                    return;
                }
            };
            if data.len() > self.options.max_frame_bytes {
                self.finish(Close::new(4011, "duplex frame exceeds size limit"), true);
                return;
            }
            let frame = match decode(&data, self.role) {
                Ok(frame) => frame,
                Err(error) => {
                    self.finish(Close::new(4011, error), true);
                    return;
                }
            };
            match frame.kind.as_str() {
                "response" => {
                    if let Some(answer) = self.pending.lock().unwrap().remove(&frame.id) {
                        answer.send_replace(Some(match frame.error {
                            Some(error) => Err(error),
                            None => Ok(frame.result),
                        }));
                    }
                }
                "cancel" => {
                    if let Some(cancel) = self.incoming.lock().unwrap().get(&frame.id) {
                        cancel.cancel();
                    }
                }
                "request" => self.start_request(frame).await,
                "event" => {
                    let result = tokio::select! {_=self.stop.cancelled()=>return,result=timeout(self.options.write_timeout,events.send(frame))=>result};
                    if !matches!(result, Ok(Ok(()))) {
                        self.finish(Close::new(1006, "consumer is stalled"), false);
                        return;
                    }
                }
                _ => unreachable!(),
            }
        }
    }
    async fn start_request(self: &Arc<Self>, frame: Wire) {
        if self.incoming.lock().unwrap().contains_key(&frame.id) {
            self.finish(
                Close::new(4011, "duplicate active duplex request identifier"),
                true,
            );
            return;
        }
        let handler = self
            .handlers
            .lock()
            .unwrap()
            .get(&frame.method)
            .cloned()
            .or_else(|| {
                let path = decode_path(&frame.method).ok()?;
                let receiver = self.routes.find(&path)?;
                let limit = self.options.max_frame_bytes;
                Some(Arc::new(move |ctx, params| -> BoxFuture<'static, Answer> {
                    Box::pin(crate::access::dispatch_request(
                        ctx,
                        path.clone(),
                        params,
                        receiver.clone(),
                        limit,
                    ))
                }) as Handler)
            });
        let rejection = if handler.is_none() {
            Some(PublicError::new("method_not_found", "Unknown method"))
        } else if self.incoming.lock().unwrap().len() >= self.options.max_concurrent_handlers {
            Some(PublicError::new("busy", "Too many concurrent requests"))
        } else {
            None
        };
        if let Some(error) = rejection {
            let answer = Wire {
                version: 1,
                kind: "response".into(),
                id: frame.id,
                trace: frame.trace,
                error: Some(error),
                ..Wire::default()
            };
            match self.encode(&answer) {
                Ok(data) => {
                    if let Err(mpsc::error::TrySendError::Full(data)) = self.outgoing.try_send(data)
                    {
                        tokio::task::yield_now().await;
                        if self.outgoing.try_send(data).is_err() {
                            self.finish(Close::new(1006, "consumer is stalled"), false);
                        }
                    }
                }
                Err(_) => self.finish(Close::new(1006, "rejection exceeds frame limit"), false),
            }
            return;
        }
        let cancel = self.stop.child_token();
        self.incoming
            .lock()
            .unwrap()
            .insert(frame.id.clone(), cancel.clone());
        let ctx = self.context(frame.trace.clone(), frame.meta, cancel.clone());
        let inner = self.clone();
        let handler = handler.unwrap();
        tokio::spawn(async move {
            let timer = {
                let cancel = cancel.clone();
                let within = inner.options.request_timeout;
                tokio::spawn(async move {
                    tokio::time::sleep(within).await;
                    cancel.cancel();
                })
            };
            let operation =
                AssertUnwindSafe(async { handler(ctx, frame.params).await }).catch_unwind();
            let result =
                tokio::select! {_=inner.stop.cancelled()=>None,result=operation=>Some(result)};
            timer.abort();
            if let Some(result) = result {
                let answer = match result {
                    Ok(Ok(_)) if cancel.is_cancelled() => Err(cancelled()),
                    Ok(answer) => answer,
                    Err(_) => Err(PublicError::new("internal", "Internal error")),
                };
                inner.respond(frame.id.clone(), frame.trace, answer).await;
            }
            cancel.cancel();
            inner.incoming.lock().unwrap().remove(&frame.id);
        });
    }
    async fn respond(self: &Arc<Self>, id: String, trace: Trace, result: Answer) {
        let mut frame = Wire {
            version: 1,
            kind: "response".into(),
            id,
            trace,
            ..Wire::default()
        };
        match result {
            Ok(value) => frame.result = default_payload(value, "null"),
            Err(error) => {
                frame.error = Some(if error.code.is_empty() || error.message.is_empty() {
                    PublicError::new("internal", "Internal error")
                } else {
                    error
                })
            }
        }
        if self.enqueue(frame.clone(), &self.stop).await.is_err() && !self.stop.is_cancelled() {
            frame.result = Payload::Absent;
            frame.error = Some(PublicError::new(
                "internal",
                "Response could not be encoded",
            ));
            if self.enqueue(frame, &self.stop).await.is_err() {
                self.finish(Close::new(1006, "response exceeds frame limit"), false);
            }
        }
    }
    async fn event_loop(self: Arc<Self>, mut events: mpsc::Receiver<Wire>) {
        loop {
            let frame = tokio::select! {biased;_=self.stop.cancelled()=>return,frame=events.recv()=>match frame{Some(frame)=>frame,None=>return}};
            let ctx = self.context(frame.trace, frame.meta, self.stop.child_token());
            let handler = self.events.lock().unwrap().get(&frame.event).cloned();
            let routed = decode_path(&frame.event)
                .ok()
                .and_then(|path| self.routes.find(&path).map(|receiver| (path, receiver)));
            let listeners: Vec<_> = self.listeners.lock().unwrap().values().cloned().collect();
            let operation = AssertUnwindSafe(async {
                if let Some(handler) = handler {
                    handler(ctx.clone(), frame.data.clone()).await;
                } else if let Some((path, receiver)) = routed {
                    crate::access::dispatch_event(ctx.clone(), path, frame.data.clone(), receiver);
                }
                for listener in listeners {
                    listener(ctx.clone(), frame.event.clone(), frame.data.clone()).await;
                }
            })
            .catch_unwind();
            tokio::select! {_=self.stop.cancelled()=>return,_=operation=>{}}
        }
    }
}
fn disconnected() -> PublicError {
    PublicError::new("disconnected", "Duplex connection closed")
}
fn cancelled() -> PublicError {
    PublicError::new("cancelled", "Request cancelled")
}
fn timed_out() -> PublicError {
    PublicError::new("request_timeout", "Request deadline exceeded")
}
fn invalid(message: impl Into<String>) -> PublicError {
    PublicError::new("invalid", message)
}
fn default_payload(value: Payload, default: &str) -> Payload {
    if value.is_absent() {
        Payload::from_json(default).unwrap()
    } else {
        value
    }
}
fn child_trace(trace: &Trace) -> Result<Trace, PublicError> {
    fn random(n: usize) -> Result<String, PublicError> {
        let mut bytes = vec![0; n];
        getrandom::fill(&mut bytes)
            .map_err(|_| PublicError::new("internal", "Trace randomness unavailable"))?;
        Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
    }
    if let Some(parent) = trace
        .parent
        .as_deref()
        .filter(|value| valid_traceparent(value))
    {
        Ok(Trace {
            parent: Some(format!("{}{}{}", &parent[..36], random(8)?, &parent[52..])),
            state: trace.state.clone(),
        })
    } else {
        Ok(Trace {
            parent: Some(format!("00-{}-{}-01", random(16)?, random(8)?)),
            state: None,
        })
    }
}

struct WirePending {
    ctx: Context,
    request: Message,
    call: Mutex<Option<Call>>,
}
struct WireRequest {
    pending: Arc<WirePending>,
    completed: bool,
    cancel_queued: bool,
    cancelled: bool,
}
struct WireDelivery {
    path: Vec<String>,
    message: Message,
    pending: Option<Arc<WirePending>>,
    refusal: Option<PublicError>,
}
#[derive(Default)]
struct WireQueue {
    deliveries: VecDeque<WireDelivery>,
    data: usize,
    calls: BTreeMap<(usize, String), WireRequest>,
}

struct PeerWire(Peer);
fn wire_key(message: &Message) -> (usize, String) {
    (
        message
            .returning
            .as_ref()
            .map_or(0, |r| Arc::as_ptr(r) as usize),
        message.frame.id.clone(),
    )
}
impl nightseam_duplex::Wire for PeerWire {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        let inner = &self.0.inner;
        crate::access::validate(path, &message, inner.options.max_frame_bytes)?;
        if inner.stop.is_cancelled() {
            return Err(disconnected());
        }
        if message.frame.kind == ProfileKind::Response {
            return Err(invalid("responses use a return address"));
        }
        let key = wire_key(&message);
        let mut queue = inner.wire_queue.lock().unwrap();
        if inner.stop.is_cancelled() {
            return Err(disconnected());
        }
        let mut pending = None;
        let mut refusal = None;
        if message.frame.kind == ProfileKind::Cancel {
            let Some(state) = queue.calls.get_mut(&key) else {
                return Ok(());
            };
            if state.completed || state.cancel_queued || state.cancelled {
                return Ok(());
            }
            state.cancel_queued = true;
            pending = Some(state.pending.clone());
        } else {
            if queue.data >= inner.options.queue_capacity {
                drop(queue);
                inner.finish(Close::new(4011, "wire queue limit reached"), true);
                return Err(PublicError::new("backpressure", "wire queue limit reached"));
            }
            queue.data += 1;
            if message.frame.kind == ProfileKind::Request {
                if queue.calls.contains_key(&key)
                    || queue.calls.len() >= inner.options.max_pending_requests
                {
                    let code = if queue.calls.contains_key(&key) {
                        "invalid_message"
                    } else {
                        "busy"
                    };
                    refusal = Some(PublicError::new(code, "Request admission refused"));
                } else {
                    let ctx = Context::received(message.frame.trace.clone(), BTreeMap::new())
                        .with_meta(message.frame.meta.clone());
                    let request = Arc::new(WirePending {
                        ctx,
                        request: message.clone(),
                        call: Mutex::new(None),
                    });
                    queue.calls.insert(
                        key,
                        WireRequest {
                            pending: request.clone(),
                            completed: false,
                            cancel_queued: false,
                            cancelled: false,
                        },
                    );
                    pending = Some(request);
                }
            }
        }
        queue.deliveries.push_back(WireDelivery {
            path: path.to_vec(),
            message,
            pending,
            refusal,
        });
        drop(queue);
        inner.wire_wake.notify_one();
        Ok(())
    }
    fn receive(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError> {
        let _registration = self.0.inner.registrations.lock().unwrap();
        let name = encode_path(path);
        if !receiver.namespace
            && (name.is_empty()
                || self.0.inner.handlers.lock().unwrap().contains_key(&name)
                || self.0.inner.events.lock().unwrap().contains_key(&name))
        {
            return Err(invalid("wire path is empty or already registered"));
        }
        self.0.inner.routes.receive(path, receiver)
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        self.0.close_with(code, reason);
        Ok(())
    }
}
