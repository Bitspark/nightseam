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

The lifetimes below are separate. Named cases refer to the paired
[Go](../../live/go/livetest/conformance.go) and
[TypeScript](../../live/ts/src/conformance.ts) shared suites unless another
source is linked.

| lifetime | what ends, and what remains | evidence |
| --- | --- | --- |
| call | Cancellation withdraws one invocation; its binding remains available. Application effects are not rolled back. | `cancellationIsNotRelease`; [socket case](../../conformance/scenarios/live/withdrawing-an-invocation-is-not-releasing.json) |
| binding | One export names one function and can outlive the call that introduced it. Release prevents later use but lets an already-dispatched implementation finish. | `higherOrder`, `independentSuppliers`, `releaseIsABarrier`; [returned-callable case](../../conformance/scenarios/live/a-returned-callable-reaches-a-supplied-one.json) |
| aliases | Repeated remote imports share one attachment. Releasing that binding invalidates every existing alias; each import does not acquire a separate lease. | `aliases`, `releaseInvalidatesAliases`; [repeated-import case](../../conformance/scenarios/live/one-binding-imported-twice-is-one-attachment.json) |
| record | Its callable members carry individual references. The record has no runtime identity, remote-object equality, shared lease or atomic record-wide revocation. | The per-reference `Export`, `Import` and `Release` APIs in [Go](../../live/go/live.go) and [TypeScript](../../live/ts/src/index.ts); no record-wide lifecycle API |
| connection | Closing its scopes invalidates their bindings and settles their calls. A new connection revives no binding and replays no invocation. | `closeSettles`, the [Go closure cases](../../live/go/livetest/close.go) and TypeScript's `closeImplementation`; `serializedReferenceNewConnection` and its [socket case](../../conformance/scenarios/live/serialized-reference-scope.json) |

**A binding is a function, not a remote object.** Each call to `Export` /
`export` allocates a new binding id, even for the same native function. Those
exports have independent releases; native function identity does not combine
them. This follows directly from the allocation in both runtime
implementations. `independentSuppliers` separately holds that distinct exports
route to their own implementations. A reference to this side's own export,
handed back, reaches its implementation locally; `selfReference` holds that
behavior without a network loop.

**Release invalidates a binding; it does not count owners.** Releasing one alias
invalidates its siblings, rather than decrementing an ownership count until
the last holder leaves. Releasing one member of a record leaves different
bindings untouched, including separately exported returned functions; other
members that alias the released binding are invalidated with it. A record groups
values; it does not supply a disposal operation for that group.

**Release is a local barrier, not a synchronized global revocation point.**
The releasing scope updates its tables and sends one `live.release` event;
the receiver updates its own tables when that event arrives. There is no
acknowledgment. Go discards an event-send error and TypeScript catches it, so a
successful local return does not prove remote receipt. Release permits work
already dispatched to its implementation to finish; a request merely sent may
still lose the race to release before dispatch. It cancels neither that RPC nor
the application's work, and rolls back no effects. Cancelling an invocation is
a separate operation, and an application-defined `Job.cancel()` is separate
again. The [wire contract](../wire/live.md#release-is-a-barrier-closure-is-not)
describes this boundary.

**Scope closure settles calls.** Closing a scope settles its outgoing, incoming
and local self-reference calls with `scope_closed`, while the peer remains
usable for ordinary RPC. Implementations are told to cancel; a body that ignores
cancellation can continue its effects, but its late result cannot replace the
caller's closure outcome. Connection loss ends the scopes carried on that
connection. The paired closure cases above and
[#260](https://github.com/Bitspark/nightseam/issues/260) hold these rules.

**Reconnection revives and replays nothing.** The next connection has new scopes
and needs fresh bindings. An application may retain its own durable identity
or resume cursor, use that data to resume its own protocol, and acquire new
temporary live bindings. Neither the live layer nor the removal of tunnel
`after` forbids such application behavior; neither supplies its storage, replay
or recovery guarantees.

**Forwarding creates a dependent binding and takes no origin ownership.**
`Forward` / `forward` exports an imported invocation function in the destination
scope. Releasing the destination binding leaves the origin usable. Releasing
or losing the origin, or losing the intermediary connection, makes later use
through that route fail; forwarding makes no binding durable. `forwarding` and
the [scalar socket case](../../conformance/scenarios/live/forwarding-gives-the-destination-its-own-lifetime.json)
hold the independent destination release. The generated higher-order proof in
[#263](https://github.com/Bitspark/nightseam/issues/263) is described below.

These runtime facts do not settle how generated plain functions expose
ownership or disposal, or who retains bindings after an uncertain publication.
Those decisions belong to [#257](https://github.com/Bitspark/nightseam/issues/257)
and [#259](https://github.com/Bitspark/nightseam/issues/259).
[#241](https://github.com/Bitspark/nightseam/issues/241) reconciles their landed
resolutions with this guide for the final release.

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
