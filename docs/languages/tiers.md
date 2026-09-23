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
built and how a consumer adopts it. What holding a profile means to a
consumer is one sentence, its description, which `conformance/profiles.json`
declares and this table carries as the data spells it:

| profile | a language that holds it | scenarios | promise |
|---|---|---|---|
| `core` | A peer of this language speaks the wire with a peer of any other: it sends and receives frames, correlates calls with their answers, cancels, closes with a code, applies backpressure, and refuses what the wire validator refuses, held against the reference on both sides of a socket. | the seam (`seam/*`) and the peer (`peer/*`): frames, correlation, cancellation, close codes, backpressure, the wire validator held to `tables/validator.json`, the malformed frames of `tables/frames.json` | P1 |
| `generator` | The generator renders this language: the packages it renders build against the language's runtime, hold the round trip and the generic-versus-bound diagram, and serve as well as call, including values carried over prepared channels and live callables, which is why holding it takes the tunnel and live runtimes too. | `generated/*`: the target renders the corpus, the output builds against the language's runtime, the round trip and the diagram hold; names follow `tables/naming.json`; the scenarios that carry a Cell over a prepared channel or convert callables declare `tunnel` and `live` among their `needs` | P2 |
| `tunnel` | A peer of this language multiplexes channels over one connection, with per-channel credit and closure, and reports the tunnel's events to its observer. | `tunnel/*`: channels over one peer, credit, closure, the tunnel's observer events | P3 |
| `live` | A peer of this language passes callable values across a connection: a scope over the peer exports bindings, imports references, releases and forwards them, and refuses a wrong contract or a stale reference. | `live/*`: a scope over a peer, exported bindings and imported references, repeated import and release, forwarding, and the refusals a wrong contract or a stale reference earns | P3 |
| `observability` | A peer of this language reports what it did, at every layer, under the common event names and never with a payload, and mints and carries trace context across calls. | the scenarios that `needs` `observer` or `propagator`, in any layer: trace propagation, the observer and its no-payload rule, the shipped adapter | P3 |

A scenario's `layer` places it in a profile; `core` is the two lowest
layers, `observability` cuts across them by feature. The runner refuses a
scenario it cannot place, so nothing is ever unclassified. A generated
scenario that runs over the tunnel or live runtime says so in its `needs`,
and the generated testee answers `hello` with the runtimes it links, so a
language whose generator renders neither skips those scenarios with that
reason; the profile it is placed in stays `generator`.

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
there. The runner keeps its skip reason visible, and the matrix keeps its
count. Skips outside the tier's required profiles remain informational;
only failures there spend
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
| TypeScript | Yes, including reverse-call handlers and live conversion | Yes, since #315 |

The client role can handle calls and export callables; that is not a
generated implementation of the declaration's server side, which is why
the two columns are separate and why a target answering `gen.serve` with
`unsupported` is a target with one of them. The runtime `core` and `live`
profiles exercise both languages in both peer roles, separately from the
`generator` profile. Mirroring exchanges driver sides, so a generated
scenario run across the two languages serves from each in turn. The
`generator` cells of [the matrix](../../conformance/matrix.json) record no
skip for either language, and the
[proof inventory](../declaration/proof-findings.md#generated-roles-and-skips)
names which generated operation each pairing exercises.

### What the gate checks

`conformance/profiles.json` names the profiles by the layers and the
features a scenario needs, the tiers by what each requires, the lag it
allows and what a red cell does, and each language's tier. The
runner reads it on every run and reports a **matrix**: one row per language,
one column per profile, each cell passed / skipped / failed with the count,
and the language's tier beside it. Beside it the runner records which
scenarios each cell counts: one outcome per scenario file per language, the
rows a table expands and both orientations collapsed into it, a failure
with the run, pairing and step it happened at, a skip with its reason, and
the commit and CI run the record came from. It is written as
`conformance/results.json` only when the run held the whole suite, it gates
nothing, and CI keeps it as the run's artifact rather than a lane committing
it. The gate is a star: every language
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

A testee that will not build is that same reading one step earlier. Where the
tier's `onFailure` is not `stop`, the build no longer ends the run: the row is
recorded **absent**, naming the testee that failed — runtime or generated —
with its command, its exit status and the last lines it wrote, the language is
marked provisional, its scenarios are skipped with that reason, and the star
continues with every other pairing. The absence is the row's own state and not
a cell's, so it marks the language whatever the cells that did run say, and a
generated testee that fails where the runtime one built leaves that language's
runtime cells standing. A tier-1 or tier-2 testee that will not build still
fails the job, and the nightly matrix fails on every build failure.

The CI star and release gate agree: only a nonempty `Matrix.Blocking` fails
the star, and the release script reads a row's absence and its cells rather
than the verdict stored beside them. A tier-3/4 required-profile failure or
skip stays provisional, and a tier-2 failure outside `core`/`generator` stays
nonblocking for this release. Each remains visible in the matrix and the CI
job summary, beside its counts and disposition. Scenario diagnostics stay in
the verbose test log. Setup, toolchain and testee startup failures still fail
the job. The nightly full matrix fails on every failed scenario and every
required-profile skip, regardless of tier.

The release workflow refuses a tag whose matrix has a cell, or a testee
recorded absent, that the tier table says stops the release, and marks the
languages that the table says are provisional in the release notes. The
matrix of the last run on `main` is rendered into the README. CI checks the
committed table against the committed `conformance/matrix.json` before
running conformance; the new run's matrix, its provisional cells and its
absent testees are published separately as its artifact and job summary.
