# Languages, profiles and tiers

Nightseam exists in more than one language, and a consumer choosing one
needs to know what it is promised there. This page says what: the
**profiles** a language can hold, the **tiers** that say which profiles a
language guarantees and when, and how the conformance suite under
`conformance/` enforces both. `conformance/profiles.json` is the data the
suite reads; this page is what it means. How a language joins is
[onboarding](onboarding.md).

## Four promises

A language runtime can make a consumer four distinct promises, and every
tier is a bundle of them:

| | promise | a consumer can rely on |
|---|---|---|
| **P1 wire** | a peer of this language speaks `nightseam.duplex/1` with a peer of any other | interoperation |
| **P2 generated** | the generator has a target for it; the packages it renders build against the language's runtime and hold the round trip and the generic-versus-bound equivalence | `nightseam generate` for this language |
| **P3 complete** | every component exists — every profile in the table below — and every scenario of the suite passes | never asking "does X exist here" |
| **P4 simultaneous** | a feature lands in this language before it is released, not within a release after | this language is never behind |

The promises nest: P2 assumes P1, P3 assumes P2, P4 assumes P3. There is no
fifth: anything finer than these is progress within a language, which the
matrix shows and no tier needs to name.

## Profiles

A profile is a named set of scenarios, and a scenario belongs to exactly
one. The profiles follow the components, because that is how a language is
built and how a consumer adopts it:

| profile | scenarios | promise |
|---|---|---|
| `core` | the seam (`seam/*`) and the peer (`peer/*`): frames, correlation, cancellation, close codes, backpressure, the wire validator held to `tables/validator.json`, the malformed frames of `tables/frames.json` | P1 |
| `generator` | `generated/*`: the target renders the corpus, the output builds against the language's runtime, the round trip and the diagram hold; names follow `tables/naming.json` | P2 |
| `tunnel` | `tunnel/*`: channels over one peer, credit, closure, the tunnel's observer events | P3 |
| `live` | `live/*`: a scope over a peer, exported bindings and imported references, repeated import and release, forwarding, and the refusals a wrong contract or a stale reference earns | P3 |
| `observability` | the scenarios that `needs` `observer` or `propagator`, in any layer: trace propagation, the observer and its no-payload rule, the shipped adapter | P3 |

A scenario's `layer` places it in a profile; `core` is the two lowest
layers, `observability` cuts across them by feature. The runner refuses a
scenario it cannot place, so nothing is ever unclassified.

The testee protocol is tiered the same way: a testee answers `hello` with
the `layers` and `features` it implements, and a scenario a testee lacks a
need of is skipped, not failed. A language holding `core` alone implements
`conn.*`, `peer.*` and `call.*` and nothing else
([the driver protocol](../../conformance/DRIVER.md)).

## Tiers

A tier says which profiles a language **guarantees** and **when**. The
difference between tiers is not which scenarios run for a language — every
language runs the whole suite, and the matrix shows every cell — but which
red cells stop a release.

| tier | guarantees | lag | a red cell |
|---|---|---|---|
| **1** | every profile | none: a feature is not released until every tier-1 language has it | stops the release |
| **2** | `core` and `generator` always; every other profile within one minor release of tier 1 | one minor release | in `core` or `generator`, stops the release; elsewhere, stops the *next* one |
| **3** | `core` and `generator` | — | in `core` or `generator`, marks the language *provisional* in the matrix; the release ships; elsewhere, informational |
| **4** | `core` | — | in `core`, marks the language provisional; elsewhere, informational |

A skipped scenario in a required profile is a red cell, just like a failure
there. Its skip reason and count stay visible in the matrix. Skips outside
the tier's required profiles remain informational; only failures there spend
tier 2's release lag.

Tier 1 defines the profiles: a scenario is born as a pair of tier-1 twins,
and the Go testee is the reference every other language is held to on both
sides of the wire. Tiers 3 and 4 differ in one yes-or-no fact — whether the
generator has a target for the language — and are presented to a consumer as
one band, *reference-held*, with that fact as a column of the matrix; the
data keeps them apart because the suite gates on the difference.

### The assignment

| tier | languages |
|---|---|
| 1 | Go, TypeScript |
| 2 | Python, Rust, C++, Haskell — the four pilots |
| 3–4 | Java, Swift |

A language is promoted by passing the next tier's gate for one release, and
the promotion is a change to `profiles.json` with the release that makes it.
The assignment is a policy about promises, not a ranking of languages, and
not a forecast of effort: the four pilots are planned for tier 2 because
they are the four that get pushed to every profile first, and two of them —
C++ and Haskell — are there *because* they are the hardest ([tiers are
promises, not rankings](../decisions/tiers-are-promises-not-rankings.md)).
Java and Swift follow at tier 4 and rise as they hold.

## How the suite enforces it

### Generated roles currently exercised

| Target | Generated client | Generated server binding |
|---|---|---|
| Go | Yes, including reverse-call handlers and live conversion | Yes |
| TypeScript | Yes, including reverse-call handlers and live conversion | Yes |

The runtime `core` and `live` profiles exercise both languages in both peer
roles, separately from the `generator` profile. Generated scenarios exercise
the generated clients and server bindings in both languages. Mirroring
exchanges driver sides, so each language must serve as well as call. The
[proof inventory](../declaration/proof-findings.md#generated-roles-and-skips)
records the generated roles and their scenario coverage.

### What the gate checks

`conformance/profiles.json` names the profiles by the layers and the
features a scenario needs, the tiers by what each requires, the lag it
allows and what a red cell does, and each language's tier. The
runner reads it on every run and reports a **matrix**: one row per language,
one column per profile, each cell passed / skipped / failed with the count,
and the language's tier beside it. The gate is a star: every language
against the Go reference on both sides, which is what CI runs; the full
matrix of every language against every other runs nightly, and a failure
there — two non-reference languages disagreeing on something the reference
tolerates — is an issue against the scenario, since the reference decides.

The runtime testee's advertised layers and features are checked against its
tier by `HoldToTier`; the generated layer belongs to a separate testee and
is excluded from that hello check. `Matrix.Verdict` treats both failures and
skips in required profiles according to the tier's failure disposition. The
[`TestVerdictsFollowTheTierTable`](../../conformance/go/profiles_test.go)
test holds this rule at every tier, including a missing required generated
server role. The release script independently applies the same rule to the
matrix's cells; a stored `ok` verdict cannot hide missing coverage.

The release workflow refuses a tag whose matrix has a cell that the tier
table says stops the release, and marks the languages that the table says
are provisional in the release notes. The matrix of the last run on `main`
is rendered into the README, and a table that drifted from
`conformance/matrix.json` fails the pull request that drifted it.
