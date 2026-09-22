# nightseam-duplex

Ordered bidirectional frames for Nightseam on Tokio: an asynchronous
`Connection` interface, bounded in-memory `pipe`, and WebSocket `ws::dial`
and `ws::Listener` adapters using tokio-tungstenite. Frames preserve their
text or binary identity and bytes; connections expose close codes and
reasons, receive limits and backpressure.

This component contains transport mechanics. The
[`nightseam` peer](https://github.com/Bitspark/nightseam/tree/main/runtime/rs)
handles request correlation, cancellation and the duplex profile above it.

The crate is packaged and tested from the repository. It has not been
published to crates.io. See the
[Rust guide](https://github.com/Bitspark/nightseam/blob/main/docs/languages/rust.md)
for installation from a checkout and the packaged consumer smoke.

Licensed under Apache-2.0; the package includes `LICENSE` and `NOTICE`.

Import shared access and profile types from `bitwire`, the dependency named
`bitspark-bitwire` on crates.io (version 0.2.0). `at` accepts send-only access;
`mount` borrows `Arc<dyn bitwire::Endpoint>` children. The runtime's `Dispatcher`
owns one endpoint attachment and supplies receiving selections.
