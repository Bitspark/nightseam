# A side may extend another family's

**The question.** Types compose across families — an import, an
application, a reference. May *operations*? A family that offers everything
`workbench` does and more has no way to say so; it restates them, and the
two drift.

**Decided.** A protocol side may `extends` another family's same side: its
methods and events arrive under their own names, its errors with them, and
its governance where the session tier is extended too. The extending
family's surface is a superset, so a consumer of the base may speak to it.
The family it extends is one it imports; a side that extends its own
family's, a chain that returns, and a name that means two things across the
join are each refused, because a superset in which one name means two
things is not a superset. `check` holds all of this; the rendering — the
base's own type names read in the extending family's package — is each
target's, and until a target has it the target refuses the family and says
so.

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
family](a-tier-is-a-built-in-family.md) records where the line fell.

**Serves.** Composability — a family is built from families, in operations
as well as in types.

**Since.** 0.4.0, [#61](https://github.com/Bitspark/nightseam/issues/61),
landed by #101.
