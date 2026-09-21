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

**Why.** The identity is a **string the declaration already has**, so it is
computed once, in rendering, and stamped into the wire descriptor every
language's validator reads. No second spelling has to be kept in step across
languages: Go does not derive it and TypeScript re-derive it; both read it. A
structural digest — the alternative that would have let inline callables carry
identities — would have had to be specified as a canonical grammar and
implemented identically in every runtime, and every language added later would
have had to reproduce it exactly.

What it costs is anonymity. A record cannot say `{"name": "report", "type":
{"callable": …}}`; the callable is lifted into the tier's types and named, and
a callable written inline is refused with a diagnostic that says so. For a
one-off callback that is a line of ceremony. Generic callables remain refused:
their identities could still be nominal, with applied arguments, but that
requires a canonical spelling of arguments shared by every language, which does
not follow from nominal identity alone. The operator chose option C in
[#247](https://github.com/Bitspark/nightseam/issues/247) — keep that refusal
and support generic containers of live values. A *container* generic over a
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
its identity is derived from it by one rule rather than computed separately
wherever it is needed. Agnosticism — that identity is a string every language
reads rather than an algorithm every language must reproduce.

**Since.** 0.5.0, [#201](https://github.com/Bitspark/nightseam/issues/201),
with the generic refusal held by
[#247](https://github.com/Bitspark/nightseam/issues/247).

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
SHA-256 digest from the family's rendered wire description and supplies it
through `WireDigest()` in Go and `wireDigest` in TypeScript. Moving or renaming
a callable still changes its nominal contract; retaining its path while the
rendered description changes now produces a different digest. If the reference
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
