# Duplex WebSocket runtime

`@nightseam/runtime` is the handwritten browser/Node peer for generated
`nightseam.duplex/1` APIs. It has no third-party runtime dependencies and exports
TypeScript source. Every client Nightseam generates depends on it. It
descends from Nightshift's runtime by way of Nighthall.

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

Both peers can call methods while processing an incoming method. Registered
handlers receive `{ signal, peer, requestId }`; a `dispatch` option provides a
fallback dispatcher. Register handlers before connecting. Application code owns
authentication and incoming-method authorization. A `webSocketFactory` supplies
an alternate socket without assuming browser header support. `attach(socket)`
accepts an externally authenticated connection; `role: 'server'` selects server
request identifiers. `DuplexError` explicitly marks errors safe to send; other
handler exceptions become a generic `internal` error.

The endpoint selects the profile; this library requires no WebSocket subprotocol
negotiation. Text envelopes carry `version: 1` and a `kind`: `request` contains
`id`, `method`, and `params`; `response` contains `id` and exactly one of `result`
or `error`; `event` contains `event` and `data`; `cancel` contains `id`. Request
IDs use the initiator's `c:` or `s:` prefix. Errors contain `code`, `message`, and
optional `data`. Detected invalid envelopes and binary frames close the
connection. Parsing currently uses `JSON.parse`, so duplicate object members
keep their last value and number precision can be lost before generated schema
validation. Go rejects duplicate top-level envelope members. Empty public-error
messages are also accepted here but rejected by Go; see the
[interoperability limits](../../../docs/API_GENERATION.md#known-validation-and-interoperability-limits).

Defaults are 64 active incoming handlers, 128 pending calls, 128 queued events or
outgoing messages, 1 MiB frames, 30-second request/connection deadlines, and
10-second output/event-handler deadlines. Options can select positive finite
integer limits. Cancellation aborts incoming signals but cannot forcibly interrupt
application JavaScript; cancelled handlers retain their capacity slot until
settled. Incoming saturation returns `busy`; stalled output/event consumers are
disconnected. Event callbacks are ordered and do not block response routing.

The event deadline applies to asynchronous listeners. A callback that blocks the
JavaScript thread also blocks timers and routing. Listener rejections/exceptions
are reported through `onError` and processing continues; a listener that never
settles disconnects at its deadline. An unencodable or oversized incoming
handler result closes the connection rather than returning a fallback error.

`emit()` resolves when `send()` accepts the frame and the socket's reported byte
buffer drains. Browser APIs provide no delivery acknowledgment, so this does not
establish remote receipt. Timeouts and cancellation leave mutation outcomes
potentially unknown. No request retry or reconnect is automatic. `onClose`
observes disconnections; the caller may explicitly connect again. Application
code owns durable acceptance, replay, subscriptions, and deduplication.

## The connection beneath the peer

The peer never touches a WebSocket directly. It holds a frames duplex
connection: ordered, message-framed, bidirectional, with an explicit close that
carries a code and a reason. The connection knows nothing about JSON, requests,
correlation or events; those stay in the peer. `webSocketConnection(socket)` is
the adapter from a `WebSocketLike` to that connection: `readyState` becomes
`state`, `bufferedAmount` becomes `buffered`, a string message becomes a text
frame, an `ArrayBuffer`, `Uint8Array` or `Blob` becomes a binary frame, and the
close event's code and reason reach the close handler. `connect(url)` and
`attach(socket)` go through it.

To run the peer over another transport, implement `FrameConnection` and pass it
to `attach(connection)`. `state` is `connecting`, `open`, `closing` or `closed`.
`buffered` counts the bytes accepted by `send` and not yet handed to the
transport; the peer sends the next frame only once it reads zero. `send` throws
when the connection is not open. `listen` registers `open`, `frame`, `close` and
`error` handlers and returns a function that detaches them. Close codes are the
WebSocket registry's numbers on every transport (1000 normal, 1008 policy, 1009
too big, 1011 internal, 4000–4999 application), so close semantics travel with
the peer. The peer sends only text frames and refuses an incoming binary frame
with `invalid_message`. A closing or closed connection cannot be attached. The
connection does not retry, reconnect, or acknowledge delivery.

Run `pnpm --filter @nightseam/runtime check` and
`pnpm --filter @nightseam/runtime test` from the repository root.
