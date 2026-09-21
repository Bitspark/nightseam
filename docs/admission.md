# Admitting a concept

What makes a concept Nightseam's — a primitive of the declaration language
or of a runtime — rather than a composition a consumer writes or a concept
of a consumer's own. This page is the test, the four classes a concept ends
in, and the classes of every concept the tree has or has proposed, so that
the next proposal is measured the way the last one was. It is the
contributor's form of [the boundary](goals/boundary.md), beside the boundary
rule in [COLLABORATION.md](../COLLABORATION.md#the-boundary-rule); the wire
has its own instance of it, [the vocabulary test](wire/vocabulary.md#the-test),
which this page generalizes. It was settled on
[#199](https://github.com/Bitspark/nightseam/issues/199) and is recorded as
[a decision](decisions/a-concept-is-admitted-by-composition.md).

Scope plus irreducibility is the admission policy selected for this project.
It classifies a proposal's responsibility; it does not by itself establish
the proposal's ergonomics, operational safety or value to users. A reusable
composition may still ship when it satisfies the boundary, with its behavior
and evidence made explicit. Classifying it as a composition does not require
every consumer to build it again.

The [repository-home decision](decisions/the-reusable-foundation-lives-in-nightseam.md)
admits the selected optional rooted-grant authority profile here. Its
evidence checking and guard preservation are reusable mechanism; consumers
still choose trust, issued authority and resource policy. This extends the
admitted scope explicitly, without admitting a general policy or proof engine.
The revised decision also selects Bitwire for the shared Wire contract once
ready; Nightseam's runtime, declaration model and optional-auth implementation
remain here. [#421](https://github.com/Bitspark/nightseam/issues/421) holds that
contract adoption in 0.6.0. Admission is not a claim that either the dependency
migration or an admitted implementation has shipped.

Three arguments are named here because each was made once and does not
count. **It already exists**: the present inventory may itself be misplaced,
and this page classifies it with no exemption. **It owns no state of the
consumer's**: separating a mechanism from the ownership of its state can make
an integration feasible without making the mechanism Nightseam's. **It is
reusable**: a model can serve every application and still be a domain
model; calling it an authority scope, an admission boundary or an
uncertainty mechanism does not make it a primitive.

## The four classes

A concept, once put to the test, is exactly one of these.

- **A primitive** — a guarantee stated in the terms of the profile or the
  declaration language, the same for every consumer, that the layer beneath
  does not give and no composition from the permitted basis can give. It has
  one responsibility, an observable behavior, and only the identity, scope,
  failure and lifetime rules that responsibility needs.
- **A composition** — a behavior built from the basis with every guarantee
  it needs kept, and each invariant owned by a named layer. Whether a
  composition is **shipped** is a different question, answered by the
  boundary's second dimension — every consumer would write it the same way,
  and it names no concept of theirs — and a shipped composition is still a
  composition: the tunnel is one. *Shipped* is not a class.
- **Domain semantics** — a concept of a consumer's, or choosing a policy:
  who may do what, for how long, in what order of preference. Checking
  evidence against those supplied terms is mechanism. Policy is out of scope
  however generic its name, however many applications share it, and whether or not
  it is in a package of its own.
- **An unresolved candidate** — a concept whose test is not finished: the
  row names the step that has produced no evidence and where that evidence
  is owed. It is a proof obligation, not permission to implement the
  candidate whole.

**Atomic** means irreducible against the basis of the level the concept is
proposed at. It does not mean one method, one type, a mutex-protected
implementation, or an indivisible state transition: an application may need
an atomic transition and compose it from ordinary synchronization and the
primitives beneath.

## The test

Six steps, in order; a design issue that admits a concept answers each in
its *Admission evidence*, and the first step that fails ends the argument.

### 1. Scope

State the level the concept is proposed at — data, RPC, live, the selected
authority profile or the runtime surface — and its purpose in its own terms,
without defining it as what the tree happens to contain. The concept must be
stateable in the terms of the profile and the declaration language and be
the same for every consumer. Scope and irreducibility are separate
questions: a concept can be indivisible in one API and still be a
consumer's.

| level | its purpose | its basis — what a composition at this level may use |
| --- | --- | --- |
| transport | a connection that sends, receives and closes frames | the floor; not Nightseam's to decompose |
| RPC | correlated requests, events, cancellation, refusal, bounds and headers over one connection | the seam, and ordinary application facilities |
| live | values that are implementations to invoke, scoped and released | the peer, the tunnel's channels and handles, the value constructors, the generated packages, and ordinary application facilities |
| data | the shapes of values, stated once and validated everywhere | the constructors already justified, each with its decision |
| optional authority | verify evidence under the selected rooted-grant profile and preserve guards through invocation | reviewed signing and verification dependencies, the public runtime surface, ordinary bounded state and consumer-supplied trust and policy facts |

### 2. The basis

A composition uses the public shipped surface of the level **beneath** the
candidate, plus ordinary application facilities — state, functions, maps,
queues, locks, storage. Two rules bound it from either side:

- **Nothing is grandfathered.** No element is in the basis because it ships
  or has a name; each is itself subject to this test, and the table below
  is where it was put to it. The whole public surface is not the basis,
  since that would make the present inventory the definition of a
  primitive.
- **A composition is a client.** It calls the basis through its surface; it
  does not speak the basis's wire itself. A consumer pairing ids over a raw
  seam has written a peer, not composed one. Forks, private internals and
  re-deriving a lower level from bytes are not permitted, since by that
  argument nothing is irreducible.

### 3. The composition attempt

Describe the observable behavior required — not the API wished for — and
build it from the basis, saying which layer owns each decision and each
invariant. If it holds with its guarantees intact, it is a composition and
the test is over: a useful pattern, repeated code and a missing convenience
do not change its class. That two shipped parts do not coordinate by
themselves proves that automatic coordination is absent, nothing more.

### 4. The obstruction

If the composition fails, say what it loses: a guarantee of that level —
identity, scope, ordering, correlation, cancellation, lifetime, refusal —
the scenario in which it is lost, and why the basis cannot preserve it. The
argument is written; it need not be executed to be weighed, though a
decision may require an executed construction of itself, as
[#202](https://github.com/Bitspark/nightseam/issues/202) did — and that one
repaid it, since what the construction found is why the live layer is not the
shape the argument alone would have given it. What is not
an obstruction is routed instead of argued:

| what was found | what it is | where it goes |
| --- | --- | --- |
| more code, or the same code in every consumer | a composition; perhaps a shipped one | a lane, or nothing |
| a worse API than the primitive would give | ergonomics | a lane, or nothing |
| two parts do not coordinate unless told to | a composition | the consumer's loop |
| it would be slower composed | an optimization | its own discussion, after the class is settled |
| an existing primitive does not keep its promise | a defect | *Something is wrong* |
| the capability exists and is not written down | a documentation gap | a docs lane |
| the capability exists and is not exposed | an exposure | the smallest change to the existing primitive |

### 5. The smallest missing primitive

Derive the candidate from the obstruction and nothing else: one
independently meaningful responsibility at that level, explicit observable
behavior, and only the identity, scope, failure and lifetime rules that
responsibility needs. Then decompose it again. Exposing or repairing an
existing primitive is preferred to a new one; a candidate that bundles two
responsibilities is two candidates, each shown necessary.

### 6. The alternatives

Show that consumers keep their choices — authority, routing, lifecycle —
without fighting a model implicit in the primitive: a callback for
consumer-owned state does not show this by itself if the mechanism around it
still names a role or an approval. State the alternatives considered and
what each costs. An optional package, a new name or a configurable policy
waives no step; examples may compose a domain concept without making it a
concept of the tree's.

## Where the answers live

A proposal states the six answers in the *Design* form's *Admission
evidence*; the verdict is copied onto the issue verbatim; a verdict that
settles a class rewrites this page's table and, where it gives a reason the
state pages will cite, [a decision page](decisions/README.md). A lane cut
from it names the issue as its provenance. Nothing is admitted from a
comment, a pull request or an example.

## The concepts, classified

Every concept the tree has had or has been offered, in the class the test
gives it. A row with a verdict outstanding says which step is open and where
its evidence is owed; nothing is pre-approved because it sounds lower-level.
The identities the live layer must keep apart are named where they
occur: a **declaration reference** names a type; an **entity key**
identifies a consumer's datum; a **live binding** addresses an exported
implementation within a scope; a **correlation id** pairs a request with its
response. A fifth, **declaration identity**, pairs a nominal path with its
generated digest. Sharing a representation makes none of them another.

### The session layer that v0.5.0 removes

Removed by [#203](https://github.com/Bitspark/nightseam/issues/203) on the
direction recorded in [#196](https://github.com/Bitspark/nightseam/issues/196)
and [#200](https://github.com/Bitspark/nightseam/issues/200). Each row is
the class the test gives the concept; the removal follows from the class
and did not decide it.

| concept | class | the argument |
| --- | --- | --- |
| controller, participant and observer roles | domain semantics | a rule about who may do what: a consumer's fact, applied by the consumer in its handlers. Composition: a role held in the consumer's state, a refusal returned by the handler. |
| `decides` — the deciding operations | domain semantics | classifies a family's operations by one consumer's authority model; a second consumer classifies them otherwise. |
| `asks` and attention — the question raised to the holder | domain semantics | routing a request the machine makes to whoever a consumer says holds control is a routing policy. Composition: the machine's request arrives through the peer; the consumer forwards it, by its own table, as a request of its own. |
| the control holder, handover and `session.control` | domain semantics, over a composition | the token is a variable of the consumer's; its change is an ordinary event of the family. Nothing here is a guarantee the peer lacks. |
| `conversation` and origin stamping | domain semantics | an identity of a consumer's, stamped by the consumer. |
| the log, replay and `session.cursor`; `Tunnel.Open`'s `after` cursor | composition | the consumer sees every frame as the endpoint that sends and receives it or the forwarder that passes it on; a store of them in one order, a replay from a sequence and a resume cursor are a consumer's protocol over ordinary operations ([the log is bound at its head](decisions/the-log-is-bound-at-its-head.md) records the composition's own binding rule and stays as history). `after` existed only to serve that log and goes with it. |
| the one-up, many-down relay: fan-out, id remapping, forwarding | composition | fan-out is a loop over peers; remapping is a correlation map; forwarding is a call made in turn. Each invariant — one answer to the one that asked, an event to every attached — is the consumer's loop's. [The relay mints its own ids](decisions/the-relay-mints-its-own-ids.md) records the composition's own id rule and stays as history. Re-demonstrated as a live composition by [#205](https://github.com/Bitspark/nightseam/issues/205). |
| the governed registry — `Bind`, `Attach`, `Control`, `Attention` | domain semantics, over compositions | bookkeeping of the rows above. Its shape carried the roles; renaming it would have kept them. |
| `not_controlling` and the session's refusals | domain semantics | refusals of a policy; the profile's own refusals ([`busy`](decisions/busy-is-a-refusal-not-a-failure.md), [codes not prose](decisions/refusals-are-codes-not-prose.md)) are unaffected. |
| running over any connection of the seam | composition | [the decision](decisions/the-session-runs-over-any-connection-of-the-seam.md) that a channel is a connection of the seam is not the session's: it is the tunnel's row below, and holds. |

[#139](https://github.com/Bitspark/nightseam/issues/139)'s verdict — (b),
compose the relay under caller-owned control, ordering and lifetime — and
[#167](https://github.com/Bitspark/nightseam/issues/167)'s scoping of it are
preserved as recorded; both are superseded by #196 and #200, which say so
on each. Their seven acceptance cases remain a consumer's integration
scenarios, not axioms of this table. The session's decision pages are
marked superseded in place by #203 and stay in [the record](decisions/README.md).

### What stays beneath it

Kept because each passes the test at its level, not because it was there.

| concept | class | the argument |
| --- | --- | --- |
| the seam — a connection that sends, receives, closes | primitive, transport | the floor: the level beneath it is not Nightseam's. |
| the envelope: kind, `id`, `method`, correlation | primitive, RPC | the peer acts on each member ([decision](decisions/envelope-members-are-what-the-peer-acts-on.md)); a client of the seam cannot tell two concurrent answers apart without speaking a wire of its own, which is not composition. |
| request and response, event | primitive, RPC | kinds of the envelope: a request answered exactly once by correlation, above; an event delivered in the order it was sent, which a client of the seam could not promise across two of its own frames. |
| cancel | primitive, RPC | withdraws a request by the id the peer minted, cancels the handler's context and answers `cancelled` once. A consumer's stop request cannot name that id and is itself a request, accepted after the one it means to withdraw: it is the application's `Job.cancel()`, a different thing with a different contract ([a deadline is not a cancel](decisions/a-deadline-is-not-a-cancel.md)). |
| coded refusals, `busy`, the bounds | primitive, RPC | the peer refuses before any handler runs; a composition would have already accepted the frame ([busy](decisions/busy-is-a-refusal-not-a-failure.md), [queues](decisions/queues-are-paced-for-one-deadline.md)). |
| trace context; the `meta` header | primitive, RPC | a composition puts a fact about the call into every family's payloads, where the validator holds it as a value of the family's; the peer propagates the one and delivers the other beside the payload, validated by form alone ([header, not member](decisions/meta-is-a-header-not-a-member.md)). |
| the observer | primitive, runtime surface | no public surface reveals a write the peer makes except the peer telling of it ([at the write](decisions/the-observer-is-told-at-the-write.md), [never a payload](decisions/an-observer-never-sees-a-payload.md)). |
| the tunnel: channels, ids by parity, credit, close | composition, shipped | ordinary requests and events of the profile in a reserved prefix ([vocabulary](wire/vocabulary.md#a-layers-own-vocabulary)), which a client of the peer could send; shipped because every consumer would, identically, and it names no concept of theirs. Its prepared channel is a Wire; its raw Connection is the transport seam for a host that supplies preparation itself. |
| `duplex.Handle` | data | a record that addresses a channel within its carrying connection. It states no contract and has no lifetime of its own: it is not a live binding. |

### Relative-path access

[#289](https://github.com/Bitspark/nightseam/issues/289)'s construction in
[#321](https://github.com/Bitspark/nightseam/issues/321) exposes the existing
profile guarantees at a shared access boundary. The
[decision](decisions/the-session-runs-over-any-connection-of-the-seam.md)
records the retained live responsibilities as well as what composes.

| concept | class | the argument |
| --- | --- | --- |
| Wire carrying profile frames | primitive, RPC surface | an exposure of existing correlation, cancellation, refusal and bounds, not a second request stack. A return capability and received context remain local; the physical peer continues to own its ids and the four frame kinds. |
| `at`, selecting a relative origin | composition, shipped | prefix a segment array and translate delivered paths back to the selected origin. No peer, channel or queue is allocated, including on first use. Opaque strings need no namespace or authority interpretation. |
| `mount`, routing among child Wires | composition, shipped | choose a child by one segment, preserving the same frame and return capability. The mount owns registrations, borrows children and does not close them when it ends. |
| forwarding a Wire | composition, shipped | register a namespace in each direction and pass messages through the other's Send. Existing roots own admission and correlation. Crossing live scopes still uses generated value converters; raw forwarding does not rewrite hidden references. |
| the head and follow of a recorded Wire | composition, shipped | [record/follow](runtime/record.md) stores admitted opaque messages in order and atomically registers a subscriber with its replay boundary. A bounded append worker and separate replay workers isolate slow storage and subscribers. Consumers supply storage; existing reference scopes and lifetimes are preserved. The generated family event helpers remain a separate delivery under #291. |
| a live binding presented at `[binding]` | composition, demonstrated | checked import supplies an invocation at one opaque path segment. The nonce, expected contract, active owner and release ledger remain live responsibilities. Wire.Close terminates carrier work; owner release preserves already admitted results and revokes later invocation, so one cannot replace the other. |

### The data level

| concept | class | the argument |
| --- | --- | --- |
| the value constructors: record, enum, alias, union, containers, nullable, literal, parameters and `apply` | primitives, data | each is a shape the others cannot state, and each has its decision — [one reference form](decisions/one-reference-form.md), [adjacently tagged](decisions/a-union-is-adjacently-tagged.md), [nullness](decisions/nullness-is-a-fact-of-a-value.md), [named by where it sits](decisions/a-shape-is-named-by-where-it-sits.md), [one parameter mechanism](decisions/one-parameter-mechanism-of-two-sorts.md). |
| `entity` and `{"ref": …}` | composition, shipped | a record with a named `key`, and a reference that is the key's type with a stated target: a convenience with a defined expansion into the constructors, validated as data. It requires no registry and is never invoked; it is an entity key, not a live binding. |
| a tier as a built-in family | composition, shipped | the tier rule and the import mechanism, applied to the profile's own declarations ([decision](decisions/a-tier-is-a-built-in-family.md)); a third tier file is the same rule applied once more, not a new mechanism. |

### The live layer that v0.5.0 adds

The replacement was put to the same test before it shipped, and these are the
classes it came back with. Both verdicts are given —
[#201](https://github.com/Bitspark/nightseam/issues/201)'s callable grammar and
[#202](https://github.com/Bitspark/nightseam/issues/202)'s binding identity,
scope, lifetime and forwarding — and the composition from the existing
mechanisms was built and run rather than argued: it is
[what a consumer composes](runtime/compositions.md), six families over real
sockets, whose findings are why the layer has the shape it has. The rows below
are written in the mechanisms that shipped ([the live layer on the
wire](wire/live.md), [its surface](runtime/live.md)); a composition that
shipped is still a composition.

| concept | class | the argument |
| --- | --- | --- |
| a callable — a declared kind with a request and a result | primitive, declaration language, live level | composition attempted: a `Handle` in a record field and a convention naming the family it serves. It loses two guarantees of the level: **refusal** — a validator cannot refuse a reference whose implementation has the wrong contract, since the type states none — and the native **projection**, since a generator cannot render a function value from a record. The smallest primitive is the type constructor alone: a nominal contract, a request, a result, errors; no wire kind, no lifetime of its own. Admitted by [#201's verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544): a fifth kind, named, never inline, and recorded as [a callable is a declared kind](decisions/a-callable-is-a-declared-kind.md). |
| a named interface — a record of callables | composition | a record; each member its own binding, no shared identity or lifetime unless a contract says so (#201). |
| `live.json` and the live tier | composition | the tier rule applied once more: sides of the same shape as `protocol.json`, adding operations; a lower tier cannot name a live type by the existing upward-reference rule. |
| a live binding — export and import | composition, shipped | composition argued in #202 and **run** before anything shipped: one channel per exported binding, a peer served on it, the handle passed as data with its contract, in both languages across a real socket ([what a consumer composes](runtime/compositions.md)). It works, which is why no wire kind was added. What ships is *not* that construction: a channel per callable imposes a tunnel on every peer that might merely **receive** a live value, puts a `channel.open` round trip inside the encode path of every call carrying one, and has nowhere to put the nominal contract — the only member of `channel.open` that could carry `probe/Report` is `family`, which would stop meaning a family. A callable is one unary function, and the smallest existing mechanism that invokes one is the peer's own request; so the layer speaks `live.invoke` and `live.release` under a reserved `live.` prefix, which is the tunnel's own answer to the same question one step down ([the live layer](wire/live.md)). The composition's three findings above it — repeated import, contract checking, scope evidence — are what the layer provides and the reason it exists. Lifetime settled by [#202's verdict](https://github.com/Bitspark/nightseam/issues/202#issuecomment-5745200987): connection-bounded scope and release a barrier. The token-prohibition claim is corrected by the [serialized-reference contract](runtime/live.md#native-references-and-serialized-bytes): caller-supplied bytes can be decoded again. |
| scope — identity bounded by one connection | composition, shipped | a scope is the live layer over one peer: what this side exported over that connection, what it imported over it, and nothing that outlives it. It is bookkeeping over a lifetime the peer already has, plus the one thing the bookkeeping mints — a random nonce inside every binding id, which is what makes a reference of one connection resolve nowhere on the next instead of resolving to whatever now holds that position. Reconnection makes a new scope; nothing is revived and nothing is replayed. The stale-token obligation #202 set is held by nonce freshness and export lookup at invocation. Decode accepts caller-supplied bytes and associates a native reference with its receiving scope; it establishes no message provenance. A foreign native object fails import locally, while stale bytes can import and then fail invocation. Durable references that outlive a connection remain a consumer's persistence — domain semantics, not admitted ([#202](https://github.com/Bitspark/nightseam/issues/202) option b). |
| release, and aliases | composition, shipped | `live.release`, an event either way under the layer's own prefix: the sender has already released, the receiver releases and says nothing back, and it is idempotent and reaches every alias of the binding at once. Separate lifetimes are separate bindings. [#202's verdict](https://github.com/Bitspark/nightseam/issues/202#issuecomment-5745200987) settled the race it left open — release is a **barrier**, refusing the next invocation while the ones already dispatched settle and are delivered, where closing the scope is the harder stop and settles every invocation in flight at once. Neither rolls back an effect already had, and neither is the consumer's `Job.cancel()`, which is an ordinary callable this layer has never heard of: cancelling an invocation and releasing a binding stay two things because they are two different frames. |
| forwarding into another scope | composition, shipped | `Forward(destination, contract, digest, origin)` gives another scope a binding of its own over a function this one imported — an export whose implementation is an import, which a consumer could write itself. It ships spelled out only because the lifetime relationship has to be **stated**: forwarding takes no ownership, releasing the forwarded binding leaves the origin as it was, and an invocation through a released origin fails with the origin's refusal, which is what the destination's caller is told. Serialized bytes alone do not create an export on another connection; forwarding explicitly creates a dependent binding there. |
| the scope's bookkeeping and its owners — what #200 called the registry | composition, shipped | a `Scope` retains connection identity, `Decode`, raw-reference `Release`, aggregate `Counts` and bounds. Its root `Owner` and nested children supply caller-chosen lifetimes: `Export` owns a new binding, `Import` owns a fresh attachment and borrows a reused one, and owner release revokes its allocations and children without counting aliases or re-parenting them. `ExportValue` and `ImportValue` unwind only a failed batch's fresh allocations. Generated conversion uses these owners while handing the consumer plain native functions; the consumer chooses when its lifetime ends. This is bookkeeping of the rows above that every consumer would otherwise repeat, with no catalog, entity store or durable host: bindings and lookup remain connection-scoped even though serialized descriptors can be presented again. [#257's verdict](https://github.com/Bitspark/nightseam/issues/257#issuecomment-5752853362) and [the owner decision](decisions/an-owner-is-a-lifetime-the-caller-supplies.md) settle the local lifetime surface without adding wire vocabulary. |
| stream, cell, topic | compositions, demonstrated | the [composition examples and evidence](runtime/compositions.md#the-four-compositions) state their chosen behavior: the stream awaits reports and attempts one ending on normal completion/cancellation; the cell versions writes under a lock and reports after unlocking; the topic bounds each subscriber's queue and drops overflowing reports without evicting the subscriber. The fixtures hold the sequential cases they execute, not ordered concurrent notification, atomic fan-out or guaranteed terminal delivery after connection loss. Those limits and the different backpressure choices belong to the compositions. Their classification introduces no new kind and does not preclude shipping a reusable composition. |
| collection, space, factory and assembly | domain semantics | storage with its query and history; hosting, construction and durable records. A consumer's, or a host's. |
| service policy and issuance choices | domain semantics | the consumer chooses trusted roots, privileged issuance, resource meaning and current access rules; an interface remains a record of callables. Checking evidence under the selected authority profile is classified separately below. |

### The selected optional authority profile

| concept | class | the argument |
| --- | --- | --- |
| rooted-grant verification and guarded invocation | composition, admitted for implementation | reviewed signing and verification, bounded state and the public runtime surface compose checking supplied evidence and preserving guards; consumers supply trust and resource policy. The [repository-home decision](decisions/the-reusable-foundation-lives-in-nightseam.md) admits this selected profile, with independent adoption and no dependency from bare data/RPC back into auth. [#345](https://github.com/Bitspark/nightseam/issues/345) holds implementation and conformance; this row does not claim either has shipped. |

### Declaration identity

| concept | class | the argument |
| --- | --- | --- |
| declaration identity — `(path, digest)` | primitive exposure, RPC and live | the [canonical declaration](declaration/declaration-identity.md) supplies one revision identity, checked at generated wire interpretation, channel admission and live import before consumer dispatch. A consumer's `describe()` call runs a model handler before checking, and a model-agnostic intermediary cannot supply it for every transferred reference. The exposure carries the digest beside the existing name and refuses differing specified digests with `contract_mismatch`; absence makes no revision claim. Strict equality is [#292's verdict](https://github.com/Bitspark/nightseam/issues/292#issuecomment-5753285816). [#339](https://github.com/Bitspark/nightseam/issues/339) places the connection check at interpretation through an ordinary `identity.check` request, preserving consumer subprotocol selection. Compatibility between distinct revisions remains consumer policy, and declaration identity establishes neither authenticated peer identity nor permission. |

## Prior art

The three levels are not this model's invention: *pure* values and *live*
things, and the `isPure` / `isWire` / live views in which a callable inside a
value is registered on send and rebuilt as a proxy on receive, were drawn
before it; a data value needs no binding to be read, a reference's
representation is data while what it represents is not. Adopted: the
distinction, the ownership rule that a handler receives a typed value and a
validator rejects rather than repairs, and the concrete callback,
returned-interface and higher-order scenarios. Not adopted, by the rows
above: the inventory of kinds around them, access inheritance, and a durable
space that would make every reference outlive its connection.
