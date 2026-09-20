# The live layer on the wire

A callable value crossing a connection: what names one, what invokes one and
what ends one. The layer speaks as a layer speaks — a reserved prefix, `live.`,
and ordinary frames of the profile beneath, exactly as the tunnel speaks
`channel.open` ([how a layer speaks](vocabulary.md)). Nothing of it reaches the
envelope, no frame kind is added, and the peer acts on nothing it did not act on
before. This page is what crosses; the surface in both languages is
[the live layer](../runtime/live.md).

## A binding, a scope and a reference

A **binding** is one function made addressable from the other side of one
connection. A **scope** is the live layer over one peer: what this side has
exported over that connection, what it has imported over it, and nothing that
outlives it. A **reference** names a binding of a scope, and travels as an
ordinary value of whatever message holds it:

```json
{"binding": "9f2c4ab11e07d3a5.3", "contract": "probe/Report"}
```

`binding` is opaque and the exporting side's to mint; `contract` names the
declaration the callable was declared at, `family/Type`. The runtime compares
that string and never parses it: what makes a declaration produce one is the
callable kind of
[#201's verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544),
which the declaration tier renders ([a family in
tiers](../declaration/families.md#livejson)) and which is not this layer's. Both members are
compared before an invocation is dispatched, and neither is an authorization:
the layer proves *which binding of which contract*, never *who may call it*.

Contract equality is equality of that declaration path, not a signature
comparison. Different paths are refused even for identical signatures;
renaming or moving a declaration changes the contract. The descriptor has
no signature fingerprint or version, so an unchanged path does not prove
compatibility after a signature change. Generated native function aliases
remain assignable by signature, and the exporter supplies the destination
contract; it does not infer semantic identity from the function. The
[callable decision](../decisions/a-callable-is-a-declared-kind.md#native-assignment-and-contract-evolution)
states this boundary and the evidence in both languages.

A binding id carries the scope's own nonce, minted at random when the scope is
made. That is what makes a reference of one connection meaningless on another:
the nonce of a scope that has ended is not the nonce of the one that follows, so
a token carried across resolves nowhere rather than resolving to whatever binding
happens to hold that position now. Reconnection makes a new scope; nothing is
revived and nothing is replayed.

## The two operations

**`live.invoke`** — a request, from the side holding a reference to the side
that exported it:

```json
{"binding": "9f2c4ab11e07d3a5.3", "contract": "probe/Report", "request": 50}
```

The result is the callable's own, and its declared errors come back as the
public errors of this request. `request` is absent for a callable that takes
nothing, and a callable that returns nothing answers `null`.

It is an ordinary request, so everything the profile already does for a request
it does for an invocation: it is correlated by `id`, bounded by
`max_pending_requests` and `max_concurrent_handlers`, carries a trace, and is
withdrawn by the profile's own `cancel`. **That is why cancelling an invocation
and releasing a binding stay two things without any work: they are two different
frames.**

**`live.release`** — an event, from either side:

```json
{"binding": "9f2c4ab11e07d3a5.3"}
```

The side that sends it has already released; the side that receives it releases
too and says nothing back. Release is idempotent and reaches every alias of the
binding at once.

## What is refused, and with what

Every refusal is a public error of `live.invoke`, or is raised where the caller
stands before a frame is sent. The same eight codes in every language:

| code | when |
| --- | --- |
| `contract_invalid` | an export or an import of no contract, or an invocation naming neither a binding nor a contract |
| `contract_mismatch` | the reference carries one contract where another is expected, or names a binding exported for another |
| `reference_unknown` | no binding of that id in this scope — a token of an ended connection among them |
| `reference_foreign` | a reference minted in another scope, refused here before it reaches the wire |
| `reference_released` | an invocation of a binding that was released |
| `scope_closed` | anything at all after the scope ended |
| `too_many_exports`, `too_many_imports` | the scope's bounds |

## Release is a barrier; closure is not

Releasing a binding refuses the **next** invocation of it and leaves the ones
already dispatched to settle and be delivered: a release is a statement about the
reference, not about work already asked for. Closing the scope, or the connection
carrying it, is the harder stop — every invocation in flight is settled at once.
Neither rolls back an effect an invocation already had, and neither is an
application's own cancellation: a `Job.cancel()` that an application declares is
an ordinary callable, and this layer has never heard of it.

## Why this and not a channel each

A binding could have been a tunnel channel: the tunnel already opens one per
family and a channel is a connection of the seam. It is the heavier of the two.
It would impose a tunnel on every peer that might *receive* a live value, since a
live value can sit in any family's payload; it would put a `channel.open` round
trip inside the encode path of every call that carries a callable, with a credit
window and an accept-queue slot behind every callback; and it has nowhere to put
the contract, since the only member of `channel.open` that could carry
`probe/Report` is `family`, which would make "family" stop meaning a family.

A callable is one unary function, and the smallest existing mechanism that
invokes one is the peer's own request. What this layer adds is a name, not a
mechanism — which is the whole of what
[the vocabulary test](vocabulary.md#the-test) admits at step 2, and the same
answer it gives the tunnel. The full argument, and the operator's verdicts on
scope, release and stale tokens, are on
[#202](https://github.com/Bitspark/nightseam/issues/202).
