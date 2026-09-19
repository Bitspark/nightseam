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

`TestReleaseCancelAndJobCancelDiffer` runs all three against one job:

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
exchange. The differences between them are the point.

| | ordering | completion | cancellation | consistency | backpressure |
| --- | --- | --- | --- | --- | --- |
| **interface** (`job`) | none; two independent operations | none | its own `cancel`, which the application defines | each call answers from the state at the moment it ran | the peer's `MaxConcurrentHandlers` |
| **stream** (`worker` + `sink`) | one report in flight, each awaited, so 1, 2, 3 … | exactly one `end` — `done`, `failed` or `cancelled` — and no report after it | `Job.cancel()` ends it `cancelled` | — | the sink's answer paces the worker; a sink that does not answer holds it |
| **cell** | watchers told in version order, never a version twice | a watch ends on `unwatch` or with its connection; the cell never ends the observer's sink | — | one writer at a time; `set` takes the lock that mints the version | the cell **awaits** each report, so a slow watcher holds the write that provoked it |
| **topic** | one sequence for everybody, one delivering goroutine per subscriber | a subscription ends on `unsubscribe` or with its connection | — | — | each subscriber has a bounded queue and a subscriber that fills it is **dropped**; the publisher is never its hostage |

The cell and the topic make **opposite** choices about backpressure, on
purpose, and `TestDerivedCell` and `TestATopicDropsRatherThanStalls` hold both.
Neither choice is in the wire, and neither would be implied by a callable type.

The completion race that #202 asks to be specified is specified here, in the
job: a `cancel` arriving while the last report is in flight loses, the job ends
once, and the answer's `stopped` says whether the cancellation is what ended
it. A caller that needs to know reads the answer instead of assuming.

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

## Data needs none of this

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
becomes, freshness has to be *in* it or the check cannot exist. **Answered,
twice over:** a live binding id carries its scope's random nonce, so an id of an
ended connection is in no later scope's table and is refused
`reference_unknown`; and a `Reference` has no public constructor, so there is no
detached token to replay in the first place — it comes from an export or from a
scope's own decode and is refused `reference_foreign` anywhere else. That is the
operator's verdict on
[#202](https://github.com/Bitspark/nightseam/issues/202), and this finding is
the evidence it was decided on.

**4. A carried built-in as a union variant imported an absent package.**
[#210](https://github.com/Bitspark/nightseam/issues/210) traced this to import
collection treating the payload's built-in provenance as a package dependency.
Carried payloads now remain local in both languages, and this checkout uses
`{"variants": {"sink": "duplex.Handle"}}` directly.
`TestReferencesInProductsSumsAndContainers` and its TypeScript cross-wire
counterpart exercise that form; the golden corpus also includes
`duplex.Envelope` as a variant and compiles both renderings.

One further asymmetry is worth writing down even though it does not survive
the callable verdict on
[#201](https://github.com/Bitspark/nightseam/issues/201): the generator
renders a Go binding and a Go client, and for TypeScript a client alone. On
*this* basis that decides direction — a callback a TypeScript peer exports has
to be declared on a family's client side, which is why `sink`'s operations are
there. Under the settled callable kind a live value is a function the live
scope dispatches, not a side a generated `Handler` implements, so either peer
may export one and the asymmetry does not reach the live layer.

## What this proves, and what it does not

It proves that the callback-and-returned-interface behavior of
[#196](https://github.com/Bitspark/nightseam/issues/196) needs no new wire
primitive: peers, tunnels and handles carry all of it, in both languages,
across a real socket, in both directions. It proves that interfaces, streams,
cells and topics are compositions with their own stated contracts rather than
kinds a language needs.

It does not prove that this is the surface v0.5.0 should ship. Every rule in
the table above is bookkeeping a consumer would otherwise write again for each
application, and two of the four findings are repairs the generated import
owes. That is the case for the live tier, not against it — and it is the
evidence [#201](https://github.com/Bitspark/nightseam/issues/201) and
[#202](https://github.com/Bitspark/nightseam/issues/202) asked for before
settling what replaces it.
