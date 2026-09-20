# @nightseam/live

```sh
npm install @nightseam/live
```

Callable values across one connection of the `nightseam.duplex/1` profile. A
function on one side is exported as a **binding**; the **reference** that names
it travels as an ordinary value inside any payload; the other side **imports**
it and holds a function that calls back across the wire. That is what makes a
callback an argument and an interface a result.

The layer speaks `live.invoke` and `live.release` as ordinary frames of the
profile, so a scope is made over a `DuplexPeer` and needs no tunnel.

```ts
import { DuplexPeer } from '@nightseam/runtime';
import { liveOver, scopeOf } from '@nightseam/live';

const peer = new DuplexPeer();
const scope = liveOver(peer); // before the peer is attached, as a tunnel is made
await peer.connect('wss://example.test/hall');

// One side exports a function and sends what names it.
const supplied = scope.owner().child();
const progress = supplied.export('probe/Report', async (percent) => {
  console.log(percent);
  return null;
});
await peer.call('start', { progress });

// The other side reads it out of the payload and calls it.
const arrived = scope.decode(params.progress);
const received = scope.owner().child();
const report = received.import(arrived, 'probe/Report');
await report(50);

// And ends it when it is done with it.
received.release();
supplied.release();
```

Owners are explicit lifetimes inside a scope. An owner owns the bindings it
creates; an import that reuses an existing attachment is a borrow and does not
extend that binding's lifetime. Releasing an owner also releases its children.
`owner.counts()` reports only its direct allocations; `scope.counts()` counts
all bindings. `scope.release(reference)` still revokes a binding regardless of
owner. Releasing the root leaves the scope open: `scope.owner()` then provides
a fresh root, while the released owner remains ended.

A native reference comes from `export` or `decode` and is associated with that
scope. Importing that object into another scope is refused `reference_foreign`.
Its serialized bytes are ordinary data: `decode` accepts caller-supplied bytes
and associates the receiving scope without proving where they arrived from.
Bytes naming a still-live binding work on the original connection. On a new
connection, old bytes can decode and import, but invocation fails
`reference_unknown` because fresh scope nonces and export lookup do not revive
the old binding. Explicit `forward` gives another connection its own dependent
binding; serialization alone does not.

Release is a barrier: the next invocation is refused `reference_released` and
the ones already dispatched settle and are delivered. Closing the scope, or the
connection carrying it, settles them all. Cancelling an invocation is neither —
it is the profile's own cancellation of that one request, and it releases
nothing.

The surface in both languages is
[docs/runtime/live.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/live.md);
what crosses the wire is
[docs/wire/live.md](https://github.com/Bitspark/nightseam/blob/main/docs/wire/live.md).
