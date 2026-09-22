# Research: The model behind the ladder — twins, merge versus release, and what a tag waits for

**ID:** 0002
**Date:** 22 September 2026
**Status:** applied
**Run-ID:** run_d9f8ce66-b2c7-4ee0-9f80-f3ff08583d45
**Document-ID:** doc_ab403efb-8e43-47e7-bfc0-ca2667a6ffe1
**Earlier run:** run_befbec5f-2530-4861-880c-20012a08ddce failed on the service side before any expert work (session expired on its worker); re-submitted under a fresh key.

## Question

This is a follow-up to a consultation earlier today (research document
0001) about what a language's support tier should promise in a protocol
and runtime implemented in eight languages. That consultation's answer was
accepted in outline: drop the timed lag, stop packing three facts into one
tier number, and record per language which profiles it promises, what a
breach costs at a tag, and whether it is one of the languages a feature is
born in. The ladder it replaces and the findings that survive from it are
summarized under "What the first consultation settled" below; nothing here
depends on having read 0001 itself. Talking that answer through raised
questions the first document did not ask, and the operator (the project's
single human decision-maker, described below) wants them explored before
the model is settled.

**The question is the model itself, at the level of its nouns and rules,
and four consequences of it that the first answer left open.** Every term
used here is defined in the Vocabulary section.

1. **Is the decomposition right?** We now describe the model as four nouns
   (scenario, profile, language, release), four facts recorded per language
   (promised set, disposition, twin, reference) and three rules (the matrix
   records, the gate enforces a promise at a tag, the twin rule decides
   where a feature is born). "Feature" is not a noun; it decomposes into
   scenarios placed in existing profiles plus implementations per language.
   What is missing, superfluous, or at the wrong unit, and which of these
   facts must be machine-readable rather than prose, given who operates
   them?

2. **How are twins chosen when there are many languages?** Today the two
   languages a feature is born in are fixed: the reference and one other.
   With six more languages of deliberately different idioms, is a fixed pair
   still right, should the second twin be chosen per feature by the idiom it
   stresses, or does the idea stop making sense past two?

3. **How does a feature merge while a release-required language is still
   behind?** In this repository the check that must be green for a pull
   request to merge reads the same verdict the release gate refuses on, so
   "must hold before the tag" silently becomes "must be born in the same
   lane". Today no language but the two twins refuses a tag, so this
   collapse is prospective: it is a reason not to promote a port, not a
   current outage. The first advice said to split merge eligibility from
   release readiness without letting the gap become a green skip, and left
   the mechanism to us.

4. **What is the right release coupling across eight languages, and what
   does a refuse disposition really cost?** All packages share one version
   and one tag, yet only two of the eight languages publish an artifact. A
   language whose breach refuses the tag is a veto on every release,
   including releases of unrelated fixes, and eight toolchains on the
   release path are eight sources of transient red. Is one lockstep tag the
   right unit, and if it is, how is the veto priced?

**The stakes.** The first consultation settled what a tier should not be.
This one decides what the project writes into its policy file and its
collaboration rules, and those two documents drive an agent fleet that cuts
work from them with no human reviewer between a lane and main. A model with
a rule nobody can operate, or a twin cohort that turned out to be whichever
languages CI happened to gate on, is what the fleet will build towards.
Rewriting the policy and both enforcers is a lane of a few hours; rewriting
how lanes are cut is a change to how every future feature is made. We
expect this model to hold through 1.0 and to be what the successor project
starts from; we do not expect to revisit it once the six ports are
release-required.

## Context

### System Overview

**What Nightseam is.** A duplex RPC protocol over a bidirectional byte
stream, a runtime that implements it, a small declaration language in which
a consumer describes their API, and a code generator that renders typed
clients and server bindings from that declaration. "Duplex" means both ends
can initiate calls and emit events over one connection; the seam package
that carries raw frames is also called `duplex`. Above the base protocol
sit three optional components: a **tunnel** (many channels multiplexed
over one connection, with credit-based flow control), **live** (callable
values: a function can be passed over the wire and invoked remotely, with
explicit ownership and release), and **observability** (structured events
at every layer, trace propagation, and a shipped adapter for
OpenTelemetry, called `otel` in the tree). An optional **auth** component
is its own package beside these and outside the tier obligations.

**Languages.** Go is the reference, the implementation every other is
tested against and the one that settles a disagreement. TypeScript is the
second full implementation. Python, Rust, C++, Haskell, Java and Swift,
**the six ports**, each landed a base runtime in the last week and promise
only the wire today. All code, all languages and all documentation live in
one repository. Every published package in every language carries one
version number; tags move in lockstep. In practice only TypeScript
publishes to a registry and Go is consumed from its module proxy at the
tag; the other six build artifacts in CI that never leave the runner, so
the tag couples eight languages by evidence and version string, not by
artifact.

**Cadence and horizon.** Pre-1.0, moving fast: four releases were tagged
on 19, 19, 20 and 22 September 2026 (UTC). No date for 1.0. The next
milestones are auth and the language ports. Consumers today are
hypothetical external adopters plus one internal tool that ran an early
release; no installed base constrains the model, which is also why the
project tags from main every one to two days.

**How the project works.** Autonomous agents do most implementation work in
parallel **lanes**: a lane is one unit of work, roughly a branch and its
pull request, cut from an issue that names what it does, the surface it
touches, what tests guard it, and its parity obligation. A lane lands on
its own when its checks are green; no human review is required to merge.
Adding a language to a feature lane costs agent hours, not engineer weeks,
but it costs them serially: the lane is not done until every language it
must include passes. Design questions with no written answer become issues
and are decided by one human operator; agents decide on the operator's
behalf only where the written goals and decisions already answer the
question. What a language promises is the operator's decision. The
operator's stated taste: the beautiful decision recognizes an existing
thing at the right unit and adds nothing; rewrites are cheap before 1.0, so
a design is chosen for being right, never for being cheap to roll out; and
this repository exists to push into unknown territory and find where the
model breaks, so a bold failure here is worth more than a cautious success.

**The successor.** A sibling project, bitlink, generalizes "language" to
"target": a database or a UI framework renders from the same declaration
and sits in one table with Go and Rust, but is not a runtime and cannot
speak the wire, carry a tunnel or export a live value. bitlink is
documentation-only today, with no start date. Its written plan is that
each of its steps passes its part of Nightseam's conformance suite, and it
names Nightseam's `generator` profile by name; it has no support ladder of
its own. Whatever model Nightseam settles on is the one bitlink inherits or
rejects.

### Vocabulary

The four-tier ladder this document is leaving behind, in one sentence:
tier 4 promised the wire, tier 3 added a generator target, tier 2 added
everything else within one minor release, tier 1 added simultaneity, and
a red cell in a promised profile refused the tag at tiers 1 and 2 and
marked the language provisional at tiers 3 and 4.

**The suite and its parts**

- **Scenario.** One JSON file describing a scripted exchange between two
  parties, called `a` and `b`, with expected outcomes. There are 79 files;
  after case-table expansion (a scenario that names a case table runs once
  per case) and mirroring (a scenario that does not fix which side
  initiates runs both ways) they are 314 runs per pairing of two languages.
- **Case table.** A JSON file of cases under `conformance/tables/` that
  scenarios and the languages' own unit tests read. Distinct from the tier
  table (the policy) and the matrix table (the report).
- **Layer.** Every scenario declares the layer it exercises: `seam` (the
  raw byte stream and framing), `peer` (the protocol over it), `tunnel`,
  `live`, or `generated` (scenarios that drive code the generator
  produced).
- **Capability.** What a testee announces it implements: layers, and the
  features `observer`, `propagator`, `listen`, `pipe`, `lazy`. A scenario's
  `needs` field lists the capabilities it requires of the party under test,
  layers and features alike; a scenario needing a capability the testee did
  not announce is skipped. Only the two features `observer` and
  `propagator` take part in profile placement.
- **Profile, and the placement rule.** A profile is a named set of
  scenarios: `core`, `generator`, `tunnel`, `live`, `observability`. The
  placement rule: a scenario needing `observer` or `propagator` goes to
  `observability`, whatever its layer; otherwise it goes to the profile its
  layer names, with `seam` and `peer` both going to `core`. Every scenario
  is in exactly one profile.
- **Testee.** A small program, one per language, that the runner drives
  over the driver protocol. It answers `hello` with its capabilities. A
  language with a generator target has a second, generated testee.
- **The suite, the runner, the driver protocol.** The suite is the 79
  scenarios. The runner is the Go test binary that executes them against
  two testees. The driver protocol is the JSON control protocol the runner
  speaks to a testee.
- **Reference.** The Go testee, and only Go. Every language is tested
  against Go on both sides of a real socket, and Go settles a
  disagreement. The policy file marks it with a `reference` flag.
- **Star.** Every language paired with Go, both orientations. What CI runs
  on every pull request.
- **All-pairs matrix.** Every language against every other, run nightly. A
  failure there becomes an issue against the scenario; nothing reads it at
  release time.
- **Matrix, row, cell.** The report of one run: one row per language, one
  column per profile, each cell a count of passed, skipped and failed. A
  row also carries the language's tier and a computed verdict; the release
  script recomputes rather than trusting the stored verdict. The last run's
  matrix is committed as a JSON file and rendered into the README.
- **Holds.** A language holds a profile when every scenario in it passed
  against Go with no skip. The word is used in that sense only.

**The policy and its consequences**

- **Policy file.** `conformance/profiles.json`, the file both enforcers
  read. The **two enforcers** are the runner (in CI) and the release script
  `scripts/release-prepare.mjs` (at a tag).
- **Promised set.** The profiles a language promises. Today carried by a
  tier's `requires` list.
- **Disposition.** What a red cell inside the promised set does at a tag.
  Two values, with their spellings:

  | in prose | in the policy file (`onFailure`) | as a row's verdict | consequence |
  |---|---|---|---|
  | refuse | `stop` | `blocking` | the tag is refused; also the merge, as shown below |
  | mark | `provisional` | `provisional` | the tag ships and the language is named provisional in the notes |

  A third value, `stop-next`, encodes the one-release lag the first
  consultation retired; it appears in the policy file and in no prose here.
- **Breach.** A red cell inside a language's promised set.
- **Red cell.** A failure, or a skip in a promised profile. A skip counts
  as red because a scenario the testee could not run is not evidence of
  coverage.
- **Absent.** A row state: the language's testee would not build this run.
- **Release-required.** A language whose disposition is refuse.
- **Provisional.** The verdict, and the label, for a language at the mark
  disposition with a breach. Both enforcers also return it for a language
  with no policy entry at all, which the first consultation flagged as a
  defect; every language has an entry today.
- **Green skip.** A gap reported as a skip that reads as harmless. What the
  extensibility goal forbids: an extension is held to the same scenarios,
  not trusted because it compiled.
- **Pilot.** A port the operator has slated for promotion: Python, Rust,
  C++ and Haskell, recorded in the policy file's `planned` map as tier 2,
  which in the model is "promises all five profiles, refuses the tag".
  Java and Swift are not in that map.
- **The definition.** What every language is one realization of: the wire
  protocol, the declaration language, the case tables, and the 79
  scenarios. The agnosticism goal says no language is its home.

**How work is done**

- **Lane, epic, catch-up lane.** A lane is one issue, one branch, one pull
  request, landing alone. An epic is an umbrella issue listing a port's
  planned lanes and holding no work itself. A catch-up lane is a port's
  later implementation of a scenario the twins already hold.
- **Twin, twin cohort.** A twin is a language every feature lane must
  include: the scenario is written and made to pass there in the lane that
  creates the feature. The twin cohort is the set of twins; today Go and
  TypeScript. The word comes from the tiers page ("a scenario is born as a
  pair of tier-1 twins").
- **Parity.** The repository's word for the twin obligation: every runtime
  component exists in both Go and TypeScript, and a change is not done until
  its twin has it. A required field of the lane issue form.
- **Merge gate, release gate.** What must be green for a pull request to
  land on main; what must pass for a tag to be cut.
- **Port.** A language implementation added after the definition existed;
  today the six languages that are not twins.
- **The tree.** The repository's working source at a commit.
- **Dry run, smoke, round trip, preflight.** A dry run is the release
  workflow run on demand without publishing. A smoke is a script that
  installs what a step produced into a scratch consumer and runs it. The
  round trip is the smoke that installs the just-published packages from
  the registries and runs the getting-started example against them, after
  the tag exists. The preflight is a release job, landed today, that runs
  the seconds-cheap checks before any toolchain or reviewer.

"Tier" and "profile" have other meanings in this repository that are not
meant here: "tier" also names the two CI job speeds, `fast` and `full`;
"profile" also names the wire-level protocol `nightseam.duplex/1`, which a
scenario note below calls "the profile". "Publication" below means
shipping an artifact to a registry; the wire rule about request serials is
called emission order.

### What the first consultation settled

Research document 0001 asked what tiers 3, 2 and 1 should promise. The
advice, verified against the tree and applied:

- **Drop the lag.** The mechanism as built measured "no two consecutive
  releases with a failure outside the required profiles", not "features
  arrive within one release": skips never started the clock, and two
  unrelated gaps on consecutive releases counted as one episode.
- **Retire tier 3 as a rung.** "Generated" is a promise a language may make
  and a column of the matrix. As the suite is built, holding it presupposes
  the tunnel and live runtimes, so it is not an intermediate step.
- **Keep three roles apart.** The reference (settles disagreement), the
  twin cohort (which implementations must participate before a feature lane
  is accepted), and the release-required implementations (whose unmet
  promises prevent a tag). Their coincidence in Go and TypeScript does not
  make them one thing.
- **A language declares two facts the tiers already carried:** the profiles
  it promises and what a breach costs at a tag. Blocking is not derived
  from a "simultaneous" flag.
- **"Complete at every tag" already delivers the consumer-facing promise
  of simultaneity.** The distinction between "complete" and "born with
  every feature" belongs in contributor policy.
- **A hidden cost in one candidate:** the CI check on every pull request
  fails on the same verdict the release gate refuses, so a language whose
  promise blocks a release also blocks every merge that adds a scenario to
  a promised profile. Either stage every release-required implementation
  before merge, or distinguish merge eligibility from release readiness
  while still reporting the gap truthfully.
- **Known accepted failures** get an operator-reviewed annotation that
  leaves the cell red. **Demotion and removal** become explicit decisions.
  **Provisional** must not mean both "no promise" and "promise unmet".
- **Promote the pilots to release-required only when their coverage holds
  and the operator accepts the release cost.** The 0001 advisor favored
  eventually accepting a six-language release barrier, but warned against
  imposing six-language feature birth by accident.

### The model as we now state it

Four nouns, four facts per language, three rules.

| noun | what it is | unit of |
|---|---|---|
| scenario | one scripted exchange between two parties, in exactly one profile | evidence |
| profile | a named set of scenarios: core, generator, tunnel, live, observability | promise |
| language | one implementation, with a testee; one row of the matrix | obligation |
| release | a tag; the moment promises are checked | enforcement |

A feature arrives as new scenarios, each placed in an existing profile, plus
implementations per language. Once its scenarios are in a profile they bind
whoever promised that profile. Adding a whole new profile is a separate
policy decision.

The four facts, recorded per language in the policy file:

1. **Promised set.** Which profiles it promises. Any set the suite's own
   dependencies allow: `generator` presupposes `tunnel` and `live`.
2. **Disposition.** What a red cell inside the promised set does at a tag:
   refuse, or mark. Red outside the set is evidence only.
3. **Twin.** Whether every feature lane must include it. Decided by the
   operator, never earned by passing a gate.
4. **Reference.** Whether it settles a disagreement between
   implementations. Go, and only Go.

A candidate fifth fact, whether the language publishes an artifact at all,
is raised under "What We've Considered" and asked about in question 4; we
have not put it in the model.

The three rules, each reading different facts:

- **The matrix records what happened.** Every language runs every scenario;
  every cell is shown; a testee that will not build is an absent row. Reads
  no policy.
- **The gate enforces the promise at a tag.** For each language, red inside
  its promised set does what its disposition says. Reads facts 1 and 2.
- **The twin rule decides where a feature is born.** A feature lane is done
  when its scenarios pass in every twin. Reads fact 3.

Two things beside the model: known-gap annotations (per language and
scenario: an accepted failure with a reason, an issue and an expiry; the
cell stays red), and names for common bundles ("wire" for {core},
"complete" for all five, "marked" for the mark disposition), which are
presentation only.

What "tier 1" was, unbundled under this model:

| language | promises | breach | twin | reference |
|---|---|---|---|---|
| Go | all five | refuses the tag | yes | yes |
| TypeScript | all five | refuses the tag | yes | no |
| the six ports today | core | ships marked | no | no |
| a pilot promoted as planned | all five | refuses the tag | no | no |

The fourth row is the one today's ladder cannot express, and it is the only
thing the model adds: a language that promises everything and blocks the
tag but is not where features are born.

### Architectural Context

#### How a change lands, and what has to be green

Every change reaches main by a pull request from its own worktree, squash
merged, and it lands by itself when its checks are green. From the
collaboration guide, `COLLABORATION.md`:

```markdown
`main` takes no direct push. Every change — a lane's, the coordinator's, a
one-line doc fix — is a branch in a worktree of its own, a pull request, and
a squash merge that lands by itself when the checks are green. The rules
are a file, `.github/ruleset-main.json`; `node scripts/protect-main.mjs
apply` puts them on the repository and CI's `protection` job fails when the
live rules have drifted from the file. Nobody is exempt.
```

The live ruleset, read from GitHub on 22 September 2026, requires five
status checks, no approving review, no bypass actor, and squash as the only
merge method, with "branch must be up to date" off so that parallel lanes
do not serialize on rebases; the repository itself allows auto-merge:

```json
{ "type": "required_status_checks",
  "parameters": {
    "strict_required_status_checks_policy": false,
    "do_not_enforce_on_create": false,
    "required_status_checks": [
      { "context": "fast (ubuntu-latest)" },
      { "context": "fast (windows-latest)" },
      { "context": "full" },
      { "context": "protection" },
      { "context": "scope" }
    ] } }
```

The five checks: `fast` (Go only, seconds, on two platforms: formatting,
vet, the short test tier, the script tests, the README table check, the
link and documentation checks); `full` (every toolchain, about 22 minutes:
every port's unit tests and smokes, the TypeScript packages, and the
conformance star); `protection` (the live ruleset matches the file);
`scope` (the pull request closes exactly one milestoned issue, its title
is the commit it becomes, it carries no trailer, and files outside the
issue's declared directories are noted, never refused). A separate `swift`
job is required transitively because `full` fails if it did not succeed.
The reason for no review, from the ruleset file's own comment:

```json
"  - no approval is required, so that a green, in-scope PR is its author's",
"    to merge and a lane never waits on a person — review is a comment,",
"    not a gate;",
```

The `full` check is the one that matters here. From `.github/workflows/ci.yml`:

```yaml
      # Eight native testee builds and the complete star need an overall
      # budget beyond the default ten minutes; each build retains its own
      # five-minute deadline in conformance/go/build.go.
      - run: go test -timeout 25m ./...
      # The conformance suite is in go test ./... above; it is run again by
      # name so that a failure of the matrix reads as one, and its
      # matrix.json is kept as the run's artifact.
      - run: go test -v -timeout 25m ./conformance/go -run 'TestSelf|TestStar|TestGenerated' -count=1
```

The three named tests are the reference against itself, the star, and the
generated testees against Go. The job does not set the `NIGHTSEAM_MATRIX`
environment variable. That variable does not choose the pairings; it makes
a skip or failure inside a promised profile fail the job immediately for
every language, and only the nightly workflow sets it. The all-pairs
expansion is a separate flag.

#### The merge gate and the release gate are one gate

The runner is a Go test binary. Whether a red cell fails the job is decided
in one place, at the end of each of the three named tests, from the same
verdict the release script applies at a tag. `Open` is the constructor that
builds the testees and installs this cleanup; `Errorf` marks the test
failed and lets it finish. From `conformance/go/suite.go`:

```go
	t.Cleanup(func() {
		if blocking := s.Matrix.Blocking(s.Profiles); len(blocking) > 0 {
			t.Errorf("the tier table stops a release on: %v", blocking)
		}
	})
```

`Matrix.Blocking` names every language whose verdict is `blocking`. The
verdict, from `conformance/go/profiles.go`, whole. `m.rows` is the matrix's
cells by language and profile; `m.absent` records testees that would not
build; `p.Languages` and `p.Tiers` are the policy file's maps:

```go
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

How one outcome reaches the job before that cleanup, from the same
`suite.go`. The reporter is the Go test handle with four methods, `Helper`,
`Skip`, `Fatalf` and `Logf`, of which only `Fatalf` fails the job; `held`
is the language whose row the outcome lands in; `s.Placed` is the profile
the placement rule gave each scenario; `Record` adds the outcome to a cell.
Two elisions are marked:

```go
func (s *Suite) reportOutcome(t scenarioReporter, sc Scenario, a, b, held string, outcome Outcome) {
	t.Helper()
	strict := os.Getenv("NIGHTSEAM_MATRIX") != ""
	// ... a skip caused by the partner is re-attributed to the partner
	s.Matrix.Record(held, s.Placed[sc.Key()], outcome)
	// ... an optional observer callback
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

So, in the default run: a skip is recorded and never fails the job here; a
failure fails immediately only if the row is already blocking; and at the
end of the test, the cleanup fails the job if any row's verdict is
`blocking`, which happens only at the refuse disposition. For a
release-required language, a new scenario it cannot run is a skip in a
promised profile, which the verdict counts as red, which makes the row
blocking, which fails `full`, which is a required check. For a marked
language, the same skip is recorded and logged, and nothing fails. The
tiers page states the doctrine:

```markdown
The CI star and release gate agree: only a nonempty `Matrix.Blocking` fails
the star, and the release script reads a row's absence and its cells rather
than the verdict stored beside them.
```

By construction, there is no configuration in which a language is
release-required but merge-optional. Refuse blocks both the pull request
and the tag; mark blocks neither. Three things fail the job at every
disposition regardless: a missing toolchain, a testee that will not start,
and a testee whose `hello` announces fewer layers or features than its
promised profiles name. That last check is coarse: it checks capabilities,
not scenarios, so a new scenario inside an already-announced layer is not
caught there and becomes a per-scenario skip.

#### What the release does

The release workflow runs on a pushed `v*` tag, or on demand as a dry run
that publishes nothing. As of this afternoon it has two jobs. The
`preflight` job, with no reviewer gate and no toolchain beyond Node, runs
the seconds-cheap checks first: the remote tag against the checkout, every
spelling of the version and the committed matrix against the policy, which
npm names are a first publish, the repository's visibility, and that every
script a workflow names loads. The `release` job then waits at a GitHub
environment for a named human reviewer before any step runs, repeats the
tag, version and matrix checks, installs all eight toolchains, runs every
port's unit tests and smokes and the whole star, uploads the npm packages,
runs the round trip against the registries, and creates the GitHub release.
From `.github/workflows/release.yml`:

```yaml
  release:
    needs: preflight
    runs-on: ubuntu-latest
    # Every run, a tag's or a rehearsal's, waits here for the reviewer the
    # environment names before a step runs.
    environment: release
```

So a tag has one human gate that a merge does not, and it is a reviewer of
the run, not of the code. The policy gate reads the matrix committed on
the tag; the suite runs again later in the same job, without the nightly
variable, as a net. The release gate is exactly as strict as the merge gate
for release-required languages and exactly as lenient for marked ones.
Nothing at release time reads the all-pairs matrix.

The cost of a tag before today's change, in the project's own two records:
the issue that asked for the preflight says the cheapest gates sat behind
"about twenty minutes" of toolchains and matrix checks, and that the
0.6.0 publication surfaced two seconds-cheap script defects after twenty
minutes; the pull request that landed the preflight says forty minutes.
Both agree the defects were found after the immutable tag, and that
release's round trip was red, which the releasing guide names as the one
failure that cannot be answered by fixing the tree and tagging again.

#### The committed matrix is evidence that can go stale

The README's table is held to the committed matrix by a check in the
`fast` job. Nothing holds the committed matrix to the run that just
happened: the fresh matrix is published as an artifact and a job summary,
and the rule that a pull request commits the matrix its own run wrote is
prose. Observed on 22 September 2026: five of the six ports had landed their
request-serial fixes, and the committed matrix still recorded every port as
failing two `core` scenarios, because those pull requests changed outcomes
but not scenarios and so did not regenerate it. A tag cut from main at that
moment would have named six languages provisional on stale evidence; in the
other direction, a stale matrix could hide a regression from the release
gate's policy check.

#### How a feature lane is cut, and how the twin rule is operated

A lane is an issue on a form. Its fields: What (the change, in the terms
of the code), Surface (the exported names it adds or changes), Held by (the
tests that guard it, written first), Changelog, Provenance (what it was
decomposed from), Waits on (issues that land before it), Touches (the
directories it edits, which the `scope` check reads), and Parity. Parity is
a required dropdown with exactly three values, from
`.github/ISSUE_TEMPLATE/lane.yml`:

```yaml
  - type: dropdown
    id: parity
    attributes:
      label: Parity
      description: Every runtime component exists in both languages and is held to one suite.
      options:
        - Both languages, in this lane
        - The generator or the docs only — parity does not apply
        - An additional-language port, following docs/languages/tiers.md
    validations:
      required: true
```

"Both languages" means Go and TypeScript; the third value is the catch-up
lane's escape from the rule rather than a parity claim. That dropdown is
the twin rule's operational form. The rule itself, from the collaboration
guide:

```markdown
A change to one language is not
done until its twin has it and the conformance suite says so; a scenario
is written once, for every language.
```

The dropdown is advisory in practice. Of 79 issues carrying a Parity
field, 18 say the first value verbatim, 18 the second, 2 the third, and 41
rewrite the field as free prose; most of the prose expands one of the three,
and a few name a scope the dropdown has no value for ("all eight languages
in this lane", "language-agnostic"). The Python, Rust and C++ base-runtime
port lanes each give, as their Parity field, a near-identical clause that
routes any divergence back to the operator rather than into a port-local
convention: "any unruled semantic discrepancy becomes a design issue rather
than a Rust-only convention", where unruled means not yet settled by an
operator verdict.

A twin lane lands Go and TypeScript files side by side in one commit. In
pull request #444, discussed below, nearly every runtime file landed beside
a counterpart: `runtime/go/dispatcher.go` beside
`runtime/ts/src/dispatcher.ts`, `runtime/go/serial_test.go` beside
`runtime/ts/src/serial.test.ts`, through 22 files under `runtime/go` and
21 under `runtime/ts`, of 371 files the pull request touched in all. The
scenario is born in the same lane: a JSON file under
`conformance/scenarios/<layer>/`, its driver operations added to both
testees and to the driver protocol page. Two tests then pick it up with no
registration. `Open` builds the testees and installs the cleanup shown
above; `pair` runs every scenario for one ordered pair; `Pairings(false)`
is the star. From `conformance/go/conformance_test.go`:

```go
// TestSelf holds the Go testee to every scenario on both sides of the
// wire: the reference passes its own suite before anyone is held to it.
func TestSelf(t *testing.T) {
	s := Open(t)
	s.pair(t, "go", "go")
}

// TestStar holds every other language to Go, on either side in turn: the
// gate holds the tier's promise; nonblocking failures remain in the matrix.
func TestStar(t *testing.T) {
	s := Open(t)
	for _, p := range s.Pairings(false) {
		if p[0] == "go" && p[1] == "go" {
			continue
		}
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
}
```

A port implements the same scenario later, in a catch-up lane: one issue per
port, filed from one template, each naming the scenario file and the Go and
TypeScript unit tests that hold the rule, each saying before it says where
to look, "Until this lands, this port stays provisional." The Rust catch-up
for the serial rule landed as a pull request of two files. The six issues
name a case table the rows are not in; the pull request that created the
rule explains that the operator's ruling asked for the case in one table
and construction gave it a table of its own, and the issue bodies were
filed from the ruling. The scenario file they name is right and points at
the right table. Bodies rot against the tree; the scenario does not.

The onboarding page, `docs/languages/onboarding.md`, makes joining the
twin cohort a decision, not a gate. Its "join the reference" is the older
wording for joining the cohort, not for becoming the arbiter:

```markdown
Tier 1 is a separate decision: a language whose lanes have shipped
simultaneously with Go's and TypeScript's for a sustained period may join
the reference, and thereafter a feature lane includes it too.
```

Where the pilots' profile ladders stand as filed issues: Python's epic
lists five profile stages and a promotion step but has only its base
runtime and three catch-ups filed. Rust's epic has the whole ladder filed
as four open lanes after the base runtime (generator, tunnel, live,
observability), serialized to one another by their Waits-on fields.

#### What a tag couples, and what each language ships

One tag, `v0.6.0`, plus one tag per nested Go module, cut by hand on the
same commit. From the releasing guide, `RELEASING.md`:

```markdown
A release is one version across every published TypeScript package under
the `@nightseam` scope, the version the generator writes into a generated
client's manifest (`DefaultRuntimeVersion` in
`internal/targets/typescript/target.go`), and the tag of the Go module and
of every Go module nested in it. They move together, and a test in the fast
tier (`cmd/nightseam`, `TestVersions…`) fails when they drift.

The Rust Cargo workspace also follows this version. `scripts/version.mjs`
moves its package version, local crate requirements and local lock entries;
`scripts/release-prepare.mjs` and the script tests refuse drift. The core
Rust crates are packaged and consumed by an outside smoke but are not
published to crates.io in this lane.

This is the current lockstep release policy. The 0.5.0 removal of the governed
session layer is a clean break under [COLLABORATION.md](COLLABORATION.md), not
a permanent compatibility policy for consumers of future releases.
```

"The fast tier" there is the `fast` CI job. What the tag actually
publishes, per language, read from the manifests and the release workflow
on 22 September 2026:

| language | published artifact | registry | version held to the tag by |
|---|---|---|---|
| TypeScript | six npm packages, with npm provenance attestations | npm, the `@nightseam` namespace | each `package.json`, the version script, the release script, a Go test |
| Go | three module tags; nothing uploaded | the Go module proxy, from the tag | the nested modules' requirement on the root at the tag |
| Python | a wheel built and installed into a scratch environment; nothing leaves the runner | none | `pyproject.toml`, read by the release script |
| Rust | crate archives packed and consumed by a smoke; nothing uploaded | none; the public crates carry no `publish = false` | the Cargo workspace, by a helper the release script imports |
| Haskell | source distributions installed into a scratch consumer; nothing uploaded | none | three cabal files in a fixed list; the check is skipped when the Haskell testee is absent |
| Java | two jars built into a directory; the public sources are subprojects of the private conformance build | none | not checked; the build file says `0.6.0-SNAPSHOT` |
| C++ | nothing; consumption is a CMake subdirectory of a pinned checkout | none | not checked; the CMake project declares no version |
| Swift | nothing; the runtime package depends on the duplex package by relative path, so it is checkout-only | none | not checked; the manifest has no version field |

Which components exist per language, from the directory tree:

| language | duplex | runtime | tunnel | live | otel | auth |
|---|---|---|---|---|---|---|
| Go, TypeScript | yes | yes | yes | yes | yes | yes |
| the six ports | yes | yes | no | no | no | no |

No port has begun the second onboarding step, a generator target. The only
checked consumer of a release, the getting-started example, is a Go server
and a TypeScript client.

#### What the eight languages can express, against what the protocol asks

Which languages a feature is born in is a question about idioms, so this
section says what the definition demands and where each port stands. Its
second half is our own reading, marked as such.

**What the definition demands of a host language.** The declaration
language has two sorts of generic parameter: a type parameter filled by a
type expression, and a **family parameter** filled by a whole family, where
a family is one declared API (its types, operations, events and callables
in a set of files) and the parameter draws associated types from it. In
TypeScript a family parameter is one type parameter with indexed access to
its associated types; in Go it becomes one type parameter per type drawn,
tied together by a tag. The rendering must satisfy the **instantiation
law**: binding the parameters into a declaration and rendering it plain
must equal rendering it generically and instantiating. A **witness** is a
mechanical check that the law holds; Go supplies one by reflection and
TypeScript by a type-level equality assertion. The decision page that
adopted the two sorts of parameter,
`docs/decisions/one-parameter-mechanism-of-two-sorts.md`, forecast what
each pilot will cost:

```markdown
What this costs is visible ahead of the pilots and is the point of taking
it here: Haskell and Rust have bounded generics and will render both sorts
directly; C++ has templates with no bound short of concepts and will render
a family parameter as an unconstrained one; Python's generics are erased
and will render the bound as a runtime check or not at all. A form only the
bounded languages can hold would be the wrong form, and this one is not:
the *sort* is a fact of the declaration, and a language that cannot express
it still renders the shape.
```

("Bound" in that quote means a constraint on a parameter, not the binding
of the law.)

The live component demands more of a language than the type language does.
A callable value crossing the wire becomes a binding with explicit
ownership; the design refuses reference counting, native-function identity
and disposal wrappers, which are the three things Rust, C++ and Swift reach
for first. A returned callable may outlive the call that produced it, and
conversion must complete synchronously within an owner's batch. The two
twins already disagree on the shape of a generated callable: Go threads
the invocation's context as a first argument, TypeScript as a trailing
options bag. We call a settled, language-wide convention for threading a
per-call context an **ambient-context idiom**, whether by argument or by
dynamic scope; a language with neither has to invent a third shape.

The runtime's one demand that matters here: request serials must increase
in emission order, under one lock per direction. That is the rule that
broke all six ports at once last week.

**Where the six ports stand today.** All six announce the two core layers
and nothing above them; three announce the observer feature and three do
not. Two divergences are already recorded in the tree, and both were found
only because a port existed:

- Java's observer receives payload-free maps rather than a typed event
  union, because Java's sum-type idiom, sealed interfaces, is closed by
  construction and cannot be extended by the tunnel and live layers the way
  Go's marker method and TypeScript's declaration merging can.
- Swift compares strings by canonical equivalence and its native dictionary
  conflates keys the wire keeps distinct, so the Swift port carries a
  parallel key representation and compares paths by their UTF-8 bytes.
  Neither twin would have surfaced this.

The serial rule is a concurrency-ordering rule that neither the type
language nor the lifetime model touches. Go's answer is a channel and
TypeScript's is an event loop that serializes for free; the six ports have
six different answers.

**Our reading of the idioms, marked as ours.** We group the six into two
**idiom clusters**, sets of ports whose idioms stress the same part of the
design:

| cluster | languages | what they stress | why the twin pair is silent on it |
|---|---|---|---|
| declaration-time | Python, C++, Java | generics rendering and the instantiation law: erased or unconstrained parameters, no witness for the law, and in Java no open-yet-exhaustive sum type | Go and TypeScript both supply a witness and both have an open sum-type idiom |
| runtime-lifetime | Rust, Haskell, Swift | the live component and the callable signature: affine ownership, purity and laziness, reference counting, and no ambient-context idiom | Go and TypeScript are both garbage-collected and both have an ambient-context idiom |

Predicted hardest area per port, with our confidence: Python, generics
(high); Rust, live values (high); C++, generics (medium, live a close
second); Haskell, live values (high); Java, the open observer event union
(medium-high, already observed); Swift, wire framing and string identity
(medium-high, already observed).

### Relevant Code

#### The policy file, whole, and how it maps to the model

`conformance/profiles.json`, the file both enforcers read:

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

| fact in the model | in the policy file | in the Go runner |
|---|---|---|
| promised set | a tier's `requires` | `tier.Requires` |
| disposition | a tier's `onFailure` | `tier.OnFailure` |
| twin | no field; onboarding prose only | none |
| reference | `languages.<name>.reference` | `Language.Reference` |

A profile's own `promise` label (`wire`, `generated`, `complete`) names the
bundle it belongs to and is not the per-language promised set. Every port
is at tier 4, so no port can refuse a tag today, and the cost of a refuse
disposition is entirely prospective.

#### A scenario as it is born

The scenario that made every port red on 21 September 2026,
`conformance/scenarios/peer/request-serials-increase-in-publication-order.json`,
whole with whitespace collapsed. How to read it: `a` and `b` are the two
parties and `runner` is the harness; an op is `family.verb`; `bind` names
the handles an op returns and `$name` refers to one, with `$case.member`
reading a field of the current case-table row (the file spells the loop
variable `row`); `foreach` with `where` selects the cases; 4011 is the
protocol's close code for a violation of the profile's rules; "the
profile's own close" in the notes means the wire-level protocol.

```json
{
  "name": "a request serial that does not increase ends the connection",
  "layer": "peer",
  "needs": ["peer", "seam"],
  "foreach": {
    "table": "tables/serials.json",
    "as": "row",
    "where": { "valid": false }
  },
  "steps": [
    { "on": "runner", "op": "pair.peer_and_conn",
      "args": { "peer": "b", "role": "server" },
      "bind": { "peer": "pb", "conn": "ca" } },
    { "on": "b", "op": "peer.handle", "args": { "on": "$pb", "method": "echo", "behavior": { "kind": "echo" } } },
    { "on": "b", "op": "peer.handle", "args": { "on": "$pb", "method": "wait", "behavior": { "kind": "wait" } } },
    { "on": "b", "op": "peer.handle", "args": { "on": "$pb", "method": "read", "behavior": { "kind": "echo" } } },
    { "on": "a", "op": "conn.send",
      "args": { "on": "$ca", "kind": "text", "text": "$row.before" },
      "note": "what the direction published first, which the receiver takes as its mark" },
    { "on": "a", "op": "conn.receive", "args": { "on": "$ca" },
      "note": "its answer, so that the mark is set and the peer is idle before the serial that does not increase arrives: what is judged is the order, not a race with a write" },
    { "on": "a", "op": "conn.send",
      "args": { "on": "$ca", "kind": "text", "text": "$row.frame" },
      "note": "and the serial that does not increase, refused as a malformed frame is" },
    { "on": "b", "op": "peer.await_close", "args": { "on": "$pb" },
      "expect": { "clean": false, "code": 4011 },
      "note": "the profile's own close: a side that broke it is told so, and told the same thing by either language" },
    { "on": "a", "op": "conn.await_close", "args": { "on": "$ca" },
      "expect": { "code": 4011 },
      "note": "and the raw side reads that code off the wire, where an abort would have left it 1006 and nothing to act on" }
  ]
}
```

Its `needs` lists two layers and no feature, so its layer `peer` places it
in `core`, the one profile every port promises. Its case table has four
rows, two invalid; a sibling scenario runs the two valid ones. Our
arithmetic for the matrix, marked as ours: two scenarios times two cases
times two orientations is eight runs per port; six pass and two fail,
because the rule binds the receiving peer, so a port that does not check
serials fails only the two invalid cases in the orientation where it is the
receiver. The diff below shows exactly that: six more passes and two
failures.

### Data / Control Flow

#### A feature landing with the ports behind, as it happened

Pull request #444, merged 21 September 2026, adopted a wire-contract change
that added per-request serial numbers. It touched Go and TypeScript only,
added the two serial scenarios, and said what would follow. From its "What
changed" section:

```markdown
Six issues are filed for the tier-4 ports — #448 Python, #449 Rust, #450
Java, #451 C++, #452 Haskell, #453 Swift — which stay provisional until
they conform.
```

And from its "What the gate and the acceptance ran" section:

```markdown
The regenerated matrix is committed in its own commit. It records the
intended consequence of the serial rule: the six tier-4 ports each fail
the two new serial scenarios and drop from `ok` to `provisional`, which is
what #448-#453 exist to close. Tier 4's `onFailure` is `provisional`, so
the star stays green and the release still ships, marked.
```

The matrix diff at that commit, for one port:

```diff
     "python": {
       "tier": 4,
-      "verdict": "ok",
+      "verdict": "provisional",
       "cells": {
         "core": {
-          "passed": 214,
+          "passed": 220,
           "skipped": 0,
-          "failed": 0
+          "failed": 2
         },
```

All five required checks passed; `full` was green with six languages
failing two promised scenarios each. The next day five of the six ports'
catch-up lanes landed, the Rust one as two files; Java's is still open.
Under a refuse disposition for any port, the first pull request would have
been unmergeable until that port's implementation was in it.

#### The same feature under the proposal, three ways

- **All six ports at refuse, merge gate as is.** The pull request carries
  eight implementations or does not merge. The twin cohort is everyone.
- **All six ports at refuse, merge split from release.** The pull request
  merges with the twins; main is untaggable until six catch-up lanes land;
  the release gate refuses in between. The catch-ups run in parallel, so
  the wait is the slowest of six, plus whatever else those lanes carry.
- **Ports at mark, as today.** The pull request merges, the tag ships
  marked, the catch-ups land when they land. The promise to a consumer of
  a port is the label, not the gate.

#### What the tag costs today, without any port at refuse

After the preflight's two minutes, the tag waits for a human reviewer,
installs eight toolchains, runs eight languages' unit suites and smokes and
the whole star, and uploads six npm packages. A port that fails to build at
the mark disposition is recorded absent and does not stop the tag; a
missing toolchain does, at every disposition.

### Constraints

**Fixed by the operator's framing.** Tier 4, now "promises core, marked",
is fine. The four facts per language are the vocabulary. The matrix stays
as the picture of what exists. Every language keeps running the whole
suite. The first consultation's conclusions above are the starting point,
not up for re-argument.

**Flexible.** The nouns and facts; whether "feature" or "release" needs to
be more than it is; how twins are chosen and how many; whether merge is
split from release and by what mechanism; whether one tag is the coupling;
what a refuse disposition should refuse; whether publication should precede
a promise.

**Fixed by the operator's rulings and the written goals.** Ratified prose
comes in three genres: goals (standing commitments a review measures the
tree against), decision pages (one settled question each, with what it was
chosen over), and guides (how-to). Each quote below is labelled.

- **Parity is the contributor's form of a goal.** From the collaboration
  guide:

  ```markdown
  Parity is the contributor's
  form of a goal, the language dimension of
  [docs/goals/agnosticism.md](docs/goals/agnosticism.md): the thing is
  defined outside every language, and each language is one realization of
  it, none ahead and none the definition's home.
  ```

  The operator's standing rule, given when an issue proposed that
  TypeScript did not need a generated server side: parity between Go and
  TypeScript holds independent of consumer demand and is never a
  per-feature question. We read that ruling as fixing Go and TypeScript as
  a floor, not as forbidding an additional per-lane twin; if the expert
  reads it as forbidding candidate B under question 2, say so.

- **The reference is a tool, not the definition.** From the agnosticism
  goal: "the one that decides is a tool for settling disagreement, not the
  definition's home — its decisions are written back into the definition,
  so that the silence closes."
- **A lane lands alone.** From the collaboration guide: "One lane, one
  session, one worktree, one pull request: a lane lands on its own, green,
  and carries nothing of another lane." And: "No approval is required: a
  green, in-scope PR is its author's to land."
- **No legacy, rewrites are cheap.** From the same guide: "A change is made
  directly and whole: a wire change alters both peers, both validators, the
  tables, the generator and the docs in one lane, emitting what they accept
  in the same commit." There is no released consumer to stay compatible
  with.
- **Lockstep is the current policy, not a settled one.** The releasing
  guide calls it "the current lockstep release policy" and says the last
  clean break was "not a permanent compatibility policy for consumers of
  future releases".
- **What a language promises is the operator's decision.** Any change here
  is a design issue with a verdict.
- **Adopting the per-language model needs one sentence overturned.** The
  tiers page, a guide, says "There is no fifth: anything finer than these is
  progress within a language, which the matrix shows and no tier needs to
  name." The first advice flagged that a per-language promised set gives
  contractual consequences to distinctions that sentence assigns to
  progress. Below this is called the "no fifth promise" sentence.
- **An extension is held the same, or it is not in.** From the
  extensibility goal: an extension is held "to the suite the shipped parts
  are held to, by the same scenarios and the same promises," not "trusted
  because it compiled." This forbids a merge-time tolerance that reads as a
  green skip.
- **Auth stays outside the tier obligations** unless the operator reopens
  it.
- **The pilots are the hardest languages on purpose.** C++ and Haskell are
  among the four planned for promotion because pushing them to every
  profile is how the model is stressed.

### What We've Considered

#### On the decomposition (question 1)

- **Feature as a noun.** We dropped it because nothing reads it: the gate
  reads profiles, the matrix reads scenarios. Two things argue for a
  feature-shaped record anyway: the first advice noted that a genuine lag
  promise would need "the feature obligations introduced at each release"
  tracked, and that a newly discovered defect and a deliberately deferred
  feature are treated alike today. The known-gap annotation and a
  merge-time "new since the last tag" tolerance are both feature-shaped
  records in disguise.
- **Profile as the unit of promise.** Profiles are disjoint accounting
  groups assembled by the placement rule, so editing a scenario's needs can
  move it out of a promised profile. A promise over a profile is a promise
  over whatever the rule puts there. We considered promising per layer
  (five layers, one cross-cut by observability needs) and per scenario
  (too fine, and what the "no fifth promise" sentence forbids).
- **Release as a noun.** Today a release is one tag for everything. If
  languages released separately the noun would split into "release of the
  suite" (what is promised) and "release of a language" (when it holds it),
  which is how OpenTelemetry's specification version and its per-language
  SDK versions relate.
- **What is machine-held and what is prose-held.** Three parts of the
  model are held only by prose today: the twin fact has no field in the
  policy file; the parity obligation is a dropdown that half of lane issues
  rewrite freehand; and the freshness of the committed matrix the release
  gate reads is a sentence in the collaboration guide. The stakes
  paragraph says a rule nobody can operate is what the fleet builds
  towards, and these are the three places that could happen.

#### On choosing twins (question 2)

- **A. A fixed pair: the reference plus one other.** Today's rule. Cheap to
  operate, no decision per feature, and TypeScript exists as "the second
  proof the driver is not Go-shaped". Cost: every feature is checked
  against the same two idioms, and both are garbage-collected, both have an
  ambient-context idiom, and both supply a witness for the instantiation
  law, so an assumption they share is never found at birth. The two
  divergences found so far (Java's closed sum types, Swift's string
  identity) were found only because a port existed.
- **B. The reference plus one twin chosen per lane.** The lane's issue
  names the second twin by what the feature stresses: a generics change is
  born in Go and Rust or Haskell; a callable-values change in Go and a
  language with affine ownership or no ambient-context idiom; a wire change
  in Go and TypeScript. Cost: a decision per lane, and the chosen language's
  lane owner on the feature's critical path. Benefit: the hardest-first
  logic the operator applied to pilot selection, applied at birth. The
  Parity dropdown would gain a way to name the twin.
- **C. A fixed cohort larger than two, chosen for idiom diversity.** One
  from each idiom cluster beside the reference. Cost: every feature waits
  on all of them at birth.
- **D. No twin rule; the gate only.** Every language implements after the
  scenario is settled; the reference alone defines. Rejected by the
  agnosticism goal and by the reason TypeScript exists.

#### On merge versus release (question 3)

- **A. Stage every release-required implementation before merge.** The
  feature lane carries all of them. Honest and simple; makes every
  release-required language a twin in practice, and puts up to eight lane
  owners on one critical path, against the "a lane lands alone" rule.
- **B. The pull-request gate tolerates a release-required language being
  behind on a scenario newer than its last green tag.** The star still runs
  everything and records every cell; a red cell in a promised profile fails
  the pull request only if the scenario existed at the last tag; new
  scenarios fail the pull request only for twins. The release gate is
  unchanged. Needs a way to know which scenarios are new since the last tag
  (the scenario's commit, or a per-tag record of scenarios, which is the
  feature-shaped record from question 1). The gap must be reported as a
  failing cell rather than a skipped one, to honor "held the same, not
  trusted because it compiled".
- **C. Main may be untaggable.** Merge whatever passes for the twins; the
  release gate refuses tags until the release-required languages catch up.
  Simplest mechanism; the cost is that every tag waits for the slowest lane
  and unrelated fixes cannot ship in between.
- **D. Release branches.** Cut a branch when the twins hold; the
  release-required languages catch up on it; tag from it. Standard
  elsewhere; foreign to a repository that has tagged four times in four days
  from main.
- **E. Feature staging by declaration.** A new scenario carries the tag it
  first binds at ("required from 0.8.0"); before that it is reported but
  required only of twins. Makes the feature-per-release record explicit and
  is what a genuine lag promise would have needed.

#### On release coupling and the cost of refuse (question 4)

- **Lockstep, as today.** One tag, one version, every package. A consumer
  who mixes languages has one number to align. Every release-required
  language is a veto on every tag; eight toolchains are eight sources of
  transient red on the release path (a broken-pipe native suite and a
  network fetch of a dependency both happened this week).
- **Lockstep with per-language absence.** One tag, but a language whose
  promise is unmet is left out of that release's publication, visibly, and
  catches up at the next. "Refuse" would mean refusing that language's
  publication rather than the tag. Today this has nothing to act on for six
  of eight languages, which publish nothing.
- **Independent language releases against a versioned suite.** The suite
  is versioned; each language releases when it holds a suite version, and
  its version says which. The federated shape of OpenTelemetry and gRPC's
  wrapped languages, which the first document noted cannot promise
  simultaneity; but simultaneity is exactly what "complete at every tag"
  would no longer mean.
- **Publication as a prerequisite of promise.** A promise is made to a
  consumer, and a language nobody can install has no consumer to be
  promised anything. This suggests a fifth fact per language, published or
  not, or a rule that a refuse disposition presupposes an artifact.
- **The cost of refuse, itemized.** Tag latency becomes the slowest
  release-required language's; each such language is a veto on unrelated
  releases; toolchain flakiness becomes release risk; and if the merge gate
  is not split, merge coupling on top. The work itself is owed either way;
  the cost is when it must be done and what tagging looks like while it is
  not. What refuse buys: a promise the gate holds instead of a label, and
  the model stressed in six languages at every tag.

#### Where we lean, so you can argue with it

On twins, B with A as the floor: Go and TypeScript stay the fixed pair, and
a lane may name a third twin when its feature touches something a port's
idiom is likely to break, the idiom-cluster table being the guide. On
merge versus release, B: a pull-request tolerance for scenarios newer than
the last tag, reported as failing, with the release gate unchanged; C is
the fallback if the "new since last tag" record is more machinery than it
is worth. On coupling, keep lockstep while nothing but Go and TypeScript
publishes, and make an artifact a precondition of a refuse disposition, so
that a port becomes release-required only once a consumer can install it;
the per-language table above reads to us as six languages whose version
strings ride the tag for no consumer's benefit yet. What would change our
mind: a project that runs a per-lane twin choice and found it produced
worse definitions than a fixed pair; a simpler merge mechanism than a
per-tag scenario record; or a reason a language that publishes nothing
should still be able to refuse a tag.

## Questions for the Expert

We are not asking for a finished design. Your reading of the
decomposition, a ranking of the candidates under questions 2 to 4 with your
reasoning, and anything under question 5 is the right size.

1. **Is the decomposition right?** We model this as four nouns (scenario,
   profile, language, release), four facts per language (promised set,
   disposition, twin, reference) and three rules (the matrix records, the
   gate enforces at a tag, the twin rule decides where a feature is born).
   In systems you know that hold many implementations to one definition,
   what are the equivalent nouns, and where do the boundaries fall
   differently from ours? Three things we are least sure of: whether
   "feature" needs to exist as a record even though no gate reads it;
   whether a profile, a set of scenarios assembled by a placement rule in
   which a scenario's needs override its layer, is a sound thing to promise
   over; and which of these facts have to be machine-readable fields and
   which can stay prose, given that our executors are autonomous agents
   reading these documents with no human reviewer between them and main,
   and that three parts of the model are prose-held today.

2. **How should the languages a feature is born in be chosen when there
   are many?** Today it is a fixed pair, the reference and one other,
   chosen so the definition is not one language's code in disguise. With
   six more languages of deliberately different idioms, how have projects
   you know decided where a change is first implemented: a fixed cohort, a
   per-change choice by what the change stresses, a rotation, something
   else? And does a birth cohort remain the right idea at eight
   implementations, or does it dissolve back into "the reference plus the
   gate"? What does each do to the definition's quality and to a change's
   latency?

3. **How does a feature merge while a language that blocks releases is
   still behind on it?** Our merge check and our release check read the
   same verdict, so a language whose breach refuses a tag also refuses
   every merge that adds a scenario to a profile it promises. Taking
   everything up to the merge as this question's scope, what mechanisms
   have you seen keep "must hold before the tag" from collapsing into "must
   be born in the same change", while never letting the gap read as green:
   staging all implementations, a per-release record of obligations,
   release branches, or an untaggable main? What would you recommend for a
   repository that tags from main every one to two days because nothing is
   installed downstream yet, and lands changes without human review?

4. **Is one lockstep tag the right coupling for eight implementations, and
   what does a release veto cost?** Taking everything from the tag onward
   as this question's scope: every package shares one version and one tag;
   only two of eight languages publish an artifact at all; a language
   whose breach refuses the tag vetoes unrelated releases; and eight
   toolchains on the release path are eight sources of transient failure.
   Where has lockstep across many implementations held up, where have
   projects moved to per-implementation releases against a versioned
   specification, and what did each cost a consumer who mixes
   implementations? Relatedly: what should a breach refuse, the tag or
   that language's own publication, and what, if anything, should a
   promise presuppose about whether anyone can install the implementation
   making it?

5. **What have we not asked?** Knowing the model as stated, the fleet that
   will build from it, and the successor that will inherit it, what would
   you warn us about, or point us toward?

## Applied

The advice (`0002-model-twins-and-release-coupling.advice.md`, run
`run_d9f8ce66-b2c7-4ee0-9f80-f3ff08583d45`, verified genuine; an earlier
run failed on the service side before any expert work) answered the
document at the revision committed as `2d79f6a6`; the only drift since is
the header. Each discrete recommendation was checked against the tree at
`48d391e1` on 22 September 2026 and ruled HOLDS (premise intact),
NEEDS ADAPTATION (right direction, a detail differs), or STALE (premise
gone or never true).

| # | recommendation | verdict | evidence |
|---|---|---|---|
| 1 | Refine the nouns: a scenario is an executable specification, a profile a promise over the scenarios assigned to it at a definition revision, the subject is an implementation rather than a language, a release is a certified snapshot followed by distribution; the evidence is an execution report with its own identity (source, suite and case-table revisions, runner, toolchains, expected executions) | HOLDS | the matrix carries counts and a tier per row and nothing about the revision or toolchain it came from |
| 2 | The Go verdict accepts a missing required cell by falling through to `ok`; checking recorded failures is weaker than checking that all required evidence exists | NEEDS ADAPTATION | `profiles.go:311` tests `cell != nil &&`, so a missing cell is not red there; the release script refuses a matrix with a missing row or cell (`release-prepare.mjs:88-91`) and the runner refuses to write a partial matrix, so completeness is held at release and at write time, not in the verdict that fails the CI job |
| 3 | The committed matrix is a presentation snapshot, not release authority, unless its provenance matches the candidate | HOLDS | the stale serial-rule matrix on 22 September; nothing ties the file to the run that wrote it |
| 4 | Profile membership is contractual: a definition-changing lane should emit an obligation diff (additions, revisions, removals, transfers between profiles, and which implementations gain or lose requirements) | HOLDS, gap | the placement rule can move a scenario out of `core` by adding a need; no diff of obligations exists |
| 5 | An execution dependency is not a contractual entailment: the generator suite needing tunnel and live runtimes does not make promising `generator` entail promising every scenario in `live`; any such closure is an explicit policy choice | HOLDS | corrects this document's "any set the suite's own dependencies allow" |
| 6 | Use stable implementation or target identifiers internally; "language" is a convenient identifier, not the subject | HOLDS | a language with a generator target already has two testees; bitlink's targets are not runtimes |
| 7 | The missing record is the lane's machine-readable contract (affected obligations, birth implementations, governing decision, admitted gaps), resolving against the tree; not a feature catalogue | NEEDS ADAPTATION | the lane issue form has Touches and Parity as text; the scope check reads Touches only to note; nothing resolves against the tree |
| 8 | Machine-readable: all four policy facts, placement and dependency rules, lane birth requirements, admitted gaps, evidence identity and completeness, and the distribution inventory; prose for rationale | HOLDS | today only promised set, disposition and reference are fields |
| 9 | "The matrix reads no policy" should read "evidence collection does not depend on what an implementation promises; placement reads the scenario catalogue" | HOLDS | placement lives in `profiles.json`, so literal file independence is false today |
| 10 | Twins: B with A as the floor first, a diverse fixed cohort second, the fixed pair alone third; replacing TypeScript, or no twin rule, is inconsistent with the parity ruling | HOLDS | matches the document's lean; the ruling is read as requiring both, not as forbidding a third |
| 11 | Vocabulary: a permanent twin is required for every definition-changing lane; an additional birth witness is required for the obligations a particular lane names; the lane's cohort is both | HOLDS | keeps the per-language twin flag stable |
| 12 | Select a witness by the assumption Go and TypeScript could share that would make the change look sound when it is not; the cluster table is a hypothesis guide; selection happens when the lane is cut, mechanically where a mapping exists, by the operator where new | HOLDS | the Swift string-identity finding already escapes its cluster |
| 13 | A witness cannot supply proof it lacks the components for; land prerequisites first or put them on the critical path; a stub or handwritten demonstration is not evidence | HOLDS | no port has tunnel, live, observability or a generator target |
| 14 | Arrow, WebAssembly and TC39 support keeping a birth cohort at eight implementations; none is comparative evidence that idiom-selected witnesses beat a fixed diverse cohort | HOLDS | external; taken as the advisor's reading |
| 15 | "New since the last tag" is insufficient: a case table can change an old scenario's meaning; a new scenario can expose an old defect; and a port that catches up then regresses before the next tag would stay tolerated | HOLDS | the third case is decisive; corrects this document's lean on question 3 |
| 16 | Merge rule: every breach in the candidate is either an outstanding admitted gap from the tested base or a gap this lane's authorized change admits; once an obligation passes its admission closes; four requirements (valid contract and complete evidence, birth cohort passes, established coverage preserved, every remaining breach accounted for); marked implementations keep mark semantics | HOLDS | design; nothing implements it |
| 17 | Do not rewrite a skip as a failure; keep execution outcome, promise interpretation, merge admission and release verdict as four separate readings of one result | HOLDS | corrects this document's "reported as a failing cell" |
| 18 | Known-gap annotations supply the record format; a merge admission never waives a refusing implementation's release obligation; expiry forces reconsideration | HOLDS | design |
| 19 | The coupling is more than the cleanup: toolchain, startup and capability checks fail unconditionally, and unit suites sit in `full`; a newly owed capability must be representable as missing evidence, and an unrelated failure must not become a catch-up exception | HOLDS | `HoldToTier` and toolchain checks call `t.Fatal` at every tier; the port unit suites run in `full` |
| 20 | Validate admission against the actual integration candidate: with branch-up-to-date off, two lanes each acceptable against an older base can combine unacceptably; a merge queue or an equivalent final check | HOLDS, gap | the ruleset sets `strict_required_status_checks_policy: false` by design |
| 21 | Ranking on question 3: strengthened B with untaggable main as its resulting state; A second; D when independent fixes become necessary; bare C too permissive; E last because it reintroduces a release-bound promise | HOLDS | reasoning |
| 22 | Keep lockstep for the intended cohort, the four pilots once their promises hold and the operator accepts the cost; do not extend it automatically to Java and Swift | HOLDS | matches the policy file's `planned` map |
| 23 | Do not make registry publication a prerequisite for a refuse disposition; require a documented, reproducible way to consume the implementation outside its harness, which a pinned checkout plus an external consumer smoke can satisfy; record distribution as a per-implementation inventory, not a boolean | HOLDS | corrects this document's lean; Python, Rust, Haskell, C++, Java and Swift each already have an out-of-checkout consumer smoke |
| 24 | The veto's cost is that an unrelated fix may wait; four properties cannot all hold (merge before catch-up, complete at every release, releases from current main, unrelated fixes always ship); immediate availability gives; when a release is wanted the fleet prioritizes closing admitted gaps | HOLDS | reasoning |
| 25 | Price promotion by observed behavior: time blocking ready candidates, toolchain first-attempt reliability, catch-up latency, redesign, operator intervention | HOLDS | method; the document had incidents, not rates |
| 26 | Separate semantic and infrastructure costs: an unavailable marked implementation should yield missing evidence and a provisional mark, not a global veto through a hard-failing shell step | HOLDS | the open workflow-step issue #454 is exactly this |
| 27 | The release gate is on the wrong side of the tag: the workflow starts on a pushed `v*` tag, Go is consumed from module tags, and tags are immutable, so preflight and reviewer cannot refuse the tag's creation; sequence should be prepare, certify, approve, tag and publish, verify | NEEDS ADAPTATION | `release.yml` triggers on `push: tags`; the releasing guide's step 3 requires a dry run of the exact commit before tagging, so the pre-tag gate exists by discipline, not by mechanism |
| 28 | Partial publication needs an explicit state and recovery procedure; the post-upload round trip is confirmation, not a gate | HOLDS | the 0.6.0 round trip was red after the tag |
| 29 | Ranking on question 4: certified lockstep first; independent releases against identified definition revisions second when autonomy becomes a requirement; automatic per-language omission last, and calling it "refuse" would conceal the change | HOLDS | matches the lean's first choice |
| 30 | Who authorizes changes to the rules: agents can edit the policy file, the tests and both enforcers, and merge without review; bind enforcement-changing edits to an operator-authorized decision the candidate cannot self-approve | HOLDS, gap | the scope check refuses only on issue, milestone, title and trailer; the ruleset has no path rules and code-owner review is off |
| 31 | Give both enforcers shared behavioral fixtures (missing cells, unknown implementations, absent testees, profile transfers, admitted gaps, stale evidence) | NEEDS ADAPTATION | each enforcer has its own tests with inline fixtures; nothing is shared |
| 32 | Passing against the reference is not transitive; a known reproducible all-pairs incompatibility between required implementations should force an explicit release decision | HOLDS, gap | all-pairs failures become issues and nothing reads them at release |
| 33 | The displayed serial scenario proves receiver rejection, not sender allocation under contention; a profile's name should not imply evidence its scenarios do not supply | HOLDS | the sender side is held by the two unit tests, not by the scenario |
| 34 | bitlink needs applicability from the definition and policy (inapplicable versus unimplemented), and the declaration-rendering laws separated from generated-RPC integration before it adopts the `generator` name | HOLDS | design for the successor |
| 35 | Same-version completeness is not cross-version compatibility; do not let the lockstep number read as that promise at 1.0 | HOLDS | reasoning |
| 36 | Two rules to build around: merge admission (birth evidence, established coverage preserved, unfinished obligations named) and release certification (every release-required implementation supplies complete passing evidence against the exact candidate before it is publicly released) | HOLDS | the model's second and third rules, made operational |

**What was integrated.** Nothing in code; the model and the rules are the
operator's to settle. The integration is this section and the model as
the advice leaves it, below, for the design issue.

**The model as the two consultations leave it.**

- **Nouns.** Scenario (an executable specification), profile (a promise
  over the scenarios assigned to it at a definition revision),
  implementation (the subject; a language today, a target later), release
  (a certified snapshot, then distribution). Evidence is an execution
  report with its own identity; the matrix is its projection.
- **Facts per implementation.** Promised set; disposition (refuse or
  mark); permanent twin; reference. Distribution is a per-implementation
  inventory beside them, not a fact of the promise.
- **Rules.** Evidence collection does not depend on promises. Release
  certification: every release-required implementation supplies complete,
  passing evidence against the exact candidate before the first public
  release marker. Merge admission: a lane supplies its birth evidence
  (permanent twins in full, its named witnesses on its named obligations),
  preserves established required coverage, and names every deliberately
  unfinished obligation as an admitted gap; once an obligation passes, its
  admission closes.
- **Twins.** Go and TypeScript are the permanent twins. A lane names an
  additional birth witness when its change rests on an assumption the two
  could share; the witness passes the lane's obligations through the real
  implementation, and may need its prerequisites landed first.
- **Coupling.** One certified release for the release-required cohort,
  which is the four pilots once their promises hold and the operator
  accepts the cost. Untaggable main is a tolerated state with a scheduling
  rule, not a permanent one. The certification runs before the tag exists.

**Where this changes the document's lean.** Three places: the merge
tolerance is per admitted gap with regression prevention, not per file
age; a refuse disposition presupposes reproducible consumption, not a
registry upload; and a skip stays a skip, with the merge admission and
the release verdict as separate readings of it.

**What only the operator can decide, and what the advice adds to the list
from 0001:** whether to overturn "no fifth promise"; whether the pilots
become release-required and when; the twin-witness rule and the Parity
form that carries it; the merge-admission rule and the lane contract it
needs; that enforcement-changing edits require an operator-authorized
decision a lane cannot self-approve; and that certification moves before
the tag.

**Defects and gaps found on the way, each a lane of its own if wanted
regardless of the verdict:** the committed matrix is not held to the run
that wrote it (3); the Go verdict does not check completeness (2); the two
enforcers share no fixtures (31); nothing prevents a lane from editing the
policy or the enforcers (30); a workflow build step can fail the required
check for a marked language, already filed as #454 (26); all-pairs
incompatibilities between required implementations reach no release
decision (32).
