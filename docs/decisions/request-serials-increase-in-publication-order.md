# Request serials increase in publication order

**The question.** A request's `id` is minted per connection with the sender's
prefix and answered exactly once. Nothing said what order ids are minted in,
and a receiver refused only a duplicate that was still active. So once a
request was answered and retired, its id could be taken again — and a cancel
for the first one, arriving after the reuse, was indistinguishable from a
cancel for the second. Correlation the peer cannot make unambiguous is
correlation the peer does not have.

**Decided.** Within one physical connection instance and one direction, the
decimal part of each published request's `id` — its serial — is greater than
that of every request published before it on that direction. Gaps are allowed.
Only a request's admission advances the receiver's high-water mark; responses
and controls never do. A request whose serial is not greater than the previous
one is a protocol violation and ends the connection with 4011, as a malformed
frame does.

Reservation and publication happen under the one ordering gate the peer's
outgoing queue already is. A reservation that is never published has still
spent its serial, and a retry takes a fresh one. A sender that would wrap
refuses before wrapping and ends the connection. Serials are scoped to the
connection instance and the role, and every carrier bridge — a tunnel channel,
a forwarder onto another connection — mints its own and maps the replies back.

`nightseam.duplex/1` is tightened in place, not renamed. The profile is
pre-1.0 with no external consumer and has already changed under that name in
0.5.0, when the session vocabulary went and `meta` and the live layer came:
the profile is versioned by the release until 1.0, not by the string.

**Why.** [Envelope members are what the peer acts
on](envelope-members-are-what-the-peer-acts-on.md): `id` is the member the
peer correlates by, and this is the least that makes that correlation total.
A queued control already carries the invocation it was queued against; what
was missing was the other race — a control that arrives *after* an id is
reused. A monotonic serial answers it by construction: a later invocation has
a greater serial, so an older control can never name it. No acknowledgment, no
used-id set, no tombstone, and nothing that grows.

[The relay mints its own ids](the-relay-mints-its-own-ids.md) — superseded in
what it names, not in what it decided — is the same rule at a carrier hop:
each connection instance owns its serial space, and a thing that carries
frames across connections rewrites exactly the member it owns.

Nothing is added. Every implementation already mints ids from a counter; the
rule makes that counter's order a promise the receiver can enforce. What it
costs is that reservation and publication must share one gate rather than
being two independent steps — which is a bug the gate also fixes, since two
callers could otherwise publish their serials inverted.

**Serves.** [Observability](../goals/observability.md), which wants what a
peer did "in one order, from one place": a serial that increases per direction
*is* that order, told by the wire itself rather than reconstructed from
arrival times. And [layering](../goals/layering.md), since the rule is stated
entirely in the profile's own vocabulary and a bridge keeps it by owning its
own space rather than by knowing what is above it.

**Since.** #439, with the 0.6.0 adoption of the Bitwire contract, whose public
invocation lifecycle needed an identity discipline it could rely on at the
carrier boundary. Both tier-1 runtimes emit and refuse it in that lane; the
tier-4 ports each hold an issue to conform, and stay provisional until they
do.
