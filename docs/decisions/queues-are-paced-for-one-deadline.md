# Queues are paced for one deadline

**The question.** A producer fills one of a peer's bounded queues — outgoing
frames, events waiting for their handlers. Is the connection ended, or is
the producer made to wait, and for how long?

**Decided.** Paced for one write deadline (10 seconds by default), the same
rule for every queue and in both runtimes; a consumer that still has not
drained it by then is disconnected rather than allowed to hold the
connection up. Pacing an inbound queue means pacing the remote, which is
done by not taking what it sends — while the event queue is full, the
responses and cancellations on that connection wait with the events. A
runtime that cannot pause what its transport hands it holds the events
instead of the reading; the deadline is the same either way. [The
profile](../wire/profile.md#limits-and-backpressure).

**Why.** A burst that would drain in a second should not end a connection:
durable replay, a tight decoder loop outrunning a ready consumer, a socket
that is briefly behind are all ordinary, and ending the connection on the
first full queue would have made every one of them a disconnect a consumer
has to reconnect from. Waiting without bound is the other failure — a
consumer that never drains would hold the connection, and the peer's
memory, forever. One deadline, the same for every queue, is what a
consumer can reason about; a deadline per queue would have been a knob per
queue with no consumer able to say which one it wanted. The cost is
accepted knowingly: while an inbound queue is paced, responses on that
connection wait too, which is what the deadline bounds.

**Serves.** Configurability — one bound, one name, one meaning; and
composability, since a transport that cannot pause is held to the same
rule by a different mechanism.

**Since.** 0.3.0, where the two runtimes were held to the one rule.
