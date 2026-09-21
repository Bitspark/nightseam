use nightseam::Schema;
use serde_json::{Value, json};
use std::collections::BTreeMap;

fn descriptor(wire: &Value, imports: BTreeMap<String, Schema>) -> Schema {
    Schema::new(&serde_json::to_vec(wire).unwrap(), "", imports).unwrap()
}

fn table_schema(table: &Value) -> (Schema, BTreeMap<String, Schema>) {
    let mut imports = BTreeMap::new();
    // Dependencies are constructed before their importers.
    for name in ["peer", "wide", "implicit"] {
        imports.insert(
            name.into(),
            descriptor(&table["imported"][name], imports.clone()),
        );
    }
    (descriptor(&table["wire"], imports.clone()), imports)
}

#[test]
fn every_shared_validator_case_and_exact_diagnostic() {
    let table: Value =
        serde_json::from_str(include_str!("../../../conformance/tables/validator.json")).unwrap();
    let (schema, imports) = table_schema(&table);
    for (index, row) in table["cases"].as_array().unwrap().iter().enumerate() {
        let mut types = BTreeMap::new();
        let mut families = BTreeMap::new();
        if let Some(slots) = row["slots"].as_object() {
            for (name, slot) in slots {
                if let Some(family) = slot["family"].as_str() {
                    families.insert(name.clone(), imports[family].clone());
                } else {
                    types.insert(name.clone(), slot["type"].clone());
                }
            }
        }
        let actual = schema.bind(types, families).validate_expression_raw(
            &row["expression"],
            &serde_json::to_vec(&row["value"]).unwrap(),
        );
        assert_eq!(
            actual.is_ok(),
            row["valid"].as_bool().unwrap(),
            "case {index}: {row}; got {actual:?}"
        );
        if let Some(message) = row["message"].as_str() {
            assert_eq!(
                actual.unwrap_err().to_string(),
                message,
                "case {index}: {row}"
            );
        }
    }
    for law in table["equivalence"].as_array().unwrap() {
        for value in law["values"].as_array().unwrap() {
            let generic = schema.validate_value(&law["generic"], value);
            let bound = schema.validate_value(&law["bound"], value);
            assert_eq!(
                generic.is_ok(),
                bound.is_ok(),
                "{law}; value {value}: {generic:?} / {bound:?}"
            );
        }
    }
    assert_eq!(
        schema.fields("Payload"),
        vec!["text", "count", "note", "tag", "when"]
    );
    assert!(schema.validate_raw("Status", br#""on" "off""#).is_err());
    assert_eq!(
        schema
            .validate_raw_at("Status", br#""bad""#, "$.body")
            .unwrap_err()
            .to_string(),
        "$.body: expected Status"
    );
}

#[test]
fn every_shared_pattern_and_pattern_value() {
    let table: Value =
        serde_json::from_str(include_str!("../../../conformance/tables/validator.json")).unwrap();
    for row in table["patterns"].as_array().unwrap() {
        let wire = json!({"types":{"Probe":{"kind":"alias","type":{"array":{"nullable":{"kind":"record","fields":[{"name":"tag","type":"string","pattern":row["pattern"]}]}}}}}});
        let actual = Schema::new(&serde_json::to_vec(&wire).unwrap(), "", BTreeMap::new());
        assert_eq!(
            actual.is_ok(),
            row["valid"].as_bool().unwrap(),
            "{row}: {actual:?}"
        );
    }
    for row in table["patternValues"].as_array().unwrap() {
        let wire = json!({"types":{"Probe":{"kind":"record","fields":[{"name":"text","type":"string","required":true,"pattern":row["pattern"]}]}}});
        let actual = descriptor(&wire, BTreeMap::new())
            .validate_value(&json!("Probe"), &json!({"text":row["value"]}));
        assert_eq!(
            actual.is_ok(),
            row["valid"].as_bool().unwrap(),
            "{row}: {actual:?}"
        );
    }
}

#[test]
fn shared_unicode_is_checked_before_decoding_or_duplicate_overwrite() {
    let schema = descriptor(&json!({"types":{}}), BTreeMap::new());
    let table: Value =
        serde_json::from_str(include_str!("../../../conformance/tables/unicode.json")).unwrap();
    for row in table["rows"].as_array().unwrap() {
        let actual = schema.validate_raw("json", row["raw"].as_str().unwrap().as_bytes());
        assert_eq!(
            actual.is_ok(),
            row["valid"].as_bool().unwrap(),
            "{row}: {actual:?}"
        );
        if let Err(error) = actual {
            assert_eq!(
                error.to_string(),
                "invalid Unicode: expected Unicode scalar strings"
            );
        }
    }
    for bytes in [&b"\"\xff\""[..], &b"\"\xed\xa0\x80\""[..]] {
        assert!(schema.validate_raw("json", bytes).is_err());
    }
    assert!(
        Schema::new(
            br#"{"types":{"Bad\uD800":{"kind":"enum","values":[]}}}"#,
            "",
            BTreeMap::new()
        )
        .is_err()
    );
}

#[test]
fn integer_tokens_are_not_rounded_before_validation() {
    let schema = descriptor(&json!({"types":{}}), BTreeMap::new());
    for raw in [
        "9007199254740991",
        "-9007199254740991",
        "90071992547409910e-1",
        "1.000000000000000000",
        "0e9999999999999999999999999999",
    ] {
        assert!(
            schema.validate_raw("integer", raw.as_bytes()).is_ok(),
            "{raw}"
        );
    }
    for raw in [
        "9007199254740992",
        "9007199254740991.1",
        "1.0000000000000001",
        "1e-9999999999999999999999999999",
        "1e400",
    ] {
        assert!(
            schema.validate_raw("integer", raw.as_bytes()).is_err(),
            "{raw}"
        );
    }
}

#[test]
fn recursive_records_alias_cycles_and_scalar_lengths() {
    let schema = descriptor(
        &json!({"types":{
            "Node":{"kind":"record","fields":[{"name":"next","type":{"nullable":"Node"}}]},
            "Loop":{"kind":"alias","type":"Loop"},
            "Parent":{"kind":"record","extends":["Parent"]},
            "Text":{"kind":"record","fields":[{"name":"value","type":"string","length":{"min":1,"max":1}}]}
        }}),
        BTreeMap::new(),
    );
    assert!(
        schema
            .validate_raw("Node", br#"{"next":{"next":null}}"#)
            .is_ok()
    );
    assert_eq!(
        schema.validate_raw("Loop", b"1").unwrap_err().to_string(),
        "$: expected acyclic type expression"
    );
    assert_eq!(
        schema
            .validate_raw("Parent", b"{}")
            .unwrap_err()
            .to_string(),
        "$: expected acyclic inheritance"
    );
    assert!(
        schema
            .validate_value(&json!("Text"), &json!({"value":"😀"}))
            .is_ok()
    );
    assert!(
        schema
            .validate_value(&json!("Text"), &json!({"value":"é"}))
            .is_err()
    );
}

#[test]
fn callable_digest_uses_its_defining_schema_and_explicit_draws_keep_scope() {
    let digest = "a".repeat(64);
    let wire = json!({"types":{"Read":{"kind":"callable","contract":"source/Read"}}});
    let source = Schema::new(
        &serde_json::to_vec(&wire).unwrap(),
        &digest,
        BTreeMap::new(),
    )
    .unwrap();
    let schema = descriptor(
        &json!({"types":{"Text":{"kind":"alias","type":"string"},"Wrapped":{"kind":"record","fields":[{"name":"a","type":"S.A"},{"name":"b","type":"S.B"}]}}}),
        BTreeMap::from([("source".into(), source)]),
    );
    let reference = json!({"binding":"1","contract":"source/Read","digest":"b".repeat(64)});
    let error = schema
        .validate_value(&json!("source.Read"), &reference)
        .unwrap_err();
    assert_eq!(error.code.as_deref(), Some("contract_mismatch"));
    let bound = schema
        .bind(
            BTreeMap::from([("S.A".into(), json!("Text"))]),
            BTreeMap::new(),
        )
        .bind(
            BTreeMap::from([("S.B".into(), json!("integer"))]),
            BTreeMap::new(),
        );
    assert!(bound.validate_raw("Wrapped", br#"{"a":"x","b":1}"#).is_ok());
    assert!(bound.validate_raw("Wrapped", br#"{"a":1,"b":1}"#).is_err());
    assert!(Schema::new(b"{\"types\":{}}", "BAD", BTreeMap::new()).is_err());
}
