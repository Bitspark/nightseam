use nightseam::{Options, Payload, Peer, PublicError, Role, handle_wire, wire_pair};
use nightseam_duplex::{
    Detach, Frame, Message, ProfileFrame, ProfileKind, Receiver, ReturnAddress, Wire, pipe,
};
use serde_json::{Value, json};
use std::{sync::Arc, time::Duration};
use tokio::{sync::Notify, time::timeout};

struct Replies(tokio::sync::mpsc::UnboundedSender<Message>);
impl Wire for Replies {
    fn send(&self, _: &[String], message: Message) -> Result<(), PublicError> {
        self.0
            .send(message)
            .map_err(|_| PublicError::new("closed", "test sink closed"))
    }
    fn receive(&self, _: &[String], _: Receiver) -> Result<Detach, PublicError> {
        unreachable!()
    }
    fn close(&self, _: u16, _: &str) -> Result<(), PublicError> {
        Ok(())
    }
}
fn request(id: &str, returning: &Arc<ReturnAddress>) -> Message {
    let mut frame = ProfileFrame::new(ProfileKind::Request);
    frame.id = id.into();
    frame.params = Payload::from_json("null").unwrap();
    Message {
        frame,
        returning: Some(returning.clone()),
        context: None,
    }
}

#[tokio::test]
async fn busy_refusals_keep_the_dispatch_order_and_consume_bounded_queue_capacity() {
    let (sender, receiver) = wire_pair(Options {
        max_pending_requests: 1,
        queue_capacity: 2,
        ..Options::default()
    })
    .unwrap();
    let (reply_tx, mut reply_rx) = tokio::sync::mpsc::unbounded_channel();
    let returning = Arc::new(ReturnAddress {
        wire: Arc::new(Replies(reply_tx)),
    });
    let started = Arc::new(Notify::new());
    receiver
        .receive(
            &["hold".into()],
            Receiver::new({
                let started = started.clone();
                move |_, _| {
                    started.notify_one();
                }
            }),
        )
        .unwrap();
    let entered = Arc::new(Notify::new());
    let (release_tx, release_rx) = std::sync::mpsc::channel();
    let release_rx = Arc::new(std::sync::Mutex::new(release_rx));
    receiver
        .receive(
            &["block".into()],
            Receiver::new({
                let entered = entered.clone();
                move |_, _| {
                    entered.notify_one();
                    release_rx.lock().unwrap().recv().unwrap();
                }
            }),
        )
        .unwrap();
    sender
        .send(&["hold".into()], request("c:1", &returning))
        .unwrap();
    timeout(Duration::from_secs(1), started.notified())
        .await
        .unwrap();
    let mut event = ProfileFrame::new(ProfileKind::Event);
    event.data = Payload::from_json("null").unwrap();
    sender.send(&["block".into()], Message::new(event)).unwrap();
    timeout(Duration::from_secs(1), entered.notified())
        .await
        .unwrap();
    sender
        .send(&["hold".into()], request("c:2", &returning))
        .unwrap();
    sender
        .send(&["hold".into()], request("c:3", &returning))
        .unwrap();
    let overtook = timeout(Duration::from_millis(30), reply_rx.recv()).await;
    let overflow = sender.send(&["hold".into()], request("c:4", &returning));
    release_tx.send(()).unwrap();
    assert!(
        overtook.is_err(),
        "a busy response overtook the blocked prior event"
    );
    assert_eq!(overflow.unwrap_err().code, "backpressure");
    let mut settled = std::collections::BTreeSet::new();
    for _ in 0..3 {
        let reply = timeout(Duration::from_secs(1), reply_rx.recv())
            .await
            .unwrap()
            .unwrap();
        assert_eq!(reply.frame.error.unwrap().code, "disconnected");
        settled.insert(reply.frame.id);
    }
    assert_eq!(
        settled,
        ["c:1", "c:2", "c:3"]
            .into_iter()
            .map(str::to_owned)
            .collect()
    );
}

#[tokio::test]
async fn closing_a_return_capability_rejects_a_late_response() {
    let (sender, receiver) = wire_pair(Options::default()).unwrap();
    let (reply_tx, mut reply_rx) = tokio::sync::mpsc::unbounded_channel();
    let returning = Arc::new(ReturnAddress {
        wire: Arc::new(Replies(reply_tx)),
    });
    let (captured_tx, mut captured_rx) = tokio::sync::mpsc::unbounded_channel();
    receiver
        .receive(
            &["hold".into()],
            Receiver::new(move |_, message| {
                captured_tx.send(message).unwrap();
            }),
        )
        .unwrap();
    sender
        .send(&["hold".into()], request("c:1", &returning))
        .unwrap();
    let captured = timeout(Duration::from_secs(1), captured_rx.recv())
        .await
        .unwrap()
        .unwrap();
    let address = captured.returning.unwrap();
    address.wire.close(1000, "done").unwrap();
    let mut frame = ProfileFrame::new(ProfileKind::Response);
    frame.id = "c:1".into();
    frame.result = Payload::from_json("null").unwrap();
    assert_eq!(
        address
            .wire
            .send(&[], Message::new(frame))
            .unwrap_err()
            .code,
        "disconnected"
    );
    assert!(reply_rx.try_recv().is_err());
    sender.close(1000, "done").unwrap();
}

#[tokio::test]
async fn cancelled_routed_handler_keeps_its_slot_until_it_returns() {
    let (raw, connection) = pipe(0);
    let server = Peer::over(
        connection,
        Role::Server,
        Options {
            max_concurrent_handlers: 1,
            ..Options::default()
        },
    )
    .unwrap();
    let started = Arc::new(Notify::new());
    let cancelled = Arc::new(Notify::new());
    let release = Arc::new(Notify::new());
    handle_wire(server.wire(), &["hold".into()], {
        let (started, cancelled, release) = (started.clone(), cancelled.clone(), release.clone());
        move |ctx, _| {
            let (started, cancelled, release) =
                (started.clone(), cancelled.clone(), release.clone());
            async move {
                started.notify_one();
                ctx.cancelled().await;
                cancelled.notify_one();
                release.notified().await;
                Ok(Payload::from_json("null").unwrap())
            }
        }
    })
    .unwrap();
    handle_wire(server.wire(), &["echo".into()], |_, value| async move {
        Ok(value)
    })
    .unwrap();
    let request = |id: &str, method: &str| {
        Frame::Text(
            serde_json::to_vec(
                &json!({"version":1,"kind":"request","id":id,"method":method,"params":null}),
            )
            .unwrap(),
        )
    };
    raw.send(request("c:1", "4:hold")).await.unwrap();
    timeout(Duration::from_secs(1), started.notified())
        .await
        .unwrap();
    raw.send(Frame::Text(
        br#"{"version":1,"kind":"cancel","id":"c:1"}"#.to_vec(),
    ))
    .await
    .unwrap();
    timeout(Duration::from_secs(1), cancelled.notified())
        .await
        .unwrap();
    assert!(
        timeout(Duration::from_millis(30), raw.receive())
            .await
            .is_err(),
        "cancellation must signal work without manufacturing its completion"
    );
    raw.send(request("c:2", "4:echo")).await.unwrap();
    let busy = timeout(Duration::from_secs(1), raw.receive())
        .await
        .unwrap()
        .unwrap();
    let busy: Value = serde_json::from_slice(busy.data()).unwrap();
    assert_eq!(busy["id"], "c:2");
    assert_eq!(busy["error"]["code"], "busy");
    release.notify_one();
    let ended = timeout(Duration::from_secs(1), raw.receive())
        .await
        .unwrap()
        .unwrap();
    let ended: Value = serde_json::from_slice(ended.data()).unwrap();
    assert_eq!(ended["id"], "c:1");
    assert_eq!(ended["error"]["code"], "cancelled");
    raw.send(request("c:3", "4:echo")).await.unwrap();
    let echo = timeout(Duration::from_secs(1), raw.receive())
        .await
        .unwrap()
        .unwrap();
    let echo: Value = serde_json::from_slice(echo.data()).unwrap();
    assert_eq!(echo["id"], "c:3");
    assert!(echo.get("result").is_some());
    server.close();
}

#[tokio::test]
async fn receiver_deadline_answers_once_while_raw_and_routed_work_keep_their_slots() {
    for routed in [false, true] {
        let (raw, connection) = pipe(0);
        let server = Peer::over(
            connection,
            Role::Server,
            Options {
                max_concurrent_handlers: 1,
                request_timeout: Duration::from_millis(50),
                ..Options::default()
            },
        )
        .unwrap();
        let cancelled = Arc::new(Notify::new());
        let release = Arc::new(Notify::new());
        let handler = {
            let (cancelled, release) = (cancelled.clone(), release.clone());
            move |ctx: nightseam::Context, _: Payload| {
                let (cancelled, release) = (cancelled.clone(), release.clone());
                async move {
                    ctx.cancelled().await;
                    cancelled.notify_one();
                    release.notified().await;
                    Ok(Payload::from_json("null").unwrap())
                }
            }
        };
        let method = if routed {
            handle_wire(server.wire(), &["hold".into()], handler).unwrap();
            "4:hold"
        } else {
            server.handle("hold", handler).unwrap();
            "hold"
        };
        server
            .handle("echo", |_, value| async move { Ok(value) })
            .unwrap();
        let request = |id: &str, method: &str| {
            Frame::Text(
                serde_json::to_vec(&json!({
                    "version": 1, "kind": "request", "id": id,
                    "method": method, "params": null
                }))
                .unwrap(),
            )
        };
        raw.send(request("c:1", method)).await.unwrap();
        timeout(Duration::from_secs(1), cancelled.notified())
            .await
            .unwrap();
        let answer = timeout(Duration::from_secs(1), raw.receive()).await;
        if answer.is_err() {
            release.notify_one();
            server.close();
            panic!("receiver deadline waited for the body to return (routed={routed})");
        }
        let answer: Value = serde_json::from_slice(answer.unwrap().unwrap().data()).unwrap();
        assert_eq!(answer["id"], "c:1");
        assert_eq!(answer["error"]["code"], "cancelled");
        raw.send(request("c:2", "echo")).await.unwrap();
        let busy = timeout(Duration::from_secs(1), raw.receive())
            .await
            .unwrap()
            .unwrap();
        let busy: Value = serde_json::from_slice(busy.data()).unwrap();
        assert_eq!(busy["id"], "c:2");
        assert_eq!(busy["error"]["code"], "busy");
        release.notify_one();
        timeout(Duration::from_secs(1), async {
            for n in 3.. {
                let id = format!("c:{n}");
                raw.send(request(&id, "echo")).await.unwrap();
                let reply = raw.receive().await.unwrap();
                let reply: Value = serde_json::from_slice(reply.data()).unwrap();
                assert_eq!(reply["id"], id, "a completed deadline replied twice");
                if reply.get("result").is_some() {
                    break;
                }
                assert_eq!(reply["error"]["code"], "busy");
                tokio::task::yield_now().await;
            }
        })
        .await
        .unwrap();
        assert!(
            timeout(Duration::from_millis(30), raw.receive())
                .await
                .is_err()
        );
        server.close();
    }
}
