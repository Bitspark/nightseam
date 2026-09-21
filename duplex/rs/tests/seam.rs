use nightseam_duplex::{Frame, pipe};
use std::time::Duration;
use tokio::time::timeout;

#[tokio::test]
async fn frames_are_whole_ordered_and_bidirectional() {
    let (a, b) = pipe(1024);
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
    a.send(Frame::Text(b"first".to_vec())).await.unwrap();
    a.send(Frame::Text(b"second".to_vec())).await.unwrap();
    a.close(4001, "finished").await.unwrap();
    assert_eq!(b.receive().await.unwrap(), Frame::Text(b"first".to_vec()));
    assert_eq!(b.receive().await.unwrap(), Frame::Text(b"second".to_vec()));
    let ended = b.receive().await.unwrap_err();
    assert_eq!((ended.code, ended.reason.as_str()), (4001, "finished"));
    assert!(a.send(Frame::Binary(vec![])).await.is_err());
    assert!(b.send(Frame::Binary(vec![])).await.is_err());
}

#[tokio::test]
async fn receive_limit_prevents_delivery_and_ends_the_pipe() {
    let (a, b) = pipe(2);
    a.send(Frame::Text(b"big".to_vec())).await.unwrap();
    assert!(b.receive().await.is_err());
    assert_eq!(a.receive().await.unwrap_err().code, 1006);
    assert!(b.send(Frame::Binary(vec![])).await.is_err());
}

#[tokio::test]
async fn pipe_paces_after_eight_frames_and_abort_releases_both_directions() {
    let (a, b) = pipe(1024);
    for _ in 0..8 {
        a.send(Frame::Binary(vec![1])).await.unwrap();
    }
    assert!(
        timeout(Duration::from_millis(20), a.send(Frame::Binary(vec![2])))
            .await
            .is_err()
    );
    assert_eq!(b.receive().await.unwrap(), Frame::Binary(vec![1]));
    a.send(Frame::Binary(vec![3])).await.unwrap();
    let blocked = {
        let a = a.clone();
        tokio::spawn(async move { a.send(Frame::Binary(vec![4])).await })
    };
    a.abort();
    assert!(
        timeout(Duration::from_secs(1), blocked)
            .await
            .unwrap()
            .unwrap()
            .is_err()
    );
    for _ in 0..8 {
        assert!(b.receive().await.is_ok());
    }
    assert_eq!(b.receive().await.unwrap_err().code, 1006);
    assert!(a.receive().await.is_err());
}
