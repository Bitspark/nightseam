use bitwire::{Detach, Endpoint, Message, ProfileKind, PublicError, Receiver, ReturnAddress, Wire};
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex, Weak},
};

struct Registration {
    receiver: Receiver,
}
type Captured = (Weak<ReturnAddress>, Arc<Registration>);
#[derive(Default)]
struct State {
    closed: bool,
    routes: BTreeMap<(Vec<String>, bool), Arc<Registration>>,
    captured: BTreeMap<(usize, String), Captured>,
    detach: Option<Detach>,
}

/// Explicit exact/longest-prefix dispatch over one borrowed Bitwire endpoint.
pub struct Dispatcher {
    root: Arc<dyn Endpoint>,
    state: Mutex<State>,
}

impl Dispatcher {
    pub fn new(root: Arc<dyn Endpoint>) -> Result<Arc<Self>, PublicError> {
        let owner = Arc::new(Self {
            root: root.clone(),
            state: Mutex::default(),
        });
        let delivery = Arc::downgrade(&owner);
        let ending = delivery.clone();
        let detach = root.receive(Receiver {
            message: Some(Arc::new(move |path, message| {
                if let Some(owner) = delivery.upgrade() {
                    owner.deliver(path, message);
                }
            })),
            closed: Some(Arc::new(move |code, reason| {
                if let Some(owner) = ending.upgrade() {
                    owner.close(code, &reason);
                }
            })),
        })?;
        let mut state = owner.state.lock().unwrap();
        if state.closed {
            drop(state);
            detach();
            return Err(crate::access::closed());
        }
        state.detach = Some(detach);
        drop(state);
        Ok(owner)
    }

    pub fn register(
        self: &Arc<Self>,
        path: &[String],
        receiver: Receiver,
    ) -> Result<Detach, PublicError> {
        self.install(path, receiver, false)
    }

    pub fn register_prefix(
        self: &Arc<Self>,
        path: &[String],
        receiver: Receiver,
    ) -> Result<Detach, PublicError> {
        self.install(path, receiver, true)
    }

    fn install(
        self: &Arc<Self>,
        path: &[String],
        receiver: Receiver,
        prefix: bool,
    ) -> Result<Detach, PublicError> {
        let key = (path.to_vec(), prefix);
        let registration = Arc::new(Registration { receiver });
        let mut state = self.state.lock().unwrap();
        if state.closed {
            return Err(crate::access::closed());
        }
        if state.routes.contains_key(&key) {
            return Err(PublicError::new(
                "receiver_exists",
                "route already registered",
            ));
        }
        state.routes.insert(key.clone(), registration.clone());
        let weak = Arc::downgrade(self);
        Ok(Arc::new(move || {
            if let Some(owner) = weak.upgrade() {
                let mut state = owner.state.lock().unwrap();
                if state
                    .routes
                    .get(&key)
                    .is_some_and(|held| Arc::ptr_eq(held, &registration))
                {
                    state.routes.remove(&key);
                }
            }
        }))
    }

    fn deliver(&self, path: Vec<String>, message: Message) {
        let registration = {
            let mut state = self.state.lock().unwrap();
            state
                .captured
                .retain(|_, (address, _)| address.strong_count() != 0);
            let key = message
                .returning
                .as_ref()
                .map(|address| (Arc::as_ptr(address) as usize, message.frame.id.clone()));
            if message.frame.kind == ProfileKind::Cancel {
                key.and_then(|key| {
                    state
                        .captured
                        .remove(&key)
                        .map(|(_, registration)| registration)
                })
            } else {
                let found = state
                    .routes
                    .get(&(path.clone(), false))
                    .cloned()
                    .or_else(|| {
                        state
                            .routes
                            .iter()
                            .filter(|((prefix, namespace), _)| {
                                *namespace && path.starts_with(prefix)
                            })
                            .max_by_key(|((prefix, _), _)| prefix.len())
                            .map(|(_, r)| r.clone())
                    });
                if message.frame.kind == ProfileKind::Request {
                    if let (Some(key), Some(address), Some(registration)) =
                        (key, &message.returning, &found)
                    {
                        state
                            .captured
                            .insert(key, (Arc::downgrade(address), registration.clone()));
                    }
                }
                found
            }
        };
        if let Some(callback) = registration
            .as_ref()
            .and_then(|r| r.receiver.message.as_ref())
        {
            callback(path, message);
        } else if message.frame.kind == ProfileKind::Request {
            let _ = crate::access::response(
                &message,
                Err(PublicError::new(
                    "method_not_found",
                    "No handler at this path",
                )),
            );
        }
    }

    pub fn select(self: &Arc<Self>, path: &[String]) -> Arc<dyn Endpoint> {
        Arc::new(Selected {
            owner: self.clone(),
            prefix: path.to_vec(),
            state: Arc::default(),
        })
    }

    pub fn close(&self, code: u16, reason: &str) {
        let (detach, routes) = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return;
            }
            state.closed = true;
            (state.detach.take(), std::mem::take(&mut state.routes))
        };
        if let Some(detach) = detach {
            detach();
        }
        for registration in routes.into_values() {
            if let Some(callback) = &registration.receiver.closed {
                callback(code, reason.to_owned());
            }
        }
    }
}
impl Wire for Dispatcher {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        if self.state.lock().unwrap().closed {
            return Err(crate::access::closed());
        }
        self.root.send(path, message)
    }
}

#[derive(Default)]
struct SelectedState {
    closed: bool,
    next: u64,
    attachment: Option<(u64, Receiver, Option<Detach>)>,
}
struct Selected {
    owner: Arc<Dispatcher>,
    prefix: Vec<String>,
    state: Arc<Mutex<SelectedState>>,
}
impl Wire for Selected {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        if self.state.lock().unwrap().closed {
            return Err(crate::access::closed());
        }
        self.owner.send(
            &self.prefix.iter().chain(path).cloned().collect::<Vec<_>>(),
            message,
        )
    }
}
impl Endpoint for Selected {
    fn receive(&self, receiver: Receiver) -> Result<Detach, PublicError> {
        let id = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return Err(crate::access::closed());
            }
            if state.attachment.is_some() {
                return Err(PublicError::new(
                    "receiver_exists",
                    "endpoint already attached",
                ));
            }
            state.next += 1;
            let id = state.next;
            state.attachment = Some((id, receiver.clone(), None));
            id
        };
        let length = self.prefix.len();
        let ending = self.state.clone();
        let result = self.owner.register_prefix(
            &self.prefix,
            Receiver {
                message: receiver.message.map(|callback| {
                    Arc::new(move |path: Vec<String>, message| {
                        callback(path[length..].to_vec(), message)
                    }) as bitwire::Delivery
                }),
                closed: Some(Arc::new(move |code, reason| {
                    let held = {
                        let mut state = ending.lock().unwrap();
                        if state.attachment.as_ref().is_some_and(|a| a.0 == id) {
                            state.attachment.take()
                        } else {
                            None
                        }
                    };
                    if let Some((_, receiver, _)) = held {
                        if let Some(callback) = receiver.closed {
                            callback(code, reason);
                        }
                    }
                })),
            },
        );
        let mut state = self.state.lock().unwrap();
        match result {
            Err(error) => {
                state.attachment = None;
                Err(error)
            }
            Ok(stop) => {
                if let Some(held) = state.attachment.as_mut().filter(|a| a.0 == id) {
                    held.2 = Some(stop.clone());
                } else {
                    drop(state);
                    stop();
                    return Err(crate::access::closed());
                }
                let state = self.state.clone();
                Ok(Arc::new(move || {
                    let held = {
                        let mut state = state.lock().unwrap();
                        if state.attachment.as_ref().is_some_and(|a| a.0 == id) {
                            state.attachment.take()
                        } else {
                            None
                        }
                    };
                    if let Some((_, _, Some(stop))) = held {
                        stop();
                    }
                }))
            }
        }
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        let held = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return Ok(());
            }
            state.closed = true;
            state.attachment.take()
        };
        if let Some((_, receiver, stop)) = held {
            if let Some(stop) = stop {
                stop();
            }
            if let Some(callback) = receiver.closed {
                callback(code, reason.to_owned());
            }
        }
        Ok(())
    }
}
