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
consumer's concept to be stated, it belongs in the consumer.

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
the point of this phase. `docs/layers.md` is the test for where a thing
on the wire belongs; it was written after this rule was learned the hard
way in one afternoon.

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
`duplex/go/duplextest`, `session/go/sessiontest` and their TypeScript
twins, stay as each component's unit suite. A change to one language is not
done until its twin has it and the conformance suite says so; a scenario
is written once, for every language. A third language joins by writing a
testee, `conformance/<lang>/testee.json` and the program it names — at the
tier it can hold: what each tier promises and how the suite enforces it is
[docs/tiers.md](docs/tiers.md), and the last run's matrix is
`conformance/matrix.json`.

## The two tiers of tests

`go test -short ./...` is the fast tier and needs Go alone: every package's
own tests, every target's `Check`, and the corpus under
`cmd/nightseam/testdata` — families, what every target renders for them held
file for file under `golden`, the exported surface of every generated Go
package under `surface`, what each target reserves, and one checkout per rule
the tool refuses with what `validate` says. It runs on Linux and Windows in
CI, since the fixtures are byte comparisons.

`go test ./...` is the full tier: the fixtures compile and run the generated
packages in both languages, and the conformance suite holds every language's
testee to Go's — `go test ./conformance/go` alone runs it. They need Node 22.12 or later and the TypeScript
compiler `pnpm install` brings — and **fail rather than skip** when one is
missing, since a skip nobody reads is a gate nobody passes.

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

## Lanes

Work is cut into lanes, one issue each, of a size one session lands by
itself. An issue says:

- **What** — the change, in the terms of the code.
- **Surface** — the exported names it adds or changes, spelled out.
- **Held by** — the tests that hold it, named; the test that guards the
  invariant is written first.
- **Provenance** — what it was decomposed from.
- **Waits on / Unblocks / Touches** — the issues before and after it, and
  the files it edits, so that two lanes are not cut through one file at
  once.

One lane, one session, one branch or one tree: a lane lands on its own, with
both tiers green, and does not carry another lane's change with it.

## Working in one tree

Several sessions may work in one checkout. Stage by path, never `git add
-A` over a tree you do not own wholesale; commit only the files of your lane;
read `git status` before you commit and leave what is not yours as you found
it. A file another lane is editing is not yours to reformat. Line endings are
LF everywhere (`.gitattributes` says so), and a stray binary is never
committed.

## Commits

One commit is one change, and its message says what changed and why, in one
sentence, from the area it changes: `runtime/go: …`, `tunnel/ts: …`,
`generator: …`, `docs: …`, `goldens: regenerate — feature: …`. The sentence
is the review's first line and the changelog's raw material; a commit whose
message needs a paragraph is usually two commits.

## Releases

Versions move in lockstep across the published TypeScript packages, the
generator's `DefaultRuntimeVersion` and the Go module's tag; `RELEASING.md`
has the procedure. Between releases, `CHANGELOG.md` collects what landed
under *Unreleased*, in the words of the commits.

## Asking and reporting

Design questions and reports of what is wrong go in issues, in the form
above where they describe work; a security concern goes the way
`SECURITY.md` says, not in an issue.
