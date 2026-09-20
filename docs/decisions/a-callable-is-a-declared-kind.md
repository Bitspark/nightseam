# A callable is a declared kind, and its identity is its declaration

The live tier needed a way to say "a value here is something the other side
can call". Two shapes were on the table, both with prior art in the sibling
projects this model is drawn from. The choice involved two dimensions:
where a callable is written in the grammar, and how its wire contract is
identified.

**A constructor.** Glyph's arrow: a callable is a type *expression*,
`Callable<Percent, Unit>`, written wherever a type is named, and a service is
a product of arrows. Nothing is declared; the shape is the type.

**A kind.** TISL's thing types: a callable is a *declaration*, named among the
family's types, and a value type refers to it. There is no anonymous callable.

The operator chose named callable declarations and nominal wire identity,
on 2026-09-19 — [#201's
verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544).

## Two selected design dimensions

A reference on the wire names the contract its binding implements. This
design chooses **nominal identity**, the declaration's `family/Type` path:
`worker/Report` and `worker/SetVolume` are different contracts even if both
take an integer and answer nothing. A descriptor carrying one is refused
where the other is expected.

It separately chooses **named declarations** over inline callable
expressions. Naming and nominality are not logically inseparable: a
language could name callables yet compare normalized signatures, or give
inline callables explicit identities. Nightseam chooses neither of those
combinations. Its named declaration supplies a direct, shared spelling for
the nominal wire contract without specifying a structural signature digest.

This check distinguishes wire contract names. It does not prove which
implementation a native function value originally represented.

## Native assignment and contract evolution

Generated Go callables are aliases of function types; TypeScript callables
are function type aliases. Two with the same native signature can be
assigned to each other without a cast. When that value is exported, the
generated boundary helper stamps the contract expected at that position.
It does not recover a semantic identity from the function. The guarantee
is nominal wire checking, not end-to-end nominal typing of native values.

The contract path contains no signature fingerprint or version. Moving or
renaming a callable changes its wire identity; retaining the path while
changing its signature leaves the identity string unchanged. Equality of
that string therefore establishes no compatibility between incompatible
signature revisions. Argument and result validation still applies, but a
future contract-evolution strategy needs its own deliberate decision.
This design introduces neither host-type branding nor versioned identities.

## What it costs, and what it does not

It costs anonymity. A record cannot say `{"name": "report", "type":
{"callable": …}}`; the callable is lifted into the tier's types and named, and
a callable written inline is refused with a diagnostic that says so. For a
one-off callback that is a line of ceremony.

Generic callables remain refused. Their identities could still be nominal,
with applied arguments, but that requires a canonical spelling of arguments
shared by every language. It does not follow from nominal identity alone.
The operator chose option C in [#247](https://github.com/Bitspark/nightseam/issues/247):
keep that refusal and support generic containers of live values.

A *container* generic over a live type — `Page<Job>` — needs no identity of
its own. Generated conversion helpers take converters for their parameters;
the live caller supplies converters closed over its scope. The declaration
of `Page` remains data-only, with no live runtime dependency, while its live
application belongs in `live.json`. Only the callable members carry contracts.

It costs nothing in expressiveness that matters here. An interface is a record
of callable members, which is what both prior models converge on, and nothing
about it needs a service kind, a stream kind, a cell kind or a topic kind —
each of which would need its own admission argument under
[the admission test](../admission.md).

## What it buys beyond the refusal

The identity is a **string the declaration already has**, so it is computed
once, in rendering, and stamped into the wire descriptor every language's
validator reads. No second spelling has to be kept in step across languages:
Go does not derive it and TypeScript re-derive it; both read it. A structural
digest would have had to be specified as a canonical grammar and implemented
identically in every runtime, and every language added later would have had to
reproduce it exactly.

## What was measured

`worker` in the generator's corpus is the whole of #196's example — a supplied
`ProgressSink`, a returned `Job`, callables in a record, a union, an array, a
map and a nullable — and `supervisor` names worker's callables across a family
boundary. `conformance/tables/validator.json` holds the refusal both runtimes
must spell the same, including the one that matters: a reference carrying
`wide/Cancel` where `wide/Report` is expected, refused by name.

[`TestGeneratedCallableNominality`](../../cmd/nightseam/callable_nominality_test.go)
generates two different callable declarations with identical signatures.
Its Go and TypeScript programs compile the assignment from `Report` to
`SetVolume`, export the value as `nominal/SetVolume`, and invoke that
implementation through the destination import. Both generated validators
refuse the resulting descriptor as `Report`, and both generated imports
refuse it with `contract_mismatch`. Together with the shared runtime case
*a contract the binding does not carry is refused* in
[Go](../../live/go/livetest/conformance.go) and
[TypeScript](../../live/ts/src/conformance.ts), this holds both the accepted
native assignment and the rejected mismatched descriptor.
