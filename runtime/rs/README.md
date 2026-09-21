# nightseam

The Rust peer for `nightseam.duplex/1`, built on Tokio and the
[`nightseam-duplex` connection seam](https://github.com/Bitspark/nightseam/tree/main/duplex/rs).
`Peer` calls and serves in both directions over any connection. It provides
correlation, cancellation, bounded requests and event delivery, explicit
payload presence, metadata and trace context.

`Payload::Absent` differs from JSON null. Handlers receive `Context` and
return a payload or `PublicError`. `Call::result` awaits a reply and
`Call::cancel` cancels an outstanding call. The transport is supplied by the
host through `Peer::over`; no application policy belongs in the peer.

The crate is packaged and tested from the repository. It has not been
published to crates.io. See the
[Rust guide](https://github.com/Bitspark/nightseam/blob/main/docs/languages/rust.md)
for installation from a checkout, the current language tier and verification.

Licensed under Apache-2.0; the package includes `LICENSE` and `NOTICE`.
