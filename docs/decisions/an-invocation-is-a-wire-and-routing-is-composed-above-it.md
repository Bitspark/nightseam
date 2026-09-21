# An invocation is a Wire, and routing is composed above it

This page records two verdicts, because one reason covers both: what the
access contract owes, and what an admitted request owes. They are Nightseam's
realization of Bitwire [ADR0002](https://github.com/Bitspark/bitwire/blob/616a2fc5e3a0972f67f40331a9d9ca102bc9698d/docs/decisions/0002-delivery-dispatch-and-ownership.md)
and [ADR0003](https://github.com/Bitspark/bitwire/blob/7edac41d964a24100c801e2efc2b6efb775dfd97/docs/decisions/0003-public-invocation-lifecycle.md).

**The question.** `Receive(path, receiver)` made every Wire implementation
carry exact matching, a namespace flag, longest-prefix selection and
duplicate-registration refusal — routing policy, in the primitive every carrier
must implement. And routing policy alone cannot say when an admitted request is
finished: a receiver that returns has not necessarily finished, and its
response goes straight through the return capability, past the router. A
dispatcher that guesses either loses cancellation or keeps captures forever.
Nightseam knew both answers privately, by recognizing its own concrete types.
What does it owe publicly?

**Decided.** Two things, adopted together.

*Delivery is separated from dispatch.* `Wire` grants send access. `Endpoint`
adds one owning receive attachment and closure. A second attachment is refused
without replacing the first; detach is idempotent and permits a later one;
there is no implicit broadcast; the delivered path is relative to the
endpoint's origin. Handler registration, exact and prefix matching, overlap
precedence and duplicate-path refusal live in **one reusable Nightseam
dispatcher composed above the primitive**, whose root attachment the runtime
and the generated binders share. Sibling and nested selected views are views of
that one owner, not competing claims on the root. A value that only sends takes
`Wire`; `Endpoint` is required only where attachment or closure is genuinely
needed.

*An admitted request's return capability is the invocation, presented as a
Wire.* Its empty path carries the outcome, as it always has. Its other paths
carry the lifecycle — capture, ready, release, begin, done, control — as
ordinary events of the profile, under the `invocation.` prefix. Admission is
the answer, since `Send` returns on admission or refusal; the participant mints
the identifier, so no operation needs a return value. Queue admission,
invocation admission, caller withdrawal, a fixed outcome, body completion, the
admitted-control drain and retirement are distinct states of that one
lifecycle. Captures and leases are bounded per invocation, as totals. A
dispatcher refuses a request whose return capability carries no lifecycle
rather than routing it with weaker guarantees; generic addressed delivery
remains usable without the facility. The whole surface is
[relative-path wires](../runtime/wire.md).

**Why.** Both answers are the same answer: *the thing that already exists is
the right unit.*

Sending supplies a destination and receiving observes one — that is all a
carrier can promise, and it is all every carrier should have to implement. A
generated dispatcher, a dynamic map lookup and a forwarder do not share a
registration abstraction; they share an addressed message. Moving policy up
makes the common boundary smaller without making anything harder: the root
namespace receiver already permitted forwarding, so this removes an obligation
rather than adding a capability.

And an invocation is already a thing you address messages at, over a
correlation, with the profile's four kinds. That is a Wire. The return
capability *is* the invocation — it is what the outcome is sent to, what
carries the runtime's context, and what survives forwarding unchanged. Giving
it the rest of the lifecycle adds no primitive, no second native interface and
no type to recognize: a participant needs the Wire it was handed and six
agreed path names. This is [how a layer speaks](../wire/vocabulary.md#a-layers-own-vocabulary),
for the fourth time — `channel.` for the tunnel, `live.` for the live layer,
`identity.` for declaration agreement — and it is decided by rule 2 of that
test: one layer produces it, another reads it, the peer only forwards. The
verbs never reach a peer root and never cross a physical hop, so unlike those
three they take no built-in family and reserve no namespace there; a return
capability's path space has no other claimant.

What the protocol form gives up, named rather than hidden: it does not make
the verbs unforgeable with respect to each other. One capability carries all
of them, so a participant is restrained by what it was handed rather than by
what it can be cast to. The native-interface alternative does not establish
that either — its facades are both resolved from the same message — and
holding a return capability is already full authority over its invocation,
since it can settle it outright. The protocol declines to invent a boundary
rather than weakening one that existed. Its other cost is that a non-participant
is detected by refusal rather than by type: a return capability that accepts a
path it does not implement is a broken implementation, in exactly the way a
peer that accepts a frame it does not know is.

Two independently authored endpoint integrations — one composing the public
`Invocation`, one answering the vocabulary out of its own ledger — plus an
opaque forwarding wrapper, exercise this through public facilities alone, with
no shared private ledger and no concrete type recognized on either side. That
experiment is what settled the representation, and it is why no change to
Bitwire's published declarations was needed.

**Serves.** [Composability](../goals/composability.md) — one seam, not an
adapter per pair, and a part that behaves the same in every assembly;
[layering](../goals/layering.md) — the lifecycle is a layer's own vocabulary
carried as the ordinary frames of the one beneath, which knows nothing of it;
and [the boundary](../goals/boundary.md) — what every carrier owes is now the
smallest thing that can be stated, and everything else is a composition above
it.

**Since.** #439, the 0.6.0 adoption of the Bitwire contract at v0.2.0, under
[#421](https://github.com/Bitspark/nightseam/issues/421) and the epic
[#320](https://github.com/Bitspark/nightseam/issues/320). It replaces in place
the registration primitive `Receive(path, receiver)` and `Receiver.Namespace`
of v0.1.0, which no compatibility layer retains. The identity discipline the
lifecycle relies on at a carrier boundary is [request serials increase in
publication order](request-serials-increase-in-publication-order.md).
