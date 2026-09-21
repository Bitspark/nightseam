# How a layer speaks on the wire

Nightseam is a stack: the transport carries frames, the profile correlates
them, and the tunnel multiplexes channels over one peer. A prepared channel,
a peer and a local endpoint expose the same relative-path
[Wire](../runtime/wire.md). A layer takes that interface without asking
what carries it. Each layer speaks the one beneath; none knows the ones
above. This page says what that means for the question that comes up
whenever a layer needs to say something new on the wire: **where does it
go?** It was written after two wrong answers in one day, so that the third
person to ask reads the test rather than repeating them.

## The test

This is the wire's instance of the general test for what is Nightseam's,
[admitting a concept](../admission.md): a member the peer acts on is a
primitive of the profile, and a layer's own vocabulary is a composition of
it. A member belongs in the profile's envelope when, and only when, **the peer
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
   knows nothing of channels. Anything a later layer must say on the wire
   is the same kind of thing.
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

Two things that look like members and are not — a sequence a layer above
assigns and the peer never sees, and a fifth kind, "a frame of that layer",
which is an ordinary event of that layer's vocabulary — were each tried for
an afternoon; [envelope members are what the peer acts
on](../decisions/envelope-members-are-what-the-peer-acts-on.md) is the
record, and [`meta` is a header, not a
member](../decisions/meta-is-a-header-not-a-member.md) the one header's.

## A layer's own vocabulary

A Wire path is an array of opaque Unicode scalar strings. At a physical
peer boundary its canonical encoding occupies the existing `method` or
`event` member: each segment is its UTF-8 byte length, a colon, and the
segment. Thus `["space", "read"]` becomes `5:space4:read`. Empty segments,
slashes, dots and different Unicode spellings retain their identities.
There is no new envelope member and no normalization. Selection and
mounting transform this routing address; they establish neither resource
ancestry nor permission. The receiver's verified context is delivered
beside the frame and is never inferred from its path or metadata.

A layer that speaks on the wire does it as the tunnel does:

- **A reserved prefix**, one per layer: `channel.` for the tunnel, `live.`
  for the live layer, `identity.` for declaration agreement at interpretation
  and `auth.` for [the authenticated connection](../auth/connection.md).
  The namespace is the layer's, so that it is never
  contested, and a layer that takes one is what makes the generator refuse a
  consumer's operation under it. Each target also holds its own identifiers
  under `cmd/nightseam/testdata/reserved`.
- **Ordinary frames of the profile.** A layer's request is a request, its
  event an event, minted, correlated and cancelled by the peer like any
  other. The layer registers its handlers on the peer it runs over — the
  tunnel's `channel.open` handler — and the peer dispatches by name and
  knows nothing of what the name means.
- **Imported, implicitly.** A layer's vocabulary is a **family**: `duplex`
  for the profile, `tunnel` for the tunnel, each declared in the declaration
  language under `internal/model/builtin/` and carried in the binary. A
  family that has the layer's tier file carries that family's types, with no
  `imports` line, and there is no injection mechanism at all ([a tier is a
  built-in family](../decisions/a-tier-is-a-built-in-family.md), [the
  declaration](../declaration/families.md#what-a-tier-brings)). The protocol
  tier's built-in is carried — `duplex.Envelope` and `duplex.Handle` are the
  carrying family's own types, since a family's envelope is a message of
  that family. No family declares any of it by hand, and one that names a
  built-in in `imports`, or declares a type it carries, is refused. The
  observer then labels a layer's operations with the family, as any
  operation.
- **Never across the layer's boundary.** A layer's own frames are produced
  by that layer and forwarded up from nobody. The vocabulary belongs to the
  layer that defined it.

There is one vocabulary that is spoken somewhere other than a peer root: the
invocation's. An admitted request's return capability is the invocation,
presented as a Wire — the empty path is its outcome, and `invocation.capture`,
`invocation.ready`, `invocation.release`, `invocation.begin`, `invocation.done`
and `invocation.control` are its lifecycle, as ordinary events. It is the same
shape by the same rule: one layer produces it, another reads it, the peer only
forwards. It differs in one thing only, and the difference is why it takes no
built-in family: those operations never reach a peer root and never cross a
physical hop, so no consumer's operation can collide with them, and a return
capability's path space has no other claimant. The reasoning is [an invocation
is a Wire](../decisions/an-invocation-is-a-wire-and-routing-is-composed-above-it.md),
and the surface is [relative-path wires](../runtime/wire.md#the-invocation-lifecycle).

## How a change to the wire is made

Directly and whole, in one lane — both peers, both validators, the tables,
the generator and the docs, emitting what they accept in the same commit —
because there is nothing to stay compatible with, and a design is chosen
for being right rather than for being cheap to roll out. That working
condition, and why the boundary is settled now rather than later, is
[COLLABORATION.md](../../COLLABORATION.md)'s; this page is rewritten with
the model, not kept stable against it.
