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

The index is shared as well as the tree. Two lanes each stage by path and
the first to run a bare `git commit` carries the other's staged files under
its own message — it happened on 2026-09-19. Two ways to commit only what
is yours, and which one depends on where your change is:

- Your change is in the **working tree** and nobody else is in those files:
  `git commit -F message -- <paths>` commits the working-tree state of
  exactly those paths and nothing else the index holds (a new file is
  `git add`ed first).
- A sibling is mid-edit in the **same file**: do not touch the working tree.
  Rebuild HEAD plus your own hunks, stage that as a blob
  (`git hash-object -w` and `git update-index --cacheinfo`), confirm with
  `git diff --cached --name-only` that the index holds your files alone,
  and then a bare `git commit` — a pathspec would commit the working tree,
  which is the union, and undo the point of the rebuild.

Either way, `git diff --cached --stat` is read before every commit as the
question "is every line of this mine?".

## Commits

One commit is one change, and its message says what changed and why, in one
sentence, from the area it changes: `runtime/go: …`, `tunnel/ts: …`,
`generator: …`, `docs: …`, `goldens: regenerate — feature: …`. The sentence
is the review's first line and the changelog's raw material; a commit whose
message needs a paragraph is usually two commits.

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
