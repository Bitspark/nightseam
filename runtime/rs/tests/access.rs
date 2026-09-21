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
