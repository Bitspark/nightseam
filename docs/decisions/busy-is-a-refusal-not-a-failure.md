# `busy` is a refusal, not a failure

**The question.** A peer has as many requests in hand as it allows — being
handled on one side, outstanding on the other. What happens to the next
one?

**Decided.** Two bounds, two refusals, neither a failure of the connection:
a **receiver** with as many requests being handled as it allows answers
`busy` on the wire; a **caller** with as many calls outstanding as it
allows refuses the next one where it stands, without sending a frame — no
request is started, so no observer is told of one, and the connection
serves the call after it. [The
profile](../wire/profile.md#limits-and-backpressure).

**Why.** The two bounds are different things and had been one in the
consumer's eye: the receiver's is a fact about the other side, told on the
wire as any answer is, where the caller's is a fact about this side, and
sending a frame to learn that one's own limit is reached would have cost a
round trip to be told what was already known. Refusing locally without a
frame keeps the wire honest — an observer sees requests that were made, not
ones that were not — and keeps the connection up: the caller is refused,
the connection is not. Ending the connection for either would have turned
a moment's saturation into a reconnect, and the other rule
([queues are paced](queues-are-paced-for-one-deadline.md)) already says
what happens when saturation lasts.

**Serves.** Observability — what an observer is told is what happened on
the wire; and configurability, two bounds under two names.

**Since.** 0.3.0.
