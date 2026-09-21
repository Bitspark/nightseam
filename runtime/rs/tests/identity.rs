use nightseam::{Context, DeclarationIdentity, Options, Payload, Peer, Role};
use nightseam_duplex::pipe;

fn identity(path: &str, digest: Option<&str>) -> DeclarationIdentity {
    DeclarationIdentity {
        path: path.into(),
        digest: digest.map(str::to_owned),
    }
}

#[tokio::test]
async fn identity_is_an_ordinary_bounded_exchange_without_closing_the_carrier() {
    let (a, b) = pipe(0);
    let server = Peer::over(a, Role::Server, Options::default()).unwrap();
    let client = Peer::over(b, Role::Client, Options::default()).unwrap();
    let digest = "a".repeat(64);
    let expected = identity("same", Some(&digest));
    server.identity(expected.clone()).unwrap();
    client
        .check_identity(Context::default(), expected)
        .await
        .unwrap();
    client
        .check_identity(Context::default(), identity("same", None))
        .await
        .unwrap();
    let error = client
        .check_identity(Context::default(), identity("other", None))
        .await
        .unwrap_err();
    assert_eq!(error.code, "contract_mismatch");
    server
        .check_identity(Context::default(), identity("absent", None))
        .await
        .unwrap();
    for raw in [
        r#"{"path":"same","digest":""}"#,
        r#"{"path":"same","extra":true}"#,
        r#"{"path":"same","digest":null}"#,
    ] {
        let error = client
            .call("identity.check", Payload::from_json(raw).unwrap())
            .result()
            .await
            .unwrap_err();
        assert_eq!(error.code, "contract_invalid");
    }
    client
        .check_identity(Context::default(), identity("same", None))
        .await
        .unwrap();
    client.close();
    server.close();
}
