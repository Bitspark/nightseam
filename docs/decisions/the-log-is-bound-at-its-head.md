# The log is bound at its head

**The question.** A session is bound with a log that already holds frames —
a durable one, after a restart. Where does the session stand: at nothing,
or at the log's end?

**Decided.** At its head. `Bind` reads the log once on the way in, through
`Replay` from after zero, and seats the session at the last sequence that
read delivered; the read happens before the machine's connection is read
and under the relay's own lock. Delivering in ascending sequence order is
the whole of what a durable log owes beyond storing frames. [The
session](../wire/session.md#the-log) and [the
surface](../runtime/session.md#the-log).

**Why.** A session bound at nothing after a restart would have given a
consumer attaching with `after: 0` before the machine spoke again nothing
at all, when the log held everything that had happened; and a relay that
handed out sequences from zero over a log that had already given out a
thousand would have written the next frame under a sequence the log
already held. Reading once at bind, under the lock and before the machine's
first frame, is what makes a frame that arrives during the read land above
the head and never under a sequence already given. The alternative — a log
that reports its head — would have been a second method every durable
implementation had to get right; a read through the one method it already
has costs one replay at bind and asks nothing new of anyone. A log that
knows its head without a read may one day say so, as something the relay
prefers where a log has it.

**Serves.** Composability — a consumer's own log plugs in by implementing
the two methods and nothing else changes.

**Since.** 0.3.0.
