use nightseam::{
    Context, Options, Payload, Peer, Role, call_wire, emit_wire, forward_wire, handle_wire,
    wire_pair,
};
use nightseam_duplex::{Message, ProfileFrame, ProfileKind, Receiver, at, mount, pipe};
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::sync::mpsc;

struct ReturnSink(mpsc::UnboundedSender<Message>);
impl nightseam_duplex::Wire for ReturnSink {
    fn send(&self, _: &[String], message: Message) -> Result<(), nightseam::PublicError> {
        self.0.send(message).unwrap();
        Ok(())
    }
    fn receive(
        &self,
        _: &[String],
        _: Receiver,
    ) -> Result<nightseam_duplex::Detach, nightseam::PublicError> {
        unreachable!()
    }
    fn close(&self, _: u16, _: &str) -> Result<(), nightseam::PublicError> {
        Ok(())
    }
}

fn addressed(
    kind: ProfileKind,
    address: &Arc<nightseam_duplex::ReturnAddress>,
    id: &str,
) -> Message {
    let mut frame = ProfileFrame::new(kind);
    frame.id = id.into();
    if kind == ProfileKind::Request {
        frame.params = Payload::from_json("{}").unwrap();
    }
    Message {
        frame,
        returning: Some(address.clone()),
        context: None,
    }
}

#[tokio::test]
async fn closing_a_peer_settles_a_request_still_in_its_wire_queue() {
    let (near, far) = pipe(0);
    let peer = Peer::over(near, Role::Client, Options::default()).unwrap();
    let (tx, mut rx) = mpsc::unbounded_channel();
    let address = Arc::new(nightseam_duplex::ReturnAddress {
        wire: Arc::new(ReturnSink(tx)),
    });
    peer.wire()
        .send(
            &path(&["operation"]),
            addressed(ProfileKind::Request, &address, "c:1"),
        )
        .unwrap();
    // No await has let the wire worker consume the admitted request.
    peer.close();
    let reply = tokio::time::timeout(Duration::from_millis(200), rx.recv())
        .await
        .unwrap()
        .unwrap();
    assert_eq!(reply.frame.error.unwrap().code, "disconnected");
    assert!(rx.try_recv().is_err());
    far.abort();
}

#[tokio::test]
async fn wire_cancellation_uses_reserved_capacity_in_admission_order() {
    let (near, far) = pipe(0);
    let peer = Peer::over(
        near,
        Role::Client,
        Options {
            queue_capacity: 2,
            max_pending_requests: 1,
            ..Options::default()
        },
    )
    .unwrap();
    let wire = peer.wire();
    let (tx, mut rx) = mpsc::unbounded_channel();
    let address = Arc::new(nightseam_duplex::ReturnAddress {
        wire: Arc::new(ReturnSink(tx)),
    });
    wire.send(
        &path(&["operation"]),
        addressed(ProfileKind::Request, &address, "c:1"),
    )
    .unwrap();
    emit_wire(
        Context::default(),
        wire.clone(),
        &path(&["event"]),
        Payload::from_json("null").unwrap(),
    )
    .unwrap();
    // Both data slots are full. Control has exactly one reservation per request.
    for _ in 0..5 {
        wire.send(
            &path(&["operation"]),
            addressed(ProfileKind::Cancel, &address, "c:1"),
        )
        .unwrap();
        wire.send(
            &path(&["operation"]),
            addressed(ProfileKind::Cancel, &address, "c:999"),
        )
        .unwrap();
    }
    for kind in ["request", "event", "cancel"] {
        let frame = tokio::time::timeout(Duration::from_secs(1), far.receive())
            .await
            .unwrap()
            .unwrap();
        let nightseam_duplex::Frame::Text(bytes) = frame else {
            panic!("expected profile text")
        };
        let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["kind"], kind);
    }
    assert_eq!(
        rx.recv().await.unwrap().frame.error.unwrap().code,
        "cancelled"
    );
    wire.send(
        &path(&["operation"]),
        addressed(ProfileKind::Cancel, &address, "c:1"),
    )
    .unwrap();
    emit_wire(
        Context::default(),
        wire,
        &path(&["fence"]),
        Payload::from_json("null").unwrap(),
    )
    .unwrap();
    let nightseam_duplex::Frame::Text(bytes) = far.receive().await.unwrap() else {
        panic!("expected profile text")
    };
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&bytes).unwrap()["kind"],
        "event"
    );
    peer.close();
    far.abort();
}

#[tokio::test]
async fn exact_wire_and_raw_registrations_refuse_duplicates_in_both_orders() {
    let (near, far) = pipe(0);
    let peer = Peer::over(near, Role::Client, Options::default()).unwrap();
    let wire = peer.wire();
    let route = path(&["operation"]);
    let name = nightseam_duplex::encode_path(&route);
    let detach = wire.receive(&route, Receiver::new(|_, _| {})).unwrap();
    assert!(
        peer.handle(&name, |_, value| async move { Ok(value) })
            .is_err()
    );
    assert!(peer.on_event(&name, |_, _| async {}).is_err());
    detach();
    detach();
    peer.handle(&name, |_, value| async move { Ok(value) })
        .unwrap();
    assert!(wire.receive(&route, Receiver::new(|_, _| {})).is_err());
    let other = path(&["event"]);
    peer.on_event(&nightseam_duplex::encode_path(&other), |_, _| async {})
        .unwrap();
    assert!(wire.receive(&other, Receiver::new(|_, _| {})).is_err());
    wire.receive(
        &[],
        Receiver {
            namespace: true,
            ..Receiver::new(|_, _| {})
        },
    )
    .unwrap();
    peer.handle("5:exact", |_, value| async move { Ok(value) })
        .unwrap();
    peer.close();
    far.abort();
}

#[tokio::test]
async fn refused_requests_consume_bounded_data_capacity_and_close_with_their_carrier() {
    let (near, far) = pipe(0);
    let peer = Peer::over(
        near,
        Role::Client,
        Options {
            queue_capacity: 2,
            max_pending_requests: 1,
            ..Options::default()
        },
    )
    .unwrap();
    let wire = peer.wire();
    let (tx, mut rx) = mpsc::unbounded_channel();
    let address = Arc::new(nightseam_duplex::ReturnAddress {
        wire: Arc::new(ReturnSink(tx)),
    });
    wire.send(
        &path(&["op"]),
        addressed(ProfileKind::Request, &address, "c:1"),
    )
    .unwrap();
    // A pending-capacity refusal remains an admitted data delivery until dispatch.
    wire.send(
        &path(&["op"]),
        addressed(ProfileKind::Request, &address, "c:2"),
    )
    .unwrap();
    assert!(rx.try_recv().is_err());
    assert_eq!(
        wire.send(
            &path(&["op"]),
            addressed(ProfileKind::Request, &address, "c:3")
        )
        .unwrap_err()
        .code,
        "backpressure"
    );
    let mut ids = Vec::new();
    for _ in 0..2 {
        let reply = tokio::time::timeout(Duration::from_millis(200), rx.recv())
            .await
            .unwrap()
            .unwrap();
        assert_eq!(reply.frame.error.unwrap().code, "disconnected");
        ids.push(reply.frame.id);
    }
    ids.sort();
    assert_eq!(ids, ["c:1", "c:2"]);
    far.abort();
}

fn path(segments: &[&str]) -> Vec<String> {
    segments.iter().map(|s| (*s).to_owned()).collect()
}

#[tokio::test]
async fn local_paths_mounts_forwarding_and_identity_keep_the_borrowed_carriers() {
    let (caller, left) = wire_pair(Options::default()).unwrap();
    let (right, callee) = wire_pair(Options::default()).unwrap();
    let detach = forward_wire(left.clone(), right).unwrap();
    let view = at(
        mount(BTreeMap::from([(
            "out".into(),
            at(callee.clone(), &path(&["source"])),
        )])),
        &path(&["out"]),
    );
    let seen = Arc::new(Mutex::new(Vec::new()));
    let entered = seen.clone();
    handle_wire(view.clone(), &path(&["é", ""]), move |ctx, value| {
        entered.lock().unwrap().push(ctx.meta().clone());
        async move { Ok(value) }
    })
    .unwrap();
    let value = call_wire(
        Context::default().with_meta(BTreeMap::from([("tenant".into(), "one".into())])),
        caller.clone(),
        &path(&["source", "é", ""]),
        Payload::from_json("1e3").unwrap(),
        Options::default(),
    )
    .await
    .unwrap();
    assert_eq!(value.raw(), Some("1e3"));
    assert_eq!(seen.lock().unwrap()[0]["tenant"], "one");
    view.close(1008, "view closed").unwrap();
    handle_wire(callee.clone(), &path(&["still"]), |_, value| async move {
        Ok(value)
    })
    .unwrap();
    assert!(
        call_wire(
            Context::default(),
            caller.clone(),
            &path(&["still"]),
            Payload::from_json("null").unwrap(),
            Options::default()
        )
        .await
        .is_ok()
    );
    detach();
    caller.close(1000, "done").unwrap();
    callee.close(1000, "done").unwrap();
}

#[tokio::test]
async fn peer_bridge_preserves_canonical_paths_and_cancellation_after_detach() {
    let (a, b) = pipe(0);
    let a = Peer::over(a, Role::Client, Options::default()).unwrap();
    let b = Peer::over(b, Role::Server, Options::default()).unwrap();
    let (entered_tx, mut entered_rx) = mpsc::unbounded_channel();
    let (cancelled_tx, mut cancelled_rx) = mpsc::unbounded_channel();
    let detach = handle_wire(b.wire(), &path(&["a/b", ""]), move |ctx, _| {
        let entered = entered_tx.clone();
        let cancelled = cancelled_tx.clone();
        async move {
            entered.send(()).unwrap();
            ctx.cancelled().await;
            cancelled.send(()).unwrap();
            Ok(Payload::from_json("null").unwrap())
        }
    })
    .unwrap();
    let ctx = Context::default();
    let wire = a.wire();
    let task_ctx = ctx.clone();
    let task = tokio::spawn(async move {
        call_wire(
            task_ctx,
            wire,
            &path(&["a/b", ""]),
            Payload::from_json("{}").unwrap(),
            Options::default(),
        )
        .await
    });
    tokio::time::timeout(Duration::from_secs(2), entered_rx.recv())
        .await
        .unwrap()
        .unwrap();
    detach();
    ctx.cancel();
    assert_eq!(task.await.unwrap().unwrap_err().code, "cancelled");
    tokio::time::timeout(Duration::from_secs(2), cancelled_rx.recv())
        .await
        .unwrap()
        .unwrap();
    a.close();
    b.close();
}

#[tokio::test]
async fn exact_then_longest_namespace_and_ordered_event_dispatch() {
    let (sender, receiver) = wire_pair(Options::default()).unwrap();
    let (tx, mut rx) = mpsc::unbounded_channel();
    for (p, namespace, label) in [
        (vec![], true, "root"),
        (path(&["a"]), true, "prefix"),
        (path(&["a", "b"]), false, "exact"),
    ] {
        let tx = tx.clone();
        receiver
            .receive(
                &p,
                Receiver {
                    namespace,
                    ..Receiver::new(move |p, m| {
                        tx.send((label, p, m.frame.data.value().unwrap())).unwrap();
                    })
                },
            )
            .unwrap();
    }
    for (p, value) in [
        (path(&["a", "b"]), 1),
        (path(&["a", "b", "c"]), 2),
        (path(&["z"]), 3),
    ] {
        emit_wire(
            Context::default(),
            sender.clone(),
            &p,
            Payload::from_value(&value).unwrap(),
        )
        .unwrap();
    }
    for label in ["exact", "prefix", "root"] {
        assert_eq!(rx.recv().await.unwrap().0, label);
    }
    let mut frame = ProfileFrame::new(ProfileKind::Event);
    frame.data = Payload::from_json("null").unwrap();
    assert!(sender.send(&path(&["z"]), Message::new(frame)).is_ok());
    sender.close(1000, "done").unwrap();
}
