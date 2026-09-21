use crate::{Payload, PublicError, Trace};
use std::{
    any::Any,
    collections::BTreeMap,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicU64, Ordering},
    },
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ProfileKind {
    Request,
    Response,
    Event,
    Cancel,
}

/// A structured profile frame. Routing names belong to Wire's relative path;
/// return addresses and received context stay local and are never serialized.
#[derive(Clone, Debug)]
pub struct ProfileFrame {
    pub kind: ProfileKind,
    pub id: String,
    pub params: Payload,
    pub result: Payload,
    pub error: Option<PublicError>,
    pub data: Payload,
    pub trace: Trace,
    pub meta: BTreeMap<String, String>,
}
impl ProfileFrame {
    pub fn new(kind: ProfileKind) -> Self {
        Self {
            kind,
            id: String::new(),
            params: Payload::Absent,
            result: Payload::Absent,
            error: None,
            data: Payload::Absent,
            trace: Trace::default(),
            meta: BTreeMap::new(),
        }
    }
}

pub struct ReturnAddress {
    pub wire: SharedWire,
}

#[derive(Clone)]
pub struct Message {
    pub frame: ProfileFrame,
    pub returning: Option<Arc<ReturnAddress>>,
    pub context: Option<Arc<dyn Any + Send + Sync>>,
}
impl Message {
    pub fn new(frame: ProfileFrame) -> Self {
        Self {
            frame,
            returning: None,
            context: None,
        }
    }
}

pub type Detach = Arc<dyn Fn() + Send + Sync>;
pub type SharedWire = Arc<dyn Wire>;
type Delivery = Arc<dyn Fn(Vec<String>, Message) + Send + Sync>;
type Ending = Arc<dyn Fn(u16, String) + Send + Sync>;

#[derive(Clone, Default)]
pub struct Receiver {
    pub namespace: bool,
    pub message: Option<Delivery>,
    pub closed: Option<Ending>,
}
impl Receiver {
    pub fn new(callback: impl Fn(Vec<String>, Message) + Send + Sync + 'static) -> Self {
        Self {
            message: Some(Arc::new(callback)),
            ..Self::default()
        }
    }
}

/// Send admits without waiting for application code or a response. Roots own
/// dispatch queues; path views never create peers, channels, or queues.
pub trait Wire: Send + Sync {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError>;
    fn receive(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError>;
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError>;
}

pub fn encode_path(path: &[String]) -> String {
    let mut name = String::new();
    for segment in path {
        name.push_str(&format!("{}:{segment}", segment.len()));
    }
    name
}
pub fn decode_path(mut name: &str) -> Result<Vec<String>, PublicError> {
    let invalid = || PublicError::new("invalid", "invalid wire path");
    let mut path = Vec::new();
    while !name.is_empty() {
        let colon = name.find(':').ok_or_else(invalid)?;
        let digits = &name[..colon];
        if digits.is_empty()
            || (digits.len() > 1 && digits.starts_with('0'))
            || !digits.bytes().all(|byte| byte.is_ascii_digit())
        {
            return Err(invalid());
        }
        let length: usize = digits.parse().map_err(|_| invalid())?;
        name = &name[colon + 1..];
        let segment = name.get(..length).ok_or_else(invalid)?;
        path.push(segment.to_owned());
        name = &name[length..];
    }
    Ok(path)
}

pub fn at(root: SharedWire, prefix: &[String]) -> SharedWire {
    Arc::new(Selected {
        root,
        prefix: prefix.to_vec(),
    })
}
struct Selected {
    root: SharedWire,
    prefix: Vec<String>,
}
impl Selected {
    fn path(&self, path: &[String]) -> Vec<String> {
        self.prefix.iter().chain(path).cloned().collect()
    }
}
impl Wire for Selected {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        self.root.send(&self.path(path), message)
    }
    fn receive(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError> {
        let length = self.prefix.len();
        let callback = receiver.message;
        self.root.receive(
            &self.path(path),
            Receiver {
                namespace: receiver.namespace,
                closed: receiver.closed,
                message: callback.map(|callback| {
                    Arc::new(move |path: Vec<String>, message| {
                        callback(path[length..].to_vec(), message)
                    }) as Delivery
                }),
            },
        )
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        self.root.close(code, reason)
    }
}

struct Registration {
    receiver: Receiver,
    detach: Mutex<Option<Detach>>,
    active: AtomicBool,
}
struct Mount {
    children: BTreeMap<String, SharedWire>,
    state: Arc<Mutex<MountState>>,
    next: AtomicU64,
}
#[derive(Default)]
struct MountState {
    closed: bool,
    registrations: BTreeMap<u64, Arc<Registration>>,
}

/// Closing a mount detaches its registrations, leaving borrowed children alive.
pub fn mount(children: BTreeMap<String, SharedWire>) -> SharedWire {
    Arc::new(Mount {
        children,
        state: Arc::new(Mutex::new(MountState::default())),
        next: AtomicU64::new(0),
    })
}
impl Mount {
    fn child(&self, path: &[String]) -> Result<SharedWire, PublicError> {
        if self.state.lock().unwrap().closed {
            return Err(closed());
        }
        path.first()
            .and_then(|key| self.children.get(key))
            .cloned()
            .ok_or_else(|| PublicError::new("no_route", "wire path has no destination"))
    }
    fn register(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError> {
        let child = self.child(path)?;
        let key = path[0].clone();
        let registration = Arc::new(Registration {
            receiver: receiver.clone(),
            detach: Mutex::new(None),
            active: AtomicBool::new(true),
        });
        let id = self.next.fetch_add(1, Ordering::Relaxed);
        {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return Err(closed());
            }
            state.registrations.insert(id, registration.clone());
        }
        let state = self.state.clone();
        let remove: Arc<dyn Fn(bool, u16, String) + Send + Sync> = {
            let registration = registration.clone();
            Arc::new(move |tell, code, reason| {
                state.lock().unwrap().registrations.remove(&id);
                if registration.active.swap(false, Ordering::SeqCst) {
                    let detach = registration.detach.lock().unwrap().take();
                    if let Some(detach) = detach {
                        detach();
                    }
                    if tell {
                        if let Some(callback) = &registration.receiver.closed {
                            callback(code, reason);
                        }
                    }
                }
            })
        };
        let ending = remove.clone();
        let result = child.receive(
            &path[1..],
            Receiver {
                namespace: receiver.namespace,
                message: receiver.message.map(|callback| {
                    Arc::new(move |path: Vec<String>, message| {
                        callback(std::iter::once(key.clone()).chain(path).collect(), message)
                    }) as Delivery
                }),
                closed: Some(Arc::new(move |code, reason| ending(true, code, reason))),
            },
        );
        match result {
            Err(error) => {
                remove(false, 0, String::new());
                Err(error)
            }
            Ok(detach) => {
                let mut held = registration.detach.lock().unwrap();
                if !registration.active.load(Ordering::SeqCst) {
                    drop(held);
                    detach();
                    return Err(closed());
                }
                *held = Some(detach);
                Ok(Arc::new(move || remove(false, 0, String::new())))
            }
        }
    }
}
impl Wire for Mount {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        self.child(path)?.send(&path[1..], message)
    }
    fn receive(&self, path: &[String], receiver: Receiver) -> Result<Detach, PublicError> {
        if !path.is_empty() || !receiver.namespace {
            return self.register(path, receiver);
        }
        if self.state.lock().unwrap().closed {
            return Err(closed());
        }
        let mut detaches = Vec::new();
        let remaining = Arc::new(AtomicU64::new(self.children.len() as u64));
        for key in self.children.keys() {
            let remaining = remaining.clone();
            let callback = receiver.closed.clone();
            let registration = Receiver {
                closed: Some(Arc::new(move |code, reason| {
                    if remaining.fetch_sub(1, Ordering::SeqCst) == 1 {
                        if let Some(callback) = &callback {
                            callback(code, reason);
                        }
                    }
                })),
                ..receiver.clone()
            };
            match self.register(std::slice::from_ref(key), registration) {
                Ok(detach) => detaches.push(detach),
                Err(error) => {
                    for detach in detaches {
                        detach();
                    }
                    return Err(error);
                }
            }
        }
        Ok(Arc::new(move || {
            for detach in &detaches {
                detach();
            }
        }))
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        let registrations = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return Ok(());
            }
            state.closed = true;
            std::mem::take(&mut state.registrations)
        };
        for (_, registration) in registrations {
            if registration.active.swap(false, Ordering::SeqCst) {
                let detach = registration.detach.lock().unwrap().take();
                if let Some(detach) = detach {
                    detach();
                }
                if let Some(callback) = &registration.receiver.closed {
                    callback(code, reason.to_owned());
                }
            }
        }
        Ok(())
    }
}
fn closed() -> PublicError {
    PublicError::new("disconnected", "Wire closed")
}
