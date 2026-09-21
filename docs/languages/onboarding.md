# Onboarding a language

A language is in Nightseam when it is in the README's table, and what it
promises is its tier ([languages, profiles and tiers](tiers.md)). A third
language is `<component>/<lang>` for each published component, a target
under `internal/targets/` (Go's is `golang`, and `markdown` is a writer of
the specification, a target that is no language), and a testee under `conformance/<lang>`; nothing else moves.
Every language is held to the same scenarios, written once, and the Go
testee is the reference it is held to on both sides of a real socket.

Language-support tiers describe those tested promises. They are separate
from the declaration tiers: `model.json` describes data, `protocol.json`
adds ordinary operations and events, and `live.json` adds callable values.
A tier-4 language has core wire coverage; that number says nothing about
which declaration file a consumer writes.

## The order

The tiers are the order a language is built in, and each step is a lane of
its own that lands alone:

1. `duplex/<lang>` and `runtime/<lang>` with a testee holding `core` — the
   language enters the matrix at tier 4. What the runtime implements is
   [the profile](../wire/profile.md), and nothing else; what the seam
   beneath it promises, every transport of the language is held to by a
   conformance suite of the seam's own, as `duplex/go/duplextest` and
   `duplex/ts/src/conformance.ts` hold Go's and TypeScript's. The shared
   [seam](../../conformance/scenarios/seam) and
   [peer](../../conformance/scenarios/peer) scenarios hold the runtime
   against Go in both roles.
2. Start `internal/targets/<lang>` and its generated testee toward
   `generator` coverage and tier 3. A target renders a family from what `render`
   presents ([the pipeline](../declaration/pipeline.md)), plans every
   identifier it will declare, and is held by goldens of its own under
   `cmd/nightseam/testdata`; what it reserves goes under `reserved`, and the
   names it derives follow `conformance/tables/naming.json`. The
   [generated scenarios](../../conformance/scenarios/generated) exercise
   the client and server binding separately, including reverse calls;
   implementing only the client does not complete the generated profile.
   Its live-dependent cases also require step 4: claim complete generated
   coverage and promotion only once every required case runs and passes.
3. `tunnel/<lang>` holds the [tunnel profile](../wire/tunnel.md): channels,
   credit and closure, through the driver's `tunnel.*` operations and the
   shared [tunnel scenarios](../../conformance/scenarios/tunnel).
4. `live/<lang>` holds the [live profile](../wire/live.md). Its runtime
   installs a scope before the peer reads, implements callable export and
   import, explicit owners, release, forwarding, bounds and refusals;
   extend the testee with the driver's `live.*` operations. The shared
   [live scenarios](../../conformance/scenarios/live) check behavior and
   retained binding counts before teardown. Extend the generator alongside
   it: [boundary conversion](../declaration/generated.md#live-values)
   exports native functions and imports typed proxies under the supplied
   owner, including callable arguments and results inside generic values.
   The generated profile holds this separately through callback/result,
   higher-order, nested-value, owner, uncertain-publication and forwarding
   scenarios; a passing runtime live cell alone does not prove generated
   conversion.
5. The observer, propagator, shipped standard-logging adapter and
   `otel/<lang>` adapter hold `observability`, the remaining P3 profile. Implement the
   [observer's events](../runtime/observer.md) under the common names and
   [trace propagation](../runtime/observer.md#the-opentelemetry-adapter),
   including the tunnel and live layers. The shared scenarios whose `needs`
   include `observer` or `propagator` belong to this profile, wherever their
   layer places them; both adapters also have local tests.

When every P3 profile holds for a release, the language may be promoted to
tier 2 under the [existing promotion policy](tiers.md#the-assignment).
Tier 1 is a separate decision: a language whose lanes have shipped
simultaneously with Go's and TypeScript's for a sustained period may join
the reference, and thereafter a feature lane includes it too.

A language's lanes serialize among themselves; across languages they run in
parallel, and nothing of one language waits on another's beyond the
reference. This is a scheduling order: live runs over a peer and does not
require a tunnel. Both runtime components and generated conversion must
hold before a language claims complete support.

## The current lanes

The [language rollout epic](https://github.com/Bitspark/nightseam/issues/34)
coordinates these planned assignments:

| language | epic | planned tier |
|---|---|---|
| Python | [#38](https://github.com/Bitspark/nightseam/issues/38) | 2 |
| Rust | [#39](https://github.com/Bitspark/nightseam/issues/39) | 2 |
| C++ | [#42](https://github.com/Bitspark/nightseam/issues/42) | 2 |
| Haskell | [#43](https://github.com/Bitspark/nightseam/issues/43) | 2 |
| Java | [#41](https://github.com/Bitspark/nightseam/issues/41) | 4 |
| Swift | [#63](https://github.com/Bitspark/nightseam/issues/63) | 4 |

These are targets, not achieved coverage. The `languages` entries in
[`profiles.json`](../../conformance/profiles.json) and the executed
[matrix](../../conformance/matrix.json) record registration and evidence;
the `planned` entries do not register a testee or promise that tier today.
Each epic's scoped child issues hold its implementation work and ownership.
For 0.6.0, the ports wait for the shared wire construction in
[#321](https://github.com/Bitspark/nightseam/issues/321), as required by
[#320](https://github.com/Bitspark/nightseam/issues/320), so they implement
the settled access contract.

## The testee

A testee is a program the runner drives over the protocol of [the driver
protocol](../../conformance/DRIVER.md): it answers `hello` with the layers
and features it implements, and a scenario a testee lacks a need of is
reported as skipped. A skip in a profile required by the language's tier
follows that tier's failure policy; it is not evidence of coverage. Skips
outside those profiles remain informational. The
[tier gate](tiers.md#what-the-gate-checks) applies the same rule to a
missing generated role. `conformance/<lang>/testee.json` names the program and
how it is built; the runner reads it, and `conformance/profiles.json` places
the language at its tier. The suite's own testees — `conformance/go`,
`conformance/ts` — are private, and a language's is too: what is published
is the components, and the testee is how they are held.

## What a language promises before it is in the table

Nothing. A language is in Nightseam when its row is in the matrix and its
tier says what a consumer may rely on; a `<component>/<lang>` directory
without a testee is work in progress, and the README's table says
*planned, with no testee yet* of it.
