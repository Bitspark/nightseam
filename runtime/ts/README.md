# @nightseam/runtime

The peer of the `nightseam.duplex/1` profile for the browser and Node: JSON
frames carrying requests, responses, events and cancellations both ways over
a `FrameConnection` of `@nightseam/duplex`, with no third-party dependency.
Every client Nightseam generates depends on it; it is also usable on its own.

```ts
import { DuplexPeer, DuplexError } from '@nightseam/runtime';

const peer = new DuplexPeer();
peer.handle('session.describe', (_params, context) => {
  if (context.signal.aborted) throw new DuplexError('cancelled', 'Cancelled');
  return { ready: true };
});
peer.onEvent('work.changed', data => console.log(data));
await peer.connect('wss://example.test/session');
const result = await peer.call('work.read', { id: 'work-1' });
await peer.emit('session.ready', { ready: true });
peer.close();
```

Both peers can call methods while serving one. A handler receives
`{ signal, peer, requestId, trace }`; a `dispatch` option is the fallback for
methods with no handler. Register handlers before connecting. Application
code owns authentication and the authorization of incoming methods. A
`webSocketFactory` supplies a socket of the platform's own; `attach(connection)`
takes an externally authenticated socket, or any `FrameConnection` — a
tunnel channel, an in-memory pipe; `role: 'server'` selects server request
ids. A `DuplexError` is safe to send and crosses the wire with its code,
message and data; any other exception a handler throws becomes `internal`.

## The wire

The endpoint selects the profile; no WebSocket subprotocol is negotiated.
Text envelopes carry `version: 1` and a `kind`: `request` has `id`, `method`
and `params`; `response` has `id` and exactly one of `result` or `error`;
`event` has `event` and `data`; `cancel` has `id`. Request ids carry the
initiator's `c:` or `s:` prefix. Errors carry `code`, `message` and optional
`data`. Every kind may carry W3C `traceparent` and `tracestate`. A malformed
envelope or a binary frame closes the connection. Parsing uses `JSON.parse`,
so a duplicate member keeps its last value and number precision beyond
JavaScript's safe integers is lost before the validator sees it; the Go peer
rejects both. The profile is described in full in `docs/profile.md` of the
repository.

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

Defaults are 64 incoming requests being handled, 128 pending calls, 128
queued events or outgoing frames, 1 MiB frames, 30-second request and
connection deadlines, and 10-second output and event-handler deadlines; the
options take positive integers. Cancellation aborts a handler's signal but
cannot interrupt running JavaScript; a cancelled handler keeps its slot until
it settles. Incoming saturation answers `busy`; a stalled output or event
consumer is disconnected. Event listeners run in order and never block the
routing of responses; a listener that throws is reported through `onError`
and processing continues, and one that never settles disconnects at its
deadline.

`emit()` resolves when the frame was accepted and the connection's byte
buffer drained — not on remote receipt, which the browser cannot report. No
request is retried and no connection is reopened; `onClose` observes a
disconnection, and the caller may connect again. Durable acceptance, replay,
subscriptions and deduplication are the application's.

## The validator

`createValidator(description)` builds the wire validator a generated client
uses from the family's wire description; both runtimes' validators are held
to one conformance table. It validates calls, replies, reverse calls and
events alike, and what fills a slot of a bound family by that family's
binding.

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

A layer running over the peer — `@nightseam/tunnel`, `@nightseam/session`, or
one of your own — declares its events into `ObserverEvents` and emits them
through the same observer, so a `switch (event.type)` stays exhaustive over
every layer imported. `docs/observability.md` in the repository has the rule
and every event.

## The connection beneath

The peer never touches a WebSocket directly: `webSocketConnection(socket)`
from `@nightseam/duplex` adapts one to a `FrameConnection`, and `connect(url)`
goes through it. To run the peer over another transport, implement
`FrameConnection` — `state`, `buffered`, `send`, `close`, `listen` — and pass
it to `attach`; the peer sends the next frame only once `buffered` reads
zero, and close codes are the WebSocket registry's numbers on every
transport.

Run `pnpm --filter @nightseam/runtime check` and
`pnpm --filter @nightseam/runtime test` from the repository root.
