# A deadline is not a cancel

**The question.** A caller's request has not been answered within its
deadline. Is that the same thing as the caller withdrawing it?

**Decided.** No. A request past its deadline is failed locally as
`request_timeout` — the caller's own error, never a frame it received —
and a cancel is sent; a request the caller withdrew is `cancelled`. The
receiver's own deadline for a handler is the same length, and what it
answers on the wire when it passes is `cancelled`, the request having been
abandoned. An observer is told the outcome `timeout` for the first and
`cancelled` for the second. [The profile](../wire/profile.md#requests).

**Why.** The caller withdrawing a request and the caller giving up waiting
for one are different facts, and the receiver may have answered either way:
what the caller learned from a deadline is that its own deadline passed and
nothing about the request's fate, where a cancel is the caller's own act.
One code for both would have told a consumer branching on it — retry,
compensate, report — the wrong thing half the time, and an observer
counting timeouts would have counted withdrawals with them. The two
runtimes had drifted apart on exactly this, one answering `cancelled` and
the other `request_timeout` for the same event, which is what made the
distinction worth stating rather than leaving to each.

**Serves.** Observability — a consumer can tell, after the fact, which of
two different things happened.

**Since.** 0.3.0.
