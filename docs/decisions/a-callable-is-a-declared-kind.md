# A callable is a declared kind, and its identity is its declaration

The live tier needed a way to say "a value here is something the other side
can call". Two shapes were on the table, both with prior art in the sibling
projects this model is drawn from, and they disagree about the same thing
twice — once in the grammar and once on the wire.

**A constructor.** Glyph's arrow: a callable is a type *expression*,
`Callable<Percent, Unit>`, written wherever a type is named, and a service is
a product of arrows. Nothing is declared; the shape is the type.

**A kind.** TISL's thing types: a callable is a *declaration*, named among the
family's types, and a value type refers to it. There is no anonymous callable.

The operator chose the kind, on 2026-09-19, and with it the nominal identity
that follows from it — [#201's
verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544).

## Why they are one decision and not two

A reference on the wire has to say what it is a reference *to*, or a callback
for one thing reaches an implementation of another. What it can say depends
entirely on which shape was chosen:

- With a constructor there is no declaration to name, so the identity has to
  be **structural** — a digest of the normalized signature. Then `report` and
  `setVolume`, both taking an integer and answering nothing, are the same
  contract, and a reference to one is accepted where the other is expected.
  Nothing in the model can tell them apart, because in that model there is
  nothing to tell apart.
- With a kind every callable has a declaration site, so the identity is
  simply **the declaration**: `worker/Report`. Two callables of the same shape
  are two contracts, and the refusal is exact.

Choosing the kind is therefore choosing the safety; the grammar and the wire
form are the same choice seen twice.

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
