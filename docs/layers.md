# What belongs where: the profile, the layers over it, and the test that decides

Nightseam is a stack: the seam carries frames, the profile correlates them,
the tunnel multiplexes channels over one peer, the session governs a
conversation over a tunnel's channels — or, since #48, over any connection
of the seam. Each layer is built on the one beneath and speaks it; none
knows the ones above. This page says what that means for the one question
that comes up whenever a layer needs to say something new on the wire:
**where does it go?** It was written after two wrong answers in one day, so
that the third person to ask reads the test rather than repeating them.

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
   same kind of thing (`docs/session.md`).
3. **Is it a consumer's fact about a call, with no layer to carry it?**
   Then it is a **header**: a member on the request or event it is about,
   which the peer delivers to the handler beside the payload and reads
   nothing into. There is one, `meta` (#49) — a flat string map, validated
   by form alone, delivered as `MetaFrom(ctx)` in Go and the handler's
   context in TypeScript, so that the peer does act on it, in the one way a
   header is acted on: it reaches the handler without being the payload.
   That is what keeps it on the right side of the test, and why a second
   header should be a key of `meta` and not a member of its own. It is also
   why a header does not propagate: delivering is the peer's one action on
   it, and a header is a fact about *this* call — a credential, a tenant —
   not about the calls a handler makes in turn, which is the opposite of a
   trace. A handler that means to pass one on says so (`docs/profile.md`).

Two things that look like members and are not:

- **A sequence.** The session's log assigns it and the consumer resumes by
  it; the peer never sees it. It is the session's vocabulary (`session.cursor`),
  not an envelope member, however cheap a member would have been.
- **A fifth kind.** A frame of a layer above the peer — "a session frame" —
  is an ordinary event or request of that layer's vocabulary. A kind is what
  the peer correlates by, and the peer correlates four ways.

## A layer's own vocabulary

A layer that speaks on the wire does it as the tunnel does:

- **A reserved prefix**, one per layer: `channel.` for the tunnel,
  `session.` for the session. The generator refuses a family that declares a
  method or event under a reserved prefix, so the namespace is never
  contested — a rule #50 lands with the session's first operations; today
  `internal/check` refuses neither, and `cmd/nightseam/testdata/reserved`
  holds each target's identifiers, not yet the prefixes.
- **Ordinary frames of the profile.** A layer's request is a request, its
  event an event, minted, correlated and cancelled by the peer like any
  other. The layer registers its handlers on the peer it runs over (the
  tunnel's `channel.open` handler) or produces them in its own relay (the
  session's `session.control` and `session.cursor`); either way the peer
  dispatches by name and knows nothing of what the name means.
- **Injected, not declared.** Where a layer's vocabulary reaches a family's
  generated code — a session family's client exposing `onControl` — it is
  because the layer's tier *injects* the operations into the family's
  protocol, as the protocol tier injects the `Envelope` and `Handle` types
  (`internal/model/tiers.go`, `Injected()`): a family that has the tier
  carries them, one that does not does not, and no family declares them by
  hand. The observer then labels them with the family, as any operation.
- **Never logged as the family's.** A session's log holds the family's
  frames; the session's own frames are state, not messages, and replay
  never reports them stale.
- **Never across the layer's boundary.** A `session.` frame sent by a
  machine is refused by the relay, and `session.control` and
  `session.cursor` are produced by the relay and never forwarded up from
  anyone. The vocabulary belongs to the layer that defined it.

## Why now, and how a change is made

Nightseam has no released consumer. That is not a caveat but the working
condition: there is nothing to stay compatible with, so a change to the
wire is made *directly* — both peers, both validators, the tables, the
generator and the docs in one lane, emitting what they accept in the same
commit — and a change to a surface changes every caller in the same commit.
No "older runtime" is provided for, no accept-before-emit rollout is
staged, no optional parameter is added to spare a call site. Any of those
would be machinery for consumers that do not exist, kept forever once
written.

The same condition is why the boundary is settled now. The profile has two
runtimes and will have eight; every member of the envelope is a promise all
of them keep. A design is chosen here for being right, never for being
cheap to roll out — which is the opposite of the instinct that put
`sequence` in the envelope for an afternoon. A rewrite today is cheaper
than it will ever be again, and this page is rewritten with the model, not
kept stable against it.
