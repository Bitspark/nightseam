# The session's vocabulary is not logged

**The question.** The relay tells a consumer who holds control and where it
stands, on the wire, as `session.control` and `session.cursor`. Do those
frames take a place in the session's log, and may a machine send one?

**Decided.** Neither is logged, neither is a family event, the machine
never sends one, and the cursor is the log's sequence rather than a count.
A machine that sends any `session.*` frame is ended with 1002 and a reason
naming the frame. [The session](../wire/session.md#the-sessions-own-vocabulary).

**Why.** The log holds the family's frames and nothing else, so that a
replay never gives a stale holder or a cursor of its own: a consumer that
reattaches is told both afresh, by the relay, where it now stands — state,
not messages. Logging them would have replayed a holder that had since
changed and a cursor that named a place in an older replay. The machine is
refused because the vocabulary belongs to the layer that defined it: a
machine speaking for the layer above it would have made every consumer
unable to tell the relay's word from the machine's. And a cursor that
counted frames rather than naming the log's sequence would have stood one
short after every truncation, since a cut frame is delivered as nothing —
so the cursor after the next frame names that frame's own sequence, and
never moves backwards.

**Serves.** Layering — a layer's vocabulary is produced by the layer,
consumed by the one above, and crosses no other boundary.

**Since.** 0.3.0, with the session's first vocabulary.
