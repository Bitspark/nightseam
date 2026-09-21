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
{"binding": "9f2c4ab11e07d3a5.3", "contract": "probe/Report", "digest": "4433469c3fb5e66b667a7b4463cb878ab59d214bb1f9163e92bc2005b9987cc3"}
```

Each export mints a separate binding, including repeated exports of the same
native function. Repeated imports of one binding are aliases of one attachment,
not separately owned leases. A record of callable references has no additional
wire identity, equality, shared lifetime or atomic record-wide release.

`binding` is opaque and the exporting side's to mint; `contract` names the
declaration the callable was declared at, `family/Type`. The runtime compares
that string and never parses it: what makes a declaration produce one is the
callable kind of
[#201's verdict](https://github.com/Bitspark/nightseam/issues/201#issuecomment-5745189544),
which the declaration tier renders ([a family in
tiers](../declaration/families.md#livejson)) and which is not this layer's. Both members are
compared before an invocation is dispatched, and neither is an authorization:
the layer proves *which binding of which contract*, never *who may call it*.

The declaration identity is its path and generated digest. Different paths
are refused even for identical signatures; renaming or moving a declaration
changes the contract. The optional `digest` is exactly 64 lowercase SHA-256
hex characters. Import refuses `contract_mismatch`, naming the contract,
when the reference and expected declaration both carry nonempty digests
and they differ. It does so before allocating an attachment or interpreting
an invocation. A malformed present digest, including an empty string or null,
is `contract_invalid`; absence makes no revision claim and does not itself
cause a mismatch. The digest is declaration identity, not authorization or
proof of a remote endpoint's behavior.

Generated native function aliases
remain assignable by signature, and the exporter supplies the destination
contract; it does not infer semantic identity from the function. The
[callable decision](../decisions/a-callable-is-a-declared-kind.md#native-assignment-and-contract-evolution)
states this boundary and the evidence in both languages.

A binding id carries the exporting scope's nonce, minted at random when that
scope is made. Invocation resolves the complete id in that scope's export
table; fresh nonces keep counter reuse on another connection from identifying
an unrelated binding. Reconnection makes a new scope; nothing is revived and
nothing is replayed.

An application can carry its own persistent identity or resume cursor as data
and reacquire temporary bindings on the new connection. That is an application
protocol, with its own persistence and recovery rules; removing tunnel `after`
does not prohibit application cursors or add replay to the live layer.

The descriptor is serializable, and public `Decode` / `decode` accepts
caller-supplied bytes without proving inbound-message provenance. Bytes from a
live binding can be decoded, imported and invoked on its original connection.
Bytes from an ended connection can also decode and import on a new one, but the
new exporting scope refuses their invocation as `reference_unknown`, including
when it already holds fresh bindings. A native reference object associated with
another scope is instead refused locally at import as `reference_foreign`.
The [surface and paired evidence](../runtime/live.md#native-references-and-serialized-bytes)
distinguish those checks; none establishes caller authorization.

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
too and says nothing back. Release is idempotent and invalidates every existing
alias in each scope when that scope processes the release. It is not reference
counting: another alias does not keep the binding alive. There is no
acknowledgment, so the sender's return is not evidence that the remote side has
processed the event, nor a synchronized global revocation point. Both runtimes
keep the local release even if sending the event fails.

A timeout, cancellation or error response does not prove that a carried reference was never delivered; local proof of an unqueued send is not a wire field or acknowledgment, and uncertain publication retains bindings under their existing owner until release or scope end.

## What is refused, and with what

Every refusal is a public error of `live.invoke`, or is raised where the caller
stands before a frame is sent. The same eight codes in every language:

| code | when |
| --- | --- |
| `contract_invalid` | an export or an import of no contract, a malformed digest, or an invocation naming neither a binding nor a contract |
| `contract_mismatch` | the reference carries another contract or a differing nonempty declaration digest, or conflicts with an already held binding's identity |
| `reference_unknown` | invocation found no export of that id in the receiving scope — a token of an ended connection among them |
| `reference_foreign` | a native reference object associated with another scope, refused locally before a frame is sent; serialized bytes are decoded separately |
| `reference_released` | an invocation of a binding that was released |
| `scope_closed` | anything at all after the scope ended |
| `too_many_exports`, `too_many_imports` | the scope's bounds |

## Release is a barrier; closure is not

After a scope processes release, it refuses subsequent use of that binding.
Implementations already dispatched may finish and deliver their results; a
request merely sent is not guaranteed to have reached that point before release.
Release cancels no invocation or application work, and releases no other binding,
including separately exported functions previously returned by the released
function. Record members that alias the same binding share its invalidation.

Closing the scope, or the connection carrying it, settles its calls in flight
and signals cancellation to their implementations. Neither release nor closure
rolls back an effect an invocation already had. Application cancellation is
another contract: a `Job.cancel()` that an application declares is an ordinary
callable. The [lifetime table and named evidence](../runtime/live.md#the-rules-a-consumer-can-rely-on)
distinguish calls, bindings, aliases, records and connections in both runtimes.

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
