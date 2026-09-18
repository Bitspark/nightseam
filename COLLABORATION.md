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

## Parity

Every runtime component exists in Go and in TypeScript, and the two are held
to one suite: the seam to `duplex/go/duplextest`, the wire validator to
`runtime/testdata/validator-cases.json`, the session component to
`session/go/sessiontest` and its twin `session/ts/src/conformance.ts`, the
tunnel and the session to a cross-language gate over a real socket. A change
to one language is not done until its twin has it and the shared suite says
so. A third language joins by implementing the suite, in its own directory
under each component — at the tier it can hold: what each tier promises and
how the suite enforces it is [docs/tiers.md](docs/tiers.md).

## The two tiers of tests

`go test -short ./...` is the fast tier and needs Go alone: every package's
own tests, every target's `Check`, and the corpus under
`cmd/nightseam/testdata` — families, what every target renders for them held
file for file under `golden`, the exported surface of every generated Go
package under `surface`, what each target reserves, and one checkout per rule
the tool refuses with what `validate` says. It runs on Linux and Windows in
CI, since the fixtures are byte comparisons.

`go test ./...` is the full tier: the fixtures compile and run the generated
packages in both languages, and the gates hold the Go and TypeScript
components to each other. They need Node 22.12 or later and the TypeScript
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
