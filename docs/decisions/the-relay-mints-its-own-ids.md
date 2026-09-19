# The relay mints its own ids

> **Superseded.** The relay this records was removed whole in 0.5.0
> ([#196](https://github.com/Bitspark/nightseam/issues/196),
> [#200](https://github.com/Bitspark/nightseam/issues/200)): nothing between
> two peers re-mints ids, and a peer's own ids are per connection as the
> profile has always said. The page is kept for the reasoning — a forwarder
> rewrites exactly the member it owns and forwards the rest verbatim — which a
> later forwarding layer still answers to; what it decided no longer describes
> the tree.

**The question.** Every peer mints request ids per connection with its
role's prefix, so two consumers attached to one session over its life both
send `c:1`. The relay forwards both towards one machine. Whose ids does
the machine see?

**Decided.** The relay is the family's client towards the machine and mints
the ids it sends up itself, unique per session, keeping which consumer's
request each stands for and mapping the responses back; the machine's own
ids, `s:N`, travel down as they are. Everything else in a frame reaches
the other side verbatim, in the place it arrived in.

**Why.** Ids are per connection by the profile's rule, and a session is a
thing that outlives connections: a relay that forwarded consumers' ids
unchanged would collide the moment a second consumer attached, and a relay
that asked consumers to coordinate would have made the profile's simplest
promise — mint your own, the prefix keeps you apart — false for anyone
behind a session. Rewriting exactly one member and forwarding the rest is
what lets a member of a later profile, or a trace context, cross the relay
untouched: the relay knows `id` and nothing else.

**Serves.** Layering — the session does its one job on the wire and reads
nothing else of the profile's.

**Since.** 0.2.0, with the session.
