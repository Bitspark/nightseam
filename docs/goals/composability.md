# Composability

## At the limit

Every part is usable alone, stacks on any part beneath it, and is replaced
without any part beside it knowing. A consumer assembles exactly what it
needs from parts each of which carries no knowledge of the assembly it is
in, and the seam between two parts is the same seam everywhere — one
shape, not an adapter per pair. A part behaves the same in every assembly
it can be put in, and a thing that carries frames is interchangeable with
any other thing that carries frames, whatever it is made of.

## The dimensions

- **Independence.** Whether a part is usable with nothing above it — a
  peer without a tunnel, a tunnel without a session, a seam with no peer
  over it — and whether a part requires a specific part beneath it or only
  a shape.
- **Substitutability.** Whether what carries frames, what stores them, what
  watches them, what puts a trace on them can each be swapped for a
  consumer's own without any other part changing — and whether the shipped
  one and the consumer's own are held to the same promise.
- **Uniformity of the seam.** Whether there is one seam or several: whether
  a part that runs over one kind of carrier runs over every kind, or has a
  path per kind.
- **Assembly-blindness.** Whether a part asks what assembled it — whether a
  relay asks what multiplexed its connection, whether a layer behaves
  differently over one carrier than another, whether a part reaches
  sideways to a sibling it should not know exists.
- **The cost of a new part.** How much of the tree a new carrier, a new
  store, a new watcher must know to join; the limit is the shape of the
  seam and nothing else.

## What it yields to

Layering: a part composes with what is beneath it, never sideways, and a
composition that would have a part know a sibling is refused however
convenient. The boundary: a part is a mechanism, and a part that exists to
carry one consumer's policy is that consumer's, outside. Where composing
would cost a copy or a hop at every seam, the seam is made cheap rather
than the composition being made special.

## What it is not

Not "everything combines with everything": a part composes with the shape
beneath it, and two parts of the same layer do not compose with each other.
Not a plugin system, and not configuration: assembling is done by handing
one part to another, not by naming it in a setting. Not a promise that
every assembly is sensible — a consumer may stack what it does not need —
only that every assembly behaves as its parts do.
