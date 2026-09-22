use bitwire::{Payload, PublicError};
use nightseam::{Context, Options, Peer, Role};
use nightseam_duplex::pipe;
use serde_json::json;
use std::{sync::Arc, time::Duration};
use tokio::{
    sync::{Mutex, Notify},
    time::timeout,
};

fn payload(value: serde_json::Value) -> Payload {
    Payload::from_value(&value).unwrap()
}
fn pair(options: Options) -> (Peer, Peer) {
    let (a, b) = pipe(1 << 20);
    (
        Peer::over(a, Role::Client, Options::default()).unwrap(),
        Peer::over(b, Role::Server, options).unwrap(),
    )
}

#[tokio::test]
async fn call_reverse_call_null_and_public_error() {
    let (client, server) = pair(Options::default());
    client
        .handle("reverse", |_, value| async move { Ok(value) })
        .unwrap();
    server
        .handle("echo", |_, value| async move { Ok(value) })
        .unwrap();
    server
        .handle("relay", |ctx, value| async move {
            ctx.peer()
                .unwrap()
                .call_with(ctx, "reverse", value)
                .result()
                .await
        })
        .unwrap();
    server
        .handle("deny", |_, _| async {
            Err(PublicError::new("denied", "No access"))
        })
        .unwrap();
    for method in ["echo", "relay"] {
        for value in [json!(null), json!({"text":"hello"}), json!([1, true])] {
            assert_eq!(
                client
                    .call(method, payload(value.clone()))
                    .result()
                    .await
                    .unwrap()
                    .value()
                    .unwrap(),
                value
            );
        }
    }
    assert_eq!(
        client
            .call("deny", Payload::Absent)
            .result()
            .await
            .unwrap_err()
            .code,
        "denied"
    );
    assert_eq!(
        client
            .call("missing", Payload::Absent)
            .result()
            .await
            .unwrap_err()
            .code,
        "method_not_found"
    );
    client.close();
    assert_eq!(
        timeout(Duration::from_secs(1), server.wait_closed())
            .await
            .unwrap()
            .code,
        1000
    );
}

#[tokio::test]
async fn cancellation_signals_work_but_does_not_answer_until_it_returns() {
    let (client, server) = pair(Options {
        max_concurrent_handlers: 1,
        ..Options::default()
    });
    let started = Arc::new(Notify::new());
    let cancelled = Arc::new(Notify::new());
    let release = Arc::new(Notify::new());
    server
        .handle("hold", {
            let started = started.clone();
            let cancelled = cancelled.clone();
            let release = release.clone();
            move |ctx, _| {
                let started = started.clone();
                let cancelled = cancelled.clone();
                let release = release.clone();
                async move {
                    started.notify_one();
                    ctx.cancelled().await;
                    cancelled.notify_one();
                    release.notified().await;
                    Ok(payload(json!("late")))
                }
            }
        })
        .unwrap();
    let call = client.call("hold", Payload::Absent);
    started.notified().await;
    call.cancel();
    assert_eq!(call.result().await.unwrap_err().code, "cancelled");
    cancelled.notified().await;
    assert_eq!(
        client
            .call("hold", Payload::Absent)
            .result()
            .await
            .unwrap_err()
            .code,
        "busy"
    );
    release.notify_one();
    server
        .handle("echo", |_, value| async move { Ok(value) })
        .unwrap();
    // The response releases the slot, and an unrelated call proves the late
    // cancelled response did not close or corrupt the client's correlation.
    let result = timeout(Duration::from_secs(1), async {
        loop {
            match client.call("echo", payload(json!(1))).result().await {
                Err(e) if e.code == "busy" => tokio::task::yield_now().await,
                answer => break answer,
            }
        }
    })
    .await
    .unwrap()
    .unwrap();
    assert_eq!(result.value().unwrap(), json!(1));
}

#[tokio::test]
async fn deadlines_are_local_timeouts_and_outstanding_calls_are_bounded() {
    let (a, b) = pipe(1 << 20);
    let client = Peer::over(
        a,
        Role::Client,
        Options {
            max_pending_requests: 1,
            request_timeout: Duration::from_millis(50),
            ..Options::default()
        },
    )
    .unwrap();
    let server = Peer::over(b, Role::Server, Options::default()).unwrap();
    server
        .handle("wait", |ctx, _| async move {
            ctx.cancelled().await;
            Ok(Payload::Absent)
        })
        .unwrap();
    let first = client.call("wait", Payload::Absent);
    assert_eq!(
        client
            .call("wait", Payload::Absent)
            .result()
            .await
            .unwrap_err()
            .code,
        "busy"
    );
    assert_eq!(first.result().await.unwrap_err().code, "request_timeout");
    server.close();
}

#[tokio::test]
async fn events_are_ordered_and_metadata_is_forwarded_only_explicitly() {
    let (client, server) = pair(Options::default());
    let seen = Arc::new(Mutex::new(Vec::new()));
    let notify = Arc::new(Notify::new());
    server
        .on_event("item", {
            let seen = seen.clone();
            let notify = notify.clone();
            move |ctx, value| {
                let seen = seen.clone();
                let notify = notify.clone();
                async move {
                    seen.lock()
                        .await
                        .push((value.value().unwrap(), ctx.meta().clone()));
                    notify.notify_one();
                }
            }
        })
        .unwrap();
    for n in 0..8 {
        client.emit("item", payload(json!(n))).await.unwrap();
    }
    timeout(Duration::from_secs(1), async {
        loop {
            if seen.lock().await.len() == 8 {
                break;
            }
            notify.notified().await;
        }
    })
    .await
    .unwrap();
    assert_eq!(
        seen.lock()
            .await
            .iter()
            .map(|x| x.0.clone())
            .collect::<Vec<_>>(),
        (0..8).map(|n| json!(n)).collect::<Vec<_>>()
    );
    client
        .handle("metadata", |ctx, _| async move {
            Payload::from_value(ctx.meta()).map_err(|e| PublicError::new("invalid", e))
        })
        .unwrap();
    server
        .handle("relay", |ctx, _| async move {
            assert_eq!(ctx.meta().get("tenant").map(String::as_str), Some("acme"));
            let peer = ctx.peer().unwrap();
            let inherited = peer
                .call_with(ctx.clone(), "metadata", Payload::Absent)
                .result()
                .await?;
            let explicit = peer
                .call_with(
                    ctx.clone().with_meta(ctx.meta().clone()),
                    "metadata",
                    Payload::Absent,
                )
                .result()
                .await?;
            Ok(payload(json!([
                inherited.value().unwrap(),
                explicit.value().unwrap()
            ])))
        })
        .unwrap();
    let ctx = Context::default().with_meta(
        [
            ("tenant".into(), "acme".into()),
            ("nightseam.private".into(), "dropped".into()),
        ]
        .into(),
    );
    assert_eq!(
        client
            .call_with(ctx, "relay", Payload::Absent)
            .result()
            .await
            .unwrap()
            .value()
            .unwrap(),
        json!([{}, {"tenant":"acme"}])
    );
}
