use nightseam_duplex::{decode_path, encode_path};

#[test]
fn paths_are_canonical_utf8_byte_lengths_and_keep_opaque_segments() {
    for path in [
        vec![],
        vec![""],
        vec!["a/b"],
        vec!["a", "b"],
        vec!["é", "😀"],
        vec!["1:a", "0:"],
    ] {
        let path: Vec<String> = path.into_iter().map(str::to_owned).collect();
        assert_eq!(decode_path(&encode_path(&path)).unwrap(), path);
    }
    assert_eq!(encode_path(&["é".into(), "😀".into()]), "2:é4:😀");
    for bad in [
        "01:a",
        "1:é",
        "3:é",
        "-1:x",
        "a",
        "1",
        "1:",
        "18446744073709551616:a",
    ] {
        assert!(decode_path(bad).is_err(), "{bad}");
    }
}
