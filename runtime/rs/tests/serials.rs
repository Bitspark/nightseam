use bitwire::Payload;
use nightseam::{Options, Peer, Role};
use nightseam_duplex::{Frame, pipe};
use serde_json::Value;
use std::time::Duration;
use tokio::time::timeout;

#[tokio::test]
async fn shared_serial_table_includes_completed_requests() {
    let table: Value =
        serde_json::from_str(include_str!("../../../conformance/tables/serials.json")).unwrap();
    for row in table["rows"].as_array().unwrap() {
        let (raw, carrier) = pipe(0);
        let peer = Peer::over(carrier, Role::Server, Options::default()).unwrap();
        peer.handle("echo", |_, value| async { Ok(value) }).unwrap();
        let before = row["before"].as_str().unwrap();
        raw.send(Frame::Text(before.as_bytes().to_vec()))
            .await
            .unwrap();
        if serde_json::from_str::<Value>(before).unwrap()["kind"] == "request" {
            timeout(Duration::from_secs(1), raw.receive())
                .await
                .unwrap()
                .unwrap();
        }
        raw.send(Frame::Text(
            row["frame"].as_str().unwrap().as_bytes().to_vec(),
        ))
        .await
        .unwrap();
        if row["valid"] == true {
            let frame = timeout(Duration::from_secs(1), raw.receive())
                .await
                .unwrap()
                .unwrap();
            assert!(matches!(frame, Frame::Text(_)), "{}", row["name"]);
        } else {
            assert_eq!(
                timeout(Duration::from_secs(1), peer.wait_closed())
                    .await
                    .unwrap()
                    .code,
                4011,
                "{}",
                row["name"]
            );
        }
        peer.close();
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn concurrent_calls_publish_increasing_serials() {
    let (raw, carrier) = pipe(0);
    let peer = Peer::over(
        carrier,
        Role::Server,
        Options {
            queue_capacity: 1,
            ..Options::default()
        },
    )
    .unwrap();
    let calls: Vec<_> = (0..64)
        .map(|_| peer.call("probe", Payload::Absent))
        .collect();
    let mut previous = 0;
    for _ in 0..calls.len() {
        let Frame::Text(bytes) = timeout(Duration::from_secs(1), raw.receive())
            .await
            .unwrap()
            .unwrap()
        else {
            panic!("text request expected")
        };
        let frame: Value = serde_json::from_slice(&bytes).unwrap();
        let serial: u64 = frame["id"].as_str().unwrap()[2..].parse().unwrap();
        assert!(serial > previous, "published {serial} after {previous}");
        previous = serial;
    }
    peer.close();
}
