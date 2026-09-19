# Layers share the peer's observer

**The question.** The tunnel and the session have things to tell —
channels opened, control moved. Does each take an observer of its own?

**Decided.** No. The peer of the profile takes the one observer; the tunnel
and the session declare events of their own and emit them through the peer
they run over, and a layer of a consumer's own does the same — through the
peer a channel hands back in Go, by declaring its events into the runtime's
event type in TypeScript. A session whose connection runs over no peer
takes the registry's observer, and one with neither observes nothing. [The
observer](../runtime/observer.md) and [the session's
surface](../runtime/session.md#observing-it).

**Why.** An observer per layer would have been a second place — and then a
third — to choose one, and a consumer that chose one and not the others
would have watched frames and not channels, or channels and not sessions,
without being told. One observer chosen once, told about frames, channels
and sessions alike in one order, is what makes a `switch` over the events
exhaustive across every layer a consumer imports. The session's fallback
to the registry's observer exists for the one case where there is no peer
to reach — a pipe, a bare socket — and the order in which it looks is
stated so that a session over a channel never silently observes twice.

**Serves.** Observability — one place; and composability, since a layer
of your own joins the same way the shipped ones do.

**Since.** 0.2.0, with the tunnel; the session's order of lookup in 0.3.0.
