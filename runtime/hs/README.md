# Haskell runtime

`nightseam-runtime` implements the four-frame duplex profile natively in
Haskell. It uses `nightseam-duplex` for transport and Wire views. A `Peer`
has a role, bounded queues, a pending-call limit, an active-handler limit,
and request and write deadlines. `PublicError` is the only handler exception
whose code, message and optional data reach the remote caller.

Create both peers with `newPeer`, register methods with `handle`, create a
`CallContext` with `newCallContext`, and invoke `call`. Cancellation through
`cancelContext` settles the caller and informs the remote handler. A handler
that ignores cancellation retains its capacity until it returns. The receiver's
own request deadline answers promptly while keeping that work counted; explicit
remote cancellation waits for the application response. Metadata
is explicit: `contextReceivedMeta` exposes incoming fields, while `contextMeta`
supplies outgoing fields. Received metadata does not automatically accompany reverse calls.
The context carries trace information for child calls.

`peerWire` supplies a Bitwire `Endpoint`, whose `endpointWire` grants send-only
access. `receivedContext` retrieves verified context associated with an incoming
message's return identity, preserved through Bitwire composition. This supplies
the relative-path interface used by future generated
adapters. Declaration identity is checked with `identityHandler` and
`checkIdentity`; a missing identity method is accepted, while malformed or
mismatched identities fail the interpretation.

`Nightseam.Runtime.Validate` validates descriptors and profile frames,
including Unicode scalar strings, generic lexical bindings, exact numeric
bounds and the common pattern dialect. Its tests read the repository's shared
validator, frame, Unicode and document-example tables.

From the repository root:

```sh
stack --stack-yaml conformance/haskell/stack.yaml test
node scripts/haskell-smoke.mjs
```

The install smoke builds source distributions, installs them in an unrelated
temporary consumer, and performs a request over a real WebSocket.
The Cabal versions and local dependency pins move with `scripts/version.mjs`,
and release preparation refuses drift from the repository version.
Haskell enters at tier 4; generator, tunnel, live and observability coverage
are separate lanes toward the tier-2 pilot.
