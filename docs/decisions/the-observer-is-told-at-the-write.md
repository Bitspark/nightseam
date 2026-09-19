# The observer is told at the write

**The question.** An observer is told a frame was sent. When — where the
frame was queued for sending, or where it left for the transport?

**Decided.** At the write: the writer tells the observer of each send
immediately before it writes, and the reader tells it of each receipt
immediately after parsing — one serialization point per direction. So an
observer's events are one order per peer, each told before the effect it
names has left the peer, and a frame observed sent is one the peer handed
to the transport: a connection that fails with frames still queued never
observed those sent. The promise is one peer's; two peers' observers are
two orders, and nothing relates them but a trace. [The
observer](../runtime/observer.md#order).

**Why.** A peer that told the observer where the frame was *queued* would
have had no order at all: the writer is free to write a queued frame, have
it answered and have the answer read before the queueing goroutine says
anything, so a reply could be observed received before the request that
drew it was observed sent. That is not a theoretical window — it was seen
once in a conformance run, and rarely enough to be worse than often, since
a gate that fails on a schedule nobody can predict invites the loosening
of the gate. The cost is the serialization point, which the writer already
is; the alternative was an observer whose order meant nothing.

**Serves.** Observability — a consumer can reconstruct what happened in the
order it happened, from one place.

**Since.** 0.3.0.
