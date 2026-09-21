# Collaborating on Nightseam

This is how work is organized in this repository: what a change is held to,
how it is described, and how several people — or several agents — work in
one tree without treading on each other. It is short because the rules are
few; each of them exists because its absence cost something once.

## The boundary rule

Nightseam owns what can be stated in terms of the profile and the
declaration language and is the same for every consumer — mechanism. A
consumer owns what names a concept of its own or decides a policy. Before
adding something here, say which side of that line it is on; if it needs a
consumer's concept to be stated, it belongs in the consumer. The rule is
the contributor's form of a goal, [docs/goals/boundary.md](docs/goals/boundary.md),
which says what the line looks like at the limit and what the other goals
yield to it. Whether a concept is a primitive of Nightseam's, a composition
a consumer writes, or a consumer's own is decided by the test in
[docs/admission.md](docs/admission.md) — scope, the basis one level down,
a composition attempted, the obstruction shown — and a design issue that
admits a concept supplies that evidence in its form; being reusable, already
existing, or owning no state of the consumer's is not the argument.

This repository implements the typed-access foundation and optional rooted-grant
authority profile. The shared Wire contract, supporting language declarations,
composition laws and independent conformance criteria will be adopted from
Bitwire when ready, as required 0.6.0 work in
[#421](https://github.com/Bitspark/nightseam/issues/421). Runtime, generator,
declaration/identity and optional-auth implementation remain here, as recorded in
[the repository-home decision](docs/decisions/the-reusable-foundation-lives-in-nightseam.md).
Until the public contract handover and behavioral evidence are ready, the current
Nightseam definitions remain in use; a private scaffold is not a public dependency.
The authority component checks evidence and preserves guards; consumers choose
roots, issued authority, resource meaning and current policy. Its dependency
direction keeps bare data and RPC independent of auth. Public contracts and
examples must be usable without private checkouts. A package boundary is not
an admission argument, and this selected profile admits no general policy or
proof engine.

## No legacy, and rewrites are cheap

Nightseam has no released consumer, and until it has one there is nothing
to stay compatible with. A change is made directly and whole: a wire change
alters both peers, both validators, the tables, the generator and the docs
in one lane, emitting what they accept in the same commit; a surface change
changes every caller in the same commit; a shape that turns out wrong is
rewritten, not layered over. No accept-before-emit rollout, no "older
runtime" clause, no optional parameter whose reason is to spare a call site
— each is machinery for consumers that do not exist and is kept forever
once written.

The same condition decides between designs: a design is chosen for being
right, never for being cheap to roll out. A rewrite now is cheaper than it
will ever be again — with a release, consumers and more runtimes each one
costs more — so the model is settled now, and the cost of settling it is
the point of this phase. [docs/wire/vocabulary.md](docs/wire/vocabulary.md)
is the test for where a thing on the wire belongs; it was written after this
rule was learned the hard way in one afternoon.

## Parity

Every runtime component exists in Go and in TypeScript, and the two are held
to one suite: [conformance/](conformance/), scenarios as data under
`conformance/scenarios`, run by one Go runner against a **testee** of each
language over the protocol of [conformance/DRIVER.md](conformance/DRIVER.md)
— Go's testee the reference, every language held to it on either side of a
real socket, and what the generator renders for each language held the
same way. The tables under `conformance/tables` — the wire validator's
cases, every envelope a peer accepts or refuses, the naming conventions —
are what each language's own tests read too. The in-process suites,
`duplex/go/duplextest` and its TypeScript twin, stay as each component's
unit suite. A change to one language is not
done until its twin has it and the conformance suite says so; a scenario
is written once, for every language. A third language joins by writing a
testee, `conformance/<lang>/testee.json` and the program it names — at the
tier it can hold: what each tier promises and how the suite enforces it is
[docs/languages/tiers.md](docs/languages/tiers.md), the order to build one
in is [docs/languages/onboarding.md](docs/languages/onboarding.md), and the
last run's matrix is `conformance/matrix.json`. Parity is the contributor's
form of a goal, the language dimension of
[docs/goals/agnosticism.md](docs/goals/agnosticism.md): the thing is
defined outside every language, and each language is one realization of
it, none ahead and none the definition's home.

## The two tiers of tests

`go test -short ./...` is the fast tier and needs Go alone: every package's
own tests, every target's `Check`, and the corpora under
`cmd/nightseam/testdata` — the families under `corpus` and `families`, what
every target renders for them held file for file under `golden` and
`golden-families`, the exported surface of every generated Go
package under `surface`, what each target reserves, and one checkout per rule
the tool refuses with what `validate` says. It runs on Linux and Windows in
CI, since the fixtures are byte comparisons.

`go test ./...` is the full tier — plus `go vet ./... && go test ./...` inside
`otel/go`, a module of its own that the root's `./...` does not enter, in both
tiers. In it the fixtures compile and run the generated packages in both
languages, and the conformance suite holds every language's testee to Go's —
`go test ./conformance/go` alone runs it. They need Node 22.12 or later and
the TypeScript compiler `pnpm install` brings — and **fail rather than
skip** when one is missing, since a skip nobody reads is a gate nobody
passes.

Every TypeScript package checks all of `src/**/*.ts`, including its tests
and conformance helpers, through `tsconfig.check.json`; check and build
extend the same `tsconfig.base.json`. The root supplies Node's test types
and the compiler used by Go's generated-code fixtures; its three runtime
workspace dependencies are the packages those fixtures import. Each
TypeScript package also declares its own compiler dependency.

`pnpm format` formats handwritten TypeScript under `*/ts/src` with the
pinned Prettier version; `pnpm format:check` holds it in CI. Generated code
and golden fixtures remain the generator's output. The duplex package
exposes `./conformance` inside the workspace for shared tests; these helpers
use repository fixtures and are not published entry points.

## The golden discipline

A renderer or a diagnostic changes as a diff of the golden files, which is
what a review reads. When the change is meant, `go test ./cmd/nightseam
-short -run 'Golden|Invalid|Surface|Reserved' -update` rewrites them
from the current output. An output-changing change is two commits: the logic,
then `goldens: regenerate — feature: <what changed>`, so that the review of
the logic is not buried under the diff of its output. `testdata/surface`
changes only when the generated packages' exported surface changes; a diff
there from a change that should not touch it is a bug, and the commit says
so.

Generated code is never edited by hand, here or in a consumer: behavior is
written against the interfaces the generated packages declare.

To compare the generated Go surface shared by two consumer checkouts, run
[`nightseam-surface`](cmd/nightseam-surface/README.md). It uses the golden
suite's declaration reader and reports differences without regenerating
either checkout.

## Lanes

Work is cut into lanes, one issue each, of a size one session lands by
itself. An issue says:

- **What** — the change, in the terms of the code.
- **Surface** — the exported names it adds or changes, spelled out.
- **Held by** — the tests that hold it, named; the test that guards the
  invariant is written first.
- **Provenance** — what it was decomposed from.
- **Waits on** — the issues that land before it. **Touches** — the
  directories it edits, as backticked paths; the PR's scope check reads this
  line and *notes* a file outside it, never refuses. Two lanes may touch one
  file; the second to land merges, which a pull request does for it.

A lane is a **sub-issue of its parent** (an Epic), not a "decomposed from"
in prose: the parent then shows its own progress, closes when its lanes do,
and what waits on what is a query rather than a paragraph. Issue types —
Epic for a parent, Task for a lane, Bug for a defect — are set by the forms;
the `design` label marks a question that waits on the operator's verdict,
which no type expresses. A PR closes exactly one issue, and that issue has a
milestone: the board *is* the milestones, and the scope check refuses a PR
whose issue is on none.

One lane, one session, one worktree, one pull request: a lane lands on its
own, green, and carries nothing of another lane.

## How a change lands

`main` takes no direct push. Every change — a lane's, the coordinator's, a
one-line doc fix — is a branch in a worktree of its own, a pull request, and
a squash merge that lands by itself when the checks are green. The rules
are a file, `.github/ruleset-main.json`; `node scripts/protect-main.mjs
apply` puts them on the repository and CI's `protection` job fails when the
live rules have drifted from the file. Nobody is exempt.

1. **Start.** `node scripts/worktree.mjs add issue-<N>-<slug>` fetches
   `origin/main` and makes `.worktrees/issue-<N>-<slug>` on a branch from
   it. Work there; the main checkout is nobody's working tree. A sub-agent
   is given `isolation: "worktree"` for the same reason.
2. **Work.** Commit in the worktree as often as the work has a whole step.
   Run what CI runs before you push — the two tiers, `pnpm -r check &&
   pnpm -r build && pnpm -r test`, `node scripts/matrix-table.mjs --check`,
   and `go vet ./... && go test ./...` inside `otel/go` — since a red gate
   only spends a CI cycle.
3. **Open.** `git push -u origin <branch> && gh pr create --fill`. The
   title is the commit's first line; the body says what holds it and
   `Closes #N`. Then **queue the merge at once**:
   `gh pr merge --auto --squash --delete-branch`. The PR lands the moment
   `fast` (both platforms), `full` and `protection` are green, whether or
   not the session that opened it is still there.
4. **Merge.** Squash only, so `main` is one commit per lane and its history
   reads as the changelog's raw material. No approval is required: a green,
   in-scope PR is its author's to land. A review is a comment on the PR,
   read by the next lane, not a gate the lane waits on; hold `--auto` only
   when the gate is red, there is feedback to answer, or you are unsure.
5. **Finish.** Stop every process you started in the worktree, leave it
   clean, and `node scripts/worktree.mjs rm <branch>` once the PR has
   merged. The remote branch goes with the merge.

What this replaces: several sessions in one working tree and one index,
staging by path and rebuilding the index by hand to keep out of each
other's commits. That worked until it did not — on 2026-09-19 three commits
carried another lane's files under the wrong message — and a worktree per
lane makes the whole discipline unnecessary rather than more careful.

`conformance/matrix.json` and the README's Languages table are regenerated
by the full suite; a PR whose scenarios change the matrix commits the one
its own run wrote, and CI holds the table to it.

## Commits

One commit is one change, and its message says what changed and why, in one
sentence, from the area it changes: `runtime/go: …`, `tunnel/ts: …`,
`generator: …`, `docs: …`, `goldens: regenerate — feature: …`. The sentence
is the review's first line and the changelog's raw material; a commit whose
message needs a paragraph is usually two commits. A commit carries no
trailer and no attribution line; a squash merge takes the PR's title and
body as the commit, so write those the same way.

## A lesson is a check, never a paragraph

When something bites — a race in CI, a file two lanes edited, a PR that
landed with the wrong title — the fix is one of three things: a line in a
workflow, a script under `scripts/`, or a field in an issue form. It is
never a dated warning appended to this page or to AGENTS.md. Two of this
repository's neighbours grew their agent instructions to four and eight
thousand lines that way — every note a real lesson, every one something an
agent had to remember rather than something a check did for it — and the
reading became the friction the notes were meant to remove. AGENTS.md stays
under fifty lines and points at this page; this page states rules, not
incidents. What cannot be made a check and is not a rule is a `design`
issue.

## Releases

Versions move in lockstep across the published TypeScript packages, the
generator's `DefaultRuntimeVersion`, the Go module's tag and every nested Go
module's requirement on the root module; `RELEASING.md`
has the procedure. Between releases, `CHANGELOG.md` collects what landed
under *Unreleased*, in the words of the commits.

## Asking and reporting

Reports of what is wrong go in issues, in the *Something is wrong* form; a security
concern goes the way `SECURITY.md` says, not in an issue.

A question whose answer is the operator's to give — which way a design
goes, what a language promises, what the model says — is a **design
issue**, in the *Design* form and under the `design` label: one question,
what exists today, the options lettered with what each costs and what not
deciding costs, and a recommendation marked as whose it is. It is answered
in plain words, on the issue or in chat, and the verdict is copied onto it
verbatim before any lane is cut from it; the lanes then name it as their
provenance. A question that is really several is several issues, under one
that indexes them and carries the round's *after the verdicts*. A lane that
cannot yet say what holds it is such a question, not a lane.
