# @nightseam/runtime

```sh
npm install @nightseam/runtime
```

The peer of the `nightseam.duplex/1` profile for the browser and Node: JSON
frames carrying requests, responses, events and cancellations both ways over
a `FrameConnection` of `@nightseam/duplex`, with no third-party dependency.
Generated models use its structured Wire helpers; it is also usable on its own.

`peer.wire()` exposes the existing peer's relative origin. `wirePair(options)`
constructs two bounded asynchronous local Wire endpoints without a physical
peer or serialized frame connection. `callWire`, `emitWire` and `registerWire`
share profile request, event and cancellation behavior. `forwardWire(left,
right)` forwards two origins through namespace receivers and returns a detach
function. Selection and mounting come from `@nightseam/duplex` and allocate no
peer. The generated per-side `toWire`/`fromWire` pair converts the same complete
model factory in both directions; the host owns carrier setup and closure.

`AdapterContext` carries runtime options and an optional `valueEnvironment`.
`ValueAdapter<T>` pairs declaration metadata with both conversions and a
`needsContext` flag. `jsonAdapter(binding)` handles ordinary data; acquiring
converters receive their active context from a consumer-supplied environment.
This package needs no live scope for scalar data. See the
[generated surface](https://github.com/Bitspark/nightseam/blob/main/docs/declaration/generated.md).

```ts
import { DuplexPeer, DuplexError } from '@nightseam/runtime';

const peer = new DuplexPeer();
peer.handle('work.describe', (_params, context) => {
  if (context.signal.aborted) throw new DuplexError('cancelled', 'Cancelled');
  return { ready: true };
});
peer.onEvent('work.changed', data => console.log(data));
await peer.connect('wss://example.test/work');
const result = await peer.call('work.read', { id: 'work-1' });
await peer.emit('work.ready', { ready: true });
peer.close();
```

Both peers can call methods while serving one. A handler receives
`{ signal, peer, requestId, trace }`; a `dispatch` option is the fallback for
methods with no handler. **Register before you attach**: a peer reads the
connection from the moment it has one, so `handle`, `onEvent` and anything
built over the peer — a `Tunnel`, which registers the channel operations —
go on before `connect` or `attach`, or the other side's first request can be
answered `method_not_found` by a peer whose handlers are still on their way.
The order is the rule and not an accident of this API; Go reaches it with
`runtime.Options.Prepare` ([docs/runtime/peer.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/peer.md)). A peer that already has a
connection refuses another: `attach` and `connect` reject `already_connected`
rather than listening twice. Application
code owns authentication and the authorization of incoming methods. A
`webSocketFactory` supplies a socket of the platform's own; `attach(connection)`
takes an externally authenticated socket, or any `FrameConnection` — a
tunnel raw connection, an in-memory pipe; `role: 'server'` selects server request
ids. A `DuplexError` is safe to send and crosses the wire with its code,
message and data; any other exception a handler throws becomes `internal`.

Generated models interpret `peer.wire()` through their family's
`prepareFromWire`. Prepare before `connect` or `attach`, await the returned
`complete(options?)` after attachment, then bind the returned model factory
once. The adapter sends a bounded `identity.check` request before exposing
that factory, and holds early model requests and events until it is bound.
Different family paths or differing specified declaration digests refuse
with `contract_mismatch`; only `method_not_found` means the remote endpoint
carries no identity. The combined `fromWire` is for an already active wire
and cannot recover earlier events.

The runtime exposes this mechanism as `prepareIdentity(wire, expected,
options)`, returning `wire`, `check(options?)`, `ready()` and `close()`.
Its preparation owns its identity responder and model registrations.
`requestTimeoutMs` bounds setup through binding, including a factory never
bound; `close()` detaches the interpretation while the host owns the
carrier. Declaration identity conveys no authentication or permission.
The [wire lifecycle](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/wire.md#preparing-an-interpretation)
describes setup and cleanup in both languages.

## The wire

The endpoint selects the profile; a subprotocol is offered only when `subprotocols` names one, and what the handshake selected is `peer.subprotocol`.
Text envelopes carry `version: 1` and a `kind`: `request` has `id`, `method`
and `params`; `response` has `id` and exactly one of `result` or `error`;
`event` has `event` and `data`; `cancel` has `id`. Request ids carry the
initiator's `c:` or `s:` prefix. Errors carry `code`, `message` and optional
`data`. Every kind may carry W3C `traceparent` and `tracestate`. A malformed
envelope or a binary frame closes the connection. The strict frame validator
rejects duplicate envelope members and malformed Unicode before delivery.
Generated payload validators enforce declared numeric constraints. A Wire
path encodes into the existing method or event string; its local return
capability is never serialized. The profile is described in full in
[docs/wire/profile.md](https://github.com/Bitspark/nightseam/blob/main/docs/wire/profile.md).

## Trace context

An incoming request's or event's trace is placed on the `RequestContext`
its handler runs with; a `call` or `emit` given that context carries a child
of it — the same trace id and flags under a span id of its own, the
`tracestate` forwarded verbatim — while one made from no context begins a
trace of its own; a response carries its request's members byte for byte,
and so does a cancel. The default propagator mints W3C ids with
`crypto.getRandomValues` and needs no tracing library; `propagator` in the
options replaces it with an adapter for one.

## Limits

`positiveInteger(value, name, safe = false)` is the shared option validator
used by the peer and the tunnel. It returns a positive integer or
throws `DuplexError` with code `invalid_options`; `safe = true` also requires
an exactly representable integer, as the peer's limits do.

Defaults are 64 incoming requests being handled (`maxConcurrentHandlers`),
128 pending calls (`maxPendingRequests`), 128 queued events or outgoing
frames (`queueCapacity`), 1 MiB frames (`maxFrameBytes`), 30-second request
and connection deadlines (`requestTimeoutMs`, `connectTimeoutMs`), and
10-second output and event-handler deadlines (`writeTimeoutMs`); the options
take positive integers. Each name is the Go runtime's own, transliterated
with a duration's unit spelled into it —
[docs/runtime/peer.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/peer.md)
tables the pairs. Cancellation aborts a handler's signal but cannot interrupt
running JavaScript; a cancelled handler keeps its slot until it settles.
Incoming saturation answers `busy`; a stalled output or event consumer is
disconnected. Event listeners run in order and never block the routing of
responses; a listener that throws is reported through `onError` and
processing continues, and one that never settles disconnects at its deadline.

`emit()` resolves when the frame was accepted for sending, which is queued
for this connection and no more — not on a drain, and not on remote receipt,
which the browser cannot report; the queue's own write deadline continues
behind it and ends a connection that never drains. No
request is retried and no connection is reopened; `onClose` observes a
disconnection, and the caller may connect again. Durable acceptance, replay,
subscriptions and deduplication are the application's.

## The validator

`createValidator(description, digest, imports)` builds the wire validator a generated client
uses from the family's wire description; both runtimes' validators are held
to one conformance table. It validates calls, replies, reverse calls and
events alike, and what fills a slot of a bound family by that family's
binding.

Generated validators also retain their canonical declaration through
`withDeclaration`. `declarationDigest(validator, slots)` identifies a closed
family application, and `typeDeclaration(binding)` retains the supplied
type's reachable declaration content. Required bindings and their declaration
metadata must be present; an open template cannot identify a closed generic
model. The generated `wireDeclaration` and `wireDigest` exports document the
family's template. The
[canonical graph](https://github.com/Bitspark/nightseam/blob/main/docs/declaration/declaration-identity.md)
defines the shared bytes used by Go, TypeScript and browsers.

## Observing it

`PeerOptions.observer` takes one `Observer`, an interface of one method, and
is told ten things about the traffic the peer carries: a connection opened
and closed, a frame sent and received, a request started and ended with its
duration and outcome, an event emitted and delivered, backpressure, and a
handler that threw. Each carries names, ids, sizes, durations, outcomes and
the frame's trace — and never a payload. The peer emits and never aggregates,
chooses no backend, and an observer that throws interrupts no routing.
`consoleObserver()` writes each event as one line, and takes the four console
methods and a clock so that a test captures it with four functions.

Wire call, emit and registration helpers accept `observer` and `family`.
Generated adapters supply their context's observer and declared family; these
model operation events are separate from a physical peer's frame events and
do not change an existing carrier's observer. A local pair does not emit a
second request lifecycle.

A layer running over the peer — `@nightseam/tunnel`, `@nightseam/live`, or
one of your own — declares its events into `ObserverEvents` and emits them
through the same observer, so a `switch (event.type)` stays exhaustive over
every layer imported.
[docs/runtime/observer.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/observer.md) has the
rule and every event.

## The connection beneath

The peer never touches a WebSocket directly: `webSocketConnection(socket)`
from `@nightseam/duplex` adapts one to a `FrameConnection`, and `connect(url)`
goes through it. To run the peer over another transport, implement
`FrameConnection` — `state`, `buffered`, `send`, `close`, `listen` — and pass
it to `attach`; the peer sends the next frame only once `buffered` reads
zero, and close codes are the WebSocket registry's numbers on every
transport.

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
