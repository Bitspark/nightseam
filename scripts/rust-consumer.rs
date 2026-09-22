use bitwire::{Payload};
use nightseam::{Options,Peer,Role};
use nightseam_duplex::ws::{Listener, dial};
use serde_json::json;
use std::time::Duration;

#[tokio::main]
async fn main() {
    tokio::time::timeout(Duration::from_secs(10), async {
        let listener = Listener::bind("127.0.0.1:0", 1 << 20, vec![])
            .await
            .unwrap();
        let address = listener.url().unwrap();
        let (client_conn, server_conn) =
            tokio::join!(dial(&address, 1 << 20, &[]), listener.accept(),);
        let client = Peer::over(client_conn.unwrap(), Role::Client, Options::default()).unwrap();
        let server = Peer::over(server_conn.unwrap(), Role::Server, Options::default()).unwrap();
        server
            .handle("echo", |_, payload| async move { Ok(payload) })
            .unwrap();
        let request = json!({"text": "packaged Rust → WebSocket → packaged Rust", "count": 1});
        let result = client
            .call("echo", Payload::from_value(&request).unwrap())
            .result()
            .await
            .unwrap();
        assert_eq!(result.value().unwrap(), request);
        client.close();
        client.wait_closed().await;
        server.wait_closed().await;
        println!("packaged Rust request round trip passed");
    })
    .await
    .expect("packaged Rust request round trip timed out");
}
