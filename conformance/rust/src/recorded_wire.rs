// A consumer composition used only by the testee. The append store owns its
// replay/follow policy; production Wire supplies path views and queued dispatch.
use nightseam::{Options, Payload, PublicError, wire_pair};
use nightseam_duplex::{
    Detach, Message, ProfileFrame, ProfileKind, Receiver, SharedWire, Wire, at, mount,
};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::sync::{mpsc, oneshot};
use tokio_util::sync::CancellationToken;

#[derive(Clone)]
struct Entry {
    path: Vec<String>,
    message: Message,
    sequence: usize,
}
struct FollowerQueue {
    sender: mpsc::Sender<Entry>,
    stop: CancellationToken,
}
#[derive(Default)]
struct Store {
    entries: Vec<Entry>,
    followers: Vec<FollowerQueue>,
}
#[derive(Clone, Default)]
struct Recorded(Arc<Mutex<Store>>);
struct Follower {
    stop: CancellationToken,
    done: Option<oneshot::Receiver<()>>,
    sent: mpsc::UnboundedReceiver<usize>,
    paused: Option<oneshot::Receiver<()>>,
    resume: Option<oneshot::Sender<()>>,
    sender: mpsc::Sender<Entry>,
}
impl Drop for Follower {
    fn drop(&mut self) {
        self.stop.cancel();
    }
}
impl Follower {
    async fn pause(&mut self) -> Result<(), PublicError> {
        self.paused
            .take()
            .unwrap()
            .await
            .map_err(|_| invalid("replay did not pause"))
    }
    fn resume(&mut self) {
        if let Some(sender) = self.resume.take() {
            let _ = sender.send(());
        }
    }
    async fn sent(&mut self, last: usize) -> Result<(), PublicError> {
        loop {
            if self
                .sent
                .recv()
                .await
                .ok_or_else(|| invalid("follower stopped before delivery"))?
                == last
            {
                return Ok(());
            }
        }
    }
    async fn done(&mut self) -> Result<(), PublicError> {
        self.done
            .take()
            .unwrap()
            .await
            .map_err(|_| invalid("follower completion lost"))
    }
}
impl Recorded {
    fn head(&self) -> usize {
        self.0.lock().unwrap().entries.len()
    }
    fn attach(
        &self,
        after: usize,
        target: SharedWire,
        bound: usize,
        pause: bool,
    ) -> (usize, Follower) {
        let (sender, mut incoming) = mpsc::channel(bound);
        let stop = CancellationToken::new();
        let (head, history) = {
            let mut store = self.0.lock().unwrap();
            let head = store.entries.len();
            let history = store.entries[after..head].to_vec();
            // The snapshot head and live handoff share append's exclusion.
            store.followers.push(FollowerQueue {
                sender: sender.clone(),
                stop: stop.clone(),
            });
            (head, history)
        };
        let (sent, sent_rx) = mpsc::unbounded_channel();
        let (paused, paused_rx) = oneshot::channel();
        let (resume, resumed) = oneshot::channel();
        let (done, done_rx) = oneshot::channel();
        let stop_task = stop.clone();
        tokio::spawn(async move {
            let mut paused = Some(paused);
            let mut resumed = Some(resumed);
            let deliver = |entry: Entry| -> bool {
                if stop_task.is_cancelled() || target.send(&entry.path, entry.message).is_err() {
                    return false;
                }
                sent.send(entry.sequence).is_ok()
            };
            let run = async {
                for (index, entry) in history.into_iter().enumerate() {
                    if !deliver(entry) {
                        return;
                    }
                    if index == 0 && pause {
                        let _ = paused.take().unwrap().send(());
                        tokio::select! { _=stop_task.cancelled()=>return, _=resumed.take().unwrap()=>{} }
                    }
                }
                loop {
                    tokio::select! {
                        biased;
                        _=stop_task.cancelled()=>return,
                        entry=incoming.recv()=>match entry { Some(entry)=>{ if !deliver(entry) { return; } }, None=>return },
                    }
                }
            };
            run.await;
            let _ = target.close(1008, "recorded handoff ended");
            let _ = done.send(());
        });
        (
            head,
            Follower {
                stop,
                done: Some(done_rx),
                sent: sent_rx,
                paused: Some(paused_rx),
                resume: Some(resume),
                sender,
            },
        )
    }
}
impl Wire for Recorded {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        let mut store = self.0.lock().unwrap();
        let entry = Entry {
            path: path.to_vec(),
            message,
            sequence: store.entries.len() + 1,
        };
        store.entries.push(entry.clone());
        store.followers.retain(|follower| {
            if follower.stop.is_cancelled() || follower.sender.try_send(entry.clone()).is_err() {
                follower.stop.cancel();
                false
            } else {
                true
            }
        });
        Ok(())
    }
    fn receive(&self, _: &[String], _: Receiver) -> Result<Detach, PublicError> {
        Err(invalid("recorded input has no receivers"))
    }
    fn close(&self, _: u16, _: &str) -> Result<(), PublicError> {
        for follower in self.0.lock().unwrap().followers.drain(..) {
            follower.stop.cancel();
        }
        Ok(())
    }
}

// A fixture loopback uses the real bounded local root. Its two endpoint
// presentations are joined only here; neither a custom scheduler nor a second
// implementation of path routing can make a broken production view pass.
struct Root {
    sending: SharedWire,
    receiving: SharedWire,
}
impl Root {
    fn create() -> Result<SharedWire, PublicError> {
        let (sending, receiving) = wire_pair(Options::default())?;
        Ok(Arc::new(Self { sending, receiving }))
    }
}
impl Wire for Root {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        self.sending.send(path, message)
    }
    fn receive(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError> {
        self.receiving.receive(path, receiver)
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        self.sending.close(code, reason)
    }
}
struct Presentation {
    wire: SharedWire,
    root: SharedWire,
    end: SharedWire,
    values: mpsc::UnboundedReceiver<usize>,
    closed: mpsc::UnboundedReceiver<u16>,
    errors: mpsc::UnboundedReceiver<PublicError>,
}
impl Drop for Presentation {
    fn drop(&mut self) {
        let _ = self.wire.close(1000, "done");
        let _ = self.root.close(1000, "done");
        let _ = self.end.close(1000, "done");
    }
}
impl Presentation {
    fn new(store: Recorded) -> Result<Self, PublicError> {
        let root = Root::create()?;
        let end = Root::create()?;
        let (values, values_rx) = mpsc::unbounded_channel();
        let (closed, closed_rx) = mpsc::unbounded_channel();
        let (errors, errors_rx) = mpsc::unbounded_channel();
        let destination = at(
            mount(BTreeMap::from([(
                "out".into(),
                at(end.clone(), &path(&["destination"])),
            )])),
            &path(&["out"]),
        );
        let wire = at(
            mount(BTreeMap::from([(
                "outer".into(),
                mount(BTreeMap::from([(
                    "in".into(),
                    at(root.clone(), &path(&["source"])),
                )])),
            )])),
            &path(&["outer", "in"]),
        );
        let store_message = store.clone();
        let destination_errors = errors.clone();
        destination.receive(
            &path(&["tick"]),
            Receiver::new(move |p, message| {
                // A real callback reenters the append store. Dispatch under its
                // exclusion deadlocks and fails the witness deadline.
                let _ = store_message.head();
                if p != path(&["tick"]) {
                    let _ = destination_errors.send(invalid("destination path changed"));
                    return;
                }
                match message.frame.data.value().ok().and_then(|v| v.as_u64()) {
                    Some(value) => {
                        let _ = values.send(value as usize);
                    }
                    None => {
                        let _ =
                            destination_errors.send(invalid("recorded value is not an integer"));
                    }
                }
            }),
        )?;
        wire.receive(
            &path(&["tick"]),
            Receiver {
                closed: Some(Arc::new(move |code, _| {
                    let _ = store.head();
                    let _ = closed.send(code);
                })),
                ..Receiver::new(move |p, message| {
                    if let Err(error) = destination.send(&p, message) {
                        let _ = errors.send(error);
                    }
                })
            },
        )?;
        Ok(Self {
            wire,
            root,
            end,
            values: values_rx,
            closed: closed_rx,
            errors: errors_rx,
        })
    }
    async fn collect(&mut self) -> Result<Vec<usize>, PublicError> {
        self.wire.send(&path(&["tick"]), message(0))?;
        let mut values = Vec::new();
        loop {
            tokio::select! {
                error=self.errors.recv()=>return Err(error.unwrap_or_else(|| invalid("error queue closed"))),
                value=self.values.recv()=>match value { Some(0)=>return Ok(values), Some(value)=>values.push(value), None=>return Err(invalid("value queue closed")) },
            }
        }
    }
}

fn path(parts: &[&str]) -> Vec<String> {
    parts.iter().map(|part| (*part).to_owned()).collect()
}
fn message(value: usize) -> Message {
    let mut frame = ProfileFrame::new(ProfileKind::Event);
    frame.data = Payload::from_value(&value).unwrap();
    Message::new(frame)
}
fn append(wire: &SharedWire, value: usize) -> Result<(), PublicError> {
    wire.send(&path(&["tick"]), message(value))
}
fn invalid(message: &str) -> PublicError {
    PublicError::new("invalid", message)
}

async fn head_case(before: bool) -> Result<Value, PublicError> {
    let store = Recorded::default();
    let mut presentation = Presentation::new(store.clone())?;
    let source = at(
        mount(BTreeMap::from([(
            "record".into(),
            Arc::new(store.clone()) as SharedWire,
        )])),
        &path(&["record"]),
    );
    for value in 1..=3 {
        append(&source, value)?;
    }
    let (cut, next) = if before {
        append(&source, 4)?;
        ("append_before_head", 5)
    } else {
        ("head_before_append", 4)
    };
    let (head, mut follower) = store.attach(0, presentation.wire.clone(), 2, true);
    follower.pause().await?;
    // Producer progress is proven while replay remains explicitly paused.
    for value in next..=5 {
        append(&source, value)?;
    }
    follower.resume();
    follower.sent(5).await?;
    append(&source, 6)?;
    follower.sent(6).await?;
    let first = presentation.collect().await?;
    let mut second = Presentation::new(store.clone())?;
    let (_, mut late) = store.attach(3, second.wire.clone(), 2, false);
    late.sent(6).await?;
    let after_three = second.collect().await?;
    follower.stop.cancel();
    late.stop.cancel();
    follower.done().await?;
    late.done().await?;
    Ok(
        json!({"cut":cut,"head":head,"first":first,"after_three":after_three,"producer_progress":true,"callbacks_outside_append":true}),
    )
}

async fn stall_case() -> Result<Value, PublicError> {
    let store = Recorded::default();
    let source: SharedWire = Arc::new(store.clone());
    for value in 1..=3 {
        append(&source, value)?;
    }
    let mut stalled = Presentation::new(store.clone())?;
    let mut healthy = Presentation::new(store.clone())?;
    let (_, mut slow) = store.attach(0, stalled.wire.clone(), 2, true);
    slow.pause().await?;
    let (_, mut fast) = store.attach(3, healthy.wire.clone(), 2, false);
    for value in 4..=5 {
        append(&source, value)?;
        fast.sent(value).await?;
    }
    let queued = slow.sender.max_capacity() - slow.sender.capacity();
    append(&source, 6)?;
    fast.sent(6).await?;
    slow.done().await?;
    let code = stalled
        .closed
        .recv()
        .await
        .ok_or_else(|| invalid("stalled carrier did not notify closure"))?;
    if append(&stalled.wire, 99).is_ok() {
        return Err(invalid("stalled carrier accepted after close"));
    }
    let (probe_tx, mut probe_rx) = mpsc::unbounded_channel();
    stalled.root.receive(
        &path(&["probe"]),
        Receiver::new(move |_, message| {
            let _ = probe_tx.send(message.frame.data.value().unwrap());
        }),
    )?;
    stalled.root.send(&path(&["probe"]), message(99))?;
    let probe = probe_rx
        .recv()
        .await
        .ok_or_else(|| invalid("borrowed root stopped"))?;
    append(&source, 7)?;
    fast.sent(7).await?;
    let values = healthy.collect().await?;
    let closed = 1 + stalled.closed.len();
    fast.stop.cancel();
    fast.done().await?;
    Ok(
        json!({"bound":2,"queued_at_bound":queued,"closed":closed,"close_code":code,"healthy":values,"underneath":[probe],"head":store.head(),"producer_progress":true}),
    )
}

pub async fn witness(within: Duration) -> Result<Value, PublicError> {
    tokio::time::timeout(within, async {
        let first = head_case(false).await?;
        let second = head_case(true).await?;
        let stalled = stall_case().await?;
        Ok(json!({"cases":[first, second],"stalled":stalled}))
    })
    .await
    .map_err(|_| PublicError::new("timeout", "recorded wire witness deadline exceeded"))?
}
