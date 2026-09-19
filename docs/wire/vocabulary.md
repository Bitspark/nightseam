# How a layer speaks on the wire

Nightseam is a stack: the seam carries frames, the profile correlates them,
the tunnel multiplexes channels over one peer, the session governs a
conversation over a tunnel's channels — or over any connection of the seam.
Each layer is built on the one beneath and speaks it; none knows the ones
above. This page says what that means for the one question that comes up
whenever a layer needs to say something new on the wire: **where does it
go?** It was written after two wrong answers in one day, so that the third
person to ask reads the test rather than repeating them.

## The test

A member belongs in the profile's envelope when, and only when, **the peer
acts on it**. Every member of every kind is there because the peer
correlates, cancels, dispatches or propagates by it: `id` correlates a
response to its request, `method` dispatches, `traceparent` is propagated
into the handler's context and out again as a child. Nothing else is in the
envelope, and the profile refuses what it does not know — a malformed frame
ends the connection — precisely so that the set stays this small.

The test, applied in order:

1. **Does the peer act on it?** Then it is the profile's: a member, or a
   kind. Trace context passes — the peer propagates it. A subprotocol passes
   — the handshake beneath the peer selects it.
2. **Does one layer produce it and another read it, with the peer only
   forwarding?** Then it is **that layer's own vocabulary**, carried as
   ordinary frames of the profile — a request, a response, an event — with
   names in that layer's reserved prefix. The tunnel is the model:
   `channel.open` is a request, `channel.credit` an event, and the profile
   knows nothing of channels. A session's control and its cursor are the
   same kind of thing ([the session](session.md#the-sessions-own-vocabulary)).
3. **Is it a consumer's fact about a call, with no layer to carry it?**
   Then it is a **header**: a member on the request or event it is about,
   which the peer delivers to the handler beside the payload and reads
   nothing into. There is one, `meta` (#49) — a flat string map, validated
   by form alone, delivered to the handler beside its context, so that the
   peer does act on it, in the one way a header is acted on: it reaches the
   handler without being the payload. That is what keeps it on the right
   side of the test, and why a second header should be a key of `meta` and
   not a member of its own. It is also why a header does not propagate:
   delivering is the peer's one action on it, and a header is a fact about
   *this* call — a credential, a tenant — not about the calls a handler
   makes in turn, which is the opposite of a trace. A handler that means to
   pass one on says so ([the profile](profile.md#request-metadata)).

Two things that look like members and are not — a sequence, which the
session's log assigns and the peer never sees, and a fifth kind, "a session
frame", which is an ordinary event of that layer's vocabulary — were each
tried for an afternoon; [envelope members are what the peer acts
on](../decisions/envelope-members-are-what-the-peer-acts-on.md) is the
record, and [`meta` is a header, not a
member](../decisions/meta-is-a-header-not-a-member.md) the one header's.

## A layer's own vocabulary

A layer that speaks on the wire does it as the tunnel does:

- **A reserved prefix**, one per layer: `channel.` for the tunnel,
  `session.` for the session. The namespace is the layer's, so that it is
  never contested; today the generator does not yet refuse a family that
  declares a method or an event under a reserved prefix (#50) — it holds
  each target's own identifiers under `cmd/nightseam/testdata/reserved`,
  not yet the prefixes.
- **Ordinary frames of the profile.** A layer's request is a request, its
  event an event, minted, correlated and cancelled by the peer like any
  other. The layer registers its handlers on the peer it runs over (the
  tunnel's `channel.open` handler) or produces them in its own relay (the
  session's `session.control` and `session.cursor`); either way the peer
  dispatches by name and knows nothing of what the name means.
- **Imported, implicitly.** A layer's vocabulary is a **family**: `duplex`
  for the profile, `tunnel` for the tunnel, `session` for the session, each
  declared in the declaration language under `internal/model/builtin/` and
  carried in the binary. A family that has the layer's tier file imports
  that family, with no `imports` line, and there is no injection mechanism
  at all ([a tier is a built-in
  family](../decisions/a-tier-is-a-built-in-family.md), [the
  declaration](../declaration/families.md#what-a-tier-brings)). The protocol
  tier's built-in is *carried* — `duplex.Envelope` and `duplex.Handle` are
  the carrying family's own types, since a family's envelope is a message of
  that family — while the session's vocabulary is one declaration for every
  family and reaches a family's generated code as a side that extends the
  built-in `session` family's. No family declares any of it by hand, and one
  that names a built-in in `imports`, or declares a type it carries, is
  refused. The observer then labels a layer's operations with the family, as
  any operation.
- **Never logged as the family's.** A session's log holds the family's
  frames; the session's own frames are state, not messages, and replay
  never reports them stale.
- **Never across the layer's boundary.** A `session.` frame sent by a
  machine is refused by the relay, and `session.control` and
  `session.cursor` are produced by the relay and never forwarded up from
  anyone. The vocabulary belongs to the layer that defined it.

## How a change to the wire is made

Directly and whole, in one lane — both peers, both validators, the tables,
the generator and the docs, emitting what they accept in the same commit —
because there is nothing to stay compatible with, and a design is chosen
for being right rather than for being cheap to roll out. That working
condition, and why the boundary is settled now rather than later, is
[COLLABORATION.md](../../COLLABORATION.md)'s; this page is rewritten with
the model, not kept stable against it.
