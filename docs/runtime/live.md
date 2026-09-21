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

Low-level peer handlers can look up the scope on their peer:

```go
scope, ok := live.ScopeOf(peer)
```

```ts
const scope = scopeOf(context.peer);
```

Generated models receive an explicit runtime adapter context instead. Supply
`runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}` in Go
or `{ valueEnvironment: valueEnvironment(scope) }` in TypeScript. This wraps
the scope's owner selection and conversion batches without retaining one
owner for the factory's lifetime. It is not a scope lookup keyed by a Wire;
selecting or mounting that Wire does not change the chosen environment.

## The surface

| | Go | TypeScript |
| --- | --- | --- |
| the scope over a peer | `live.Over(peer, options)` → `*Scope` | `liveOver(peer, options)` |
| find it again | `live.ScopeOf(peer)` → `(*Scope, bool)` | `scopeOf(peer)` |
| the root lifetime | `scope.Owner()` → `*Owner` | `scope.owner()` → `LiveOwner` |
| a nested lifetime | `owner.Child()` → `*Owner` | `owner.child()` → `LiveOwner` |
| its connection scope | `owner.Scope()` → `*Scope` | `owner.scope` |
| export a function, and name it | `owner.Export(contract, digest, invoke)` → `Reference` | `owner.export(contract, digest, invoke)` |
| construct an unpublished payload | `owner.ExportValue(build)` → `(json.RawMessage, error)` | `owner.exportValue(build)` → `unknown` |
| construct and attempt publication | `owner.PublishValue(build, publish)` → `(json.RawMessage, error)` | `owner.publishValue(build, publish)` → the publisher's result |
| import a value as one batch | `owner.ImportValue(build)` → `error` | `owner.importValue(build)` → the callback's result |
| read a reference out of a payload | `scope.Decode(raw)` → `Reference` | `scope.decode(raw)` |
| attach to one | `owner.Import(reference, contract, digest)` → `Invoke` | `owner.import(reference, contract, digest)` |
| end a binding | `scope.Release(reference)` | `scope.release(reference)` |
| end a lifetime and its children | `owner.Release()` | `owner.release()` or `owner[Symbol.dispose]()` |
| hand one on to a destination owner | `live.Forward(destination, contract, digest, origin)` | `forward(destination, contract, digest, origin)` |
| what this owner allocated directly | `owner.Counts()` → `Counts` | `owner.counts()` |
| what the whole scope holds | `scope.Counts()` → `Counts` | `scope.counts()` |
| supply a model's value environment | `live.ValueEnvironment(scope)` | `valueEnvironment(scope)` |
| carry an owner in a generated model call | `live.WithOwner(ctx, owner)` | `context.valueContext` |
| find the model handler's owner | `live.OwnerOf(ctx)` → `(*Owner, bool)` | `context.valueContext` with the live environment |
| carry an owner in a concrete callable invocation | `live.WithOwner(ctx, owner)` | `options.owner` |
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
scope that created it; importing that native object through an owner in another
scope is refused locally as `reference_foreign` and creates no attachment.

Its serialized form is ordinary data. Go's `MarshalJSON` and TypeScript's
`toJSON` expose the binding, contract and optional digest, and the public `Decode` / `decode`
accepts caller-supplied data with that shape. Decode associates the receiving
scope; it does not prove that the bytes arrived in an inbound message, that a
binding exists, or that the caller is authorized. Valid bytes may be saved or
handed around out of band and decoded again on the original, still-open
connection while the binding remains live.

Import checks the expected contract, digest and local reference state. The
generated exporter and importer supply their family's `WireDigest()` /
`wireDigest`; callers of the raw API pass an empty string for an unspecified
revision. `Reference.Digest()` / `reference.digest` retains the received
digest. Two specified digests that differ are `contract_mismatch` before any
attachment is allocated or an implementation is invoked. Reusing an attachment
cannot erase its known digest: a later conflicting digest is refused even if
another import left its expectation unspecified. The
[paired socket scenario](../../conformance/scenarios/live/declaration-digest.json)
holds revision agreement, refusal and unchanged allocation counts.

For a remote
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
| owner | A caller-chosen lifetime owns new exports and attachments, borrows reused attachments, and releases its children before its own bindings. Releasing a borrowing owner leaves the borrowed binding usable; releasing its allocating owner invalidates every alias. | `ownerReleasesWhatItCreated`, `ownerBorrowsAnAlias`, `ownersNest`, `releaseIsIdempotent`, `rootOwnerLeavesTheScopeOpen`; [generated owner case](../../conformance/scenarios/generated/live-owners.json) |
| record | Its callable members carry individual references. The record has no runtime identity, remote-object equality, shared lease or atomic record-wide revocation. | The per-reference `Export`, `Import` and `Release` APIs in [Go](../../live/go/live.go) and [TypeScript](../../live/ts/src/index.ts); no record-wide lifecycle API |
| connection | Closing its scopes invalidates their bindings and settles their calls. A new connection revives no binding and replays no invocation. | `closeSettles`, the [Go closure cases](../../live/go/livetest/close.go) and TypeScript's `closeImplementation`; `serializedReferenceNewConnection` and its [socket case](../../conformance/scenarios/live/serialized-reference-scope.json) |

Publication does not transfer ownership. Bindings stay under the owner used for
conversion until it releases them or its scope closes, except when the complete
payload could not have been published:

| publication outcome | bindings | paired evidence |
| --- | --- | --- |
| conversion fails before the payload is complete | Only fresh allocations from that construction unwind; earlier bindings and borrowed aliases survive. | `provenUnpublishedIsUnwound`, `importValueUnwindsOnlyItsOwn` |
| local send refusal proves the frame never entered the outbound queue | Only that completed publication batch unwinds; no release event is sent for its unpublished exports. | `provenUnpublishedIsUnwound`, `publicationBatchEndsBeforeSend` |
| remote retains a supplied callback, then errors | Retained under the supplier's owner; the remote can still invoke it. | `retainedAfterRemoteError`, `remoteInvokesAfterFailedSupply` |
| timeout, cancellation after dispatch, or a lost reply, while the connection remains open | Retained; failure to receive a reply does not establish non-delivery. | `retainedAfterTimeout`, `retainedAfterCancellation`, `retainedAfterLostReply` |
| a handler returns a function but its reply is lost or suppressed | Retained under the handler's reachable per-invocation owner, even when the caller received no native value. | `handlerOwnerAfterLostReply` |
| an event carries a callback | Retained under the emitter's owner after the send completes. | `eventPublicationRetainsItsOwner` |
| owner releases while a remote alias exists | Revoked binding-wide; the remote alias reports `reference_released`. | `ownerReleaseWhileRemoteAliasExists` |
| scope closes | Every owner and binding in that scope ends. | `scopeClosureEndsRetainedOwners` |

These publication cases live in the paired
[Go](../../live/go/livetest/publication.go) and
[TypeScript](../../live/ts/src/publication.conformance.ts) suites. The
[generated socket scenario](../../conformance/scenarios/generated/live-uncertain-publication.json)
uses native callbacks through methods, events and returned values in both
directions and languages. Repeated failures and explicit owner release return
counts to baseline on the same connection, with bounds smaller than the number
of cycles.

**A binding is a function, not a remote object.** Each call to `Export` /
`export` allocates a new binding id, even for the same native function. Those
exports have independent releases; native function identity does not combine
them. This follows directly from the allocation in both runtime
implementations. `independentSuppliers` separately holds that distinct exports
route to their own implementations. A reference to this side's own export,
handed back, reaches its implementation locally; `selfReference` holds that
behavior without a network loop.

**Release invalidates a binding; it does not count aliases.** Releasing one alias
invalidates its siblings, rather than decrementing an ownership count until
the last holder leaves. Releasing one member of a record leaves different
bindings untouched, including separately exported returned functions; other
members that alias the released binding are invalidated with it. A record groups
values; it does not supply a disposal operation for that group. An owner can
group the allocations made while converting a record, but does not acquire
ownership of a binding merely because one member aliases it.

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
`Forward` / `forward` exports an imported invocation function under the destination
owner. Releasing that owner or the destination binding leaves the origin
usable. Releasing or losing the origin, or losing the intermediary connection, makes later use
through that route fail; forwarding makes no binding durable. `forwarding` and
the [scalar socket case](../../conformance/scenarios/live/forwarding-gives-the-destination-its-own-lifetime.json)
hold the independent destination release. The generated higher-order proof in
[#263](https://github.com/Bitspark/nightseam/issues/263) is described below.

## Choosing a lifetime

An owner is a lifetime the caller supplies; generated values remain plain
functions and records. Start with `scope.Owner().Child()` in Go or
`scope.owner().child()` in TypeScript, use it for the conversions that belong
together, and release it when that lifetime ends. The
[decision](../decisions/an-owner-is-a-lifetime-the-caller-supplies.md) records
why ownership is separate from native value identity.

An export always belongs to the owner that allocated it. An import belongs to
that owner only when it creates a new remote attachment. Importing an already
attached binding borrows it, including when the earlier attachment belongs to
a different owner. Releasing the borrower neither revokes nor prolongs that
attachment. Releasing its allocating owner invalidates every alias. Importing
a local export creates no attachment and takes no ownership of that export.

Owners nest. Release is idempotent, releases children first, and uses the same
binding-wide barrier described above; it does not cancel dispatched work.
There is no reference counting, re-parenting or native-function lookup.
`owner.Counts()` / `owner.counts()` counts only its direct exports and import
attachments, excluding children and borrows. The scope's counts include all
owners. Empty owners hold no binding allocation and are not retained by their
parents until they or a descendant allocate one.

A released owner stays released: acquisition through it fails with
`reference_released`. Releasing the root leaves the connection scope open;
the next `scope.Owner()` / `scope.owner()` supplies a fresh root without
reviving any old owner or binding. `scope.Release(reference)` / `scope.release`
still releases one binding regardless of which owner allocated it. Scope
closure ends every owner with the connection's bindings.

A generated model call or event using the live value environment uses the owner in
`live.WithOwner(ctx, owner)` or TypeScript's `context.valueContext` when it belongs to that
connection, falling back to the scope's root otherwise. This keeps native
proxy forwarding across connections composable: an owner from the forwarding
connection does not select a lifetime on the origin connection. To choose a
narrower lifetime there, supply an owner for the origin connection. A released
owner belonging to the current connection remains selected; it is not replaced
by the root. Foreign native references are still refused by the low-level API.
An imported concrete callable likewise defaults to the connection's current root;
its TypeScript native options retain the explicit `owner` override for values
exchanged by that invocation.
This choice does not transfer ownership of the callable's attachment; a
borrowed function remains usable after its borrowing owner is released.

On receipt, generated operations and events carrying live values, and callable
wrappers, give each invocation a child owner. Go handlers retrieve it with
`live.OwnerOf(ctx)`;
TypeScript model handlers receive `context.valueContext`, whose live environment
value is a `LiveOwner`; concrete callable bodies receive `options.owner`.
Imports and returned exports use that child. A handler may
keep the owner and release it later to revoke its returned functions. Returning
from the RPC does not release it. The generated
[owner scenario](../../conformance/scenarios/generated/live-owners.json)
holds repeated use and release over one bounded, still-open connection,
including a handler's later revocation of a returned callable.

The [publication outcomes above](#the-rules-a-consumer-can-rely-on) also apply
when an invocation fails. A reply's absence does not end its handler owner or
the caller's supplying owner.

## Forwarding callable-bearing values

A checked imported callable can also be presented as a Wire operation at one
opaque `[binding]` path, then selected, mounted and forwarded. This construction
retains the live scope: import still requires the expected contract and an
allocation owner, invocation still resolves the nonce-bearing id, and release
still uses the binding barrier. Routing the path does not perform those checks
or replace that state. The paired [construction evidence](compositions.md#live-access-through-wire)
holds these distinctions. No `ImportWire` or `ExportWire` convenience API is
implied by the construction.

`Forward`/`forward` exports the raw `Invoke` it receives. It forwards the
request and result bytes/values unchanged; it does not recursively translate
references embedded in them between connection scopes. The scalar forwarding
case establishes a lifetime relationship, not arbitrary higher-order
conversion.

For a declared higher-order callable, import with its generated helper in
an owner in the origin scope and export the resulting typed function with its
generated helper under an owner in the destination scope. Those wrappers
convert the declared callable positions in arguments and results at each boundary. The same
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
so releasing that factory binding does not recursively dispose of them.
Releasing an owner instead releases all bindings it owns and its descendants.

## Options and bounds

| Go | TypeScript | default | what it bounds |
| --- | --- | --- | --- |
| `MaxExports` | `maxExports` | 1024 | bindings this side may have exported at once |
| `MaxImports` | `maxImports` | 1024 | bindings this side may hold an attachment to |

Invocations in flight are already bounded by the peer's own
`MaxConcurrentHandlers` and `MaxPendingRequests`: an invocation is a request, so
it is paced like one. A refused export or import registers nothing, and
the scope's `Counts()` is what a test reads to hold that — a binding nobody
released is a leak, and a suite that only compares payloads never sees one.

## Constructing a payload before publication

`owner.ExportValue` takes `func(*Owner) (json.RawMessage, error)`;
`owner.exportValue` takes `(owner: LiveOwner) => unknown`. The callback receives
a batch view of the same owner.
Finish conversion and validation synchronously through that view, including
any parameter-converter closures, and return the complete payload. The callback
must not publish partial values or start asynchronous conversion work.

An error, panic, throw or serialization failure unwinds only allocations made
through that view. Prior bindings and independent conversions remain usable.
Nested successful conversions join their enclosing build, so a later failure
can unwind the whole payload. No release event is sent for unpublished bindings.
Go verifies the returned JSON; TypeScript serializes and parses the completed
value into a JSON snapshot. The view retains the original owner identity, and
later conversions through a captured view start fresh builds.

Generated live helpers and operation/callable boundaries use this mechanism.
Both import and export converters of intrinsically live generic helpers receive
the helper's active owner view as their first argument; use that view for any
nested acquisition.
Generic data helpers have no live runtime dependency. When calling those data
helpers directly with live converters, enclose the whole conversion and
validation in `ExportValue`/`exportValue`, and close every converter over the
callback's owner view. Effects through some other owner handle are outside
that build.

Success commits the exports to the owner. `ExportValue` alone has no later send
feedback. `PublishValue` / `publishValue` additionally takes a publisher callback
and keeps the completed batch's exact allocations until that callback settles.
Go's publisher takes and returns `json.RawMessage` plus an error; TypeScript's
takes the JSON snapshot and returns a promise. The conversion view is already
inactive when publication starts, so a later conversion through a captured view
does not become part of the earlier send attempt.

The runtime's local `UnpublishedError` is positive proof for one send attempt:
argument or serialization rejection, an already-cancelled request, a local
pending-request bound, or refusal before acceptance into the outbound queue.
The live layer also proves a refusal at an already-released local binding or
remote attachment, and an already-cancelled local invocation, before dispatch.
Go preserves the underlying `errors.Is` / `errors.As` identity; TypeScript
preserves the `DuplexError` code, message and data with its original cause.
Adapters can use Go's `runtime.Unpublished(error)` or TypeScript's
`new UnpublishedError(cause)` only where they can establish that their own
payload was neither queued nor dispatched locally.
Generated outgoing live methods, events and callable requests use
`PublishValue` automatically and unwind their batch only on this proof.

Error codes alone are never proof. A remote `busy`, `cancelled` or
`frame_too_large`, a timeout after queuing, or an uncertain write failure retains
the allocations while the scope remains open. A transport failure that ends
the connection also ends its scope. Proof from a nested send cannot cross a
transport or local implementation dispatch boundary as proof about the outer
publication. Arbitrary publisher failures, throws and panics retain their
allocations. The runtime adds no acknowledgment message.

Generated reply conversion has no feedback from the peer's response write, so
it retains returned exports under the handler's child owner even when a reply
is suppressed or cannot be delivered. The handler can retain and release that
owner. Successful allocations otherwise stay under their owner until explicit
release or scope end; publication does not transfer their ownership.

## Importing a value as one batch

`owner.ImportValue` takes `func(*Owner) error`; `owner.importValue` takes a
synchronous callback and returns its result. Use the callback's owner view
throughout the complete import and its nested converters. An error, panic or
throw releases only the fresh attachments and exports that batch created.
Repeated references to an existing attachment are borrows, including an alias
held by another owner, and survive a failed walk. Nested successful batches
join their enclosing batch, so a later outer failure unwinds their fresh
allocations too.

Fresh remote attachments are released with the ordinary `live.release`
notification; unpublished exports are discarded locally. Completed batch
views retain their owner identity but no active allocation history, so a
later conversion starts a fresh batch. `importValueUnwindsOnlyItsOwn` and
`exportValueUnderAnOwner` hold the runtime behavior. Generated live imports
and operation boundaries use it automatically; direct generic data-helper
calls need the surrounding batch shown in the
[generated surface](../declaration/generated.md#generic-boundary-helpers).

## Observing it

A scope takes no observer of its own: it emits through the observer of the peer
it runs over, as `LiveExported`, `LiveImported`, `LiveReleased` and
`LiveRefused` — in TypeScript `live.exported`, `live.imported`, `live.released`
and `live.refused`, declared into the runtime's `ObserverEvents`. They say which
binding of which contract, and never what it was asked or what it answered.
[The observer](observer.md) has the rule and every event of every layer.
