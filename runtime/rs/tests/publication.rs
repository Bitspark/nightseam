use nightseam::{
    Context, Options, Payload, Peer, PublicError, Role, call_wire, handle_wire, wire_pair,
};
use nightseam_duplex::pipe;
use std::{sync::Arc, time::Duration};
use tokio::{sync::Notify, time::timeout};

#[tokio::test]
async fn direct_refusal_is_unpublished_but_admitted_outcomes_never_inherit_proof() {
    for socket in [false, true] {
        let (caller, callee, peers) = if socket {
            let (a, b) = pipe(0);
            let a = Peer::over(a, Role::Client, Options::default()).unwrap();
            let b = Peer::over(b, Role::Server, Options::default()).unwrap();
            (a.wire(), b.wire(), Some((a, b)))
        } else {
            let (a, b) = wire_pair(Options::default()).unwrap();
            (a, b, None)
        };
        let cancelled = Context::default();
        cancelled.cancel();
        let refused = call_wire(
            cancelled,
            caller.clone(),
            &["refused".into()],
            Payload::Absent,
            Options::default(),
        )
        .await
        .unwrap_err();
        assert!(refused.is_unpublished());
        handle_wire(callee.clone(), &["deny".into()], |_, _| async {
            Err(PublicError::new("denied", "application refusal").unpublished())
        })
        .unwrap();
        let denied = call_wire(
            Context::default(),
            caller.clone(),
            &["deny".into()],
            Payload::Absent,
            Options::default(),
        )
        .await
        .unwrap_err();
        assert_eq!(denied.code, "denied");
        assert!(
            !denied.is_unpublished(),
            "a handler's claim must not become caller rollback evidence"
        );
        let started = Arc::new(Notify::new());
        handle_wire(callee.clone(), &["hold".into()], {
            let started = started.clone();
            move |ctx, _| {
                let started = started.clone();
                async move {
                    started.notify_one();
                    ctx.cancelled().await;
                    Ok(Payload::Absent)
                }
            }
        })
        .unwrap();
        let held_caller = caller.clone();
        let held = tokio::spawn(async move {
            call_wire(
                Context::default(),
                held_caller,
                &["hold".into()],
                Payload::Absent,
                Options::default(),
            )
            .await
        });
        timeout(Duration::from_secs(1), started.notified())
            .await
            .unwrap();
        caller.close(1000, "done").unwrap();
        let ended = timeout(Duration::from_secs(1), held)
            .await
            .unwrap()
            .unwrap()
            .unwrap_err();
        assert!(
            !ended.is_unpublished(),
            "an admitted request has an unknown outcome on closure"
        );
        drop(peers);
    }
}

#[tokio::test]
async fn raw_peer_preflight_refusal_does_not_mark_a_remote_error() {
    let (a, b) = pipe(0);
    let caller = Peer::over(a, Role::Client, Options::default()).unwrap();
    let callee = Peer::over(b, Role::Server, Options::default()).unwrap();
    assert!(
        caller
            .call("", Payload::Absent)
            .result()
            .await
            .unwrap_err()
            .is_unpublished()
    );
    callee
        .handle("deny", |_, _| async {
            Err(PublicError::new("denied", "refused").unpublished())
        })
        .unwrap();
    let denied = caller
        .call("deny", Payload::Absent)
        .result()
        .await
        .unwrap_err();
    assert_eq!(denied.code, "denied");
    assert!(!denied.is_unpublished());
    caller.close();
}
