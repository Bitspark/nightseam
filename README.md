# Nightseam

[![ci](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml/badge.svg)](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Bitspark/nightseam.svg)](https://pkg.go.dev/github.com/Bitspark/nightseam)
[![npm](https://img.shields.io/npm/v/@nightseam/runtime.svg)](https://www.npmjs.com/package/@nightseam/runtime)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Declare a duplex API once, in tiers of JSON. Get a typed server and a typed
client in Go and TypeScript — every one of them typed in
both directions, since a client serves what the server calls — all speaking
one wire profile: `nightseam.duplex/1`, JSON frames carrying requests,
responses, events and cancellation over a WebSocket, a tunnel channel or an
in-memory pipe.

Duplex means both ends call. The server calls the client with the machinery
the client calls the server with, declared in the same file and typed the
same way — which is what a browser, an agent and a forwarder need, and
what a request-and-response contract has no way to state.

Version 0.4.0 releases the consumer improvements described in the
[changelog](CHANGELOG.md). Its checker and specification renderer support
the new declaration forms; complete Go/TypeScript generation and value
validation for those forms continue in
[0.5.0](https://github.com/Bitspark/nightseam/milestone/6), which also
removes the governed session layer. Until implemented, the code targets
report `unrendered_form` for those forms.

### Declare it

`api/contracts/probe/protocol.json`

```json
{
  "profile": "nightseam.duplex/1",
  "server": {
    "methods": {"echo": {"request": "Payload", "result": "Payload"}},
    "events": {"changed": {"type": "Payload"}}
  },
  "client": {
    "methods": {"reverse": {"request": "Payload", "result": "Payload"}}
  },
  "errors": {"denied": "The caller is denied."}
}
```

### Write the behavior

Go, the server side. Generated code is never edited by hand; behavior goes
in a file of your own, which `nightseam init probe` writes once:

```go
type Probe struct{}

func (Probe) Echo(ctx context.Context, remote *binding.Remote, p protocol.Payload) (protocol.Payload, error) {
	return remote.Reverse(ctx, p) // the server calls the client, typed, inside the request
}

// wherever you serve:
handler, err := binding.NewHandler(Probe{}, runtime.ServerOptions{})
http.Handle("/probe", handler)
```

### Call it

TypeScript, the client side:

```ts
const client = await Client.dial('wss://example.test/probe', {}, {
  reverse: ({ text, count }) => ({ text: [...text].reverse().join(''), count }),
}, {
  changed: p => console.log('changed', p.text),
});

const payload = await client.echo({ text: 'hello', count: 1 });
```

What you never write: the envelope, the correlation of a response to its
request, cancellation, backpressure, the validator that holds every frame to
the declaration — or the second language's copy of any of it.

## Status

Pre-1.0. The declaration language, the generated surface and the profile
move with minor versions; `CHANGELOG.md` says what each version holds. What
is already held fixed is the agreement between the languages: the
conformance suite under [conformance/](conformance/) holds every language's
seam, runtime, tunnel and generated packages to Go's over a real socket,
scenario by scenario, so a peer of any language is held to the reference
before it is released; `conformance/matrix.json` is the last run's standing
of each language in each profile.

## Languages

A language is in Nightseam when it is in this table, and what it promises is
its assigned **tier**: 1 promises every profile with no lag, 2 promises `core` and
`generator` always and every other profile within a minor release, and 3 and
4 are one band — *reference-held* — that the `generator` column tells apart.
[docs/languages/tiers.md](docs/languages/tiers.md) says what each promise
and each profile is.

Go and TypeScript provide generated clients and server bindings, including
typed reverse calls, events and live-value conversion. TypeScript's
[`serve`](docs/declaration/generated.md#the-binding-package-1) accepts a
connection supplied by the host; authentication and socket listening stay
with the application. The [role inventory](docs/declaration/proof-findings.md#generated-roles-and-skips)
maps the generated socket scenarios to each server operation and language
pairing.

<!-- matrix:start -->
| language | tier | core | generator | tunnel | live | observability | verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `go` *(reference)* | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |
| `typescript` | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |

Planned, with no testee yet: `cpp`, `haskell`, `python`, `rust` at tier 2; `java`, `swift` at tier 4.
<!-- matrix:end -->

The table is the last conformance run, rendered from
`conformance/matrix.json` by `node scripts/matrix-table.mjs`; CI fails a pull
request whose table has drifted from the matrix, as `nightseam check` fails
one whose generated output is stale. `ok` means the current gate found no
failures in required profiles; it accepts skips and does not certify that
every generated role exists. A red cell in a profile the language's
tier guarantees refuses a release; elsewhere it is what the tier's lag
allows. What CI runs is the star — every language against the Go reference on
both sides, which is the gate a language passes to have joined; the full
matrix of every language against every other runs nightly, and a scenario two
non-reference languages disagree about becomes an issue against the scenario,
since the reference decides.

## Install

```
npm install @nightseam/runtime @nightseam/tunnel           # what a generated TypeScript client needs
go get github.com/Bitspark/nightseam                       # the Go runtime packages
go get -tool github.com/Bitspark/nightseam/cmd/nightseam   # the generator, as a Go tool
```

Nightseam is developer tooling and never a runtime dependency of its own
generator: a generated package depends on the protocol types and the runtime
components it uses, including the tunnel for clients and the live layer for
live values.

## The packages

| npm | Go | what it is |
| --- | --- | --- |
| [`@nightseam/duplex`](duplex/ts) | [`duplex/go`](duplex/go) | the seam: ordered frames both ways, an explicit close with a code and a reason, a WebSocket adapter and an in-memory pipe |
| [`@nightseam/runtime`](runtime/ts) | [`runtime/go`](runtime/go) | the peer of the profile: correlation, cancellation, backpressure, presence, trace context, the wire validator, the observer and its console and slog adapters |
| [`@nightseam/tunnel`](tunnel/ts) | [`tunnel/go`](tunnel/go) | channels multiplexed over one peer, each one a connection of the seam, with per-channel credit |
| [`@nightseam/live`](live/ts) | [`live/go`](live/go) | callable values across one connection: a scope over a peer, exported bindings, imported references, release and forwarding |
| [`@nightseam/otel`](otel/ts) | [`otel/go`](otel/go) | the OpenTelemetry adapter, the one component a consumer opts into: a propagator over the W3C trace context propagator and an observer that opens a span per request, so that the four above pull in no telemetry backend — on npm they depend on nothing at all, and in Go on one third-party module, the WebSocket transport |
| — | [`cmd/nightseam`](cmd/nightseam) | the generator |

Every published component exists in both languages and both are held to one
suite; the suite's own testees live at `conformance/<lang>`, private. A third
language is `<component>/<lang>` for each of these, a target under
`internal/targets/` (Go's is `golang`, and `markdown` is a writer of the
specification, a target that is no language), and a testee under `conformance/<lang>`; nothing
else moves, and [docs/languages/onboarding.md](docs/languages/onboarding.md)
is the order to do it in.

## Using it

A family is a directory of tier files, `api/contracts/<family>/` — the
types, the protocol over them, and what each target names otherwise than the
convention does. The generator renders
it into packages it owns wholesale: `api/go/<f>-protocol`, `-binding` and
`-client`, `api/ts/<f>-client` and `-binding`, and the family's specification
as Markdown at `api/spec/<f>/README.md`.

```
go tool nightseam validate            # every diagnostic of every family
go tool nightseam generate            # render what is stale
go tool nightseam init probe          # the handlers you implement, written once
go tool nightseam check               # in CI: fail if the checked-in output is stale
go tool nightseam version             # which version of the tool is running
```

The three blocks above, made to run: [examples/](examples/) — one family, a
Go server and a TypeScript client, installed from what is published rather
than from this tree, which is also how a release finds out whether what it
publishes can be used.

## Documentation

[docs/README.md](docs/README.md) is the map. The reference is in sets by
who reads it:

| set | for | what |
| --- | --- | --- |
| [docs/goals/](docs/goals/) | a reviewer, a designer | the north stars: what Nightseam is for, in eight respects, at the limit — abstract, never done, and what a review measures the tree against |
| [docs/wire/](docs/wire/) | a runtime in any language | what crosses the wire: the profile `nightseam.duplex/1`, the tunnel's operations, and the test that says where something new on the wire belongs |
| [docs/runtime/](docs/runtime/) | a consumer of the packages | the surface of the peer, the tunnel and the observer, Go and TypeScript side by side |
| [docs/declaration/](docs/declaration/) | a consumer declaring a family | the tier files, a family generic in others, the generator's commands, what the generated packages export, and the pipeline for whoever changes it |
| [docs/languages/](docs/languages/) | a consumer choosing a language, a contributor bringing one | the four promises, profiles and tiers, and how a language joins |
| [docs/decisions/](docs/decisions/) | anyone asking why | the record: one page per decision — the question, what was decided, what the alternative cost, since when |
| [conformance/DRIVER.md](conformance/DRIVER.md) | a testee's author | the conformance suite: the protocol a language's testee speaks to the runner, every op |

## Working on Nightseam

```
go test -short ./...                              # the fast tier: Go alone, seconds
go test ./...                                     # the full tier: both languages, the conformance suite
pnpm install && pnpm -r check && pnpm -r build && pnpm -r test
(cd otel/go && go vet ./... && go test ./...)     # the nested module, which ./... does not enter
node scripts/matrix-table.mjs --check             # the README's Languages table against the matrix
node scripts/links.mjs                            # every link in every page resolves to the tree
```

The full tier needs Go, Node 22.12 or later and the TypeScript compiler
`pnpm install` brings, and fails rather than skips when one is missing.

[COLLABORATION.md](COLLABORATION.md) says how work is organized here — the
boundary rule, parity between the languages, the two tiers, the golden
discipline, lanes, and working in one tree. [SECURITY.md](SECURITY.md) says
how to report a vulnerability, [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) what
is expected of everyone here, and [RELEASING.md](RELEASING.md) what is
published and how a release is cut.

## License

Apache License, Version 2.0: [LICENSE](LICENSE), with [NOTICE](NOTICE)
beside it.
