# Research: The language tier ladder — what tiers 3, 2 and 1 should promise

**ID:** 0001
**Date:** 22 September 2026
**Status:** applied
**Run-ID:** run_84bcc7f9-667e-4e83-aad4-18e38ddc1696
**Document-ID:** doc_ad9a7942-4006-404f-9a30-9ba9fb9bf999

## Question

Nightseam is one protocol and runtime implemented in eight programming
languages. A consumer choosing a language needs to know what that language
promises. Today the answer is a **tier**, one of four, each a bundle of
promises enforced by a conformance suite and a release gate. Tier 4, the
lowest, promises only that the language speaks the wire. The operator (the
project's single human decision-maker, described below) considers tier 4
right as it stands.

The question is the three rungs above it. **What should tiers 3, 2 and 1
promise, on which axes, and what should a red cell in each cost?** A red
cell is one entry of the public conformance matrix, the per-language,
per-component table of passed, skipped and failed scenarios, that carries a
failure or a skip the language's tier does not allow. Every term used here
is defined in the Vocabulary section below. The sub-questions:

- Tier 3 differs from tier 4 by one bit that is already a column of the
  public matrix. Is it a rung at all?
- Tier 2 promises "everything, within one minor release of the reference".
  That lag is the only stateful part of the gate, it is enforced in one of
  the two enforcement paths (a JavaScript release script at tag time) and
  not the other (a Go conformance runner in CI), and a minor release
  currently happens every one to two days. Is timeliness an axis a support
  ladder should carry?
- Tier 1 is partly a promise (everything, simultaneously) and partly a
  development obligation (every feature is born with a twin in this
  language). Should the obligation be separated from the promise?
- What should a red cell cost at each rung? Today the three dispositions
  are: refuse the tag, ship and mark the language "provisional", or ignore.
  There is no way to record a known accepted gap, and promotion is written
  down while demotion and removal are not.
- The successor project will replace "language" with "target", so a
  database or a UI framework will sit in the same table as Go and Rust.
  Which of these rungs survive that?

An answer that replaces the rungs with a declared set of promises, or
with nothing above tier 4, is in bounds.

**The stakes.** The tier table is the release gate. It decides which
failures refuse a tag, and it is the one sentence a consumer reads about a
language before adopting it. A ladder with a rung nobody can explain, or an
axis the gate cannot hold, is a promise the project cannot keep. Getting the
model right now is cheap: rewriting the policy file and both enforcers is a
lane of a few hours, six ports are in flight, and no released consumer
depends on the ladder yet. It also matters beyond this repository, because
whatever ladder Nightseam settles on is the one its successor inherits or
rejects.

## Context

### System Overview

**What Nightseam is.** A duplex RPC protocol over a bidirectional byte
stream, a runtime that implements it, a small declaration language in which
a consumer describes their API, and a code generator that renders typed
clients and server bindings from that declaration. "Duplex" means both ends
can initiate calls and emit events over one connection. Above the base
protocol sit three optional components: a **tunnel** (many channels
multiplexed over one connection, with credit-based flow control), **live**
(callable values, so a function can be passed over the wire and invoked
remotely, with explicit ownership and release), and **observability**
(structured events emitted at every layer, trace propagation, and a shipped
OpenTelemetry adapter). An optional **auth** component (identity, and grants
that descend from a root authority and can only be narrowed as they are
delegated) is being built as its own package beside these.

**Languages.** Go is the reference implementation, meaning the one every
other language is tested against. TypeScript is the second full
implementation. Python, Rust, C++, Haskell, Java and Swift, called **the
six ports** below, each have a core runtime that landed in the last week.
All code, all languages and all documentation live in one repository. Every
published package in every language carries one version number; tags move
in lockstep.

**Cadence and horizon.** The repository is pre-1.0 and moves fast. The last
four releases were tagged on 19, 19, 20 and 22 September 2026, so "one
minor release" is currently one to two days of calendar time. No date is
set for 1.0. The next two milestones are auth and the language ports.
Consumers today are hypothetical external adopters plus one internal tool
that ran an early release; there is no installed base whose expectations
the ladder must preserve.

**How the project works.** Autonomous agents do most implementation work in
parallel **lanes**: a lane is one unit of work, roughly a branch and its
pull request, that lands on its own. Adding a language to a feature lane
costs agent hours, not engineer weeks, but it costs them serially: the lane
is not done until every language it must include holds. Design questions
with no written answer are filed as issues and decided by one human
operator; agents may decide on the operator's behalf only where the written
goals and decisions already answer the question. What a language promises
is explicitly listed as a question the operator answers, not an agent. The
operator's stated taste: the beautiful decision recognizes an existing thing
at the right unit and adds nothing; rewrites are cheap before 1.0, so a
design is picked for being right and never for being cheap to roll out; and
this repository exists to push into unknown territory and find where the
model breaks, so a bold failure here is worth more than a cautious success.

**The successor.** A sibling project, bitlink, is a re-implementation of the
same idea generalized from *languages* to *targets*: a target is anything
that renders from the declaration, so `go`, `typescript`, `postgres`,
`react` and a command-line framework sit in one table. A database target
can be held to what the generator renders for it, but it is not a runtime:
it cannot speak the wire, carry a tunnel or export a live value. bitlink is
documentation-only today. It inherits Nightseam's `generator` conformance
profile by name and has no support ladder of any kind.

### Vocabulary

The reader has no access to the codebase, so every term is defined here.

- **Scenario.** One JSON file describing a scripted exchange between two
  parties: a sequence of driver operations (open a connection, send a
  frame, invoke, cancel, expect a close code) with expected outcomes.
  There are 79 files. A scenario may name a **table**, a JSON file of
  cases, and run once per case (**table expansion**); a scenario that does
  not fix which side initiates runs in both directions (**mirroring**).
  After both, the 79 files are 314 runs per pairing of two languages.
- **Layer.** Every scenario declares the layer it exercises: `seam` (the
  raw byte stream and framing), `peer` (the protocol over it: correlation,
  cancellation, close codes, backpressure), `tunnel`, `live`, or
  `generated` (scenarios that drive code the generator produced). The word
  "peer" also names a protocol endpoint in ordinary prose; the layer is
  always written in code font.
- **Need.** A scenario may declare features it needs from the party under
  test: `observer` (the party emits structured events), `propagator` (the
  party carries trace context), `listen` (the party can accept a
  connection), `pipe` (an in-process connected pair), `lazy` (deferred
  reading). Only the first two matter here. The policy file calls these
  `needs`; the Go runner calls them `features`.
- **Profile.** A named set of scenarios: those of certain layers, or those
  with a certain need. There are five: `core` (layers `seam` and `peer`),
  `generator` (layer `generated`), `tunnel`, `live`, and `observability`
  (any scenario that needs `observer` or `propagator`, whatever its layer).
  Every scenario belongs to exactly one profile. A profile by need takes
  precedence over a profile by layer, so a trace scenario at the peer layer
  belongs to `observability`, not `core`. The runner refuses a scenario it
  cannot place. The names `tunnel` and `live` are deliberately shared by a
  component, a layer and a profile; `core` is the only profile whose name
  is not a layer.
- **Generator, generated.** The **generator** is the tool that renders code
  from a declaration, and the `generator` profile holds what it renders. A
  **generator target** is the backend of that tool for one language. The
  `generated` layer and the **generated testee** are what exercise the
  rendered code. The profile holds the rendered code, not the tool.
- **Testee.** A small program, one per language, that the conformance
  runner drives over a line-oriented JSON protocol. It answers `hello` with
  the layers and features it implements. A scenario that needs something
  the testee did not announce is **skipped**, with the reason kept. A
  language has a runtime testee and, once it has a generator target, a
  second generated testee.
- **Held.** A language is held to a scenario when that scenario runs
  against its testee and the outcome lands in its row. "Reference-held" is
  the published name of the two lowest tiers.
- **Reference.** The Go testee. Every other language is held against Go on
  both sides of a real socket: as the party that opens the connection and
  as the party that accepts it. The policy file marks Go with a `reference`
  flag. Being the reference (settling disagreements) is distinct from being
  tier 1 (defining features): TypeScript is tier 1 and not the reference.
- **Twin.** When a feature lands, its scenario is written and made to pass
  in both tier-1 languages at once; the two implementations are twins, and
  the lane is not done until both hold.
- **Star.** The gate CI runs on every pull request: every language paired
  with Go, both orientations. Linear in the number of languages.
- **Full matrix.** Every language paired with every other, run nightly.
  Quadratic. A failure there between two non-reference languages is filed
  as an issue against the scenario, since the reference decides.
- **Matrix, row, cell.** The report of one run. One row per language, one
  column per profile, each cell a count of passed, skipped and failed
  scenarios. The last run's matrix is committed as a JSON file and rendered
  into the README; a stale rendering fails CI.
- **Red cell.** A cell with a failure, or with a skip in a profile the
  language's tier requires.
- **Absent.** A row-level state, not a cell-level one: the language's
  testee would not build this run. The row carries `state: "absent"` and
  `reasons`, a map from testee kind (`runtime`, `generated`) to the build's
  command, exit status and last lines of output.
- **Tier.** A number, 1 to 4, assigned to a language in a policy file. It
  says which profiles the language guarantees, and what a red cell in a
  guaranteed profile does to a release.
- **The four promises, P1 to P4.** The vocabulary the tiers are built from:
  **P1 wire** (interoperates), **P2 generated** (the generator has a target
  for it), **P3 complete** (every component exists and every scenario
  passes), **P4 simultaneous** (a feature lands here before it is released,
  never in a release after). Quoted in full below.
- **Verdict.** What the tier table says of a row: `ok`, `provisional`, or
  `blocking`. A tier's `onFailure` value maps to it: `stop` gives
  `blocking`, `provisional` gives `provisional`.
- **Provisional.** The verdict for a language at a tier whose red cells do
  not stop a release. The release ships and names the language in its
  notes. Both enforcers also return `provisional` for a language with no
  tier at all, which promises nothing; so the one word covers "promised and
  broken, shipped anyway" and "promises nothing yet".
- **Lag, stop-next.** Tier 2 may have a red cell outside its guaranteed
  profiles for one release; the release after is refused if the row still
  has one. `stop-next` is the literal value of the tier's `otherwise` field
  that encodes this. Whether the row was red before is read from the
  previous tag's committed matrix.
- **Promotion.** A language moves up a tier by passing the next tier's
  gate for one release; the promotion is an edit to the policy file with
  the release that makes it.

"Tier" and "profile" each have other meanings in this repository (the files
of the declaration language are also called tiers; the wire protocol and
the auth component are each also called a profile). Here "tier" always
means language-support tier and "profile" always means conformance profile.

### Architectural Context

#### The four promises

The written agreement rests on four promises a language runtime can make to
a consumer. From `docs/languages/tiers.md`:

```markdown
| | promise | a consumer can rely on |
|---|---|---|
| **P1 wire** | a peer of this language speaks `nightseam.duplex/1` with a peer of any other | interoperation |
| **P2 generated** | the generator has a target for it; the packages it renders build against the language's runtime and hold the round trip and the generic-versus-bound equivalence | `nightseam generate` for this language |
| **P3 complete** | every component exists — every profile in the table below — and every scenario of the suite passes | never asking "does X exist here" |
| **P4 simultaneous** | a feature lands in this language before it is released, not within a release after | this language is never behind |

The promises nest: P2 assumes P1, P3 assumes P2, P4 assumes P3. There is no
fifth: anything finer than these is progress within a language, which the
matrix shows and no tier needs to name.
```

The "round trip" is a value encoded by the generated code and decoded back
unchanged. The "generic-versus-bound equivalence", elsewhere called "the
diagram", is that calling through the untyped runtime and through the
generated typed binding produce the same result and the same wire traffic.

#### The five profiles as software

Each profile is a component a consumer adopts, and they are built in this
order. From the same page:

```markdown
| profile | scenarios | promise |
|---|---|---|
| `core` | the seam (`seam/*`) and the peer (`peer/*`): frames, correlation, cancellation, close codes, backpressure, the wire validator held to `tables/validator.json`, the malformed frames of `tables/frames.json` | P1 |
| `generator` | `generated/*`: the target renders the corpus, the output builds against the language's runtime, the round trip and the diagram hold; names follow `tables/naming.json` | P2 |
| `tunnel` | `tunnel/*`: channels over one peer, credit, closure, the tunnel's observer events | P3 |
| `live` | `live/*`: a scope over a peer, exported bindings and imported references, repeated import and release, forwarding, and the refusals a wrong contract or a stale reference earns | P3 |
| `observability` | the scenarios that `needs` `observer` or `propagator`, in any layer: trace propagation, the observer and its no-payload rule, the shipped adapter | P3 |
```

Glosses: the **corpus** is the fixed set of example declarations every
target must render. The **wire validator** checks frames against the
protocol's rules, held to a table of accepted and refused frames. In the
live row, a **scope** owns the callable values one endpoint exports; an
exported value is a **binding**; the remote handle to it is a reference (in
the live sense, not the Go-testee sense); passing that handle on to a third
endpoint is **forwarding**; a **contract** is the declared signature the
binding must match.

Three facts about how the profiles relate matter for a ladder. The first
two are as the documents say; the third is as the code is, and it is the
operative one for everything below.

- **Live runs over a peer and does not require a tunnel.** The onboarding
  page says so explicitly: the order tunnel, then live, is a scheduling
  order, not a dependency. The package graphs in both tier-1 languages
  confirm it: the live package imports only the runtime, the tunnel package
  imports only the runtime and the seam, and there is no edge between them
  in either direction, tests included.
- **The generator profile reaches into live.** The generated scenarios
  include live-dependent cases (callbacks, higher-order functions, owners,
  forwarding), so complete generated coverage cannot be claimed until the
  language's live runtime exists. The onboarding page says so.
- **The generator profile also reaches into the tunnel, and the written
  nesting is inverted in the code.** The generated testee that a language
  supplies once it has a generator target links all four runtime
  components, seam, runtime, tunnel and live, in both tier-1 languages.
  Thirty rows of the generic-composition table are carrier rows; mirrored,
  they are 60 of the generator profile's 168 runs per pairing, and 42 runs
  in that profile cross prepared tunnel channels rather than sockets. So a
  language cannot hold `generator` today without a tunnel and a live
  runtime. The four promises say P2 assumes P1 and P3 assumes P2. The suite
  as built has P2 assuming most of P3. The generator tool itself depends on
  neither tunnel nor live; it is the conformance of what it renders that
  does. **In what follows, treat `generator` as sitting above `tunnel` and
  `live`, because that is where the gate puts it.**

The profiles are very different in size. Counted per pairing, from the
scenario tree, after table expansion and mirroring:

| profile | scenario files | runs per pairing |
|---|---|---|
| core | 21 | 111 |
| generator | 24 | 168 |
| tunnel | 6 | 6 |
| live | 16 | 16 |
| observability | 12 | 13 |
| total | 79 | 314 |

The reference row of the matrix carries these numbers once. A
non-reference language's row counts both orientations against Go, so the
same profiles read core 222, generator 336, tunnel 12, live 32,
observability 26 there. The generator target is more than half of
everything a language is held to, and it is also the component that
requires a whole code renderer in the reference tool, not only a runtime in
the target language.

One more oddity of the cross-cutting profile: `observability` is placed by
need rather than by layer, so no live or generated scenario is in it today,
and it is the only profile where a tier-4 language reads green while
skipping some or most of its scenarios (2 of 26 for three of the ports, 18
of 26 for the other three), because tier 4 requires only `core` and the
skips are informational.

#### The tier table

From `docs/languages/tiers.md`. The four rows date from 19 September 2026.

```markdown
| tier | guarantees | lag | a red cell |
|---|---|---|---|
| **1** | every profile | none: a feature is not released until every tier-1 language has it | stops the release |
| **2** | `core` and `generator` always; every other profile within one minor release of tier 1 | one minor release | in `core` or `generator`, stops the release; elsewhere, stops the *next* one |
| **3** | `core` and `generator` | — | in `core` or `generator`, marks the language *provisional* in the matrix; the release ships; elsewhere, informational |
| **4** | `core` | — | in `core`, marks the language provisional; elsewhere, informational |

A skipped scenario in a required profile is a red cell, just like a failure
there. The runner keeps its skip reason visible, and the matrix keeps its
count. Skips outside the tier's required profiles remain informational;
only failures there spend tier 2's release lag.

Tier 1 defines the profiles: a scenario is born as a pair of tier-1 twins,
and the Go testee is the reference every other language is held to on both
sides of the wire. Tiers 3 and 4 differ in one yes-or-no fact — whether the
generator has a target for the language — and are presented to a consumer as
one band, *reference-held*, with that fact as a column of the matrix; the
data keeps them apart because the suite gates on the difference.
```

And the assignment, from the same page:

```markdown
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
C++ and Haskell — are there *because* they are the hardest.
Java and Swift follow at tier 4 and rise as they hold.
```

A known divergence: this table is the plan. The policy file the gate reads
places all six ports at tier 4 today, with a separate `planned` map
recording the four pilots' target of tier 2. Nothing holds the two tables
together. The page presents the ladder as four gate levels but three
published bands ("reference-held" merges 3 and 4 for the reader, with the
generator column telling them apart).

#### The promotion path and tier 1's special status

The onboarding page, `docs/languages/onboarding.md`, makes tier 1 a
different kind of thing from the rest:

```markdown
When every P3 profile holds for a release, the language may be promoted to
tier 2 under the existing promotion policy.
Tier 1 is a separate decision: a language whose lanes have shipped
simultaneously with Go's and TypeScript's for a sustained period may join
the reference, and thereafter a feature lane includes it too.
```

So tiers 4, 3 and 2 are reached by passing gates. Tier 1 is reached by a
decision, and its consequence is on the development process: every future
feature lane must include the language from the start. Three roles are in
play and the policy file records only two of them: the implementation that
arbitrates disagreement (Go, the `reference` flag), the implementations
obliged to receive every feature at birth (Go and TypeScript, tier 1), and
the implementations promised to hold every component at every tag (also
tier 1 today, and what tier 2 becomes without its lag).

#### Where the ladder is enforced

Two independent enforcement paths read the same policy file and do not share
code:

| | Go path (the conformance runner) | JavaScript path (the release script) |
|---|---|---|
| what it is | a Go test binary; "fails the job" means a test failure | a script run once at tag time |
| runs | on every CI star and every nightly | at tag time |
| reads | the policy file; the live run's outcomes | the policy file; the committed matrix JSON; the previous tag's committed matrix, read through git |
| produces | a verdict per row stored in the matrix; test failures that fail the job | refusal of the tag, provisional notes, lag notes |
| knows about the one-release lag | no; its tier type has no field for it | yes, as a hard-coded boolean |
| trusts the stored verdict | writes it | ignores it, recomputes from cells |

The lag lives between two releases, so only the path that can read a
previous tag can compute it; a test binary has no previous tag to read. The
release script deliberately never trusts the verdict the runner stored; it
re-derives from the cells and the row's absent state.

The whole enforcement chain, in order:

1. The policy file names profiles, tiers and each language's tier.
2. The runner places every scenario in exactly one profile.
3. A testee's `hello` is checked against its tier: announcing less than the
   tier requires kills the run outright ("a skip is what a lower tier is
   for").
4. A scenario the testee lacks a need of is skipped. A skip in a required
   profile is a red cell.
5. A build failure at a non-stopping tier marks the row absent and the
   star continues; at a stopping tier it fails the job.
6. The row's verdict is computed; only rows whose verdict is `blocking`
   fail the CI job. Other red cells are logged and stay in the matrix.
7. Table-driven tests hold the rule at every tier.
8. The release script re-derives the verdict from cells, reads the
   previous tag's matrix for the lag, refuses a matrix missing a row or a
   cell, and refuses the tag on any blocking row.
9. The README table is rendered from the matrix; a stale table fails CI.
10. The nightly full matrix fails on every failure and every required-profile
    skip regardless of tier. The nightly run is tier-blind: the ladder
    governs the release, the nightly governs the truth.

### Relevant Code

The snippets below are evidence about which parts of the model the code
could carry and which it could not. They are not the problem this document
asks about.

#### The policy file, whole

`conformance/profiles.json`:

```json
{
  "$comment": "What each language promises and how the suite holds it; docs/languages/tiers.md says what this means. A profile is a set of scenarios by layer, or by a feature a scenario needs; a tier is what a language guarantees and when. The runner reads this on every run and refuses a scenario no profile places.",
  "profiles": {
    "core": {"layers": ["seam", "peer"], "promise": "wire"},
    "generator": {"layers": ["generated"], "promise": "generated"},
    "tunnel": {"layers": ["tunnel"], "promise": "complete"},
    "live": {"layers": ["live"], "promise": "complete"},
    "observability": {"needs": ["observer", "propagator"], "promise": "complete"}
  },
  "tiers": {
    "1": {"requires": ["core", "generator", "tunnel", "live", "observability"], "lag": 0, "onFailure": "stop"},
    "2": {"requires": ["core", "generator"], "lag": 1, "onFailure": "stop", "otherwise": "stop-next"},
    "3": {"requires": ["core", "generator"], "onFailure": "provisional"},
    "4": {"requires": ["core"], "onFailure": "provisional"}
  },
  "languages": {
    "go": {"tier": 1, "reference": true},
    "typescript": {"tier": 1},
    "haskell": {"tier": 4},
    "cpp": {"tier": 4},
    "swift": {"tier": 4},
    "java": {"tier": 4},
    "python": {"tier": 4},
    "rust": {"tier": 4}
  },
  "planned": {
    "python": 2, "rust": 2, "cpp": 2, "haskell": 2
  }
}
```

Each profile's `promise` names which of P1 to P3 it serves. No profile
carries P4, because simultaneity is a property of a release, not of a
scenario set. The `lag` field is declared, parsed into a struct field, and
read by nothing; the lag is implemented entirely by `otherwise:
"stop-next"`, with the count of one release hard-coded.

The schema that constrains a tier entry, an excerpt of
`conformance/profiles.schema.json`:

```json
"tiers": {
  "type": "object",
  "propertyNames": {"pattern": "^[1-9][0-9]*$"},
  "additionalProperties": {
    "type": "object", "additionalProperties": false, "required": ["requires", "onFailure"],
    "properties": {
      "requires": {"type": "array", "uniqueItems": true, "items": {"type": "string"}, "description": "Profiles a language of this tier holds; each must be declared above."},
      "lag": {"type": "integer", "minimum": 0},
      "onFailure": {"enum": ["stop", "provisional", "stop-next"]},
      "otherwise": {"enum": ["stop", "provisional", "stop-next"]}
    }
  }
}
```

#### The Go verdict

From `conformance/go/profiles.go`. The types the verdict reads. The `Tier`
struct has no field for `otherwise`, so the Go side is structurally blind to
the lag:

```go
// Tier is what a language of it must hold, and what a failure means.
type Tier struct {
	Requires  []string `json:"requires"`
	Lag       int      `json:"lag"`
	OnFailure string   `json:"onFailure"`
}

// Language is a language's place in the tiers.
type Language struct {
	Tier      int  `json:"tier"`
	Reference bool `json:"reference"`
}

type profileCell struct{ Passed, Skipped, Failed int }

// Matrix is the report of one run.
type Matrix struct {
	rows   map[string]map[string]*profileCell // language → profile → counts
	absent map[string]map[string]string       // language → testee kind → build failure
	// tiers and profiles omitted
}
```

The verdict itself. `p.Languages` and `p.Tiers` are the policy file's maps:

```go
// Verdict is what the tier table says of a language's row: ok when every
// cell of a required profile passed without skips or failures; else what the tier's
// onFailure says — blocking, which stops a release; provisional, which
// marks the language in the notes; blocking-next, a tier 2 language's
// second failing release. A row the run marked absent — a testee that would
// not build — reads the same way, whatever the cells beside it say, since a
// green cell cannot stand for a testee nobody could build. A language of no
// tier is never blocking.
func (m *Matrix) Verdict(p *Profiles, language string) string {
	l, ok := p.Languages[language]
	if !ok {
		return "provisional"
	}
	tier, ok := p.Tiers[fmt.Sprint(l.Tier)]
	if !ok {
		return "provisional"
	}
	row := m.rows[language]
	if len(m.absent[language]) > 0 {
		if tier.OnFailure == "stop" {
			return "blocking"
		}
		return "provisional"
	}
	for _, profile := range tier.Requires {
		if cell := row[profile]; cell != nil && (cell.Failed > 0 || cell.Skipped > 0) {
			switch tier.OnFailure {
			case "stop":
				return "blocking"
			case "provisional":
				return "provisional"
			}
			return tier.OnFailure
		}
	}
	return "ok"
}
```

Four rules to read off it: a language not in the policy file is
`provisional`, never blocking, which is how a language enters the matrix
untiered; an absent row short-circuits before cells are looked at; a skip
in a required profile is exactly as red as a failure; a cell in a
non-required profile is never consulted. The `blocking-next` the comment
promises cannot be produced by any input: the fallthrough returns the
`onFailure` string itself, and no tier sets it to `stop-next` anyway.

How the hello check holds a testee to its tier. `p.Requires` is the union
of the layers and needed features of every profile the language's tier
requires; `Hello` is what the testee answered, and `Has` is membership in
its layers or features:

```go
// HoldToTier reports what a testee's hello lacks of what its tier requires:
// nothing for a testee that carries it all, or of a language of no tier.
// The generated layer is a second testee's, held when that one starts.
func (p *Profiles) HoldToTier(language string, h Hello) error {
	layers, features := p.Requires(language)
	var missing []string
	for _, need := range append(layers, features...) {
		if need == "generated" {
			continue
		}
		if !h.Has(need) {
			missing = append(missing, need)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("the %s testee is of tier %d and answers hello without %s, which that tier requires; a skip is what a lower tier is for", language, p.Languages[language].Tier, strings.Join(missing, ", "))
}
```

How one scenario outcome becomes a job result, from
`conformance/go/suite.go`. `a` and `b` are the two testees of the pairing;
`held` is the language whose row this outcome lands in; `s.Placed` maps a
scenario to its profile; `s.Matrix.Blocking` lists the languages whose
verdict is `blocking`; the nightly flag is the `NIGHTSEAM_MATRIX`
environment variable. The reporter is the Go test handle: `Fatalf` fails
the job, `Skip` and `Logf` do not.

```go
type Outcome struct {
	Skipped string
	Failed  *Failure
}

type scenarioReporter interface {
	Helper()
	Skip(...any)
	Fatalf(string, ...any)
	Logf(string, ...any)
}

func (s *Suite) reportOutcome(t scenarioReporter, sc Scenario, a, b, held string, outcome Outcome) {
	t.Helper()
	// Nightly mode applies to star and generated pairings too, including
	// filtered runs: no provisional failure may disappear behind its tier.
	strict := os.Getenv("NIGHTSEAM_MATRIX") != ""
	s.Matrix.Record(held, s.Placed[sc.Key()], outcome)
	if s.Observe != nil {
		s.Observe(sc, a, b, outcome)
	}
	if outcome.Skipped != "" {
		profile := s.Placed[sc.Key()]
		tier := s.Profiles.Tiers[fmt.Sprint(s.Profiles.Languages[held].Tier)]
		if strict && slices.Contains(tier.Requires, profile) {
			t.Fatalf("%s: required %s/%s scenario skipped: %s", sc.File, held, profile, outcome.Skipped)
			return
		}
		t.Skip(outcome.Skipped)
		return
	}
	if outcome.Failed != nil {
		if strict || slices.Contains(s.Matrix.Blocking(s.Profiles), held) {
			t.Fatalf("%s (a: %s, b: %s)\n%v", sc.File, a, b, outcome.Failed)
		} else {
			t.Logf("nonblocking %s/%s failure: %s (a: %s, b: %s)\n%v", held, s.Placed[sc.Key()], sc.File, a, b, outcome.Failed)
		}
	}
}
```

In the default run, a failure fails the job only if the row is already
blocking; a provisional language's failure is logged and kept in the
matrix. Under the nightly flag, everything fails.

The CI job summary, from `conformance/go/summary.go`, is the only place on
the Go side that mentions the lag. This is inside the loop that writes one
note per red cell; `required` says whether the cell's profile is in the
row's tier, `verdict` is the row's verdict, and `disposition` is only a
label in the summary, not a gate decision:

```go
			required := slices.Contains(tier.Requires, profile)
			if cell.Failed == 0 && (!required || cell.Skipped == 0) {
				continue
			}
			disposition := "informational"
			if required {
				disposition = verdict
			} else if tierNumber == 2 {
				disposition = "nonblocking; due by the next minor release"
			}
			notes = append(notes, fmt.Sprintf("- `%s/%s`: %d failed, %d skipped (%s).", language, profile, cell.Failed, cell.Skipped, disposition))
```

#### The JavaScript release gate

From `scripts/matrix.mjs`. This is where the lag actually lives. `matrix`
is the committed matrix, `profiles` the policy file, `previous` the
previous tag's committed matrix or `undefined` when there is none:

```javascript
export function gate(matrix, profiles, previous) {
  const problems = [];
  const provisional = [];
  const lagging = [];
  for (const language of Object.keys(matrix.languages ?? {}).sort()) {
    const row = matrix.languages[language];
    const tier = profiles.tiers?.[String(row.tier)];
    // A language with no tier is in the matrix and promises nothing yet;
    // every cell of its row is informational.
    if (!tier) continue;
    const required = tier.requires ?? [];
    const failed = Object.entries(row.cells ?? {})
      .filter(([, value]) => (value?.failed ?? 0) > 0)
      .map(([name]) => name)
      .sort();
    const inRequired = failed.filter(name => required.includes(name));
    const skippedRequired = Object.entries(row.cells ?? {})
      .filter(([name, value]) => required.includes(name) && (value?.skipped ?? 0) > 0)
      .map(([name]) => name)
      .sort();
    const elsewhere = failed.filter(name => !required.includes(name));
    const incomplete = [];
    if (row.state === "absent") incomplete.push(`${absentTestees(row)} absent — build failed`);
    if (inRequired.length > 0) incomplete.push(`fails ${inRequired.join(", ")}`);
    if (skippedRequired.length > 0) incomplete.push(`skips ${skippedRequired.join(", ")}`);
    if (incomplete.length > 0) {
      const what = `\`${language}\` (tier ${row.tier}) ${incomplete.join(" and ")}`;
      if (tier.onFailure === "stop") problems.push(`${what}, which tier ${row.tier} stops a release for`);
      else if (tier.onFailure === "provisional") provisional.push(`${what}; tier ${row.tier} ships provisional`);
    }
    if (elsewhere.length > 0 && tier.otherwise === "stop-next") {
      const before = wasLagging(previous, language, profiles);
      const what = `\`${language}\` (tier ${row.tier}) fails ${elsewhere.join(", ")}`;
      if (before) problems.push(`${what}, and did at the last release; tier ${row.tier} allows one release of lag and this is the second`);
      else lagging.push(`${what}; tier ${row.tier} allows one release of lag, so the next release refuses it`);
    }
  }
  return { problems, provisional, lagging };
}

/** Which of a row's testees would not build, named as the reasons key them: `runtime`, `generated`, or both. */
function absentTestees(row) {
  const kinds = Object.keys(row.reasons ?? {}).sort();
  return kinds.length ? `${kinds.join(" and ")} testee${kinds.length > 1 ? "s" : ""}` : "testee";
}

/** Whether the last release's matrix had this language failing outside what its tier requires. */
function wasLagging(previous, language, profiles) {
  const row = previous?.languages?.[language];
  if (!row) return false;
  const required = profiles.tiers?.[String(row.tier)]?.requires ?? [];
  return Object.entries(row.cells ?? {}).some(([name, value]) => (value?.failed ?? 0) > 0 && !required.includes(name));
}
```

The one fact here that the prose has not already said: `wasLagging` asks
whether the language failed *anywhere* outside its required profiles at
the last release, not whether the *same* cell is still red. A tier-2
language that failed `tunnel` last release and `live` this release is
refused as a second failure. The previous matrix is obtained by walking
the version tags newest-first and reading `conformance/matrix.json` out of
the first one that carries it; no tags at all means nothing was failing
before. A test in the repository pins all of this against fixture
matrices, and its header records that no real release has yet exercised
the path, because no language has ever been at tier 2.

#### The matrix as data, and as the reader sees it

Two rows from the committed `conformance/matrix.json`, whose top level is
`{"profiles": [...], "languages": {...}}`. Go, the reference, tier 1, all
green:

```json
"go": {
  "tier": 1,
  "verdict": "ok",
  "cells": {
    "core":          {"passed": 111, "skipped": 0, "failed": 0},
    "generator":     {"passed": 168, "skipped": 0, "failed": 0},
    "live":          {"passed": 16,  "skipped": 0, "failed": 0},
    "observability": {"passed": 13,  "skipped": 0, "failed": 0},
    "tunnel":        {"passed": 6,   "skipped": 0, "failed": 0}
  }
}
```

Rust, tier 4, two failures in the one profile it guarantees, and mass skips
everywhere else because its testee announces only the core layers:

```json
"rust": {
  "tier": 4,
  "verdict": "provisional",
  "cells": {
    "core":          {"passed": 220, "skipped": 0,   "failed": 2},
    "generator":     {"passed": 0,   "skipped": 336, "failed": 0},
    "live":          {"passed": 0,   "skipped": 32,  "failed": 0},
    "observability": {"passed": 8,   "skipped": 18,  "failed": 0},
    "tunnel":        {"passed": 0,   "skipped": 12,  "failed": 0}
  }
}
```

The rendered README table, the one sentence a consumer reads:

```markdown
| language | tier | core | generator | tunnel | live | observability | verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `cpp` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | provisional |
| `go` *(reference)* | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |
| `haskell` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | provisional |
| `java` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | provisional |
| `python` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | provisional |
| `rust` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | provisional |
| `swift` | 4 | ✗ 2 failed | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | provisional |
| `typescript` | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |

Planned to rise as they hold: `cpp`, `haskell`, `python`, `rust` at tier 2.
```

The prose beneath it in the README:

```markdown
A language is in Nightseam when it is in this table, and what it promises is
its assigned **tier**: 1 promises every profile with no lag, 2 promises `core` and
`generator` always and every other profile within a minor release, and 3 and
4 are one band — *reference-held* — that the `generator` column tells apart.
```

### Data / Control Flow

**Three kinds of red, one kind of green.** A cell can be red because a
scenario failed, or because a scenario in a required profile was skipped. A
row can be red because its testee did not build. Green means every scenario
in every required profile passed and none skipped. The system deliberately
refuses to let a skip stand as coverage: this was a ruling (issue #271)
after a tier-1 language's matrix read `ok` while it lacked the generated
server side entirely.

**Where the tiers diverge.** Reading the policy file top to bottom, the
tiers differ on exactly three things: the set in `requires`, the value of
`onFailure` (stop or provisional), and whether `otherwise` is present. Tier
1 and tier 2 share `onFailure: stop`; tier 3 and tier 4 share `onFailure:
provisional`. Tier 2 and tier 3 share `requires`. So the ladder is really
two binary axes and one set:

| tier | requires | red in requires | red elsewhere |
|---|---|---|---|
| 1 | all five | stops the tag | (nothing is elsewhere) |
| 2 | core, generator | stops the tag | stops the next tag |
| 3 | core, generator | provisional | nothing |
| 4 | core | provisional | nothing |

**The lag, step by step.** A tier-2 language fails `tunnel` at release N.
The release script finds no failure outside requires at N-1, so it ships N
with a note that the lag has begun. At N+1 the same language still fails
`tunnel`, or fails `live` instead. The script finds a failure outside
requires at N, and refuses N+1. If N+1 had been green, the lag would start
over at N+2. Skips never start the lag; only failures do. At the current
cadence, N to N+1 is one or two days.

**What the code could not carry.** The one-release lag has two enforcers
of which one is blind. The lag count is a boolean, not the `lag` integer
the policy declares. The job summary labels "due next release" by testing
for the number 2. A fifth tier with `otherwise: stop-next` would gate
correctly in the release script and be labelled "informational" in CI.
None of this has bitten, because no language has ever been at tier 2 in a
real release. We read these as evidence about which parts of the model
were never load-bearing, not as the problem to solve.

### Constraints

**Fixed by the user's framing.** Tier 4 is fine. The four promises are the
vocabulary. The matrix stays as the picture of what exists. Every language
keeps running the whole suite.

**Flexible.** The number of rungs above tier 4; what each guarantees;
whether timeliness is an axis; what a red cell does at each rung; whether
tier 1's process role is part of the ladder; how optional profiles relate
to it; the mechanism (a tier number, a declared set, two axes); whether
there is a ladder above tier 4 at all.

**Fixed by the operator's rulings and the written goals.** The project
keeps ratified prose in three genres: *goals* (standing design
commitments, one page each, written to be measured against), *decision
pages* (one settled question each, with what it was chosen over), and
*guides* (how-to). Quotes below name the genre.

- **Tiers are promises, not rankings.** The decision page
  `docs/decisions/tiers-are-promises-not-rankings.md`:

  ```markdown
  **Decided.** A tier says which profiles a language guarantees and when —
  which red cells stop a release — and nothing about which scenarios run:
  every language runs the whole suite and the matrix shows every cell. Four
  promises nest; there is no fifth.

  **Why.** A tier that ranked languages would have said something about
  effort or esteem and nothing a consumer could rely on; a tier that is a
  bundle of promises says exactly what a consumer choosing a language is
  promised there, and the suite can enforce it — the release workflow
  refuses a tag whose matrix has a cell the tier table says stops it. Running
  the whole suite for every language, rather than only a tier's, is what
  makes the matrix a picture of progress instead of a picture of policy.
  ```

- **A skip in a required profile is a red cell.** Ruled on issue #271,
  implemented in both enforcement paths.
- **The gate holds exactly the tier's promise, no stricter.** From issue
  #416: "the promise a consumer is given is the tier's, and the gate's job
  is to hold that promise, not a stricter one nobody made."
- **The pilots are the hardest languages on purpose.** C++ and Haskell are
  planned for tier 2 because pushing them to every profile is how the model
  is stressed. A ladder whose top rungs are unreachable for them defeats
  that purpose.
- **What a language promises is the operator's decision.** Any proposal here
  becomes a design issue with an operator verdict, not a code change.
- **The promises a language makes must be data.** From the declarative
  goal: "Coverage. Which facts are data and which are still code or prose
  in more than one place: … the promises a language makes …". The policy
  file is the ladder; prose is held to it, not the reverse.
- **Timeliness is a named dimension of a goal.** From the agnosticism goal:

  ```markdown
  - **Language — timing.** Whether a thing lands in every language before it
    is released, or in one first and the others within some allowance; the
    allowance is what a language's promise names, and the limit is none.
  ```

  and, under what the goal is not: "Not a ranking of languages: a language
  that lags is behind on a dimension, not lesser." So the goal-level text
  already frames lag as an allowance a promise *may* name. It does not
  require that a tier name one.

- **No component is an extra.** From the boundary goal: "And not a line
  between 'core' and 'extras': every component is inside the boundary or it
  is not shipped." A tier may say which language holds a component; it may
  never imply the component is optional to Nightseam.
- **An extension is held the same, or it is not in.** From the
  extensibility goal: "Whether an extension is held to the suite the shipped
  parts are held to, by the same scenarios and the same promises, or whether
  it is trusted because it compiled." This forbids a rung that means "it
  builds". The ladder also doubles as the join order: "The tiers are the
  order a language is built in, and each step is a lane of its own that
  lands alone."
- **The reference is a tool, not the definition.** From agnosticism: "the
  one that decides is a tool for settling disagreement, not the definition's
  home."
- **Auth is a promise only where its identity library exists, and it is
  not a conformance profile yet.** Ruled on issue #336: a language's tier
  can include the optional auth profile only where archon (the auth
  component's shared identity library, ported to Go, Rust and TypeScript
  today) has a port in that language. Auth is not in the policy file, no
  tier requires it, and its conformance is held by three byte-level tables
  of 159 cases rather than by scenarios. The plan of record (issue #357) is
  a separate harness, deliberately outside the language-tier obligations
  and explicitly not a sixth matrix column. So the project already has, in
  prose, a notion of an optional promise that sits beside the tier rather
  than inside it, and has not yet decided whether it is a profile.
- **No legacy, no compatibility machinery** until there is a released
  consumer. Rewriting the policy file and both enforcers is cheap and
  expected.
- **One repository, one version number.** This is what makes P4 possible at
  all; a federated project could not promise it.

### What We've Considered

#### Observations that motivate the question

1. **Tier 3 is one bit that the matrix already shows.** The tiers page says
   3 and 4 are one band to a consumer, told apart by the generator column.
   The only thing tier 3 adds is that a red generator cell marks the
   language provisional instead of being informational. That is a real
   promise, but it suggests the underlying model is "a set of promised
   profiles", not a ladder.

2. **Tier 2's lag is the heaviest mechanism holding the thinnest promise.**
   It is the only stateful part of the gate (a previous tag's matrix read
   through git), the only rule one enforcement path cannot express, the only
   place a `lag` integer is declared and ignored, and the only place a tier
   number is tested literally. At a cadence of one to two days per minor,
   the consumer-facing promise is "complete as of the day before
   yesterday". No real release has yet exercised it.

3. **Tier 1 is partly a process obligation.** "A feature is not released
   until every tier-1 language has it" is enforced not by the gate but by
   how lanes are cut: every scenario is born as a pair of tier-1 twins. A
   language cannot pass its way into tier 1; it is admitted by decision.
   The policy file already separates the `reference` flag from tier 1.

4. **The rungs are wildly uneven in size, and tier 3 is unreachable as
   written.** Tier 4 to 3 is a whole code renderer plus 336 scenarios, and
   because the generator profile is held over tunnel channels and live
   values, a language at tier 3 must already have built the two largest P3
   components. Tier 3 to 2 is then observability, about 70 scenarios in the
   doubled count, plus the lag obligation. Tier 2 to 1 is zero scenarios
   and a development commitment. The written ladder says generated sits
   below complete; the built suite says generated sits nearly on top of it.

5. **Optional promises already live outside the ladder.** Auth is described
   as a profile a tier "can include" where a prerequisite exists, and its
   conformance is planned as a separate harness. The project has thus
   already grown a per-language promised set that is not a tier number.

6. **The gate does not distinguish "we know" from "we broke".** A language
   that claims a profile and fails one scenario has nowhere to record a
   known, accepted gap. The worked example is on the README table above:
   the two core failures in every port are one known gap, a recent wire
   change that added per-request serial numbers to the `core` profile
   which the six new ports have not yet implemented, not six unrelated
   bugs. Protobuf's conformance suite keeps a reviewed failure list per
   language, with an "unexpected pass" check so the list cannot rot;
   Nightseam derives skips from declared capability instead, which is
   better on the declarative goal and worse at recording known bugs.

7. **Nothing says how a language goes down.** Promotion is written down;
   demotion and removal are not. The one removal so far, C#, was dropped
   from the plan before it had a testee, so nothing existed to break. A
   demotion would have to say what happens to the language's published
   packages, its row in the matrix, and consumers who adopted it at the
   higher tier. Rust's and Go's platform policies both have demotion
   rules; Kubernetes' API levels are mostly about notice before removal.

#### What the record has already rejected

The decision pages and commit history record these as considered and
refused, with reasons:

- **A tier as a ranking** of effort, maturity or esteem. Rejected: says
  nothing a consumer can rely on.
- **Running only a tier's required scenarios** per language. Rejected: the
  matrix must be a picture of progress, not of policy.
- **A fifth promise** or anything finer than four. Rejected: finer is
  progress within a language, which the matrix shows.
- **Four consumer-visible bands.** Rejected in presentation only: 3 and 4
  are published as one band with the generator column.
- **Skips as evidence of coverage.** Rejected on issue #271.
- **CI failing on every red cell** regardless of tier. Rejected on issue
  #416: the gate holds the tier's promise, not a stricter one.
- **A build failure ending the whole run.** Rejected on issue #442; replaced
  by the absent row state.
- **Trusting the stored verdict** at release time. Rejected: the release
  script re-derives from cells.
- **Easiest-first pilot selection.** Rejected: the hardest are pilots so the
  model breaks while breaking is cheap.

Not in the record at all, never raised: a maturity-word ladder (stable,
beta, alpha), a per-feature rather than per-profile ladder, a who-maintains
axis, a demotion or removal rule, a distribution axis (which languages ship
a published package).

#### Prior art, as we understand it

| project | levels or mechanism | primary axis | red cell does |
|---|---|---|---|
| Rust platform targets | 3 tiers, plus a host-tools sub-axis | guarantee, as a CI gate | tier 1 blocks release; tier 2 blocks build; tier 3 nothing |
| Go ports | 2 classes | guarantee + maintainer + binaries | first-class blocks release; else dashboard |
| Node platforms | 3 tiers | guarantee + binaries | tier 1 blocks release; tier 2 ships; experimental nothing |
| Kubernetes API levels | 3 words | guarantee + removal notice in months or releases | alpha may vanish; beta and GA have a notice window |
| OpenTelemetry, per language and signal | about 5 maturity words | API stability guarantee | none; the word is the statement |
| OpenTelemetry compliance matrix | binary cells, no levels | capability | informational |
| Protobuf conformance | per-language exception list, no levels | capability | unlisted failure fails CI; unexpected pass fails CI |
| Apache Arrow status matrix | binary cells, no levels | capability | informational; integration skips gate CI |
| gRPC | no published levels | lineage (C core versus native) | interop dashboard |
| WebAssembly proposals | 6 process phases | process | blocks phase advancement |
| LSP / DAP | no levels; capabilities negotiated per connection | capability | silent degradation |

Six things we take from that survey, subject to the expert's correction:

1. Almost nobody defines levels by timeliness. Kubernetes (notice before
   removal) and Nightseam (catch-up lag) are the two, and they point in
   opposite directions.
2. Projects that block a joint release define levels by guarantee plus a CI
   gate; federated projects publish capability tables. The axis follows
   from whether there is a joint release to block. Nightseam has one.
3. Two rival designs for tolerated failure: a small policy table per level
   (Rust, Go, Node, Nightseam) versus a reviewed exception list (Protobuf,
   Arrow). Nightseam has no exception mechanism.
4. The "absent" row state (unmeasurable this run, as distinct from failing)
   is under-provisioned nearly everywhere; Nightseam's version is the most
   carefully reasoned we know of.
5. Policy granularity may exceed presentation granularity (Rust's host-tools
   sub-axis; Nightseam's 4 gated, 3 published). Node's per-OS-per-arch rows
   go the other way and are hard to summarize.
6. A ladder is an aggregation over the matrix whose only consumer worth
   having is the release gate. A ladder no gate reads is a ranking.

Two comparisons we find especially sharp. Nightseam's testee `hello` is
LSP-style capability negotiation; what Nightseam adds is a promise the
negotiated fact is judged against, so a missing capability inside a
promised profile becomes a red cell instead of a silent no-op. And
WebAssembly's phase-4 rule (two independent implementations before a
feature is standardized) is Nightseam's "a scenario is born as a pair of
tier-1 twins", applied per scenario rather than per feature.

#### Where we lean, so you can argue with it

We lean toward B or C below: dropping the lag, and making a language's
standing "which profiles it promises" plus "whether it is simultaneous",
with the reference flag recording the third role. Two reasons: the lag is
the one part of the model the code could not carry and the promise it
holds is measured in days; and the matrix row already is the promised set,
so naming the gated subset adds nothing new. What would change our mind:
a consumer-facing reason a "complete but not simultaneous" language should
be allowed to be behind at a tag, or evidence that the exception-list
design serves the pilots better than a policy table.

#### Candidate shapes for the rungs above tier 4

Each candidate is given as: what it promises, what it costs, what it fails
to answer.

**A. Keep four tiers, repair the mechanism.** Promises: today's ladder,
with `lag` read as the integer it is, the Go side taught `otherwise`, and
the literal `2` removed. Costs: little. Fails to answer: every observation
above; tier 3 stays a column, tier 2 keeps a stateful gate for a two-day
promise, and the inverted nesting stays unnamed.

**B. Three rungs above wire: held, complete, simultaneous.**

| rung | promises | red in a promised cell | red elsewhere |
|---|---|---|---|
| simultaneous (today's 1) | every profile, and every feature lane includes this language | stops the tag | — |
| complete (today's 2 without lag) | every profile, at every tag | stops the tag | — |
| held (today's 3 and 4) | a declared set that includes at least `core` | provisional; the tag ships and names it | informational |

Promises: a complete language is complete at every tag; a feature may land
there in a later pull request than in Go but the tag waits for it. Costs:
every complete language becomes a release blocker on every profile, so
four pilots at that rung means every feature lane blocks the tag until six
implementations exist. At agent-hours per language that is a delay of days
per feature, at a two-day cadence, which may be the wrong trade or exactly
the intended stress. Fails to answer: the consumer-facing difference between
the top two rungs at a tag boundary becomes nil, and whether that is
honest or a lost distinction is for the expert.

**C. A declared promised set per language, tiers as names.** Promises:
each language declares the profiles it promises, any subset that is
upward-closed under the nesting as the gate has it (promising `generator`
requires `core`, `tunnel` and `live`; promising `observability` requires
`core`), plus one boolean, simultaneous or not. The gate stops on red in a
promised cell of a simultaneous or complete language and marks provisional
otherwise. "Tier" becomes the name of a common set: wire = {core};
complete = all. Auth joins as one more profile a language may promise,
which is how the operator described it. Costs: a lattice of subsets is
harder to say in one word than a number, and the README's one sentence per
language gets longer. Fails to answer: what "provisional" should cost and
how long a language may stay there.

**D. Two explicit axes.** Promises: a number for the promised set (4 =
wire, 3 = generated, 2 = complete) and a separate timeliness class
(simultaneous / same-tag / no promise); tier 1 becomes "2 + simultaneous".
Costs: two things to publish instead of one. Fails to answer: the same as
B, and it keeps the number 3 for a set that, as the gate is built, nearly
equals 2.

**E. Keep the lag, but make it a real quantity.** Promises: today's tier
2, with the allowance tied to calendar time or to a count the policy
declares, the integer read, and the same cell compared rather than
"anything elsewhere". Costs: the stateful gate stays, and both enforcers
must learn it. Fails to answer: whether a consumer wants a lag promise at
all.

**F. A waiver list beside the ladder.** Orthogonal to A through E: a
per-language, per-scenario list of known accepted failures with a reason,
checked in both directions (a listed scenario that passes is an error).
Promises: a language can promise a profile while carrying one known gap
without going provisional. Costs: a second policy artifact and a rule for
when a waiver expires. Fails to answer: nothing about the rungs; it is a
complement.

## Questions for the Expert

We are not asking for a finished design. A ranking of the candidates with
your reasoning, corrections to how we have read the prior art, and anything
under question 5 is the right size.

1. **What should a rung above "speaks the wire" add, and how many earn
   their place?** The matrix already records per language which profiles
   exist and pass. What does a support ladder contribute that the gated
   cells themselves do not, and how would you shape the rungs between
   "speaks the wire" and "holds every component" so that each is a distinct
   promise a consumer can act on? Two facts to weigh: the generator target
   is more than half the work and, as the suite is built, cannot be held
   without the tunnel and live runtimes beneath it; and the successor
   project will put targets like a database in the same table, which can
   hold generated output but no runtime profile. Is "has a generator
   target" a rung, a column, or its own axis? And should a ladder be
   published at the granularity it is gated at, given that Nightseam gates
   four levels and publishes three?

2. **Is timeliness an axis a support ladder should carry?** Nightseam's
   tier 2 promises everything within one minor release, enforced by reading
   the previous tag's matrix, at a cadence where a minor release is one or
   two days. How have systems you know expressed catch-up lag, or declined
   to, and what happened when they did? If you kept a lag, what would you
   make it a promise about, and if you dropped it, what would you tell a
   consumer about a language that is complete at every tag but whose
   features land later in the development cycle than the reference's?

3. **How do systems that have both a normative implementation and a set of
   promised implementations model the relationship between the two?** In
   Nightseam, Go arbitrates disagreement, Go and TypeScript are obliged to
   receive every feature at birth, and tier 1 bundles that obligation with
   the promise "every profile at every tag". Where have you seen those
   roles combined, where kept apart, and what did each choice do to the
   top of the ladder and to the consumer's reading of it?

4. **What should a red cell cost at each rung, and how does a language
   stop paying?** Nightseam has three dispositions: refuse the tag, ship it
   and name the language provisional in the notes, or informational.
   Provisional is the only middle setting, no released consumer has ever
   seen one, there is no way to record a known accepted gap (Protobuf's
   reviewed failure list is one answer), and there is no rule for how long
   a language may sit there: promotion is written down, demotion and
   removal are not. In systems that ship a joint release across
   implementations, what makes a middle disposition credible to a consumer
   rather than a euphemism, what does it have to cost the implementation
   that earns it, and what have you seen keep an implementation from living
   there indefinitely?

5. **What have we not asked?** Knowing how this ladder is gated, enforced
   and published, what would you warn us about, or point us toward, that
   this document's framing hides?

## Applied

The advice (`0001-language-tier-ladder.advice.md`, run
`run_84bcc7f9-667e-4e83-aad4-18e38ddc1696`, verified genuine) answered the
document at the revision committed as `eb058f06`; the only drift since is
the status line. Each discrete recommendation below was checked against
the tree at `48a64a9c` on 22 September 2026 and ruled HOLDS (premise
intact), NEEDS ADAPTATION (right direction, a detail differs), or STALE
(premise gone or never true).

| # | recommendation | verdict | evidence |
|---|---|---|---|
| 1 | Drop the one-release lag; the mechanism measures "no two consecutive releases with a failure outside requires", not "features arrive within one release": skips never start the clock, and two different gaps on consecutive releases count as one episode | HOLDS | `scripts/matrix.mjs` `gate`: `elsewhere` is built from `failed` only; `wasLagging` returns true for any failed non-required profile at the last release |
| 2 | Retire tier 3 as a numbered rung; keep "generated" as a named promise and a column; soften "unreachable" to "reachable only after building tunnel and live" | HOLDS | the generated testee templates import `tunnel/go` and `live/go`; 42 generator runs cross channels; tiers 3 and 4 differ only in `requires` |
| 3 | Keep three roles apart: reference (settles disagreement), twin cohort (feature birth), release-required (unmet promise blocks a tag) | NEEDS ADAPTATION | the policy file records `reference` and `tier`; the twin cohort lives only in onboarding prose and needs a field of its own |
| 4 | Amended C: a language declares its promised set and its failure disposition, which are the two facts `requires` and `onFailure` already carry per tier; do not derive blocking from a "simultaneous" flag | HOLDS | `conformance/profiles.json` tiers block; both enforcers already key on exactly those two fields |
| 5 | C conflicts with the recorded "there is no fifth promise; anything finer is progress" and needs the operator to resolve that explicitly | HOLDS | `docs/languages/tiers.md` and the decision page both carry the sentence |
| 6 | If that restriction stands, take collapsed B and keep named bundles to the existing promise vocabulary | HOLDS | as a conditional |
| 7 | B has a hidden workflow cost: a required-profile skip in the CI star already fails the job, so a pull request carrying a new scenario for two languages is unmergeable while a release-required third language lacks it | HOLDS | `conformance/go/suite.go`: `reportOutcome` fails a blocking row's failure, and the suite's `Cleanup` errors on any `Matrix.Blocking`; a required skip makes the verdict `blocking` |
| 8 | Complete-at-every-tag already delivers the consumer-facing P4; the top two rows of B differ only in contributor policy | HOLDS | reasoning over the promises table |
| 9 | Consequence table: required fail or skip for a release-required language refuses the tag; a required testee that cannot build refuses for release-required and reports absence otherwise; a promised failure in the reference-held band ships provisional and names the unmet profile; outside the promised set is evidence only; a known accepted failure is annotated, never coverage | HOLDS, last row new | `gate` and `Verdict` do the first four today (`gate` names the profile in its provisional note); nothing records an accepted failure |
| 10 | "No promise assigned" and "promise unmet" must not share the word `provisional` | HOLDS, latent | `Verdict` returns `provisional` for a language absent from the policy; `gate` skips such a row silently; every testee is tiered today, so no instance exists |
| 11 | Known-gap annotations, operator-reviewed, with language, outcomes, issue, reason and expiry; the outcome stays failed; an unexpected pass removes the annotation | NEEDS ADAPTATION | no artifact exists; the six ports' serial gap (#448 to #453) is the worked case; where the annotation lives is a lane's decision |
| 12 | A release waiver is a separate, visibly qualified action, never an unqualified "complete" | HOLDS | as principle; nothing implements it |
| 13 | Make provisional informative: what failed, its consumer effect, the issue, the operator's disposition | NEEDS ADAPTATION | the release notes name the language and `gate` names the profile; the issue and the effect are not carried |
| 14 | A permanently nonblocking tier can stay broken indefinitely without an operator review; making an expired gap block the next tag would reintroduce `stop-next` | HOLDS | structural; tier 4 is fixed by the framing |
| 15 | Demotion and removal are explicit, prospective operator decisions recording commitment, effective release, reason and package consequences; removal preserves history | HOLDS, gap | nothing in the record; C# was dropped before it had a testee |
| 16 | Prior-art correction: Node tier-2 test failures block releases; only infrastructure issues may delay tier-2 binaries | HOLDS | Node's `BUILDING.md`, fetched 22 September 2026: "Test failures on tier 2 platforms will block releases." The document's table row was wrong |
| 17 | Prior-art correction: Rust tier 2 guarantees builds, a different property, not a delayed tier 1 | HOLDS | consistent with the rustc target tier policy |
| 18 | Prior-art correction: Kubernetes' relevant timing rule is version skew, not deprecation notice; OpenTelemetry's stability labels carry compatibility requirements; Arrow's checkmarks are qualified | HOLDS | taken as the expert's reading; not re-fetched |
| 19 | The star does not prove pairwise interoperation; a known failure between two release-required languages on a mutually promised profile should not be discharged by filing an issue | HOLDS, gap | nightly failures are filed against the scenario and nothing gates a release on them |
| 20 | The release gate reads a row's tier from the matrix, the runner from the policy; nothing checks they agree | HOLDS | `gate` uses `row.tier`; `release-prepare.mjs` checks rows and cells exist but not the tier |
| 21 | The hello check kills a run at any tier before the nonblocking disposition applies | HOLDS | `suite.go` calls `t.Fatal` on `HoldToTier` for every tier |
| 22 | An absent generated testee marks a language provisional even where `generator` is not promised | HOLDS, latent | `build.go` records absence for either kind; `Verdict` short-circuits on any absence; no tier-4 language has a generated recipe yet |
| 23 | Need-based placement can move a scenario out of a promised profile, so editing profile membership edits a promise | HOLDS | `Place` prefers a need over a layer |
| 24 | Auth is not in C's universe; keep it outside the tier obligations unless the operator reopens #336 and #357 | HOLDS | corrects candidate C as written |
| 25 | "Complete" needs an explicit universe; adding a profile is a policy decision, not a registry edit | HOLDS | tier 1's `requires` is an explicit list today |
| 26 | Promote the pilots to release-required only when coverage holds and the operator accepts the release cost, not because `planned` says 2 | HOLDS | all six ports are tier 4 in the policy |

**What was integrated.** Nothing in code: what a language promises is the
operator's decision, and recommendation 5 names a recorded sentence that
the proposal has to overturn. The integration is this section and the
proposal below, for a design issue the operator rules on.

**The proposal, as the advice and the verification leave it.** Keep tier 4
as it is. Remove the one-release lag and the `otherwise`, `lag` and
`stop-next` machinery with it. A language's standing becomes two facts it
already has at tier level, declared per language: the profiles it
promises, and whether a breach refuses the tag or ships it provisional.
"Generated" stays a promise a language may make and a column, not a rung;
as the suite is built it presupposes tunnel and live. "Complete at every
tag" is the release-required set, and it delivers P4 to a consumer on its
own. The twin cohort (which languages every feature lane must include)
becomes a third recorded fact beside `reference`, decided by the operator,
never earned by passing. Known accepted failures get an operator-reviewed
annotation that leaves the cell red. Demotion and removal get written
rules. Presentation can stay at two bands, reference-held and complete,
with the promised profiles readable without decoding a number. The
operator must explicitly overturn or reconcile "anything finer than four
promises is progress" to adopt this; if that sentence stands, collapsed B
is the fallback.

**Defects found on the way, each a lane of its own if the operator wants
them regardless of the design verdict:** the release gate's tier is read
from the matrix row and never checked against the policy (20); the hello
check bypasses the tier's disposition (21); an absent generated testee
marks an unpromised profile (22); `provisional` covers both "no promise"
and "promise unmet" (10); the CI summary tests for the number 2 and the
`lag` field is dead (from the body).
