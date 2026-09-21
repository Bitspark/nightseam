#[path = "../../../conformance/rust/src/recorded_wire.rs"]
mod recorded_wire;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn shared_recorded_witness_matches_observations_from_real_routing() {
    let scenario: serde_json::Value = serde_json::from_str(include_str!(
        "../../../conformance/scenarios/peer/recorded-wire-head-and-order.json"
    ))
    .unwrap();
    let observation = recorded_wire::witness(std::time::Duration::from_secs(5))
        .await
        .unwrap();
    assert_eq!(observation, scenario["steps"][0]["expect"]);
}
