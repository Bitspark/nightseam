# Nightseam

Nightseam is a contract language for duplex APIs and the runtime those APIs run
on. A family of API is declared in three layers of JSON — its data, its
operations, and how a session of it is governed — and rendered into Go and
TypeScript packages that bind to one runtime: the `nightseam.duplex/1` profile,
JSON frames carrying requests, responses, events and cancellations over a
frames duplex connection, a WebSocket today.

One directory per component, one subdirectory per language: a third
language's runtime is `runtime/<lang>` and nothing else moves.

```
duplex/go/              the seam in Go: Conn, Pipe, the close codes; duplex/go/ws, a WebSocket as a Conn; duplex/go/duplextest, the conformance suite
duplex/ts/              @nightseam/duplex: FrameConnection and the WebSocket adapter
runtime/go/             the Go peer of the profile: envelope, request correlation, cancellation, backpressure, presence, HTTP upgrade
runtime/ts/             @nightseam/runtime, the TypeScript peer for the browser and Node, no third-party dependency
cmd/nightseam/          the generator: generate, check, validate
internal/contract/      the contract model and its schema, urn:nightseam:contract:1; layers, imports, slots, generics
internal/kernel/        parses, validates and renders a family within the world of the families it imports
internal/spi/           the seam between the kernel and a language
internal/languages/     golang and typescript: each renders a family, names the other never
```

## A family in three layers

A family `f` is declared under `api/contracts/` of the consuming checkout as
`f.dto.json`, `f.rpc.json` and, when a session of it is governed,
`f.sess.json`. The `dto` layer holds records, enums and aliases; `rpc` holds
methods, events and errors; `sess` says which methods need control, which
raise a request the holder of control answers, and where the conversation id
arrives. A layer refers to itself or a lower one and never a higher one; the
tool refuses a declaration that does.

A family may `import` others and refer to their types as `other.Type`. Every
family carries an `Envelope`, one message of its own profile, and a `Handle`,
a channel reference; a type may hold a slot, `{"envelope": "f"}` or
`{"connection": "f"}`, of a named family or of the session role.

## A family generic in another

A slot names what another family contributes: `{"envelope": "codex"}` is one
message of codex, `{"connection": "codex"}` a handle to a channel that speaks
it, and the generated code refers to codex's `Envelope` or `Handle`. A slot of
the **session role** — `{"envelope": "session"}` — is of whichever session
family applies, and makes the family generic in it. Nightseam renders such a
family generically, once, and a consumer instantiates it:

- TypeScript has associated types, so a generic family has one parameter:
  `Frame<F extends AnyFamily = SessionFamily>` with `message: F["Envelope"]`,
  `SessionFamily` the union of the session families of the world, and
  `Client.dial(url, codex.family, …)` binding the family, whose validator then
  validates what fills the slot. Every family exports its `Family` descriptor
  and its `family` binding for this.
- Go has none, so a type takes a parameter per slot kind it uses, `E` for an
  envelope and `H` for a handle, and the family's `Handler`, `Client` and
  `Dial` take the union: `Frame[codexprotocol.Envelope]` validates what fills
  the slot through codex's codec; `Frame[json.RawMessage]` passes it through,
  which is what a relay wants.

The two ways to a concrete package — substituting the family into the
contract and rendering it plain, or rendering generically and instantiating —
must agree: `gen(substitute(C, F)) ≅ gen(C)[F]`. The generator's fixture
renders both for a carrier of a probe family into one module and holds them
equal: by reflection in Go, field for field and method for method; by
`Equals<>` under `tsc` in TypeScript; and on the wire, a plain client against
a generic server and the reverse.

## Using it

Nightseam is developer tooling, never a runtime dependency of the generator's
own: the generated packages depend only on the protocol types and the
runtime. A consumer runs it as a Go tool:

```
go get -tool github.com/Bitspark/nightseam/cmd/nightseam
go tool nightseam validate            # every diagnostic of every family
go tool nightseam generate [family]   # render what is stale
go tool nightseam check               # fail if the checked-in output is stale
```

The Go packages are rooted at the checkout's module, read from its `go.mod`
or given as `--module`, and land at `api/go/<f>-protocol`, `-binding` and
`-client`; the TypeScript package, `api/ts/<f>-client`, is named under an npm
scope, `--scope`, the module's last element unless given, and depends on
`@nightseam/runtime`. The runtime the Go packages bind to is
`github.com/Bitspark/nightseam/runtime/go`; in Go, a consumer requires this
module.

## Development

```
go test ./...
pnpm install && pnpm -r check && pnpm -r test
```

The generator's fixture tests compile and run the generated packages in both
languages, so they need Go, Node 22.12 or later, and the TypeScript compiler
pnpm installs. The Go fixture resolves this module to the checkout, so the
runtime under test is the real one.

## Lineage

The runtime and the generator were copied from Nightshift into Nighthall and
grew there — the seam beneath the profile, imports and slots, the three
layers — before they were extracted into this repository as their own tool,
where the generic rendering was added. Nighthall's `docs/DECISIONS.md`
records the steps: D-001, D-007, D-010, D-013, D-014 and D-015.
