# Nightseam implements the foundation and adopts the Bitwire contract

**Revised, 2026-09-21.** The earlier blanket rejection of a separate Wire
contract repository is superseded by the operator's decision in
[#421](https://github.com/Bitspark/nightseam/issues/421). Nightseam adopts the
public Bitwire v0.2.0 contract as required 0.6.0 delivery. The
[adoption evidence](../../conformance/bitwire/README.md) records exact public
coordinates, provenance, paired types and executable behavioral coverage.

**The question.** Where the adopted declaration, typed-access and authority
mechanisms live, and whether separating their responsibilities requires
separating their repositories.

**Decided.** [Bitwire](https://github.com/Bitspark/bitwire) is the selected home
for the shared Wire access contract, its supporting language declarations,
composition laws and independent conformance criteria. Nightseam retains its
runtime implementations, carriers, peers, tunnels, live-reference machinery,
generator, declaration/type model, canonical and applied contract identity,
and selected optional rooted-grant authentication profile.

The public handover is Bitwire v0.2.0 at revision
`616a2fc5e3a0972f67f40331a9d9ca102bc9698d`, maintained by Bitspark under Apache-2.0.
Its 0.2 delivery separates addressed delivery from dispatch: send-only `Wire`,
receiving and closing `Endpoint`, and no registration policy in the primitive.
Nightseam's realization of that, and of the public invocation lifecycle it
requires, is [an invocation is a
Wire](an-invocation-is-a-wire-and-routing-is-composed-above-it.md).
The operator clarified in [#468](https://github.com/Bitspark/nightseam/issues/468)
that all eight ports must use Bitwire declarations directly. Nightseam removes
its aliases, re-exports and duplicate declarations; generated Go and TypeScript
adapters name the upstream types. The common contract's published behavioral
cases exercise the migrated runtimes; Nightseam's full suite retains profile,
context, carrier and scoped-reference obligations. A second runtime or completed
Bitlink generator is not required.

[#421](https://github.com/Bitspark/nightseam/issues/421) holds this readiness and
adoption work in 0.6.0 under [#320](https://github.com/Bitspark/nightseam/issues/320).
Its acceptance includes public clean-consumer installation and the combined
generic evidence, beyond this decision record. No finished 0.7.0 authentication
dependency is introduced.

This is a shared-contract extraction, not the older proposal to move the entire
runtime into Bitwire. The existing profile remains `nightseam.duplex/1`. Sharing
the access interface still requires agreement on operation paths, value encoding,
contract identity and live-reference rules for adapters to interoperate.

This explicitly admits the reusable authority profile: checking evidence
against consumer-supplied trust and preserving guards at invocation. It does
not admit application resource models, privileged issuance choices or
current access policy. A directory called `auth` is not an admission argument.
The dependency direction keeps bare data and RPC usable without auth imports
or configuration. Within auth, grant verification is usable without a
connection; bootstrap composes it, and invocation adapters compose both with
Wire. Concrete packaging and public dependency provenance are held by
[#336](https://github.com/Bitspark/nightseam/issues/336).

Wire remains Nightseam's native public access surface, with its shared contract
adopted from Bitwire. Multiple implementations may satisfy it; none may
erase scoped reference state, ownership, release barriers or import checks to
make the interface smaller.
An abstract send with no result refines bounded admission, not delivery or a
business result. Correlation remains the peer's responsibility; a `void`
signature does not remove it or weaken acceptance bounds
([#290](https://github.com/Bitspark/nightseam/issues/290)).

The canonical declaration representation remains independent of target-language
output and incidental runtime encoding, inside Nightseam. Adopting the access
contract does not extract the type model or require a second backend, universal
backend framework or cross-backend bridge. Nominal path and structural digest
remain distinct; neither substitutes for live scope or authority.

Consumers choose trusted roots, actions, resource meanings, issued authority
and current policy. The resource owner supplies and enforces these facts at
the point of effect or disclosure. The selected authority profile supports
rooted, attenuated grants with explicit finite or unbounded validity. It
does not add a general proof engine or revocation/status subsystem. Its
signed bytes, possession proofs, bootstrap transcript and time rules need
their own accepted contracts and shared conformance evidence.

Public specifications, documentation, examples and installation must be
self-contained. Examples use synthetic public data. A private checkout is
neither a normative source nor an installation prerequisite. Reusing an
identity mechanism requires an explicit public dependency and a provenance,
license and maintenance decision, or a reviewed minimal extraction with the
same accounting. This decision grants no permission to disclose private
source and does not justify parallel, ungoverned crypto implementations.

This records an adopted direction, not completed delivery. The typed-access
work is tracked in [#320](https://github.com/Bitspark/nightseam/issues/320),
the generic baseline in [#365](https://github.com/Bitspark/nightseam/issues/365),
and optional auth in [#345](https://github.com/Bitspark/nightseam/issues/345).
The state pages and tested public packages describe what is available now.

**Why.** The access contract can be stated and checked independently of a runtime,
generator or consumer model. Bitwire gives that common boundary one owner and
versioned definition for Nightseam and other consumers. Its laws and observed
behavior establish composability; a repository split alone does not. Nightseam's
implementation and optional packages retain their own responsibilities, and bare
data/RPC remains independent of auth. No general policy framework is admitted.

**Serves.** [Boundary](../goals/boundary.md), [layering](../goals/layering.md),
[composability](../goals/composability.md) and
[agnosticism](../goals/agnosticism.md).

**Since.** The initial repository-home clarification on 2026-09-21 was recorded in
[#381](https://github.com/Bitspark/nightseam/issues/381) and
[#336](https://github.com/Bitspark/nightseam/issues/336). The subsequent operator
decision in [#421](https://github.com/Bitspark/nightseam/issues/421), documented by
[#422](https://github.com/Bitspark/nightseam/issues/422), revises the shared Wire
contract's home while preserving in-Nightseam runtime, declaration/identity,
generator and optional-auth ownership. Accepted technical verdicts for identity,
generic bindings and Wire remain in force.
