# The reusable foundation lives in Nightseam

**The question.** Where the adopted declaration, typed-access and authority
mechanisms live, and whether separating their responsibilities requires
separating their repositories.

**Decided.** Nightseam is the specification and implementation home for its
declarations, canonical and applied contract identity, native Wire runtime, generated
language adapters and selected optional rooted-grant authentication profile.
Internal module boundaries do not require a repository split, a separate
shared-type project or an external companion project.

This explicitly admits the reusable authority profile: checking evidence
against consumer-supplied trust and preserving guards at invocation. It does
not admit application resource models, privileged issuance choices or
current access policy. A directory called `auth` is not an admission argument.
The dependency direction keeps bare data and RPC usable without auth imports
or configuration. Within auth, grant verification is usable without a
connection; bootstrap composes it, and invocation adapters compose both with
Wire. Concrete packaging and public dependency provenance are held by
[#336](https://github.com/Bitspark/nightseam/issues/336).

Wire is Nightseam's native public access contract. Multiple internal
implementations may satisfy it; none may erase scoped reference state,
ownership, release barriers or import checks to make the interface smaller.
An abstract send with no result refines bounded admission, not delivery or a
business result. Correlation remains the peer's responsibility; a `void`
signature does not remove it or weaken acceptance bounds
([#290](https://github.com/Bitspark/nightseam/issues/290)).

The canonical declaration representation is independent of target-language
output and incidental runtime encoding, inside Nightseam. This requires no
second backend, universal backend framework or bridge before the current
implementation can land. Nominal path and structural digest remain distinct;
neither substitutes for live scope or authority.

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

**Why.** Splitting the repository would move the boundary without establishing it.
One public specification and conformance suite keep the languages accountable
to the same contracts. Optional packages preserve independent adoption;
explicit responsibility and dependency direction preserve the policy boundary.
Neither public reuse nor optional packaging admits a general policy framework.

**Serves.** [Boundary](../goals/boundary.md), [layering](../goals/layering.md),
[composability](../goals/composability.md) and
[agnosticism](../goals/agnosticism.md).

**Since.** The operator's repository-home clarification on 2026-09-21, recorded in
[#381](https://github.com/Bitspark/nightseam/issues/381) and
[#336](https://github.com/Bitspark/nightseam/issues/336). It supersedes the
external-companion placement proposal without reopening the accepted
technical verdicts for identity, generic bindings or Wire.
