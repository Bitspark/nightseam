# A callable is a declared kind, and its identity is its declaration

**The question.** The live tier needed a way to say "a value here is
something the other side can call". Two shapes were on the table, each with
prior art behind it, and the choice involved two dimensions: where a callable
is written in the grammar, and how its wire contract is identified.

*A constructor.* The arrow: a callable is a type *expression*,
`Callable<Percent, Unit>`, written wherever a type is named, and a service is
a product of arrows. Nothing is declared; the shape is the type.

*A kind.* The declared thing type: a callable is a *declaration*, named among
the family's types, and a value type refers to it. There is no anonymous
callable.

**Decided.** Named callable declarations and nominal wire identity, by the
operator on 2026-09-19 — [#201's
verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544).

A reference on the wire names the contract its binding implements, and that
name is **nominal**: the declaration's `family/Type` path. `worker/Report`
and `worker/SetVolume` are different contracts even if both take an integer
and answer nothing, and a descriptor carrying one is refused where the other
is expected. Separately, the grammar takes **named declarations** rather than
inline callable expressions. Naming and nominality are not logically
inseparable — a language could name callables yet compare normalized
signatures, or give inline callables explicit identities — and Nightseam
chooses neither of those combinations. Its named declaration supplies a
direct, shared spelling for the nominal wire contract rather than identifying
callables solely by their structural signatures.

The check distinguishes wire contract names. What it does not reach — native
assignment, and what the path says about a signature that changed under it —
is below.

**Why.** A nongeneric callable's nominal path is a string the declaration
already has, computed in rendering and stamped into the validator descriptor.
A closed generic application's name is rendered from that same constructor's
canonical declaration graph and ordered arguments. The shared byte and name
fixtures hold every implementation to one spelling; structural signature
equality never replaces the constructor's declared name.

What it costs is anonymity. A record cannot say `{"name": "report", "type":
{"callable": …}}`; the callable is lifted into the tier's types and named, and
a callable written inline is refused with a diagnostic that says so. For a
one-off callback that is a line of ceremony. The operator initially chose
option C in [#247](https://github.com/Bitspark/nightseam/issues/247): defer generic
callables until a shared canonical argument identity existed, while supporting
generic containers of live values. That v0.5.0 deferral is replaced by
[#366 A](https://github.com/Bitspark/nightseam/issues/366) and its paired
implementation in [#369](https://github.com/Bitspark/nightseam/issues/369).
Type arguments are fixed before export; no invocation chooses them. Pure
aliases normalize to the same application, while a separately declared
constructor remains distinct even with the same signature. A *container* generic over a
live type, `Page<Job>`, needs no identity of its own: generated conversion
helpers take converters for their parameters, and the live caller supplies
converters closed over its scope, so the declaration of `Page` stays data-only
with no live runtime dependency while its live application belongs in
`live.json`. Only the callable members carry contracts.

It costs nothing in expressiveness that matters here. An interface is a record
of callable members, which is what both prior models converge on, and nothing
about it needs a service kind, a stream kind, a cell kind or a topic kind —
each of which would need its own admission argument under
[the admission test](../admission.md).

**Serves.** Declarative — the contract is stated once, in the declaration, and
its identity is derived from it by one rule. Agnosticism — the shared canonical
graph and fixtures hold each language to the same identity.

**Since.** 0.5.0, [#201](https://github.com/Bitspark/nightseam/issues/201),
with the historical generic deferral in
[#247](https://github.com/Bitspark/nightseam/issues/247) completed by #369 for 0.6.0.

## Closed callable applications

`runtime.CallableIdentity` in Go and `callableIdentity` in TypeScript select
the existing canonical application graph. The printable name includes ordered
arguments, such as `worker/Handler<worker/Job>`; its digest hashes that selected
graph and the reachable argument declarations. An absent revision digest does
not erase arguments from the nominal contract. Nongeneric callables retain
their declaring-family revision guarantee described below.

Generated argument adapters carry identity, validation and both conversion
recipes. Exported implementations later import requests and export results;
imported proxies do the reverse. Each invocation supplies its active owner and
context, including for a returned callable used after its supplying call ends.
A source alias emits a specialized conversion body while retaining the original
constructor and argument graph, independently of the generic adapter route.
The [generated API](../declaration/generated.md#parameterized-callable-helpers)
and [shared identity cases](../../conformance/tables/callable-identities.json)
hold these concrete projections.

## Native assignment and contract evolution

The check does not prove which implementation a native function value
originally represented. Generated Go callables are aliases of function types
and TypeScript callables are function type aliases, so two with the same native
signature can be assigned to each other without a cast; when such a value is
exported, the generated boundary helper stamps the contract expected at that
position rather than recovering a semantic identity from the function. The
guarantee is nominal wire checking, not end-to-end nominal typing of native
values.

The contract-evolution decision reserved here was made in
[#292](https://github.com/Bitspark/nightseam/issues/292#issuecomment-5753285816):
declaration identity is `(path, digest)`, strict. The generator derives the
SHA-256 digest from the family's canonical declaration and supplies it
through `WireDigest()` in Go and `wireDigest` in TypeScript. Moving or renaming
a callable still changes its nominal contract; retaining its path while the
canonical declaration changes now produces a different digest. Its exact
[coverage and byte grammar](../declaration/declaration-identity.md) include
ordinary operations, events and reachable imported content, as
[#343](https://github.com/Bitspark/nightseam/issues/343) requires; the local
validator schema alone does not identify those revisions. If the reference
and its expected declaration both specify digests and those digests differ,
import refuses `contract_mismatch` before allocating an attachment or invoking
an implementation. An absent digest makes no revision claim.

Even an optional member added to the description is a different identity.
Which revisions may be used together is a consumer's compatibility policy;
the core comparison does not infer it from assignability or successful value
validation. Argument and result validation still applies. The digest does not
brand native function values, authenticate a peer or prove how an implementation
behaves.

`worker` in the generator's corpus is the whole of #196's example — a supplied
`ProgressSink`, a returned `Job`, callables in a record, a union, an array, a
map and a nullable — and `supervisor` names worker's callables across a family
boundary. `conformance/tables/validator.json` holds the refusal both runtimes
must spell the same, including the one that matters: a reference carrying
`wide/Cancel` where `wide/Report` is expected, refused by name.
[`TestGeneratedCallableNominality`](../../cmd/nightseam/callable_nominality_test.go)
generates two different callable declarations with identical signatures. Its
Go and TypeScript programs compile the assignment from `Report` to
`SetVolume`, export the value as `nominal/SetVolume`, and invoke that
implementation through the destination import. Both generated validators
refuse the resulting descriptor as `Report`, and both generated imports refuse
it with `contract_mismatch`. Together with the shared runtime case *a contract
the binding does not carry is refused* in
[Go](../../live/go/livetest/conformance.go) and
[TypeScript](../../live/ts/src/conformance.ts), this holds both the accepted
native assignment and the rejected mismatched descriptor. The same test also
generates two revisions of one family, differing only by an optional member,
and holds digest agreement and refusal through both generated languages over
a real socket. The shared [digest vectors](../../conformance/tables/digests.json)
hold the generator's bytes and both runtime validators to one identity.
