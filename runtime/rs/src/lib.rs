//! A peer of nightseam.duplex/1, and validation of its declared payloads.

mod peer;
mod wire;

pub use peer::{Call, Context, Options, Peer, Subscription};
pub use wire::{Payload, PublicError, Role, Trace};
