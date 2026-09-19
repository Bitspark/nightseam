# Extensibility

## At the limit

A language, a target, a transport, a layer, an adapter, a store — each
joins at one named point, by adding, and nothing that exists moves. What
joins is held to the same suite that holds what was there, and the shipped
parts use the same points a consumer's own would, so that there is no
private way in. The points are few and stated; a kind of extension with no
named point is a kind the tree does not admit, said plainly, rather than
one admitted by editing the tree.

## The dimensions

- **The points named.** Whether every kind of extension — a new language,
  a new rendering target, a new carrier of frames, a new layer over the
  peer, a new watcher or trace adapter — has a stated point at which it
  joins, and a stated order to do it in.
- **Nothing else moves.** Whether joining requires editing what exists —
  a list that must be appended, a switch that must gain a case, a table
  that must be told — or only adding beside it.
- **Held the same.** Whether an extension is held to the suite the shipped
  parts are held to, by the same scenarios and the same promises, or
  whether it is trusted because it compiled.
- **Symmetry.** Whether the shipped parts join through the same points —
  the shipped carrier through the seam a consumer's carrier would use, the
  shipped adapter through the hooks a consumer's adapter would bind — so
  that the points are exercised by the tree itself and cannot silently
  rot.
- **The cost of joining.** How much of the tree an extension must know; at
  the limit, the shape of its point and the suite that holds it.

## What it yields to

No legacy: an extension point is not a compatibility layer, is not kept
for a consumer that does not exist, and is removed when nothing uses it.
The boundary: a point exists for mechanism to join, not for a consumer to
put a policy inside the mechanism. Layering: a layer joins beneath nothing
and above the seam it speaks, and a point that would let a layer join
sideways is not a point.

## What it is not

Not plugins, and not hooks everywhere: the points are few, and a hook
whose reason is "someone might want it" is machinery kept forever. Not
open internals: what is not a point is not a surface. Not a promise of
stability for the points before the model is settled — a point is
rewritten with the model, and an extension written against it is rewritten
too, cheaply, now.
