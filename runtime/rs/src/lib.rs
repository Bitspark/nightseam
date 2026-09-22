//! A peer of nightseam.duplex/1, and validation of its declared payloads.

mod identity;
mod pattern;
mod peer;
mod validate;
mod wire;

pub use identity::{DeclarationIdentity, IDENTITY_METHOD};
pub use peer::{Call, Context, Options, Peer, Subscription};
pub use validate::{Schema, ValidationError};
pub use wire::Role;

mod access;
pub use access::{call_wire, emit_wire, forward_wire, handle_wire, wire_pair};

mod dispatcher;
pub use dispatcher::Dispatcher;
