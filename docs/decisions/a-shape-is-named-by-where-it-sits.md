# A shape without a name is named by where it sits

**The question.** Every record, union and entity had a name, and
`array`, `map` and an application were the only unnamed forms. May a shape
be written where a type is named — a record inline in a field's type — and
be named by the generator from the path to it?

**Decided.** Yes, and the derivation is one rule, written in
`internal/naming` and held by the `derived` rows of
`conformance/tables/naming.json` so that every language spells a derived
name one way:

> A derived name is the upper camel of the path to the shape: the
> declaration it sits in, then each step that **names** something — a
> field's name, a union's variant tag, an operation and the role the shape
> plays in it — while the steps that name nothing, `array`, `map`,
> `nullable` and an application's slot, are passed over.

So the `note` member of `EchoRequest` is `EchoRequestNote`; a method's
inline request is `EchoRequest`; an event's data is `ChangedEvent`; a
variant's shape is `PartText`; and wrapping a shape in a list does not
rename it. A shape is written inline where a *value's* type is declared — a
field, a variant, an operation's request, result or event — and nowhere
else: an alias of one gives it no name it did not have, an entity is
identified across a family and is named, and a derived name that collides
with a declared type or with another derived name is refused, since a
generated package cannot hold one name twice. A diagnostic about a shape
with no name points at the path, which is what it has instead of a name.

**Why.** The safe answer was "every shape is named", and it is safe for a
stated reason: a derived name churns when a member is renamed, and it
surfaces as a public identifier in every language. That reason is a
*measurement*, not a fact — and measuring it is worth more to the model
this repository feeds than the one line per shape the rule saves or costs.
So the rule is here, at full strength, with a real family using it, rows in
the naming table and the derivation pinned in one package; if the churn
proves worse than naming the shape, the answer is a reason to name
everything rather than a rule somebody guessed, and the form is removed
rather than carried.

**Serves.** Declarative — a shape that exists in one place is declared in
that place; the name is derived by one rule rather than invented by each
target.

**Since.** 0.4.0, [#60](https://github.com/Bitspark/nightseam/issues/60),
landed by #101.
