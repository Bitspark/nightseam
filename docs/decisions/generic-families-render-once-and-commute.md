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
