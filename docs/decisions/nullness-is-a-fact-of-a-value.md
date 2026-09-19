# Nullness is a fact of a value, presence a fact of a member

**The question.** `nullable: true` was a fact a *field* declared. So an
array of values that may be null could not be said at all: it was declared
as an array of a record with one nullable field, which is a different shape
on the wire, in eight languages.

**Decided.** `{"nullable": T}` is a type expression, and a field's
`nullable: true` is its sugar — the field's type wrapped so. `required`
stays where it is, on the member, because presence is a fact of a member
and not of a type: a member may be absent from an object, and a value
cannot be absent from itself. The two facts stay two.

**Why.** It is the one place the type language could not say what JSON can,
it is the smallest change of the round, and every planned language has the
form to render it — `*T` or `Nullable[T]` in Go, `T | null` in TypeScript,
`Maybe a` in Haskell, `Option<T>` in Rust, `Optional` in Python and Swift,
`std::optional` in C++. Leaving the gap documented would have been cheaper
by a day and would have put a workaround shape into every later contract
that needed one.

**Serves.** Declarative — the declaration says what the wire carries,
rather than the nearest thing it can spell.

**Since.** 0.4.0, [#58](https://github.com/Bitspark/nightseam/issues/58),
landed by #101.
