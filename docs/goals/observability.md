# Observability

## At the limit

A consumer can answer, after the fact, any question about what a peer did
— at every layer, in one order, from one place — without having
anticipated the question and without ever seeing what was said. Every fact
is told once, at the moment it becomes true and before its effect leaves
the peer; every fact names the thing its layer is about; and the facts of
one peer correlate with the facts of the peer at the other end, so that
one call through several hops is one story. The telling is always on, so
that the answer exists when the question is asked.

## The dimensions

- **Completeness.** Whether every fact of every layer is told: a connection
  opened and ended and how, a frame sent and received, a request begun and
  ended and with what outcome, an event emitted and delivered, a queue
  that filled, a channel that opened. A fact
  a consumer would have to infer from two others is a fact not told.
- **Order.** Whether the facts of one peer are one order, told before the
  effect leaves — so that a reply is never seen before the request that
  drew it — and whether that order is a promise the tree holds rather than
  a tendency.
- **One place.** Whether a consumer chooses one watcher and is told about
  every layer through it, or must attach one per layer and merge.
- **Nothing said.** Whether any fact carries what a frame said — a payload,
  a header's value, a handler's arguments — rather than what it was named,
  how big it was and how it ended. The line is what lets any watcher be
  attached to any peer without auditing the family first.
- **Correlation.** Across layers, by the names a fact carries — which
  family, which channel; across peers, by the trace a frame
  carries, minted where none arrived and continued where one did.
- **Cost.** Whether the telling is cheap enough to be always on, and
  whether an absent watcher costs nothing at all; a peer that must be
  configured to observe is a peer that was not observing when it mattered.
- **A layer of one's own.** Whether a layer a consumer builds is told
  through the same place, in the same shape, as the shipped ones.

## What it yields to

The boundary: a peer tells and never aggregates, samples, rate-limits or
chooses where the facts go — those are the watcher's, since each is a
policy. Agnosticism: the peer chooses no backend, and binding to one is a
component of its own. And the "nothing said" line yields to nothing: a
question that can only be answered by seeing what was said is a question
for a debugger, not for this goal.

## What it is not

Not logging: a log is one thing a watcher may write, and a peer writes no
log. Not metrics: a count is a watcher's aggregation of facts the peer
told one by one. Not a backend, a dashboard or a format. Not debugging:
seeing the payload is the one thing this goal refuses, on purpose.
