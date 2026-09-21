use serde::{Deserialize, Deserializer, Serialize, Serializer};
use serde_json::{Value, value::RawValue};
use std::fmt;

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
    #[serde(skip)]
    unpublished: bool,
}
impl PublicError {
    pub fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            data: Payload::Absent,
            unpublished: false,
        }
    }

    /// Mark a failure as proof that this operation was refused before admission.
    /// This is a local fact; an error received in a response cannot supply it.
    pub fn unpublished(mut self) -> Self {
        self.unpublished = true;
        self
    }

    /// Whether the failing operation is proven not to have been published.
    /// Error codes and messages alone never establish this fact.
    pub fn is_unpublished(&self) -> bool {
        self.unpublished
    }

    /// Remove downstream proof after this operation has already been admitted.
    /// Forwarding and response boundaries must not transfer another operation's
    /// pre-admission guarantee to their caller.
    pub fn without_unpublished_proof(mut self) -> Self {
        self.unpublished = false;
        self
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

/// Check the original bytes, including strings a decoder could overwrite.
pub fn check_unicode(raw: &[u8]) -> Result<(), String> {
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
