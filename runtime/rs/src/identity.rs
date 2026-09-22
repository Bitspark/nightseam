use crate::{Context, Peer};
use bitwire::{Payload, PublicError};
use serde::{Deserialize, Serialize};

/// Identity agreement is an ordinary request, independent of transport setup.
pub const IDENTITY_METHOD: &str = "identity.check";

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct DeclarationIdentity {
    pub path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub digest: Option<String>,
}

impl DeclarationIdentity {
    pub fn validate(&self) -> Result<(), PublicError> {
        if self.path.is_empty() {
            return Err(invalid(
                "an identity names a nonempty Unicode declaration path",
            ));
        }
        if self.digest.as_ref().is_some_and(|digest| {
            digest.len() != 64
                || !digest
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        }) {
            return Err(invalid("a declaration digest is lowercase SHA-256 hex"));
        }
        Ok(())
    }

    pub(crate) fn read(payload: &Payload) -> Result<Self, PublicError> {
        let value = payload.value().map_err(|_| {
            invalid("a declaration identity is an object with path and optional digest")
        })?;
        if value
            .get("digest")
            .is_some_and(|digest| !digest.is_string())
        {
            return Err(invalid("a declaration digest is lowercase SHA-256 hex"));
        }
        let identity: Self = serde_json::from_value(value).map_err(|_| {
            invalid("a declaration identity is an object with path and optional digest")
        })?;
        identity.validate()?;
        Ok(identity)
    }

    pub(crate) fn compare(&self, remote: &Self) -> Result<(), PublicError> {
        if self.path != remote.path
            || matches!((&self.digest, &remote.digest), (Some(a), Some(b)) if a != b)
        {
            return Err(PublicError::new(
                "contract_mismatch",
                format!("the declaration identity for {} differs", self.path),
            ));
        }
        Ok(())
    }
}

impl Peer {
    /// Install before exposing the model so no application code runs on mismatch.
    pub fn identity(&self, expected: DeclarationIdentity) -> Result<(), PublicError> {
        expected.validate()?;
        self.handle(IDENTITY_METHOD, move |_, payload| {
            let expected = expected.clone();
            async move {
                expected.compare(&DeclarationIdentity::read(&payload)?)?;
                Payload::from_value(&expected).map_err(invalid)
            }
        })
    }

    /// Only method_not_found represents absent identity. Other refusals retain
    /// their public codes and leave this borrowed carrier usable.
    pub async fn check_identity(
        &self,
        ctx: Context,
        expected: DeclarationIdentity,
    ) -> Result<(), PublicError> {
        expected.validate()?;
        let params = Payload::from_value(&expected).map_err(invalid)?;
        let result = self.call_with(ctx, IDENTITY_METHOD, params).result().await;
        match result {
            Err(error) if error.code == "method_not_found" => Ok(()),
            Err(error) => Err(error),
            Ok(payload) => expected.compare(&DeclarationIdentity::read(&payload)?),
        }
    }
}

fn invalid(message: impl Into<String>) -> PublicError {
    PublicError::new("contract_invalid", message)
}
