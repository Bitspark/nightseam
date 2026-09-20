# What a consumer composes

A reference to something a peer can call is a thing a consumer *can* build, out
of three that Nightseam has anyway: a peer that invokes in both directions, a
tunnel that multiplexes channels over it, and `duplex.Handle` — the one
reference form the language has, carried by every `protocol.json` tier,
`{"channel": N}` on the wire.

Nightseam now also has [the live layer](live.md), which is the thing itself
rather than the materials: a scope over a peer, bindings exported from it, and
references that name them. **This page is not how to use that** — it is the
composition attempt [admitting a concept](../admission.md#3-the-composition-attempt)
requires, kept because what it found is why the layer has the shape it has.
Three of its four findings below are answered by the layer, and the fourth is
somebody else's bug. A consumer writing new code reaches for
[`live/go` and `@nightseam/live`](live.md); a reader asking *why* that exists,
and what it costs to do without it, reads on.

This page is that construction, and it is held by running code rather than
described. `cmd/nightseam/testdata/compositions/` is a checkout of six
families and two languages' programs, rendered by the generator and run over
real sockets by
[`TestComposedLiveReferences`](../../cmd/nightseam/composition_test.go);
every rule below names the case that holds it. Where the construction falls
short, the page says so and names the issue it was handed to, because a
composition proof is only worth what its failures are worth.

It is also the composition attempt that [admitting a
concept](../admission.md#3-the-composition-attempt) requires before a live
binding may be called a primitive. That page's live-layer table carries what
it found.

## The exchange

The whole of it, in the terms of [a family in
tiers](../declaration/families.md):

```json
// api/contracts/worker/protocol.json
"Start":   {"kind": "record", "fields": [
  {"name": "ticket",   "type": "Ticket"},
  {"name": "progress", "type": "duplex.Handle"}]},
"Started": {"kind": "record", "fields": [
  {"name": "job",      "type": "duplex.Handle"},
  {"name": "accepted", "type": "boolean"}]}
```

A caller opens a channel for the `sink` family, serves its own
implementation over it, and sends that channel's id as `progress`. The worker
resolves the id on its own tunnel, holds the typed proxy, opens a channel for
the `job` family, serves a job over it, and answers with *that* id. The caller
resolves it and calls `cancel`. Both directions are ordinary generated clients
over ordinary channels; nothing new crosses the wire.

**Which side implements what is a declaration decision, not a runtime one.** A
family's operations sit on its server side or its client side, and that
decides which generated package implements them: the sink's are the client
side's, so its implementor attaches the generated *client* and its holder
serves the generated *binding* and calls back through `Remote`; the job's are
the server side's, and the two swap. The declaration picks the direction.

## The rules, and who makes them

This historical channel composition counts imported aliases and closes an
attachment when its last alias is released. That is its own application policy,
held by `TestRepeatedImportAndRelease`; it is **not** the production live
runtime's release contract. Production `Release` / `release` invalidates a whole
binding and every existing alias without counting owners. Its one-way event has
no acknowledgment, and neither a record of callables nor a forwarded function
acquires a shared lease. The production
[lifetime table](live.md#the-rules-a-consumer-can-rely-on) states those rules and
their paired evidence.

None of the following is a guarantee something else makes. Each is a few lines
of ordinary application state — a map, a mutex, a counter — in
`testdata/compositions/go/scope_test.go` and its TypeScript twin in
`testdata/compositions/ts/compositions.ts`, written twice because parity is
one suite and not two implementations that agree in prose.

| rule | how it is kept | held by |
| --- | --- | --- |
| A binding is a channel this side opened and serves over; a reference is its id. | `scope.export` opens, serves, records. | every case |
| A reference means nothing off the connection that carried it. | one table per connection, resolved against that connection's tunnel alone. | `TestTwoClientsKeepTheirOwnReferences` |
| A reference outlives the call that introduced it. | the served and attached peers are made under the **connection's** context, never the request's. | `TestAReturnedCallableCallsASuppliedOneLater` |
| One attachment per binding, however often it is imported. | `scope.imported` caches by id and counts aliases. | `TestRepeatedImportAndRelease` |
| The contract is checked at the import. | the channel's `Family` is compared to the family about to be spoken. | `TestWrongContractOwnAndForeignReferences` |
| Release drops an alias; the last one closes the attachment and calls nothing of the application. | `scope.release`, idempotent. | `TestReleaseCancelAndJobCancelDiffer` |
| A failed publication leaves no binding. | the channel is closed before any reference to it exists. | `scope.export` |
| The scope ends with the connection. | `peer.Done()` closes every binding and attachment. | `TestCloseSettlesPendingWork` |

**The reference outliving its call is the one a reader gets wrong first.**
`sinkbinding.Serve(ctx, channel, …)` with a handler's `ctx` makes a peer whose
life is that request's: the call returns, the context ends, and the first
report through the reference fails with a closed connection. The composition
uses `carrier.Peer().Context()` instead, and the comment in `scope.export`
says why.

## Three cancellations, three outcomes

`TestReleaseCancelAndJobCancelDiffer` runs all three against one job in the
channel composition above; its attachment release is that composition's policy:

- **An RPC cancelled** ends that call. No reference is released, and the job
  it was made beside is untouched.
- **A reference released** closes this side's attachment. Nothing of the
  implementation behind it is called — releasing a `Job` does not cancel a
  job — and the sink keeps whatever ending the job itself gives it.
- **`Job.cancel()`** is the application's own operation. It stops the work and
  ends the sink `cancelled`, and it is neither of the above.

A returned callable is **not** a stream, and this is where that matters: `Job`
is a record of two operations and says nothing about how progress arrives,
who ends it, or in what order.

## The four compositions

Each states its own behavior, because none of it follows from the shape of the
exchange. The examples below are application protocols in the channel-based
construction on this page, not a shipped stream/cell/topic library. Their
Go cases run under
[`TestComposedLiveReferences`](../../cmd/nightseam/composition_test.go).
Its [TypeScript program](../../cmd/nightseam/testdata/compositions/ts/compositions.ts)
is a client of the Go worker: it supplies a sink, receives a job, and exercises
reference and cancellation behavior. It does not implement or execute the
derived cell and topic cases. The evidence should be read at that scope.

| Example | Implemented behavior and chosen policy | Evidence and limits |
| --- | --- | --- |
| **Interface** (`job`) | `status` reads job state; `cancel` requests the application's ending and reports whether it won. The two methods have no combined transaction or record-wide atomicity. | [TestSuppliedCallbackAndReturnedCallable and TestReleaseCancelAndJobCancelDiffer](../../cmd/nightseam/testdata/compositions/go/behaviors_test.go), plus the TypeScript worker exchange. These check this job's policy, not a general service contract. |
| **Stream** (`worker` + `sink`) | The worker awaits each report before sending the next, so a blocked sink paces it. Normal completion or accepted job cancellation attempts one `end`, with no later report. There is no coalescing, replay or durable completion acknowledgment. | [TestDerivedInterfaceAndStream](../../cmd/nightseam/testdata/compositions/go/derived_test.go) holds pacing and ordered completion; [TestAReturnedCallableCallsASuppliedOneLater](../../cmd/nightseam/testdata/compositions/go/behaviors_test.go) holds the cancellation/ending agreement. In [the worker implementation](../../cmd/nightseam/testdata/compositions/go/worker_test.go), a lost sink or connection can end work without delivering `end`; even the one ending attempt can fail. The declared `failed` alternative is not proof that this worker delivers it on transport failure. |
| **Cell** | `set` updates value and version under a lock, snapshots watchers, then awaits their reports outside that lock. `watch` returns the current value and a token; `unwatch` releases the attachment without ending the observer's sink. There is no coalescing or replay. | [TestDerivedCell](../../cmd/nightseam/testdata/compositions/go/derived_test.go) tests sequential writes, their version order, reads and unwatch. [The implementation](../../cmd/nightseam/testdata/compositions/go/sinks_test.go) waits for reports but does not serialize delivery across concurrent `set` calls. The fixture does not prove ordered concurrent notifications or atomic write-and-notify behavior. |
| **Topic** | Matching subscribers receive reports through queues of capacity eight, one delivery loop per subscriber. A full queue drops that report and records an overflow flag; publishing does not await the sink. `unsubscribe` ends the subscription and releases its attachment without sending a sink ending. | [TestDerivedTopic and TestATopicDropsRatherThanStalls](../../cmd/nightseam/testdata/compositions/go/derived_test.go) hold sequential fan-out, unsubscribe, publisher progress and the overflow flag. Despite the latter test's name, [Publish](../../cmd/nightseam/testdata/compositions/go/sinks_test.go) does not remove a subscriber on overflow. Sequences are allocated before queue insertion, so concurrent publish ordering, concurrent unsubscribe safety and atomic fan-out are not established by these tests. |

The cell awaits a watcher's answer, whereas the topic drops overflowing
reports. Those are consumer choices, not guarantees of a callable type. A
reusable composition must explicitly specify ordering, completion,
overflow/backpressure, coalescing, subscription and unsubscription races,
cancellation, and the atomicity it offers. A record of functions supplies
the callable surface but none of those choices by itself.

The completion race that #202 asks to be specified is specified here, in the
job: a `cancel` arriving while the last report is in flight loses, the job ends
once, and the answer's `stopped` says whether the cancellation is what ended
it. A caller that needs to know reads the answer instead of assuming.

## Shared scheduling and authority

A shipped live callable supplies a unary invocation: a request, then a result
or refusal. A remote invocation uses the peer's ordinary request path, as
[Go's `remote`/`onInvoke`](../../live/go/live.go) and
[TypeScript's `remote`/`onInvoke`](../../live/ts/src/index.ts) show. All such
calls share that peer's [limits](peer.md#options-and-limits): outstanding
requests, concurrent handlers, frame sizes and queues. Scope export/import
bounds count bindings and attachments; they do not reserve execution capacity
for each binding. A reference returned to its own exporter can invoke locally
without passing through the peer's wire scheduling.

| Mechanism and evidence | What a composition still has to provide |
| --- | --- |
| Peer-wide outstanding-call limits are held by [outstanding-call-limit](../../conformance/scenarios/peer/outstanding-call-limit.json); queue pacing and stalled-connection behavior by [inbound-burst-is-paced](../../conformance/scenarios/peer/inbound-burst-is-paced.json), [inbound-backpressure](../../conformance/scenarios/peer/inbound-backpressure.json) and [outgoing queue backpressure](../../conformance/scenarios/peer/an-outgoing-queue-is-paced-then-ends-the-connection.json). | These peer scenarios do not prove per-binding fairness or isolation. A live binding has no automatic channel credit window, priority, reserved handler slot or independent traffic budget. A composition needing those guarantees must arrange and test them. |
| [Withdrawing an invocation is not releasing](../../conformance/scenarios/live/withdrawing-an-invocation-is-not-releasing.json) holds per-call cancellation separately from binding lifetime. | Cancellation does not prescribe stream completion, unsubscribe, application rollback or the meaning of `Job.cancel()`. Those belong to the composition's contract. |
| The paired live `onInvoke` implementations check the binding, scope state and expected contract before calling the implementation; [wrong-contract and unknown-binding evidence](../../conformance/scenarios/live/a-wrong-contract-and-a-binding-of-no-scope.json) holds those refusals. | Addressing an implementation is not a principal-authorization decision. Principal permissions, delegation/attenuation, audit policy and authority revocation need explicit policy above or alongside the runtime. Binding release can serve that policy but does not define it. |

These distinctions follow the repository's chosen
[scope and irreducibility policy](../admission.md). A composition can be useful
enough to ship without becoming a primitive; the tunnel and live scope are
already shipped compositions. Classification alone does not settle API
ergonomics, operational safety or maintenance value, and does not require
every consumer to reimplement a useful common construction.

## Forwarding

`worker.forward` imports a reference on one connection and re-exports a relay
of it into a second, so another worker can report through it. The two bindings
have their own lifetimes, which is the whole contract:

- the forwarded binding belongs to the second connection; revoking it leaves
  the origin binding alone, and the first worker can still use it
  (`TestForwardingAnImportedReference`);
- the origin going away reaches the relay as a **failed call**, not as silence,
  and the far worker's job settles rather than reporting into nothing
  (`TestForwardingFailsWhenTheOriginGoes`).

Those cases forward scalar calls. The live runtime's `Forward`/`forward`
likewise re-exports a raw `Invoke`; it does not translate references inside
its request or result. Higher-order forwarding requires generated boundary
conversion: import the declared function in the origin scope and export
its typed wrapper in the destination scope.

The shared [generated forwarding scenario](../../conformance/scenarios/generated/live-higher-order-forwarding.json)
proves that construction over two real connections and three logical peers.
A Go or TypeScript intermediary imports and re-exports a `Toolkit` record
whose functions take and return functions. The endpoint invokes retained
results after the supplying RPCs have ended. The scenario also checks
separate scope nonces, all four registries' counts before teardown,
destination release without origin release, and the origin's refusal
reaching a still-exported destination wrapper. Both languages' endpoints use
generated bindings plus a test-only retain route calling the generated
import helper. The mirrored scenario exercises each language as endpoint
and intermediary. This proves generated conversion at declared callable
positions; raw-byte forwarding does not recursively translate embedded
references, and releasing a parent does not automatically dispose of all
functions it returned.

## Data needs none of this

A consumer that wants every binding of one unit of work to end together can
give that work its own peer, or its own tunnel channel carrying a peer, and
close that peer when the work ends. Its live scope and bindings then end with
the connection. This is an application lifetime choice: it also settles calls
in flight and forfeits later use of callbacks retained after uncertain
publication. On a shared connection, an explicit child owner can instead group
the work's fresh bindings for release while ordinary RPC continues. Nightseam
does not infer either policy from a timeout or install a scope-per-work API;
the [publication outcome table](live.md#the-rules-a-consumer-can-rely-on)
states what remains until the consumer makes that choice.

`notes` is a `model.json` and nothing else. It renders a protocol package with
types and a validator, encodes, decodes and enforces its own patterns and
bounds with no peer, no connection, no tunnel and no scope in the program at
all — `TestDataAloneNeedsNoTunnel` and `testdata/compositions/ts/data.ts`,
which is why that one runs on its own with nothing started for it. References
live at the protocol tier because `duplex.Handle` is carried there; the model
tier cannot name one, and the existing tier rule is what stops it.

## Where the basis stops short

Four findings, each reproduced by code in this checkout and handed to the
issue that owns it. None of them is a missing primitive; three are repairs to
things that already exist, and one is a property of the reference form. The
first three are what [the live layer](live.md) was built to answer, and each
says below how it answers them — the composition was right that nothing was
missing from the wire, and right about what was missing above it.

**1. Generated `Open` attaches once per call.** `Open` is `Channel(id)` then
`Attach`, and `Attach` makes a *new* peer — so importing one reference twice
through it leaves two peers reading one channel, taking each other's replies.
[#202](https://github.com/Bitspark/nightseam/issues/202) requires the
opposite. `TestGeneratedOpenAttachesOncePerCall` holds the current behavior so
that the day it changes, this page is what gets rewritten. Until then an
application must keep the table itself, which is what `scope.imported` is.
**Answered:** `Scope.Import` keeps that table, and the same binding imported
twice gives the same function back — held by `live/aliases` in the conformance
suite and by the shared case of the same name in both languages.

**2. Generated `Open` checks no contract.** It never compares the channel's
family to the family it is about to speak, although `Channel.Family` and
`channel.family` are public and an application can. So today's "a wrong
reference reaches an unrelated implementation" is an omission in generated
code rather than an absent guarantee — a distinction
[#199](https://github.com/Bitspark/nightseam/issues/199) §3 asks for
explicitly.

**3. A reference carries no evidence of its scope.** `{"channel": N}` says
nothing about the connection it was minted on. Two callers of one server mint
the *same* number, and a number replayed from one connection onto another is
not refused — it resolves, in the second connection's own table, to the second
connection's own binding. Nothing crosses, because the table is per connection;
but nothing refuses either, and an application that kept one table for every
connection would cross. `TestWrongContractOwnAndForeignReferences` runs exactly
that replay and asserts where it lands. Whatever a live reference's wire form
becomes, freshness has to be *in* it or the check cannot exist. **Answered by
lookup and freshness:** a live binding id carries its exporting scope's random
nonce, so an id from an ended connection does not name a fresh export and its
invocation is refused `reference_unknown`. Public decode accepts serialized
bytes supplied by the caller; those bytes may be imported again on the original
connection while the binding remains live. A foreign native `Reference` object
is a different case, refused locally at import as `reference_foreign`.
The [paired reference tests](live.md#native-references-and-serialized-bytes)
hold this distinction and correct the token-prohibition claim in
[#202](https://github.com/Bitspark/nightseam/issues/202).

**4. A carried built-in as a union variant imported an absent package.**
[#210](https://github.com/Bitspark/nightseam/issues/210) traced this to import
collection treating the payload's built-in provenance as a package dependency.
Carried payloads now remain local in both languages, and this checkout uses
`{"variants": {"sink": "duplex.Handle"}}` directly.
`TestReferencesInProductsSumsAndContainers` and its TypeScript cross-wire
counterpart exercise that form; the golden corpus also includes
`duplex.Envelope` as a variant and compiles both renderings.

When this composition was written, the TypeScript target rendered a client
alone, which is why `sink`'s operations were placed on the family's client
side. Both targets now render a client and a server binding, so either
language can implement either declared side. The composition retains its
original declaration. Under the callable verdict on
[#201](https://github.com/Bitspark/nightseam/issues/201), a live value is a
function dispatched by the live scope, independent of which side a
generated `Handler` implements; either peer may export one.

## What this proves, and what it does not

It proves that the callback-and-returned-interface behavior of
[#196](https://github.com/Bitspark/nightseam/issues/196) needs no new wire
primitive: peers, tunnels and handles carry all of it, in both languages,
across a real socket, in both directions. It demonstrates particular interface,
stream, cell and topic behaviors as compositions with the contracts and limits
above. Their classification under the admission policy does not prove that
every desired behavior follows from a record of functions, or settle whether
a reusable implementation should ship.

It does not prove that this is the surface v0.5.0 should ship. Every rule in
the table above is bookkeeping a consumer would otherwise write again for each
application, and two of the four findings are repairs the generated import
owes. That is the case for the live tier, not against it — and it is the
evidence [#201](https://github.com/Bitspark/nightseam/issues/201) and
[#202](https://github.com/Bitspark/nightseam/issues/202) asked for before
settling what replaces it.
