# One parameter mechanism, of two sorts

*This page records two verdicts, because one reason covers both: what a
parameter is ([#57](https://github.com/Bitspark/nightseam/issues/57)) and
what it may be `of` ([#59](https://github.com/Bitspark/nightseam/issues/59)).*

**The question.** A family could be generic in another family — a
parameter `of` the `session` tier, drawn through as `S.Envelope`. A record
could not be generic in a type: `Page<T>` and `Result<T, E>` had to be
written once per element type. Adding type parameters puts two kinds of
hole in the language. Are they two mechanisms, or one? And may a family
parameter be `of` any tier, or only `session`?

**Decided.** One mechanism. A family, a record, a union and an alias each
declare `parameters`, a list of the same shape at every level, and each
parameter is of one of two **sorts**, which `of` names:

- **A type parameter** — `{"name": "T"}`, no `of`. Filled by a type
  expression, written where a type is named: `{"array": "T"}`.
- **A family parameter** — `{"name": "S", "of": "session"}`. Filled by a
  family that carries the named tier, and `of` names **any** tier the tiers'
  table has — `protocol`, `session`, whatever comes next — meaning "a family
  that has it". A type is drawn *through* it: `S.Envelope`.

One filling operation, `{"apply": X, "with": {…}}`, where `X` names a
generic type of this family or of an imported one and each value is what
fills that parameter: a type expression for a type parameter, a family name
for a family parameter. One drawing operation, `P.Type`, refused with a
diagnostic when `P` is a type parameter.

The same filling operation applies at inheritance edges, settled by
[#136](https://github.com/Bitspark/nightseam/issues/136). A generic base is
extended as `{"apply":"Base","with":{"T":"Item"}}` or with a concrete
filler. For types the target is `Type` or `family.Type`; for a protocol side
it is the family name, selecting that same side. All required captured
family parameters and own type parameters are explicit, even for a local
base. A bare name is reserved for a nongeneric base. Nothing is inherited
or bound merely because two parameters have the same spelling. Substitution
preserves the caller's argument scope and each inherited member's source.

**Whether a family parameter is a type parameter with a bound: it is not**,
and the verdict asked for the answer rather than a note. A bound narrows
what a thing may be while leaving it the same kind of thing — `T` is still
a type. A family is not a type: it has no values, nothing is an instance of
it, and what a declaration takes from it is a type *of* it, which is why
drawing exists and why `S` alone is refused where a type is named. Calling
`of` a bound would have made `{"array": "S"}` well-formed and meaningless.
So: one mechanism — one list, one `apply`, one draw — with the sort
declared, rather than one sort with a bound, or two mechanisms with a note.

**Why.** Every API of size has `Page<T>` and `Result<T, E>`; a language
without them declares them once per element type and a target renders them
as if the language had none. The alternative of a fixed set of built-in
generic forms (`{"page": T}`) is cheaper to render and the third shape
somebody needs is the one not built in. Restricting `of` to `session` was
not a rule but a step not yet taken: the rule is the tiers' table's, and a
carrier of any family's messages — a tunnel over a plain protocol, a log of
frames — cannot be declared without it.

What this costs is visible ahead of the pilots and is the point of taking
it here: Haskell and Rust have bounded generics and will render both sorts
directly; C++ has templates with no bound short of concepts and will render
a family parameter as an unconstrained one; Python's generics are erased
and will render the bound as a runtime check or not at all. A form only the
bounded languages can hold would be the wrong form, and this one is not:
the *sort* is a fact of the declaration, and a language that cannot express
it still renders the shape.

**Serves.** Declarative — one hole, one way to declare it, one way to fill
it; and agnosticism, since the sort is stated in the declaration rather
than inferred by each target.

**Since.** 0.4.0, #57 and #59, landed by #101.

The [#363 B ruling](https://github.com/Bitspark/nightseam/issues/363) extends
this same parameter and application mechanism to declared callables. The
closed constructor keeps its ordered arguments and nominal identity under
[#366](https://github.com/Bitspark/nightseam/issues/366); no call chooses a new
instantiation. The [combined acceptance findings](../declaration/proof-findings.md#combined-generic-construction-and-retained-values)
record both supplied callable arguments and complete family interpretations.
