use bitwire::{Payload, PublicError};

use serde_json::json;

#[test]
fn unpublished_proof_is_explicit_local_and_removable() {
    let mut original = PublicError::new("cancelled", "Request cancelled");
    original.data = Payload::from_json(r#"{"attempt":1e3}"#).unwrap();
    assert!(!original.is_unpublished());

    let proved = original.clone().unpublished();
    assert!(proved.is_unpublished());
    assert!(proved.clone().is_unpublished());
    assert!(proved.clone().unpublished().is_unpublished());
    assert_eq!(proved.to_string(), original.to_string());
    assert_eq!(proved.data.raw(), original.data.raw());

    let admitted = proved.without_unpublished_proof();
    assert!(!admitted.is_unpublished());
    assert_eq!(admitted.code, original.code);
    assert_eq!(admitted.message, original.message);
    assert_eq!(admitted.data.raw(), original.data.raw());
    assert!(!admitted.without_unpublished_proof().is_unpublished());
}

#[test]
fn json_neither_transmits_nor_restores_publication_proof() {
    let mut error = PublicError::new("busy", "Outstanding call limit reached").unpublished();
    error.data = Payload::from_json(r#"{"limit":1}"#).unwrap();
    let encoded = serde_json::to_vec(&error).unwrap();
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&encoded).unwrap(),
        json!({"code":"busy","message":"Outstanding call limit reached","data":{"limit":1}})
    );
    let received: PublicError = serde_json::from_slice(&encoded).unwrap();
    assert!(!received.is_unpublished());
    assert_eq!(received.code, error.code);
    assert_eq!(received.message, error.message);
    assert_eq!(received.data.raw(), error.data.raw());

    let ordinary = serde_json::from_value::<PublicError>(
        json!({"code":"unpublished","message":"unpublished"}),
    )
    .unwrap();
    assert!(!ordinary.is_unpublished());
    assert!(
        serde_json::from_value::<PublicError>(
            json!({"code":"busy","message":"refused","unpublished":true})
        )
        .is_err()
    );
}
