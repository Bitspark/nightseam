# C++ frames connection

`Nightseam::duplex` exposes the C++20 `nightseam::duplex::Conn` interface for
whole, ordered text or binary frames, with cancellable send and receive,
explicit close code and reason, and abort. Each frame owns its bytes.

`pipe` creates an in-memory pair with a bounded queue in each direction.
`ws::dial` and `ws::Listener` provide Boost.Beast WebSocket connections; their
options carry the receive byte limit and optional handshake subprotocols.
Both transports enforce receive limits and pace a sender when the receiver
stops consuming.

`Wait` carries a stop token and an optional steady-clock deadline. A cancelled
or expired pipe receive leaves the pipe usable. A cancelled or expired
WebSocket operation may end that connection, matching the transport's abort
semantics. `CloseError` preserves the remote close code and reason.

Build instructions and CMake source consumption are in the
[runtime README](../../runtime/cpp/README.md). A frames-only consumer links
`Nightseam::duplex` and includes `nightseam/duplex/conn.hpp` or
`nightseam/duplex/ws.hpp`.

Shared access types come from `<bitwire/wire.hpp>` in namespace `bitwire`.
CMake exposes the pinned upstream dependency as `Bitwire::wire`. `at` accepts
a send-only Bitwire Wire; `mount` borrows Bitwire Endpoints. `Dispatcher` owns
one endpoint attachment and provides receiving path selections.
