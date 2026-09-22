use bitwire::{Detach, Endpoint, Message, PublicError, Receiver, SharedWire, Wire};
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

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
    struct Selected {
        root: SharedWire,
        prefix: Vec<String>,
    }
    impl Wire for Selected {
        fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
            self.root.send(
                &self.prefix.iter().chain(path).cloned().collect::<Vec<_>>(),
                message,
            )
        }
    }
    Arc::new(Selected {
        root,
        prefix: prefix.to_vec(),
    })
}

struct Attachment {
    id: u64,
    receiver: Receiver,
    detaches: Vec<Detach>,
    remaining: usize,
}
#[derive(Default)]
struct State {
    closed: bool,
    next: u64,
    attachment: Option<Attachment>,
}
struct Mount {
    children: BTreeMap<String, Arc<dyn Endpoint>>,
    state: Arc<Mutex<State>>,
}

pub fn mount(children: BTreeMap<String, Arc<dyn Endpoint>>) -> Arc<dyn Endpoint> {
    Arc::new(Mount {
        children,
        state: Arc::default(),
    })
}
fn closed() -> PublicError {
    PublicError::new("disconnected", "Wire closed")
}
fn detach(state: &Mutex<State>, id: u64) {
    let held = {
        let mut state = state.lock().unwrap();
        if state.attachment.as_ref().is_some_and(|a| a.id == id) {
            state.attachment.take()
        } else {
            None
        }
    };
    if let Some(held) = held {
        for stop in held.detaches {
            stop();
        }
    }
}
impl Wire for Mount {
    fn send(&self, path: &[String], message: Message) -> Result<(), PublicError> {
        if self.state.lock().unwrap().closed {
            return Err(closed());
        }
        let child = path
            .first()
            .and_then(|key| self.children.get(key))
            .ok_or_else(|| PublicError::new("no_route", "wire path has no destination"))?;
        child.send(&path[1..], message)
    }
}
impl Endpoint for Mount {
    fn receive(&self, receiver: Receiver) -> Result<Detach, PublicError> {
        let id = {
            let mut state = self.state.lock().unwrap();
            if state.closed {
                return Err(closed());
            }
            if state.attachment.is_some() {
                return Err(PublicError::new(
                    "receiver_exists",
                    "endpoint already attached",
                ));
            }
            state.next += 1;
            let id = state.next;
            state.attachment = Some(Attachment {
                id,
                receiver: receiver.clone(),
                detaches: vec![],
                remaining: self.children.len(),
            });
            id
        };
        for (key, child) in &self.children {
            let prefix = key.clone();
            let message = receiver.message.clone();
            let state = self.state.clone();
            let incoming = Receiver {
                message: message.map(|callback| {
                    Arc::new(move |path: Vec<String>, message| {
                        callback(
                            std::iter::once(prefix.clone()).chain(path).collect(),
                            message,
                        );
                    }) as bitwire::Delivery
                }),
                closed: Some(Arc::new(move |code, reason| {
                    let receiver = {
                        let mut s = state.lock().unwrap();
                        if let Some(a) = s.attachment.as_mut().filter(|a| a.id == id) {
                            a.remaining -= 1;
                            if a.remaining == 0 {
                                Some(a.receiver.closed.clone())
                            } else {
                                None
                            }
                        } else {
                            None
                        }
                    };
                    if let Some(callback) = receiver {
                        detach(&state, id);
                        if let Some(callback) = callback {
                            callback(code, reason);
                        }
                    }
                })),
            };
            match child.receive(incoming) {
                Ok(stop) => {
                    let mut state = self.state.lock().unwrap();
                    if let Some(held) = state.attachment.as_mut().filter(|a| a.id == id) {
                        held.detaches.push(stop);
                    } else {
                        drop(state);
                        stop();
                        return Err(closed());
                    }
                }
                Err(error) => {
                    detach(&self.state, id);
                    return Err(error);
                }
            }
        }
        let state = self.state.clone();
        Ok(Arc::new(move || detach(&state, id)))
    }
    fn close(&self, code: u16, reason: &str) -> Result<(), PublicError> {
        let held = {
            let mut s = self.state.lock().unwrap();
            if s.closed {
                return Ok(());
            }
            s.closed = true;
            s.attachment.take()
        };
        if let Some(held) = held {
            for stop in held.detaches {
                stop();
            }
            if let Some(callback) = held.receiver.closed {
                callback(code, reason.to_owned());
            }
        }
        Ok(())
    }
}
