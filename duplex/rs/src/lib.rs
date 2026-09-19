//! Ordered bidirectional frames, independent of the protocol they carry.

pub mod ws;

use async_trait::async_trait;
use std::{
    fmt,
    sync::{Arc, Mutex as SyncMutex},
};
use tokio::sync::{Mutex, mpsc};
use tokio_util::sync::CancellationToken;

/// A complete frame. Text bytes are retained until the protocol validates them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Frame {
    Text(Vec<u8>),
    Binary(Vec<u8>),
}

impl Frame {
    pub fn data(&self) -> &[u8] {
        match self {
            Self::Text(data) | Self::Binary(data) => data,
        }
    }
}

/// How a connection ended. Code 1006 means that no close frame arrived.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Close {
    pub code: u16,
    pub reason: String,
}

impl Close {
    pub fn new(code: u16, reason: impl Into<String>) -> Self {
        Self {
            code,
            reason: reason.into(),
        }
    }
}

impl fmt::Display for Close {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "duplex connection closed ({}): {}",
            self.code, self.reason
        )
    }
}
impl std::error::Error for Close {}

/// A transport paces each sender and receives ordered whole frames. One send
/// and one receive may run concurrently. Dropping either future cancels that
/// wait; `abort` releases every wait and closes the underlying transport.
#[async_trait]
pub trait Connection: Send + Sync {
    async fn send(&self, frame: Frame) -> Result<(), Close>;
    async fn receive(&self) -> Result<Frame, Close>;
    async fn close(&self, code: u16, reason: &str) -> Result<(), Close>;
    fn abort(&self);
    fn subprotocol(&self) -> &str {
        ""
    }
}

pub type SharedConnection = Arc<dyn Connection>;

#[derive(Default)]
struct End {
    closed: SyncMutex<Option<Close>>,
    done: CancellationToken,
}

impl End {
    fn end(&self, close: Close) {
        let mut closed = self.closed.lock().unwrap();
        if closed.is_none() {
            *closed = Some(close);
            self.done.cancel();
        }
    }
    fn error(&self) -> Close {
        self.closed
            .lock()
            .unwrap()
            .clone()
            .unwrap_or_else(|| Close::new(1006, ""))
    }
}

struct Pipe {
    limit: usize,
    send: mpsc::Sender<Frame>,
    receive: Mutex<mpsc::Receiver<Frame>>,
    local: Arc<End>,
    remote: Arc<End>,
}

/// Eight frames may be in flight in each direction before send waits.
/// A zero receive limit is unlimited, as for the other seam transports.
pub fn pipe(limit: usize) -> (SharedConnection, SharedConnection) {
    let (atx, arx) = mpsc::channel(8);
    let (btx, brx) = mpsc::channel(8);
    let a = Arc::new(End::default());
    let b = Arc::new(End::default());
    (
        Arc::new(Pipe {
            limit,
            send: atx,
            receive: Mutex::new(brx),
            local: a.clone(),
            remote: b.clone(),
        }),
        Arc::new(Pipe {
            limit,
            send: btx,
            receive: Mutex::new(arx),
            local: b,
            remote: a,
        }),
    )
}

#[async_trait]
impl Connection for Pipe {
    async fn send(&self, frame: Frame) -> Result<(), Close> {
        tokio::select! {
            biased;
            _ = self.local.done.cancelled() => Err(self.local.error()),
            _ = self.remote.done.cancelled() => Err(self.remote.error()),
            result = self.send.send(frame) => result.map_err(|_| self.remote.error()),
        }
    }
    async fn receive(&self) -> Result<Frame, Close> {
        if self.local.done.is_cancelled() {
            return Err(self.local.error());
        }
        let mut rx = self.receive.lock().await;
        let frame = tokio::select! {
            biased;
            _ = self.local.done.cancelled() => return Err(self.local.error()),
            frame = rx.recv() => frame.ok_or_else(|| self.remote.error())?,
            _ = self.remote.done.cancelled() => match rx.try_recv() {
                Ok(frame) => frame,
                Err(_) => return Err(self.remote.error()),
            },
        };
        if self.limit > 0 && frame.data().len() > self.limit {
            self.abort();
            return Err(Close::new(
                1009,
                format!(
                    "duplex frame of {} bytes exceeds the receive limit of {}",
                    frame.data().len(),
                    self.limit
                ),
            ));
        }
        Ok(frame)
    }
    async fn close(&self, code: u16, reason: &str) -> Result<(), Close> {
        self.local.end(Close::new(code, reason));
        Ok(())
    }
    fn abort(&self) {
        self.local.end(Close::new(1006, ""));
    }
}

impl Drop for Pipe {
    fn drop(&mut self) {
        self.abort();
    }
}
