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
| construct an unpublished payload | `scope.ExportValue(build)` → `(json.RawMessage, error)` | `scope.exportValue(build)` → `unknown` |
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

## Native references and serialized bytes

Obtain a valid native `Reference` through `Export` or `Decode`. It records the
scope that created it; passing that native object directly to another scope's
`Import` is refused locally as `reference_foreign` and creates no attachment.

Its serialized form is ordinary data. Go's `MarshalJSON` and TypeScript's
`toJSON` expose the binding and contract, and the public `Decode` / `decode`
accepts caller-supplied data with that shape. Decode associates the receiving
scope; it does not prove that the bytes arrived in an inbound message, that a
binding exists, or that the caller is authorized. Valid bytes may be saved or
handed around out of band and decoded again on the original, still-open
connection while the binding remains live.

Import checks the expected contract and local reference state. For a remote
binding it creates or reuses an attachment without asking the remote scope
whether that binding exists. A reference to this scope's own export resolves
locally and creates no import attachment. Remote invocation looks up the
binding in the exporting scope and checks its
contract. Thus old bytes can decode and import on a new connection, but invoking
them is refused `reference_unknown`, even if the new scope has fresh exports.
Fresh random scope nonces keep counter reuse from naming an unrelated binding.
This is lookup and freshness protection, not a prohibition on presenting tokens
and not authentication.

| supplied value | where it is checked | result |
| --- | --- | --- |
| a native reference from another scope | import, locally | `reference_foreign`; no attachment |
| serialized bytes naming a live binding on the original connection | decode, import, then invocation | the binding remains callable |
| bytes from an ended connection, decoded in a new one | invocation in the new exporting scope | `reference_unknown`; the unsuccessful attachment still counts until released or closed |

The paired `serializedReferenceSameConnection`, `foreignNativeReference`, and
`serializedReferenceNewConnection` regressions in the
[Go shared suite](../../live/go/livetest/conformance.go),
[Go package tests](../../live/go/live_test.go),
[TypeScript shared suite](../../live/ts/src/conformance.ts), and
[TypeScript package tests](../../live/ts/src/live.test.ts) hold these distinctions,
including counts and ordinary RPC on open peers. The
[socket scenario](../../conformance/scenarios/live/serialized-reference-scope.json)
holds the serialized-byte cases across languages.

This corrects the token-prohibition claim in
[#202's resolution](https://github.com/Bitspark/nightseam/issues/202#issuecomment-5745572788),
as tracked by [#261](https://github.com/Bitspark/nightseam/issues/261).
Generated codecs still perform conversion at their decoding boundary: decode
each reference position, import it, and hand the handler a native function.
Reconnection preserves no binding; making a callable reachable through another
connection requires an explicit export there, such as forwarding.

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

## Constructing a payload before publication

`ExportValue` takes `func(*Scope) (json.RawMessage, error)`; `exportValue` takes
`(scope: LiveScope) => unknown`. The callback receives a view of the same scope.
Finish conversion and validation synchronously through that view, including
any parameter-converter closures, and return the complete payload. The callback
must not publish partial values or start asynchronous conversion work.

An error, panic, throw or serialization failure discards only exports allocated
through that view. Prior bindings and independent conversions remain usable.
Nested successful conversions join their enclosing build, so a later failure
can unwind the whole payload. No release event is sent for unpublished bindings.
Go verifies the returned JSON; TypeScript serializes and parses the completed
value into a JSON snapshot. The view retains the original scope identity, and
later conversions through a captured view start fresh builds.

Generated live helpers and operation/callable boundaries use this mechanism.
Generic helpers take arbitrary converters; generic data helpers have no live
runtime dependency. When calling generic helpers directly with live converters,
including helpers for intrinsically live generic types, enclose the whole
conversion and validation in `ExportValue`/`exportValue`, and close every
converter over the callback's scope view. Effects through some other scope
handle are outside that build.

Success commits the exports before the caller publishes the payload. This
construction boundary does not reclaim bindings on a subsequent RPC timeout,
cancellation or error, which cannot prove that the value was never delivered.
It does not define ownership transfer or disposal for published values.

## Observing it

A scope takes no observer of its own: it emits through the observer of the peer
it runs over, as `LiveExported`, `LiveImported`, `LiveReleased` and
`LiveRefused` — in TypeScript `live.exported`, `live.imported`, `live.released`
and `live.refused`, declared into the runtime's `ObserverEvents`. They say which
binding of which contract, and never what it was asked or what it answered.
[The observer](observer.md) has the rule and every event of every layer.
