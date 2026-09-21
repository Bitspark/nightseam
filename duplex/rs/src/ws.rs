//! WebSocket transport, including TLS and optional subprotocol negotiation.

use crate::{Close, Connection, End, Frame, SharedConnection};
use async_trait::async_trait;
use futures_util::{SinkExt, StreamExt};
use std::{sync::Arc, time::Duration};
use tokio::{
    io::{AsyncRead, AsyncWrite},
    net::TcpListener,
    sync::{Mutex, mpsc, oneshot},
};
use tokio_tungstenite::{
    WebSocketStream, accept_hdr_async_with_config, connect_async_with_config,
    tungstenite::{
        Error, Message,
        client::IntoClientRequest,
        handshake::server::{Request, Response},
        http::header::SEC_WEBSOCKET_PROTOCOL,
        protocol::{CloseFrame, WebSocketConfig, frame::coding::CloseCode},
    },
};
use tokio_util::sync::CancellationToken;

fn failure(error: impl std::fmt::Display) -> Close {
    Close::new(1006, error.to_string())
}
fn config(limit: usize) -> WebSocketConfig {
    WebSocketConfig::default()
        .max_message_size(if limit == 0 { None } else { Some(limit) })
        .max_frame_size(if limit == 0 { None } else { Some(limit) })
}

/// Dial with no implicit subprotocol. A supplied list is offered verbatim.
pub async fn dial(
    url: &str,
    limit: usize,
    subprotocols: &[&str],
) -> Result<SharedConnection, Close> {
    let mut request = url.into_client_request().map_err(failure)?;
    if !subprotocols.is_empty() {
        request.headers_mut().insert(
            SEC_WEBSOCKET_PROTOCOL,
            subprotocols.join(", ").parse().map_err(failure)?,
        );
    }
    let (socket, response) = tokio::time::timeout(
        Duration::from_secs(30),
        connect_async_with_config(request, Some(config(limit)), false),
    )
    .await
    .map_err(failure)?
    .map_err(failure)?;
    let protocol = response
        .headers()
        .get(SEC_WEBSOCKET_PROTOCOL)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("")
        .to_owned();
    Ok(wrap(socket, protocol))
}

/// A listener owns its listening socket; dropping it releases the port.
pub struct Listener {
    listener: TcpListener,
    limit: usize,
    subprotocols: Vec<String>,
}

impl Listener {
    pub async fn bind(
        address: &str,
        limit: usize,
        subprotocols: Vec<String>,
    ) -> Result<Self, Close> {
        Ok(Self {
            listener: TcpListener::bind(address).await.map_err(failure)?,
            limit,
            subprotocols,
        })
    }
    pub fn url(&self) -> Result<String, Close> {
        Ok(format!(
            "ws://{}/",
            self.listener.local_addr().map_err(failure)?
        ))
    }
    /// Accept one connection; the server's list determines selection order.
    pub async fn accept(&self) -> Result<SharedConnection, Close> {
        let (stream, _) = self.listener.accept().await.map_err(failure)?;
        let mut selected = String::new();
        // Tungstenite's callback fixes this error type to an HTTP response.
        #[allow(clippy::result_large_err)]
        let callback = |request: &Request, mut response: Response| {
            let offered: Vec<_> = request
                .headers()
                .get(SEC_WEBSOCKET_PROTOCOL)
                .and_then(|v| v.to_str().ok())
                .unwrap_or("")
                .split(',')
                .map(str::trim)
                .collect();
            if let Some(protocol) = self
                .subprotocols
                .iter()
                .find(|p| offered.contains(&p.as_str()))
            {
                if let Ok(value) = protocol.parse() {
                    response.headers_mut().insert(SEC_WEBSOCKET_PROTOCOL, value);
                    selected = protocol.clone();
                }
            }
            Ok(response)
        };
        let socket = tokio::time::timeout(
            Duration::from_secs(30),
            accept_hdr_async_with_config(stream, callback, Some(config(self.limit))),
        )
        .await
        .map_err(failure)?
        .map_err(failure)?;
        Ok(wrap(socket, selected))
    }
}

enum Command {
    Write(
        Message,
        oneshot::Sender<Result<(), Close>>,
        CancellationToken,
    ),
    Flush(oneshot::Sender<()>),
}

struct Socket {
    outgoing: mpsc::Sender<Command>,
    incoming: Mutex<mpsc::Receiver<Frame>>,
    ending: Arc<End>,
    closing: Arc<End>,
    stop: CancellationToken,
    protocol: String,
}

fn wrap<S>(socket: WebSocketStream<S>, protocol: String) -> SharedConnection
where
    S: AsyncRead + AsyncWrite + Unpin + Send + 'static,
{
    let (mut sink, mut stream) = socket.split();
    let (outgoing, mut outgoing_rx) = mpsc::channel::<Command>(8);
    let (incoming_tx, incoming) = mpsc::channel(8);
    let ending = Arc::new(End::default());
    let closing = Arc::new(End::default());
    let stop = CancellationToken::new();
    let writer_end = ending.clone();
    let writer_stop = stop.clone();
    tokio::spawn(async move {
        loop {
            let command = tokio::select! {
                biased;
                _ = writer_stop.cancelled() => break,
                command = outgoing_rx.recv() => match command { Some(command) => command, None => break },
            };
            match command {
                Command::Write(message, answer, cancel) => {
                    if answer.is_closed() {
                        continue;
                    }
                    let result = tokio::select! {
                        biased;
                        _ = writer_stop.cancelled() => break,
                        _ = cancel.cancelled() => break,
                        result = sink.send(message) => result.map_err(failure),
                    };
                    let failed = result.is_err();
                    let _ = answer.send(result);
                    if failed {
                        break;
                    }
                }
                Command::Flush(answer) => {
                    let result = tokio::select! {
                        _ = writer_stop.cancelled() => break,
                        result = sink.flush() => result,
                    };
                    let _ = answer.send(());
                    if result.is_err() {
                        break;
                    }
                }
            }
        }
        writer_end.end(Close::new(1006, ""));
        writer_stop.cancel();
    });
    let reader_stop = stop.clone();
    let reader_end = ending.clone();
    let reader_closing = closing.clone();
    let controls = outgoing.clone();
    tokio::spawn(async move {
        loop {
            // Reserve space before reading: a stopped consumer eventually
            // paces TCP instead of filling an unbounded queue. During a local
            // close, discard data while waiting for the close acknowledgement.
            let permit = if reader_closing.done.is_cancelled() {
                None
            } else {
                tokio::select! {
                    biased;
                    _ = reader_stop.cancelled() => break,
                    _ = reader_closing.done.cancelled() => None,
                    permit = incoming_tx.reserve() => match permit { Ok(permit) => Some(permit), Err(_) => break },
                }
            };
            let message = tokio::select! {
                biased;
                _ = reader_stop.cancelled() => break,
                message = stream.next() => message,
            };
            match message {
                Some(Ok(Message::Text(text))) => {
                    if let Some(permit) = permit {
                        permit.send(Frame::Text(text.as_bytes().to_vec()));
                    }
                }
                Some(Ok(Message::Binary(data))) => {
                    if let Some(permit) = permit {
                        permit.send(Frame::Binary(data.to_vec()));
                    }
                }
                Some(Ok(Message::Close(close))) => {
                    let close = close
                        .map(|c| Close::new(c.code.into(), c.reason.to_string()))
                        .unwrap_or_else(|| Close::new(1005, ""));
                    let (tx, rx) = oneshot::channel();
                    if controls.send(Command::Flush(tx)).await.is_ok() {
                        let _ = rx.await;
                    }
                    reader_end.end(close);
                    break;
                }
                Some(Ok(Message::Ping(_))) => {
                    let (tx, rx) = oneshot::channel();
                    if controls.send(Command::Flush(tx)).await.is_ok() {
                        let _ = rx.await;
                    }
                }
                Some(Ok(_)) => {}
                Some(Err(Error::Capacity(_))) => {
                    reader_end.end(Close::new(1009, "frame exceeds receive limit"));
                    break;
                }
                Some(Err(_)) | None => break,
            }
        }
        reader_end.end(Close::new(1006, ""));
        reader_stop.cancel();
    });
    Arc::new(Socket {
        outgoing,
        incoming: Mutex::new(incoming),
        ending,
        closing,
        stop,
        protocol,
    })
}

impl Socket {
    async fn write(&self, message: Message) -> Result<(), Close> {
        let (answer, result) = oneshot::channel();
        let cancel = CancellationToken::new();
        let _guard = cancel.clone().drop_guard();
        tokio::select! {
            biased;
            _ = self.ending.done.cancelled() => return Err(self.ending.error()),
            sent = self.outgoing.send(Command::Write(message, answer, cancel)) => sent.map_err(|_| self.ending.error())?,
        }
        tokio::select! {
            biased;
            result = result => result.unwrap_or_else(|_| Err(self.ending.error())),
            _ = self.ending.done.cancelled() => Err(self.ending.error()),
        }
    }
}

#[async_trait]
impl Connection for Socket {
    async fn send(&self, frame: Frame) -> Result<(), Close> {
        if self.closing.done.is_cancelled() {
            return Err(self.closing.error());
        }
        let message = match frame {
            Frame::Text(data) => Message::Text(String::from_utf8(data).map_err(failure)?.into()),
            Frame::Binary(data) => Message::Binary(data.into()),
        };
        self.write(message).await
    }
    async fn receive(&self) -> Result<Frame, Close> {
        if self.closing.done.is_cancelled() {
            return Err(self.closing.error());
        }
        let mut receive = self.incoming.lock().await;
        tokio::select! {
            biased;
            _ = self.closing.done.cancelled() => Err(self.closing.error()),
            frame = receive.recv() => frame.ok_or_else(|| self.ending.error()),
            _ = self.ending.done.cancelled() => receive.try_recv().map_err(|_| self.ending.error()),
        }
    }
    async fn close(&self, code: u16, reason: &str) -> Result<(), Close> {
        if self.closing.done.is_cancelled() {
            return Err(self.closing.error());
        }
        self.closing.end(Close::new(code, reason));
        self.write(Message::Close(Some(CloseFrame {
            code: CloseCode::from(code),
            reason: reason.to_owned().into(),
        })))
        .await?;
        if tokio::time::timeout(Duration::from_secs(5), self.ending.done.cancelled())
            .await
            .is_err()
        {
            self.abort();
            return Err(Close::new(1006, "close acknowledgement timed out"));
        }
        Ok(())
    }
    fn abort(&self) {
        self.closing.end(Close::new(1006, ""));
        self.ending.end(Close::new(1006, ""));
        self.stop.cancel();
    }
    fn subprotocol(&self) -> &str {
        &self.protocol
    }
}

impl Drop for Socket {
    fn drop(&mut self) {
        self.abort();
    }
}
