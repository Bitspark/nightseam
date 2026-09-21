//! Private driver: control the public Rust components, retaining payload bytes.
use base64::{Engine, engine::general_purpose::STANDARD};
use nightseam::{Call, Context, Options, Payload, Peer, PublicError, Role, Subscription};
use nightseam_duplex::{Close, Frame, SharedConnection, pipe, ws};
use serde::Deserialize;
use serde_json::{Value, json, value::RawValue};
use std::{
    collections::{BTreeMap, VecDeque},
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    sync::Notify,
    task::JoinHandle,
    time::timeout,
};

type Result<T> = std::result::Result<T, Value>;
fn failure(code: &str, message: impl ToString) -> Value {
    json!({"code":code,"message":message.to_string()})
}
fn public(error: PublicError) -> Value {
    let mut value = json!({"code":error.code,"message":error.message});
    if !error.data.is_absent() {
        value["data"] = error.data.value().unwrap_or(Value::Null);
    }
    value
}
fn closed(error: Close) -> Value {
    json!({"code":"closed","message":error.to_string(),"close_code":error.code,"reason":error.reason})
}

struct Request {
    id: Value,
    op: String,
    args: BTreeMap<String, Box<RawValue>>,
}
impl<'de> Deserialize<'de> for Request {
    fn deserialize<D: serde::Deserializer<'de>>(
        deserializer: D,
    ) -> std::result::Result<Self, D::Error> {
        let mut args = BTreeMap::<String, Box<RawValue>>::deserialize(deserializer)?;
        let id = args
            .remove("id")
            .ok_or_else(|| serde::de::Error::missing_field("id"))?;
        let op = args
            .remove("op")
            .ok_or_else(|| serde::de::Error::missing_field("op"))?;
        Ok(Self {
            id: serde_json::from_str(id.get()).map_err(serde::de::Error::custom)?,
            op: serde_json::from_str(op.get()).map_err(serde::de::Error::custom)?,
            args,
        })
    }
}
impl Request {
    fn get<T: serde::de::DeserializeOwned>(&self, key: &str) -> Result<Option<T>> {
        self.args
            .get(key)
            .map(|raw| {
                serde_json::from_str(raw.get())
                    .map_err(|e| failure("invalid", format!("{key}: {e}")))
            })
            .transpose()
    }
    fn string(&self, key: &str) -> Result<String> {
        self.get(key)?
            .ok_or_else(|| failure("invalid", format!("missing {key}")))
    }
    fn number(&self, key: &str, default: u64) -> Result<u64> {
        Ok(self.get(key)?.unwrap_or(default))
    }
    fn payload(&self, key: &str) -> Result<Payload> {
        self.args
            .get(key)
            .map(|raw| Payload::from_json(raw.get()).map_err(|e| failure("invalid", e)))
            .unwrap_or(Ok(Payload::Absent))
    }
}

struct Inbox<T> {
    items: Mutex<VecDeque<T>>,
    changed: Notify,
}
impl<T> Default for Inbox<T> {
    fn default() -> Self {
        Self {
            items: Mutex::new(VecDeque::new()),
            changed: Notify::new(),
        }
    }
}
impl<T> Inbox<T> {
    fn put(&self, value: T) {
        self.items.lock().unwrap().push_back(value);
        self.changed.notify_one();
    }
    async fn take(&self, matches: impl Fn(&T) -> bool) -> T {
        loop {
            let ready = self.changed.notified();
            let value = {
                let mut items = self.items.lock().unwrap();
                items
                    .iter()
                    .position(&matches)
                    .map(|index| items.remove(index).unwrap())
            };
            if let Some(value) = value {
                return value;
            }
            ready.await;
        }
    }
}

struct Connection {
    conn: SharedConnection,
    frames: Arc<Inbox<std::result::Result<Frame, Close>>>,
    end: Arc<Mutex<Option<Close>>>,
    ended: Arc<Notify>,
    reader: Option<JoinHandle<()>>,
}
impl Connection {
    fn new(conn: SharedConnection, lazy: bool) -> Self {
        let frames = Arc::new(Inbox::default());
        let end = Arc::new(Mutex::new(None));
        let ended = Arc::new(Notify::new());
        let reader = if lazy {
            None
        } else {
            let (conn, frames, end, ended) =
                (conn.clone(), frames.clone(), end.clone(), ended.clone());
            Some(tokio::spawn(async move {
                loop {
                    let value = conn.receive().await;
                    if let Err(error) = &value {
                        *end.lock().unwrap() = Some(error.clone());
                        frames.put(value);
                        ended.notify_one();
                        return;
                    }
                    frames.put(value);
                }
            }))
        };
        Self {
            conn,
            frames,
            end,
            ended,
            reader,
        }
    }
    async fn receive(&self) -> std::result::Result<Frame, Close> {
        if self.reader.is_some() {
            if let Some(error) = self.end.lock().unwrap().clone() {
                if self.frames.items.lock().unwrap().is_empty() {
                    return Err(error);
                }
            }
            self.frames.take(|_| true).await
        } else {
            let value = self.conn.receive().await;
            if let Err(error) = &value {
                *self.end.lock().unwrap() = Some(error.clone());
                self.ended.notify_one();
            }
            value
        }
    }
    async fn await_close(&self) -> Close {
        loop {
            let ended = self.ended.notified();
            if let Some(error) = self.end.lock().unwrap().clone() {
                return error;
            }
            if self.reader.is_none() {
                let _ = self.receive().await;
            } else {
                ended.await;
            }
        }
    }
}
impl Drop for Connection {
    fn drop(&mut self) {
        if let Some(reader) = &self.reader {
            reader.abort();
        }
        self.conn.abort();
    }
}

struct ControlledPeer {
    peer: Peer,
    events: Arc<Inbox<Value>>,
    requests: Arc<Inbox<Value>>,
    _subscription: Subscription,
}
impl ControlledPeer {
    fn new(peer: Peer) -> Self {
        let events = Arc::new(Inbox::default());
        let requests = Arc::new(Inbox::default());
        let recorded = events.clone();
        let subscription = peer.listen_events(move |ctx, name, data| {
            let recorded = recorded.clone();
            async move {
                let mut value = json!({"name":name,"data":data.value().unwrap_or(Value::Null)});
                if !ctx.meta().is_empty() {
                    value["meta"] = json!(ctx.meta());
                }
                recorded.put(value);
            }
        });
        Self {
            peer,
            events,
            requests,
            _subscription: subscription,
        }
    }
}
impl Drop for ControlledPeer {
    fn drop(&mut self) {
        self.peer.close();
    }
}

enum Object {
    Conn(Connection),
    Listener(JoinHandle<std::result::Result<SharedConnection, Close>>),
    PeerListener(JoinHandle<Result<ControlledPeer>>),
    Peer(ControlledPeer),
    Call(Call),
}
impl Drop for Object {
    fn drop(&mut self) {
        match self {
            Self::Listener(task) => task.abort(),
            Self::PeerListener(task) => task.abort(),
            Self::Call(call) => call.cancel(),
            _ => {}
        }
    }
}
#[derive(Default)]
struct Driver {
    next: u64,
    objects: BTreeMap<String, Object>,
}
impl Driver {
    fn mint(&mut self, object: Object) -> String {
        self.next += 1;
        let handle = format!("h{}", self.next);
        self.objects.insert(handle.clone(), object);
        handle
    }
    fn object(&self, r: &Request) -> Result<&Object> {
        let handle = r.string("on")?;
        self.objects
            .get(&handle)
            .ok_or_else(|| failure("unknown_handle", handle))
    }
    fn peer(&self, r: &Request) -> Result<&ControlledPeer> {
        match self.object(r)? {
            Object::Peer(peer) => Ok(peer),
            _ => Err(failure("invalid", "not a peer")),
        }
    }
    fn conn(&self, r: &Request) -> Result<&Connection> {
        match self.object(r)? {
            Object::Conn(conn) => Ok(conn),
            _ => Err(failure("invalid", "not a connection")),
        }
    }
    fn call(&self, r: &Request) -> Result<&Call> {
        match self.object(r)? {
            Object::Call(call) => Ok(call),
            _ => Err(failure("invalid", "not a call")),
        }
    }
    async fn serve(&mut self, r: &Request) -> Result<Value> {
        match r.op.as_str() {
            "hello" => Ok(
                json!({"driver":1,"language":"rust","layers":["seam","peer"],"features":["pipe","listen","lazy","propagator"]}),
            ),
            "reset" | "bye" => {
                self.objects.clear();
                Ok(json!({}))
            }
            "conn.pipe" => {
                let (a, b) = pipe(r.number("limit", 1 << 20)? as usize);
                let lazy = r.get::<String>("consume")?.as_deref() == Some("lazy");
                let a = self.mint(Object::Conn(Connection::new(a, lazy)));
                let b = self.mint(Object::Conn(Connection::new(b, lazy)));
                Ok(json!({"a":a,"b":b}))
            }
            "conn.listen" | "peer.listen" => {
                let peer = r.op == "peer.listen";
                let options = options(r)?;
                let limit = if peer {
                    options.max_frame_bytes
                } else {
                    r.number("limit", 1 << 20)? as usize
                };
                let listener = ws::Listener::bind(
                    "127.0.0.1:0",
                    limit,
                    r.get("subprotocols")?.unwrap_or_default(),
                )
                .await
                .map_err(closed)?;
                let url = listener.url().map_err(closed)?;
                let object = if peer {
                    Object::PeerListener(tokio::spawn(async move {
                        let conn = listener.accept().await.map_err(closed)?;
                        Ok(ControlledPeer::new(
                            Peer::over(conn, Role::Server, options).map_err(public)?,
                        ))
                    }))
                } else {
                    Object::Listener(tokio::spawn(async move { listener.accept().await }))
                };
                Ok(json!({"handle":self.mint(object),"url":url}))
            }
            "conn.accept" | "peer.accept" => {
                let handle = r.string("on")?;
                let object = self
                    .objects
                    .get_mut(&handle)
                    .ok_or_else(|| failure("unknown_handle", handle))?;
                let (object, protocol) = match object {
                    Object::Listener(task) => {
                        let conn = task
                            .await
                            .map_err(|e| failure("failed", e))?
                            .map_err(closed)?;
                        let protocol = conn.subprotocol().to_owned();
                        (
                            Object::Conn(Connection::new(
                                conn,
                                r.get::<String>("consume")?.as_deref() == Some("lazy"),
                            )),
                            protocol,
                        )
                    }
                    Object::PeerListener(task) => {
                        let peer = task.await.map_err(|e| failure("failed", e))??;
                        let protocol = peer.peer.subprotocol().to_owned();
                        (Object::Peer(peer), protocol)
                    }
                    _ => return Err(failure("invalid", "not a listener")),
                };
                Ok(json!({"handle":self.mint(object),"subprotocol":protocol}))
            }
            "conn.dial" | "peer.dial" => {
                let peer = r.op == "peer.dial";
                let options = options(r)?;
                let protocols: Vec<String> = r.get("subprotocols")?.unwrap_or_default();
                let tokens: Vec<_> = protocols.iter().map(String::as_str).collect();
                let conn = ws::dial(
                    &r.string("url")?,
                    if peer {
                        options.max_frame_bytes
                    } else {
                        r.number("limit", 1 << 20)? as usize
                    },
                    &tokens,
                )
                .await
                .map_err(closed)?;
                let protocol = conn.subprotocol().to_owned();
                let object = if peer {
                    Object::Peer(ControlledPeer::new(
                        Peer::over(conn, Role::Client, options).map_err(public)?,
                    ))
                } else {
                    Object::Conn(Connection::new(
                        conn,
                        r.get::<String>("consume")?.as_deref() == Some("lazy"),
                    ))
                };
                Ok(json!({"handle":self.mint(object),"subprotocol":protocol}))
            }
            "conn.send" => {
                let frame = match r.string("kind")?.as_str() {
                    "text" => Frame::Text(r.string("text")?.into_bytes()),
                    "binary" => Frame::Binary(
                        STANDARD
                            .decode(r.string("base64")?)
                            .map_err(|e| failure("invalid", e))?,
                    ),
                    _ => return Err(failure("invalid", "kind must be text or binary")),
                };
                self.conn(r)?.conn.send(frame).await.map_err(closed)?;
                Ok(json!({}))
            }
            "conn.receive" => match self.conn(r)?.receive().await.map_err(closed)? {
                Frame::Text(bytes) => Ok(
                    json!({"kind":"text","text":String::from_utf8(bytes).map_err(|e|failure("invalid",e))?}),
                ),
                Frame::Binary(bytes) => {
                    Ok(json!({"kind":"binary","base64":STANDARD.encode(bytes)}))
                }
            },
            "conn.close" => {
                self.conn(r)?
                    .conn
                    .close(
                        r.number("code", 1000)? as u16,
                        &r.get::<String>("reason")?.unwrap_or_default(),
                    )
                    .await
                    .map_err(closed)?;
                Ok(json!({}))
            }
            "conn.abort" => {
                self.conn(r)?.conn.abort();
                Ok(json!({}))
            }
            "conn.await_close" => {
                let close = self.conn(r)?.await_close().await;
                Ok(json!({"code":close.code,"reason":close.reason}))
            }
            "peer.over" => {
                let conn = self.conn(r)?;
                if conn.reader.is_some() {
                    return Err(failure("invalid", "peer.over requires a lazy connection"));
                }
                let role = match r.string("role")?.as_str() {
                    "client" => Role::Client,
                    "server" => Role::Server,
                    _ => return Err(failure("invalid", "role must be client or server")),
                };
                let peer = Peer::over(conn.conn.clone(), role, options(r)?).map_err(public)?;
                Ok(json!({"handle":self.mint(Object::Peer(ControlledPeer::new(peer)))}))
            }
            "peer.handle" => {
                let peer = self.peer(r)?;
                let method = r.string("method")?;
                let behavior: Behavior = r
                    .get("behavior")?
                    .ok_or_else(|| failure("invalid", "missing behavior"))?;
                let requests = peer.requests.clone();
                let remote = peer.peer.clone();
                let registered = method.clone();
                remote
                    .handle(&registered, move |ctx, params| {
                        let (method, behavior, requests) =
                            (method.clone(), behavior.clone(), requests.clone());
                        async move { canned(ctx, params, method, behavior, requests).await }
                    })
                    .map_err(public)?;
                Ok(json!({}))
            }
            "peer.on_event" => {
                let peer = self.peer(r)?;
                let behavior = r.get::<String>("behavior")?.unwrap_or("record".into());
                match behavior.as_str() {
                    "record" => {}
                    "block" => peer
                        .peer
                        .on_event(
                            &r.string("name")?,
                            |ctx, _| async move { ctx.cancelled().await },
                        )
                        .map_err(public)?,
                    "panic" => peer
                        .peer
                        .on_event(&r.string("name")?, |_, _| async {
                            panic!("the event handler gave up")
                        })
                        .map_err(public)?,
                    _ => return Err(failure("invalid", "unknown event behavior")),
                }
                Ok(json!({}))
            }
            "peer.call" => {
                let mut ctx = Context::default();
                if let Some(ms) = r.get::<u64>("timeout_ms")? {
                    ctx = ctx.with_timeout(Duration::from_millis(ms));
                }
                if let Some(meta) = r.get("meta")? {
                    ctx = ctx.with_meta(meta);
                }
                let params = r.payload("params")?;
                let params = if params.is_absent() {
                    Payload::from_json("null").unwrap()
                } else {
                    params
                };
                let call = self
                    .peer(r)?
                    .peer
                    .call_with(ctx, &r.string("method")?, params);
                Ok(json!({"handle":self.mint(Object::Call(call))}))
            }
            "call.await" => match self.call(r)?.result().await {
                Ok(result) => Ok(json!({"result":result.value().unwrap_or(Value::Null)})),
                Err(error) => Ok(json!({"error":public(error)})),
            },
            "call.cancel" => {
                self.call(r)?.cancel();
                Ok(json!({}))
            }
            "peer.emit" => {
                let mut ctx = Context::default();
                if let Some(meta) = r.get("meta")? {
                    ctx = ctx.with_meta(meta);
                }
                self.peer(r)?
                    .peer
                    .emit_with(ctx, &r.string("event")?, r.payload("data")?)
                    .await
                    .map_err(public)?;
                Ok(json!({}))
            }
            "peer.await_event" => {
                let name = r.string("name")?;
                let mut event = self.peer(r)?.events.take(|e| e["name"] == name).await;
                event.as_object_mut().unwrap().remove("name");
                Ok(event)
            }
            "peer.await_request" => {
                let method = r.string("method")?;
                let phase = r.string("phase")?;
                Ok(self
                    .peer(r)?
                    .requests
                    .take(|e| e["method"] == method && e["phase"] == phase)
                    .await)
            }
            "peer.close" => {
                self.peer(r)?.peer.close();
                Ok(json!({}))
            }
            "peer.await_close" => {
                let close = self.peer(r)?.peer.wait_closed().await;
                Ok(json!({"code":close.code,"clean":close.code==1000}))
            }
            _ => Err(failure("unsupported", &r.op)),
        }
    }
}

fn options(r: &Request) -> Result<Options> {
    let mut options = Options::default();
    let values: BTreeMap<String, Value> = r.get("options")?.unwrap_or_default();
    for (key, value) in values {
        if key == "propagate" {
            continue;
        }
        let n = value
            .as_u64()
            .ok_or_else(|| failure("unsupported", format!("options.{key}")))?;
        match key.as_str() {
            "max_frame_bytes" => options.max_frame_bytes = n as usize,
            "max_pending_requests" => options.max_pending_requests = n as usize,
            "queue_capacity" => options.queue_capacity = n as usize,
            "request_timeout_ms" => options.request_timeout = Duration::from_millis(n),
            "write_timeout_ms" => options.write_timeout = Duration::from_millis(n),
            _ => return Err(failure("unsupported", format!("options.{key}"))),
        }
    }
    Ok(options)
}

#[derive(Clone, Deserialize)]
struct Behavior {
    kind: String,
    #[serde(default)]
    value: Payload,
    #[serde(default)]
    code: String,
    #[serde(default)]
    message: String,
    #[serde(default)]
    data: Payload,
    #[serde(default)]
    method: String,
    #[serde(default)]
    params: Payload,
    #[serde(default)]
    event: String,
    #[serde(default)]
    then: Payload,
    #[serde(default)]
    until: String,
}
fn raw(value: &Payload) -> Payload {
    value.clone()
}
async fn canned(
    ctx: Context,
    params: Payload,
    method: String,
    b: Behavior,
    requests: Arc<Inbox<Value>>,
) -> std::result::Result<Payload, PublicError> {
    let mut start = json!({"id":"","method":method,"phase":"started"});
    if !ctx.meta().is_empty() {
        start["meta"] = json!(ctx.meta());
    }
    requests.put(start);
    let result = match b.kind.as_str() {
        "echo" => Ok(params),
        "return" => Ok(raw(&b.value)),
        "fail" => {
            let mut error = PublicError::new(b.code, b.message);
            error.data = raw(&b.data);
            Err(error)
        }
        "wait" => {
            ctx.cancelled().await;
            Err(PublicError::new("cancelled", "request cancelled"))
        }
        "hold" => {
            let released = Arc::new(Notify::new());
            let target = released.clone();
            let peer = ctx.peer().unwrap();
            let _subscription = peer.listen_events(move |_, event, _| {
                let target = target.clone();
                let until = b.until.clone();
                async move {
                    if event == until {
                        target.notify_one();
                    }
                }
            });
            tokio::select! {_=released.notified()=>{},_=peer.wait_closed()=>{}}
            Ok(raw(&b.value))
        }
        "panic" => {
            requests.put(json!({"id":"","method":method,"phase":"ended","outcome":"panic"}));
            panic!(
                "{}",
                raw(&b.value)
                    .value()
                    .unwrap_or(json!("the handler gave up"))
            )
        }
        "reverse" => {
            ctx.peer()
                .unwrap()
                .call_with(
                    ctx.clone(),
                    &b.method,
                    if !b.params.is_absent() {
                        raw(&b.params)
                    } else {
                        params
                    },
                )
                .result()
                .await
        }
        "emit" => match ctx
            .peer()
            .unwrap()
            .emit_with(ctx.clone(), &b.event, raw(&b.data))
            .await
        {
            Ok(()) => Ok(raw(&b.then)),
            Err(error) => Err(error),
        },
        _ => Err(PublicError::new(
            "internal",
            format!("no such behavior: {}", b.kind),
        )),
    };
    let outcome = if result.is_ok() {
        "ok"
    } else if ctx.is_cancelled() {
        "cancelled"
    } else {
        "error"
    };
    requests.put(json!({"id":"","method":method,"phase":"ended","outcome":outcome}));
    result
}

#[tokio::main]
async fn main() {
    let mut input = BufReader::new(tokio::io::stdin()).lines();
    let mut output = tokio::io::stdout();
    let mut driver = Driver::default();
    while let Ok(Some(line)) = input.next_line().await {
        let (id, answer, bye) = match serde_json::from_str::<Request>(&line) {
            Ok(request) => {
                let within = request.number("within_ms", 5000).unwrap_or(5000);
                let result = timeout(Duration::from_millis(within), driver.serve(&request))
                    .await
                    .unwrap_or_else(|_| {
                        Err(failure("timeout", format!("no response within {within}ms")))
                    });
                (request.id, result, request.op == "bye")
            }
            Err(error) => (Value::Null, Err(failure("invalid", error)), false),
        };
        let answer = match answer {
            Ok(value) => json!({"id":id,"ok":value}),
            Err(error) => json!({"id":id,"error":error}),
        };
        let line = serde_json::to_string(&answer).unwrap() + "\n";
        if output.write_all(line.as_bytes()).await.is_err() || output.flush().await.is_err() {
            break;
        }
        if bye {
            break;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn driver_preserves_numeric_tokens_and_absence() {
        let r: Request =
            serde_json::from_str(r#"{"id":1,"op":"peer.call","params":{"n":1e3}}"#).unwrap();
        assert_eq!(r.payload("params").unwrap().raw(), Some(r#"{"n":1e3}"#));
        assert!(r.payload("data").unwrap().is_absent());
    }
    async fn request(driver: &mut Driver, input: Value) -> Result<Value> {
        let request = serde_json::from_str::<Request>(&input.to_string()).unwrap();
        timeout(Duration::from_secs(2), driver.serve(&request))
            .await
            .unwrap()
    }
    #[tokio::test]
    async fn public_error_is_a_completed_call_and_reset_releases_the_peer() {
        let mut driver = Driver::default();
        let (client, server) = pipe(1 << 20);
        let client = Peer::over(client, Role::Client, Options::default()).unwrap();
        let server = Peer::over(server, Role::Server, Options::default()).unwrap();
        let observer = server.clone();
        let c = driver.mint(Object::Peer(ControlledPeer::new(client)));
        let s = driver.mint(Object::Peer(ControlledPeer::new(server)));
        request(&mut driver, json!({"id":1,"op":"peer.handle","on":s,"method":"deny","behavior":{"kind":"fail","code":"denied","message":"No access","data":{"reason":"policy"}}})).await.unwrap();
        let call = request(
            &mut driver,
            json!({"id":2,"op":"peer.call","on":c,"method":"deny","params":null}),
        )
        .await
        .unwrap();
        let answer = request(
            &mut driver,
            json!({"id":3,"op":"call.await","on":call["handle"]}),
        )
        .await
        .unwrap();
        assert_eq!(
            answer,
            json!({"error":{"code":"denied","message":"No access","data":{"reason":"policy"}}})
        );
        request(&mut driver, json!({"id":4,"op":"reset"}))
            .await
            .unwrap();
        assert!(driver.objects.is_empty());
        timeout(Duration::from_secs(1), observer.wait_closed())
            .await
            .unwrap();
    }
    #[test]
    fn behavior_keeps_explicit_null_distinct_from_absence() {
        let b: Behavior = serde_json::from_str(r#"{"kind":"reverse","params":null}"#).unwrap();
        assert_eq!(b.params.raw(), Some("null"));
        assert!(b.value.is_absent());
    }
    #[tokio::test]
    async fn inbox_keeps_unmatched_entries() {
        let inbox = Inbox::default();
        inbox.put(1);
        inbox.put(2);
        assert_eq!(inbox.take(|n| *n == 2).await, 2);
        assert_eq!(inbox.take(|_| true).await, 1);
    }
}
