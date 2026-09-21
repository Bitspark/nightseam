# Rust

The Rust core consists of two public Cargo crates:
[`nightseam-duplex`](../../duplex/rs) for ordered frames over bounded pipes
and WebSockets, and [`nightseam`](../../runtime/rs) for the duplex peer.
The peer depends on the connection seam and is independent of the chosen
transport. Both run on Tokio; WebSockets use tokio-tungstenite.

The [language matrix](../../README.md#languages) records the current tier
and profile results. Rust enters at tier 4 when the complete `core` profile
passes against Go in both roles. Its planned tier is 2. Generator, tunnel,
live and observability adapters are later lanes; packaging the core does
not promise those components.

## Use from a checkout

The crates are not published to crates.io in the core lane. An outside
Cargo project can depend on the checked-out components:

```toml
[dependencies]
nightseam = { path = "/path/to/nightseam/runtime/rs" }
nightseam-duplex = { path = "/path/to/nightseam/duplex/rs" }
tokio = { version = "1", features = ["rt-multi-thread", "macros"] }
```

Use `Peer::over(connection, role, options)` with a `pipe` endpoint or a
connection from `ws::dial` or `ws::Listener`. Register a handler with
`Peer::handle`, call with `Peer::call`, and await `Call::result`. A handler
receives cancellation and incoming metadata in its `Context`; metadata is
forwarded only when explicitly supplied. `Payload::Absent` represents a
missing value, while `Payload::from_value` can represent JSON null.

The [packaged consumer](../../scripts/rust-consumer.rs) is a complete
executable example: it listens on loopback, connects a WebSocket client,
registers an echo handler and checks the response before closing both peers.

## Verify and package

From the repository root, with a current stable Rust toolchain and its
`rustfmt` and `clippy` components:

```sh
cargo fmt --all --check
cargo clippy --workspace --all-targets --locked -- -D warnings
cargo test --workspace --locked
node scripts/smoke-rust-packed.mjs
```

The smoke copies `LICENSE` and `NOTICE`, packages each public crate, and
extracts the `.crate` archives into an operating-system temporary directory
outside the checkout. Its new Cargo consumer requests the release versions
and patches them only to those extracted archives. It checks dependency
resolution, compiles the normalized packaged manifests, and performs a real
WebSocket request round trip. It publishes nothing and removes its scratch
directory unless `--keep` is supplied. External dependencies use the Cargo
cache and crates.io as needed.

`scripts/version.mjs` moves the workspace version, local Cargo requirements
and local lockfile entries with the repository version.
`scripts/release-prepare.mjs` refuses version drift. CI, nightly conformance
and release validation install Rust and run the Rust checks and packaged
smoke. Registry publication is outside this lane.
