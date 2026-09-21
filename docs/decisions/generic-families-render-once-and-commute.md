# Generic families render once and commute

**The question.** A family generic in others could be rendered per binding
— one concrete package per family that fills its parameters — or once,
generically, and instantiated by the consumer. And two parameters could be
one type parameter or two.

**Decided.** Once, generically, in each language's own idiom: TypeScript has
associated types, so one parameter is one type parameter whatever it is
drawn at; Go has none, so a parameter becomes one type parameter per type
drawn from it, and a type takes only the ones it uses. Two parameters
never collapse into one. And the two ways to a concrete package must
agree — `gen(bind(C, F)) ≅ gen(C)[F]` — which the fixtures hold by
reflection in Go, by `Equals<>` in TypeScript, and on the wire.
[Generics](../declaration/generics.md).

**Why.** Rendering per binding would have produced a package per pair of
families, none of them the generic thing a relay actually wants to write
against — a `Frame[runtime.Raw]` that passes payloads through. Rendering
once in each language's own idiom, rather than forcing Go's shape on
TypeScript or the reverse, is what parity means here: the same thing, not
the same spelling. Collapsing two parameters into one would have made a
family that binds `S` and `T` to different families impossible to write.
The commuting diagram is what makes the generic rendering trustworthy: if
binding first and rendering plain gave a different package from rendering
generically and instantiating, one of them would be wrong and nobody would
know which.

**Serves.** Agnosticism — each language realizes the one declaration in
its own idiom, held to the same equivalence.

**Since.** v2 of the declaration language.

## Complete associated interpretations

The accepted [#367 A](https://github.com/Bitspark/nightseam/issues/367) contract
extends a family slot to supply the identity, validator and both conversions
of every associated type the consumer uses. The paired implementation in
[#368](https://github.com/Bitspark/nightseam/issues/368) admits plain drawn
records containing callables. It replaces the v0.5.0 `live_draw` refusal;
direct alias, callable and generic-member draws remain outside that grammar.

Go keeps its per-draw native type parameters and family-tag constraints,
adding one neutral `ValueAdapter` per draw. TypeScript keeps one family type
parameter and receives a dictionary of the required associated adapters.
Both check the complete bound source family and member identities before
model interpretation. Every conversion receives the active context and batch;
the reusable recipes retain no owner, connection or principal. A data-only
generic consumer therefore remains independent of live while interpreting a
live provider through the same generated artifact. The public
[generated surface](../declaration/generated.md#associated-type-interpretations)
describes the concrete projections.
