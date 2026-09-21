# Swift runtime

`NightseamRuntime` is the Swift 6 implementation of the core profile over any
`NightseamDuplex.Connection`. Its Swift Package Manager manifest is
[`NightseamRuntime/Package.swift`](NightseamRuntime/Package.swift). It uses Swift 6
language mode with strict concurrency. Linux is tested; Apple deployment targets
start at macOS 13, iOS 16, tvOS 16 and watchOS 9.

Construct a `Peer(connection:role:options:)`, install handlers, and `await start()`
before exposing it. `call(method:params:context:timeoutMilliseconds:meta:)` awaits
a correlated response, and `emit(name:data:context:meta:)` completes on bounded
admission. Raw frame members use `Data?`: `nil` is absent and `Data("null".utf8)`
is explicit null. Call and event helpers default a nil payload to JSON null;
optional fields inside payloads retain their original presence. `Presence<Value>` exposes the same distinction to typed callers.
Public handler errors use `PublicError`; other errors become the profile's generic
internal error. The handler's `RequestContext` carries a cancellation signal,
incoming metadata and trace context. Reverse calls inherit traces; metadata travels
only when explicitly supplied.

`await peer.wire()` prepares and reuses access to the same peer through relative
paths. `wirePair(options:)` supplies bounded local access without a socket or peer.
`callWire`, `emitWire`, `handleWire`, and `forwardWire` work with any Wire;
`NightseamDuplex.at` and `mount` compose paths without allocating a carrier. Paths
are opaque Unicode scalar strings, compared by their UTF-8 bytes. Cancellation
retains an admitted handler's active-work budget until it exits, and the local Wire
reserves cancellation separately from its data queue.

`JSONValue` retains numeric tokens and validates Unicode scalar strings before
decoding. `Descriptor` holds the shared descriptor rules, imported descriptors,
type and family bindings; `validateFrame` holds the profile envelope. Parsed JSON
objects with scalar-distinct keys that a native Swift dictionary would conflate
use `JSONValue.members`; `objectMembers` preserves all their keys.

From the repository root, run `swift test --package-path
runtime/swift/NightseamRuntime`. `node scripts/swift.mjs` runs both Swift package
suites and builds a separate consumer outside the checkout. The conformance
recipe requires `swift` on PATH and fails if it is absent. The core entry promises
tier 4; generation, live, tunnel and observer adapters are separate profiles.
