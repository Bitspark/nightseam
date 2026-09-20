# The live layer's surface

`live/go` and `@nightseam/live` carry callable values across one connection: a
scope over a peer, bindings exported from it, and references that name them
inside an ordinary payload. This page is the surface in both languages; what
crosses the wire is [the live layer](../wire/live.md).

## Making one, and when

A scope is made over a peer **before that peer reads its first frame**, for the
reason a tunnel is ([the peer](peer.md#when-a-peer-starts-reading)): a peer that
is already reading can refuse the other side's first `live.invoke`
`method_not_found` before the handler is there. In Go that place is
`Options.Prepare`; in TypeScript it is the ordering the peer has anyway — make
the scope, then `connect` or `attach`.

```go
var scope *live.Scope
peer, _, _ := runtime.Dial(ctx, url, runtime.DialOptions{
	Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		scope, err = live.Over(p, live.Options{}) // once per peer, before it reads
		return err
	}},
})
defer peer.Close()
```

```ts
const peer = new DuplexPeer();
const scope = liveOver(peer); // before the peer is attached
await peer.connect(url);
```

Inside a handler, the scope is reached from the peer the handler was given —
which is how generated code converts at the boundary without being handed one:

```go
scope, ok := live.ScopeOf(peer)
```

```ts
const scope = scopeOf(context.peer);
```

## The surface

| | Go | TypeScript |
| --- | --- | --- |
| the scope over a peer | `live.Over(peer, options)` → `*Scope` | `liveOver(peer, options)` |
| find it again | `live.ScopeOf(peer)` → `(*Scope, bool)` | `scopeOf(peer)` |
| export a function, and name it | `scope.Export(contract, invoke)` → `Reference` | `scope.export(contract, invoke)` |
| read a reference out of a payload | `scope.Decode(raw)` → `Reference` | `scope.decode(raw)` |
| attach to one | `scope.Import(reference, contract)` → `Invoke` | `scope.import(reference, contract)` |
| end a binding | `scope.Release(reference)` | `scope.release(reference)` |
| hand one on | `live.Forward(destination, contract, origin)` | `forward(destination, contract, origin)` |
| what it holds | `scope.Counts()` → `Counts` | `scope.counts()` |
| the peer beneath | `scope.Peer()` | `scope.peer` |
| end it | `scope.Close()` | `scope.close()` |

A binding is one function: `Invoke` is
`func(ctx context.Context, request json.RawMessage) (json.RawMessage, error)` in
Go and `(request: unknown, options?: {signal?: AbortSignal}) => Promise<unknown>`
in TypeScript — bytes where Go's codecs work in bytes, values where TypeScript's
do. A `*runtime.PublicError`, or a thrown `DuplexError`, crosses the wire with
its code as it does from any handler.

## A reference is minted, never constructed

`Reference` has no public constructor and no exported state. It comes from an
`Export` or from `Decode` **of the scope it belongs to**, carries that scope out
of sight, and is refused `reference_foreign` anywhere else. So a reference cannot
be persisted, carried out of band and imported again: the only way to move one to
another connection is to forward it, explicitly.

That is the operator's verdict on
[#202](https://github.com/Bitspark/nightseam/issues/202), and it is what settles
stale tokens without an epoch on the wire — there is no token API to present one
to. What a generated codec does instead is convert at the boundary: `Decode` each
reference position as the payload arrives, `Import` it, and hand the handler a
native function.

## The rules a consumer can rely on

- **A reference outlives the call that introduced it.** A callable supplied as an
  argument can be invoked long after that call returned, which is the whole point
  of the layer.
- **One binding is one attachment.** The same binding imported twice gives the
  same function back; two attachments would be two readers competing for one
  reply.
- **A reference of this side's own making, handed back, reaches the function.**
  It does not open a loop through the connection.
- **Exporting the same function twice makes two bindings.** Native identity is
  nobody's guarantee across a wire, and two bindings are two lifetimes — which is
  what separate release needs. A record of callables is a record of references,
  each with its own binding and its own release; there is no record-wide
  lifetime.
- **Release is a barrier, closure is not.** Release refuses the next invocation
  and lets the dispatched ones settle. Closing the scope settles its outgoing,
  incoming and local self-reference calls with `scope_closed`, while the peer
  remains usable for ordinary RPC. Implementations are told to cancel; a body
  that ignores cancellation can continue its effects, but its late result
  cannot replace the caller's closure outcome.
- **Reconnection revives nothing.** The next connection is another scope, and a
  binding of the old one resolves nowhere in it.
- **Forwarding takes no ownership.** `Forward` gives another scope a binding of
  its own over a function this one imported — it is composition, and it is spelled
  out only because the lifetime relationship has to be stated. Releasing the
  forwarded binding leaves the origin as it was; an invocation through a released
  origin fails with the origin's refusal, which is what the destination's caller
  is told.

## Forwarding callable-bearing values

`Forward`/`forward` exports the raw `Invoke` it receives. It forwards the
request and result bytes/values unchanged; it does not recursively translate
references embedded in them between connection scopes. The scalar forwarding
case establishes a lifetime relationship, not arbitrary higher-order
conversion.

For a declared higher-order callable, import with its generated helper in
the origin scope and export the resulting typed function with its generated
helper in the destination scope. Those wrappers convert the declared
callable positions in arguments and results at each boundary. The same
construction applies to a declared record containing callables, using that
record's generated conversion helpers. Opaque JSON is not a declaration of
the references it might contain.

The shared [higher-order forwarding scenario](../../conformance/scenarios/generated/live-higher-order-forwarding.json)
holds this construction over A-B and B-C sockets with either a Go or
TypeScript intermediary. It passes a function into another function,
retains returned functions after the supplying calls finish, and forwards
a record of callables. It checks counts in all four scopes before closing
the connections. Releasing B's destination factory leaves its origin usable;
releasing the origin producer makes the remaining destination wrapper fail
with `reference_released`. Earlier returned functions are separate bindings,
so parent release does not recursively dispose of them.

## Options and bounds

| Go | TypeScript | default | what it bounds |
| --- | --- | --- | --- |
| `MaxExports` | `maxExports` | 1024 | bindings this side may have exported at once |
| `MaxImports` | `maxImports` | 1024 | bindings this side may hold an attachment to |

Invocations in flight are already bounded by the peer's own
`MaxConcurrentHandlers` and `MaxPendingRequests`: an invocation is a request, so
it is paced like one. A refused export or import registers nothing, and
`Counts()` is what a test reads to hold that — a binding nobody released is a
leak, and a suite that only compares payloads never sees one.

## Observing it

A scope takes no observer of its own: it emits through the observer of the peer
it runs over, as `LiveExported`, `LiveImported`, `LiveReleased` and
`LiveRefused` — in TypeScript `live.exported`, `live.imported`, `live.released`
and `live.refused`, declared into the runtime's `ObserverEvents`. They say which
binding of which contract, and never what it was asked or what it answered.
[The observer](observer.md) has the rule and every event of every layer.
