use serde::{
    Deserialize, Deserializer, Serialize, Serializer,
    de::{MapAccess, Visitor},
};
use serde_json::{Value, value::RawValue};
use std::{
    collections::{BTreeMap, BTreeSet},
    fmt,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Role {
    Client,
    Server,
}
impl Role {
    pub fn name(self) -> &'static str {
        match self {
            Self::Client => "client",
            Self::Server => "server",
        }
    }
    pub(crate) fn prefix(self) -> &'static str {
        match self {
            Self::Client => "c:",
            Self::Server => "s:",
        }
    }
    pub(crate) fn opposite(self) -> Self {
        match self {
            Self::Client => Self::Server,
            Self::Server => Self::Client,
        }
    }
}

/// A wire payload retains its original JSON spelling. Absence is distinct
/// from every JSON value, including null; callers choose each explicitly.
#[derive(Clone, Debug, Default)]
pub enum Payload {
    #[default]
    Absent,
    Present(Box<RawValue>),
}
impl Payload {
    pub fn from_json(raw: &str) -> Result<Self, String> {
        check_unicode(raw.as_bytes())?;
        RawValue::from_string(raw.to_owned())
            .map(Self::Present)
            .map_err(|e| e.to_string())
    }
    pub fn from_value(value: &impl Serialize) -> Result<Self, String> {
        Self::from_json(&serde_json::to_string(value).map_err(|e| e.to_string())?)
    }
    pub fn is_absent(&self) -> bool {
        matches!(self, Self::Absent)
    }
    pub fn raw(&self) -> Option<&str> {
        match self {
            Self::Absent => None,
            Self::Present(raw) => Some(raw.get()),
        }
    }
    pub fn value(&self) -> Result<Value, String> {
        serde_json::from_str(self.raw().ok_or("absent payload has no JSON value")?)
            .map_err(|e| e.to_string())
    }
}
impl Serialize for Payload {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        match self {
            Self::Absent => Err(serde::ser::Error::custom(
                "absent payload has no JSON value",
            )),
            Self::Present(raw) => {
                check_unicode(raw.get().as_bytes()).map_err(serde::ser::Error::custom)?;
                raw.serialize(serializer)
            }
        }
    }
}
impl<'de> Deserialize<'de> for Payload {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        check_unicode(raw.get().as_bytes()).map_err(serde::de::Error::custom)?;
        Ok(Self::Present(raw))
    }
}

/// A public refusal is the only handler error whose details cross the wire.
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PublicError {
    pub code: String,
    pub message: String,
    #[serde(default, skip_serializing_if = "Payload::is_absent")]
    pub data: Payload,
}
impl PublicError {
    pub fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            data: Payload::Absent,
        }
    }
}
impl fmt::Display for PublicError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}
impl std::error::Error for PublicError {}

/// The incoming trace is retained verbatim. Propagation supplies a new span
/// for outgoing calls, while responses and cancellation echo the request.
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Trace {
    #[serde(
        default,
        rename = "traceparent",
        skip_serializing_if = "Option::is_none"
    )]
    pub parent: Option<String>,
    #[serde(
        default,
        rename = "tracestate",
        skip_serializing_if = "Option::is_none"
    )]
    pub state: Option<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub(crate) struct Wire {
    pub version: u8,
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub method: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub event: String,
    #[serde(default, skip_serializing_if = "Payload::is_absent")]
    pub params: Payload,
    #[serde(default, skip_serializing_if = "Payload::is_absent")]
    pub result: Payload,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<PublicError>,
    #[serde(default, skip_serializing_if = "Payload::is_absent")]
    pub data: Payload,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub meta: Option<BTreeMap<String, String>>,
    #[serde(flatten)]
    pub trace: Trace,
}

struct Members(BTreeMap<String, Box<RawValue>>);
impl<'de> Deserialize<'de> for Members {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct Object;
        impl<'de> Visitor<'de> for Object {
            type Value = Members;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("duplex frame object")
            }
            fn visit_map<M: MapAccess<'de>>(self, mut map: M) -> Result<Members, M::Error> {
                let mut members = BTreeMap::new();
                while let Some((key, value)) = map.next_entry::<String, Box<RawValue>>()? {
                    if members.insert(key.clone(), value).is_some() {
                        return Err(serde::de::Error::custom(format!(
                            "duplicate duplex field {key}"
                        )));
                    }
                }
                Ok(Members(members))
            }
        }
        deserializer.deserialize_map(Object)
    }
}

pub(crate) fn decode(raw: &[u8], role: Role) -> Result<Wire, String> {
    check_unicode(raw)?;
    let members = serde_json::from_slice::<Members>(raw)
        .map_err(|e| e.to_string())?
        .0;
    let frame: Wire = serde_json::from_slice(raw).map_err(|e| e.to_string())?;
    let mut allowed: BTreeSet<&str> = ["version", "kind", "traceparent", "tracestate"].into();
    let valid = match frame.kind.as_str() {
        "request" => {
            allowed.extend(["id", "method", "params", "meta"]);
            !frame.method.is_empty()
                && !frame.params.is_absent()
                && valid_id(&frame.id, role.opposite().prefix())
        }
        "response" => {
            allowed.extend(["id", "result", "error"]);
            members.contains_key("result") != members.contains_key("error")
                && (!frame.result.is_absent() || frame.error.is_some())
                && valid_id(&frame.id, role.prefix())
        }
        "event" => {
            allowed.extend(["event", "data", "meta"]);
            !frame.event.is_empty() && !frame.data.is_absent()
        }
        "cancel" => {
            allowed.insert("id");
            valid_id(&frame.id, role.opposite().prefix())
        }
        _ => false,
    };
    if frame.version != 1 || !valid || members.keys().any(|key| !allowed.contains(key.as_str())) {
        return Err("invalid duplex frame shape".into());
    }
    if members.contains_key("traceparent")
        && !frame.trace.parent.as_deref().is_some_and(valid_traceparent)
    {
        return Err("invalid duplex traceparent".into());
    }
    if members.contains_key("meta")
        && frame
            .meta
            .as_ref()
            .is_none_or(|meta| meta.keys().any(|key| key.starts_with("nightseam.")))
    {
        return Err("invalid duplex metadata".into());
    }
    if frame
        .error
        .as_ref()
        .is_some_and(|error| error.code.is_empty() || error.message.is_empty())
    {
        return Err("invalid duplex public error".into());
    }
    Ok(frame)
}

fn valid_id(id: &str, prefix: &str) -> bool {
    id.strip_prefix(prefix).is_some_and(|n| {
        !n.is_empty()
            && n.len() <= 20
            && !n.starts_with('0')
            && n.bytes().all(|b| b.is_ascii_digit())
    })
}
pub(crate) fn valid_traceparent(value: &str) -> bool {
    value.len() == 55
        && value.bytes().enumerate().all(|(i, b)| {
            if [2, 35, 52].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_digit() || (b'a'..=b'f').contains(&b)
            }
        })
}

/// Check the original bytes, including strings a decoder could overwrite.
pub(crate) fn check_unicode(raw: &[u8]) -> Result<(), String> {
    let bad = || "invalid Unicode: expected Unicode scalar strings".to_owned();
    std::str::from_utf8(raw).map_err(|_| bad())?;
    let mut quoted = false;
    let mut i = 0;
    while i < raw.len() {
        if raw[i] == b'"' {
            quoted = !quoted;
        } else if quoted && raw[i] == b'\\' {
            i += 1;
            if raw.get(i) == Some(&b'u') {
                if let Some(unit) = hex_unit(raw, i + 1) {
                    i += 4;
                    if (0xdc00..=0xdfff).contains(&unit) {
                        return Err(bad());
                    }
                    if (0xd800..=0xdbff).contains(&unit) {
                        if raw.get(i + 1) != Some(&b'\\')
                            || raw.get(i + 2) != Some(&b'u')
                            || !hex_unit(raw, i + 3)
                                .is_some_and(|low| (0xdc00..=0xdfff).contains(&low))
                        {
                            return Err(bad());
                        }
                        i += 6;
                    }
                }
            }
        }
        i += 1;
    }
    Ok(())
}
fn hex_unit(raw: &[u8], start: usize) -> Option<u16> {
    let text = std::str::from_utf8(raw.get(start..start + 4)?).ok()?;
    u16::from_str_radix(text, 16).ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};

    #[test]
    fn shared_envelope_table() {
        let table: Value =
            serde_json::from_str(include_str!("../../../conformance/tables/frames.json")).unwrap();
        for row in table["rows"].as_array().unwrap() {
            for role in [Role::Client, Role::Server] {
                if row["to"].as_str() != Some("either") && row["to"].as_str() != Some(role.name()) {
                    continue;
                }
                let result = decode(row["frame"].as_str().unwrap().as_bytes(), role);
                assert_eq!(
                    result.is_ok(),
                    row["valid"].as_bool().unwrap(),
                    "{} / {role:?}: {result:?}",
                    row["name"]
                );
            }
        }
    }

    #[test]
    fn shared_unicode_table_before_lossy_or_duplicate_decoding() {
        let table: Value =
            serde_json::from_str(include_str!("../../../conformance/tables/unicode.json")).unwrap();
        for row in table["rows"].as_array().unwrap() {
            let raw = row["raw"].as_str().unwrap();
            let checked = check_unicode(raw.as_bytes());
            assert_eq!(
                checked.is_ok(),
                row["valid"].as_bool().unwrap(),
                "{}: {checked:?}",
                row["name"]
            );
            if let Err(error) = checked {
                assert_eq!(error, "invalid Unicode: expected Unicode scalar strings");
            }
            let frame = format!(r#"{{"version":1,"kind":"event","event":"probe","data":{raw}}}"#);
            assert_eq!(
                decode(frame.as_bytes(), Role::Server).is_ok(),
                row["valid"].as_bool().unwrap(),
                "{}",
                row["name"]
            );
        }
        assert!(check_unicode(&[b'"', 0xff, b'"']).is_err());
        assert!(check_unicode(&[b'"', 0xed, 0xa0, 0x80, b'"']).is_err());
    }

    #[test]
    fn payload_keeps_absence_and_original_number_spelling() {
        assert!(Payload::default().is_absent());
        let null = Payload::from_json("null").unwrap();
        assert!(!null.is_absent());
        assert_eq!(null.value().unwrap(), json!(null));
        let decoded = decode(
            br#"{"version":1,"kind":"request","id":"c:1","method":"read","params":{"x":1e3}}"#,
            Role::Server,
        )
        .unwrap();
        assert_eq!(decoded.params.raw(), Some(r#"{"x":1e3}"#));
        let encoded = serde_json::to_string(&decoded).unwrap();
        assert!(encoded.contains("1e3"));
        assert!(Payload::from_json(r#""\uD800""#).is_err());
    }
}
