# `meta` is a header, not a member

**The question.** A consumer has a fact about a call rather than the call —
a tenant, an idempotency key, a per-request credential — that no family
declares and no layer carries. Where does it travel, and does the peer act
on it?

**Decided.** One header, `meta`: a flat string map on a `request` or an
`event`, validated by form alone, delivered to the handler beside the
payload and read into nothing. A second header is a key of `meta`, never a
member of its own. It does not propagate: a handler's own calls carry none
of what arrived unless the handler says so. Keys beginning `nightseam.` are
reserved and refused. A `response` and a `cancel` carry none.
[The profile](../wire/profile.md#request-metadata) has the form.

**Why.** The header passes the envelope test in the one way a header can:
delivering is the peer's action on it — it reaches the handler without being
the payload, so a payload a consumer signs does not sign its own carriage,
and a family generic in others can carry one for types it did not declare.
Making it a member per fact would have grown the envelope by one validator
per language per fact; making it propagate would have confused a fact about
*this* call with a trace, which is about the calls a handler makes in turn —
the opposite thing, and the one the peer already propagates. The reserved
prefix is what lets the profile define a deadline or a cause later without
colliding with a consumer's key, and refusing it now rather than ignoring it
is what keeps that space empty.

**Serves.** Layering, and the boundary rule — a consumer's fact is the
consumer's; the profile carries it and reads none of it.

**Since.** 0.3.0, #49.
