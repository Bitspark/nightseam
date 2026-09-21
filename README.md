# Nightseam

[![ci](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml/badge.svg)](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Bitspark/nightseam.svg)](https://pkg.go.dev/github.com/Bitspark/nightseam)
[![npm](https://img.shields.io/npm/v/@nightseam/runtime.svg)](https://www.npmjs.com/package/@nightseam/runtime)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Declare a duplex API once, in tiers of JSON. Get a typed server and a typed
client in Go and TypeScript — every one of them typed in
both directions, since a client serves what the server calls — all speaking
one wire profile: `nightseam.duplex/1`. Generated models convert to and from
a relative-path `Wire`, over a socket, a prepared tunnel channel, a local pair
or a selected and mounted origin. Physical peers carry the same four kinds
of JSON frame: request, response, event and cancellation.

Duplex means both ends call. The server calls the client with the machinery
the client calls the server with, declared in the same file and typed the
same way — which is what a browser, an agent and a forwarder need, and
what a request-and-response contract has no way to state.

The declaration language separates data, RPC and live levels. Go and
TypeScript generate and validate unions, nullable and literal expressions,
inline shapes, type and family parameters, and inherited operations. The live
tier adds callable values — functions that may take or return functions, and
generic data containers applied to them — with scoped export, import and
release. The [changelog](CHANGELOG.md) records the implemented forms and the
removal of the governed session layer. Closed generic callables and live types
drawn through family parameters use complete conversion adapters with the
active invocation's ownership context. The
[generated surface](docs/declaration/generated.md#generic-boundary-helpers)
describes their construction and validation.

Nightseam implements the typed-access foundation and planned optional rooted-grant
authentication. Its Go and TypeScript access surfaces use the public Bitwire
v0.1.0 contract, adopted in [0.6.0 work](https://github.com/Bitspark/nightseam/issues/421); the
runtime, generator and optional auth remain here. The
[repository-home decision](docs/decisions/the-reusable-foundation-lives-in-nightseam.md)
records contract and implementation ownership: bare data and RPC remain independent
of auth, and consumers choose trust and application policy. Optional authentication
delivery remains planned work.

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
type Probe struct{ remote protocol.Client }

func (p Probe) Echo(ctx context.Context, value protocol.Payload) (protocol.Payload, error) {
	return p.remote.Methods.Reverse(ctx, value)
}

model := func(remote protocol.Client) (protocol.Server, error) {
	return protocol.Server{Methods: Probe{remote: remote}}, nil
}
wire, err := binding.ToWire(model, runtime.AdapterContext{})
```

The host can use this Wire locally, select or mount it, or forward a physical
peer's Wire to it during peer preparation. The host owns authentication,
transport setup and closure; the model receives typed reverse calls and events.

### Call it

TypeScript, the client side:

```ts
const peer = new DuplexPeer();
const factory = await binding.fromWire(peer.wire(), {});
const server = factory({
  methods: {
    reverse: ({ text, count }) => ({ text: [...text].reverse().join(''), count }),
  },
  events: { changed: p => console.log('changed', p.text) },
});
await peer.connect('wss://example.test/probe');

const payload = await server.methods.echo({ text: 'hello', count: 1 });
```

What you never write: the envelope, the correlation of a response to its
request, cancellation, backpressure, the validator that holds every frame to
the declaration — or the second language's copy of any of it.

## Status

Pre-1.0. Published packages, the generator's runtime dependency version and
Go module tags move in lockstep. APIs change directly, with no compatibility
shim; this is the current release policy, not a settled compatibility policy
for a mature ecosystem.
`CHANGELOG.md` says what each version holds. The conformance suite under
[conformance/](conformance/) holds every language's
seam, runtime, tunnel, live and generated packages to Go's over a real socket,
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
| `cpp` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | ok |
| `go` *(reference)* | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |
| `haskell` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | ok |
| `java` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | ok |
| `python` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 2 skipped | ok |
| `rust` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | ok |
| `swift` | 4 | ✓ | — 336 skipped | — 12 skipped | — 32 skipped | ✓ 18 skipped | ok |
| `typescript` | 1 | ✓ | ✓ | ✓ | ✓ | ✓ | ok |

Planned, with no testee yet: `cpp`, `haskell`, `python`, `rust` at tier 2.
<!-- matrix:end -->

The table is the last conformance run, rendered from
`conformance/matrix.json` by `node scripts/matrix-table.mjs`; CI fails a pull
request whose table has drifted from the matrix, as `nightseam check` fails
one whose generated output is stale. `ok` means the gate found neither failures
nor skips in profiles required by that language's tier. Required-profile
failures and skips follow the tier's release policy; failures elsewhere follow
its lag policy, while optional-profile skips remain informational.
What CI runs is the star — every language against the Go reference on
both sides, which is the gate a language passes to have joined; the full
matrix of every language against every other runs nightly, and a scenario two
non-reference languages disagree about becomes an issue against the scenario,
since the reference decides.

## Install

```
npm install @nightseam/runtime @nightseam/tunnel           # a generated RPC client
npm install @nightseam/live                               # when its family has a live tier
go get github.com/Bitspark/nightseam                       # the Go runtime packages
go get -tool github.com/Bitspark/nightseam/cmd/nightseam   # the generator, as a Go tool
```

The generator is development tooling. Generated packages depend on their
protocol types and the runtime components they use. Carrier assembly chooses
the tunnel explicitly; live values add the live runtime. Generated data-only
and scalar-generic packages need no live runtime.

Rust core crates are available from a checkout and as local Cargo packages;
they are not published to crates.io. The [Rust guide](docs/languages/rust.md)
covers `nightseam-duplex`, `nightseam` and the packaged WebSocket consumer.

## The packages

| npm | Go | what it is |
| --- | --- | --- |
| [`@nightseam/duplex`](duplex/ts) | [`duplex/go`](duplex/go) | relative-path Wire frames, selection and mounting; raw ordered frame connections, a WebSocket adapter and an in-memory pipe |
| [`@nightseam/runtime`](runtime/ts) | [`runtime/go`](runtime/go) | profile peers and local Wire pairs; correlation, cancellation, backpressure, value adapters, validation, trace context and observation |
| [`@nightseam/tunnel`](tunnel/ts) | [`tunnel/go`](tunnel/go) | prepared Wire channels multiplexed over one peer with per-channel credit, and separate raw connections |
| [`@nightseam/live`](live/ts) | [`live/go`](live/go) | callable values across one connection: a scope over a peer, exported bindings, imported references, release and forwarding |
| [`@nightseam/otel`](otel/ts) | [`otel/go`](otel/go) | the OpenTelemetry adapter: a propagator over W3C trace context and an observer that opens a span per request. The four components above pull in no telemetry backend; their external Go dependencies provide WebSocket transport and strict JSON decoding. |
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
| [docs/runtime/](docs/runtime/) | a consumer of the packages | the surface of the peer, the tunnel, the live layer and the observer, and what a consumer composes out of them, Go and TypeScript side by side |
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
cargo fmt --all --check && cargo clippy --workspace --all-targets --locked -- -D warnings
cargo test --workspace --locked                   # Rust invariant tests
node scripts/smoke-rust-packed.mjs                # packaged Rust consumer outside the checkout
```

The full tier needs Go, a stable Rust toolchain, Node 22.12 or later and the TypeScript compiler
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
