//! A peer of nightseam.duplex/1, and validation of its declared payloads.

mod pattern;
mod peer;
mod validate;
mod wire;

pub use peer::{Call, Context, Options, Peer, Subscription};
pub use validate::{Schema, ValidationError};
pub use wire::{Payload, PublicError, Role, Trace};
