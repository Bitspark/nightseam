# A side may extend another family's

**The question.** Types compose across families — an import, an
application, a reference. May *operations*? A family that offers everything
`workbench` does and more has no way to say so; it restates them, and the
two drift.

**Decided.** A protocol side may `extends` another family's same side: its
methods and events arrive under their own names, and its errors with them.
The extending
family's surface is a superset, so a consumer of the base may speak to it.
The family it extends is one it imports; a side that extends its own
family's, a chain that returns, and a name that means two things across the
join are each refused, because a superset in which one name means two
things is not a superset. `check` holds all of this; the rendering — the
base's own type names read in the extending family's package — is each
target's, and until a target has it the target refuses the family and says
so.

A generic base family requires an explicit inheritance application,
`{"apply":"base","with":{"T":"Item"}}`, which selects the same side and
fills every base family parameter. A bare family name is for a nongeneric
base only. [#136](https://github.com/Bitspark/nightseam/issues/136) extends
[the one filling operation](one-parameter-mechanism-of-two-sorts.md) here;
parameter spelling never supplies a binding. A diamond may reach the same
operation declaration with the same binding; different bindings collide.

> **Superseded in one clause.** The session tier a governed family extended,
> and the `incompatible_governance` refusal that held one conversation source
> across an inheritance chain, were removed with the session in 0.5.0
> ([#196](https://github.com/Bitspark/nightseam/issues/196)). Side inheritance
> itself — what this page decided — is unchanged, and a later tier that carries
> operations inherits through the same mechanism.

**Why.** Deferring it "until a second family asks" was the safe answer and
it forfeits the experiment: what `includes` has to mean at the concern
level is exactly what building this here answers, and it is answered by
what breaks rather than by a whiteboard. Two things broke and are written
down. The first: an inherited operation's types are the *base's*, so
flattening a side is not copying its methods — every name in them has to be
read again in the extending family's namespace, which is the same rewrite
an instantiation does and is why the render lanes own it rather than the
model lane. The second: the model wanted to use this on itself — a session
family's side as the built-in `session` family's side extended — and
turning that on would have added operations to every session family in the
tree before any target could render one. So the mechanism landed and that
use of it did not; [a tier is a built-in
family](a-tier-is-a-built-in-family.md) records where the line fell. That
use is moot since 0.5.0: no tier has a built-in whose types a family does
not carry.

**Serves.** Composability — a family is built from families, in operations
as well as in types.

**Since.** 0.4.0, [#61](https://github.com/Bitspark/nightseam/issues/61),
landed by #101.
