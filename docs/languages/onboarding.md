# Onboarding a language

A language is in Nightseam when it is in the README's table, and what it
promises is its tier ([languages, profiles and tiers](tiers.md)). A third
language is `<component>/<lang>` for each published component, a target
under `internal/targets/` (Go's is `golang`, and `markdown` is a writer of
the specification, a target that is no language), and a testee under `conformance/<lang>`; nothing else moves.
Every language is held to the same scenarios, written once, and the Go
testee is the reference it is held to on both sides of a real socket.

## The order

The tiers are the order a language is built in, and each step is a lane of
its own that lands alone:

1. `duplex/<lang>` and `runtime/<lang>` with a testee holding `core` — the
   language enters the matrix at tier 4. What the runtime implements is
   [the profile](../wire/profile.md), and nothing else; what the seam
   beneath it promises, every transport of the language is held to by a
   conformance suite of the seam's own, as `duplex/go/duplextest` and
   `duplex/ts/src/conformance.ts` hold Go's and TypeScript's.
2. `internal/targets/<lang>` with the generated testee holding
   `generator` — tier 3. A target renders a family from what `render`
   presents ([the pipeline](../declaration/pipeline.md)), plans every
   identifier it will declare, and is held by goldens of its own under
   `cmd/nightseam/testdata`; what it reserves goes under `reserved`, and the
   names it derives follow `conformance/tables/naming.json`.
3. `tunnel/<lang>`, then `session/<lang>`, then the observer and the shipped
   adapter, then `otel/<lang>` — the profiles of P3, one lane each, holding
   their scenarios: [the tunnel](../wire/tunnel.md), [the
   session](../wire/session.md), [the observer](../runtime/observer.md)'s
   events under the same names. When all hold for a release the language
   may be promoted to tier 2.
4. Tier 1 is not a step but a decision: a language whose lanes have shipped
   simultaneously with Go's and TypeScript's for a sustained period may join
   the reference, and thereafter a feature lane is written as three twins.

A language's lanes serialize among themselves; across languages they run in
parallel, and nothing of one language waits on another's beyond the
reference.

## The testee

A testee is a program the runner drives over the protocol of [the driver
protocol](../../conformance/DRIVER.md): it answers `hello` with the layers
and features it implements, and a scenario a testee lacks a need of is
skipped, not failed. `conformance/<lang>/testee.json` names the program and
how it is built; the runner reads it, and `conformance/profiles.json` places
the language at its tier. The suite's own testees — `conformance/go`,
`conformance/ts` — are private, and a language's is too: what is published
is the components, and the testee is how they are held.

## What a language promises before it is in the table

Nothing. A language is in Nightseam when its row is in the matrix and its
tier says what a consumer may rely on; a `<component>/<lang>` directory
without a testee is work in progress, and the README's table says
*planned, with no testee yet* of it.
