# Envelope members are what the peer acts on

**The question.** A layer above the peer needs to say something new on the
wire — a sequence a consumer resumes by, a frame that is the session's and
not the family's. Does it go in the profile's envelope?

**Decided.** A member belongs in the envelope when, and only when, the peer
acts on it — correlates, cancels, dispatches or propagates by it. Everything
else is a layer's own vocabulary, carried as ordinary frames under the
layer's reserved prefix, or a header the peer delivers and reads nothing
into. There are four kinds and there is no fifth: a kind is what the peer
correlates by, and it correlates four ways. The whole test, applied in
order, is [how a layer speaks](../wire/vocabulary.md#the-test).

**Why.** Two wrong answers were given in one afternoon. A `sequence` member
went into the envelope because a member was cheap; but the peer never sees
a sequence — the session's log assigns it and the consumer resumes by it —
so the profile would have carried, and every runtime validated, a fact that
was one layer's business, forever, in every language. A fifth kind, "a
session frame", was tried next; it would have made every peer dispatch on
something only a relay produces. Both are the same mistake: the envelope is
a promise every runtime keeps, and a member in it costs one validator per
language for as long as the profile lives. The set stays small because the
profile refuses what it does not know, and it refuses what it does not know
precisely so that the set stays small.

**Serves.** Layering — each layer speaks the one beneath and knows nothing
of those above; a thing on the wire belongs to exactly one layer.

**Since.** v2 of the declaration and the session's first vocabulary, when
`session.cursor` replaced the sequence member.
