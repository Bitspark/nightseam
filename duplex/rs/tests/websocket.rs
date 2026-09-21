use nightseam_duplex::{Frame, SharedConnection, ws};
use std::time::Duration;
use tokio::time::timeout;

async fn pair(
    limit: usize,
    offered: &[&str],
    selected: &[&str],
) -> (SharedConnection, SharedConnection) {
    let listener = ws::Listener::bind(
        "127.0.0.1:0",
        limit,
        selected.iter().map(|s| s.to_string()).collect(),
    )
    .await
    .unwrap();
    let url = listener.url().unwrap();
    let accept = tokio::spawn(async move { listener.accept().await.unwrap() });
    let client = ws::dial(&url, limit, offered).await.unwrap();
    (client, accept.await.unwrap())
}

#[tokio::test]
async fn websocket_preserves_frames_and_close() {
    let (a, b) = pair(1024, &[], &[]).await;
    assert_eq!(a.subprotocol(), "");
    assert_eq!(b.subprotocol(), "");
    for frame in [
        Frame::Text(vec![]),
        Frame::Text("hé😀".as_bytes().to_vec()),
        Frame::Binary((0..=255).collect()),
    ] {
        a.send(frame.clone()).await.unwrap();
        assert_eq!(b.receive().await.unwrap(), frame);
        b.send(frame.clone()).await.unwrap();
        assert_eq!(a.receive().await.unwrap(), frame);
    }
    for i in 0..64 {
        a.send(Frame::Text(format!("frame {i}").into_bytes()))
            .await
            .unwrap();
        assert_eq!(
            b.receive().await.unwrap(),
            Frame::Text(format!("frame {i}").into_bytes())
        );
    }
    a.send(Frame::Text(b"last".to_vec())).await.unwrap();
    timeout(Duration::from_secs(2), a.close(4001, "finished"))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(b.receive().await.unwrap(), Frame::Text(b"last".to_vec()));
    let ended = b.receive().await.unwrap_err();
    assert_eq!((ended.code, ended.reason.as_str()), (4001, "finished"));
    assert!(a.receive().await.is_err());
    assert!(a.send(Frame::Binary(vec![])).await.is_err());
}

#[tokio::test]
async fn selection_uses_server_preference_and_abort_closes_the_socket() {
    let (a, b) = pair(1024, &["one", "two"], &["two", "one"]).await;
    assert_eq!(a.subprotocol(), "two");
    assert_eq!(b.subprotocol(), "two");
    a.abort();
    assert_eq!(
        timeout(Duration::from_secs(2), b.receive())
            .await
            .unwrap()
            .unwrap_err()
            .code,
        1006
    );
}

#[tokio::test]
async fn receive_limit_and_slow_reader_do_not_hide_an_unbounded_queue() {
    let (a, b) = pair(2, &[], &[]).await;
    a.send(Frame::Binary(vec![0; 3])).await.unwrap();
    assert!(
        timeout(Duration::from_secs(2), b.receive())
            .await
            .unwrap()
            .is_err()
    );
    assert!(b.receive().await.is_err());
    let (a, b) = pair(1 << 20, &[], &[]).await;
    let paced = timeout(Duration::from_millis(200), async {
        for _ in 0..400 {
            a.send(Frame::Binary(vec![1; 256 << 10])).await.unwrap();
        }
    })
    .await;
    assert!(
        paced.is_err(),
        "100 MiB accepted while the reader was stopped"
    );
    a.abort();
    b.abort();
}
