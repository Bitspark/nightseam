# The relay mints its own ids

**The question.** Every peer mints request ids per connection with its
role's prefix, so two consumers attached to one session over its life both
send `c:1`. The relay forwards both towards one machine. Whose ids does
the machine see?

**Decided.** The relay is the family's client towards the machine and mints
the ids it sends up itself, unique per session, keeping which consumer's
request each stands for and mapping the responses back; the machine's own
ids, `s:N`, travel down as they are. Everything else in a frame reaches
the other side verbatim, in the place it arrived in. [The
session](../wire/session.md#the-rules), rules 4 and 6.

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
