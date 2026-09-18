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
tunnel/go/              channels multiplexed over one peer, each a Conn: the third transport, with per-channel credit
tunnel/ts/              @nightseam/tunnel, the same over a DuplexPeer, each channel a FrameConnection
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
`{"connection": "f"}`, of a named family or of a parameter the family
declares.

## A family generic in others

A family declares the parameters it is generic in, and a slot names one:

```json
"parameters": [{"name": "S", "of": "session"}, {"name": "T", "of": "session"}],
"types": {
  "Frame": {"kind": "record", "fields": [
    {"name": "message", "type": {"envelope": "S"}},
    {"name": "back",    "type": {"connection": "S"}},
    {"name": "heard",   "type": {"envelope": "T"}}]}}
```

A slot target in upper camel case is a parameter, in lower case a family:
`{"envelope": "codex"}` is one message of codex and the generated code refers
to codex's own `Envelope`. A parameter is bound where the generated code is
instantiated, to any family that declares its role — today `session`, the
role a family with a `sess` layer carries. There is no limit on how many
parameters a family declares, and two parameters never collapse into one: a
consumer may bind `S` to one session family and `T` to another.

A method's `request` is a type expression like any other, so it may hold a
slot; a declared parameter no slot names is reported, as is a slot naming a
parameter the family does not declare.

Nightseam renders such a family once, generically, and a consumer
instantiates it:

- TypeScript has associated types, so one parameter is one type parameter
  whatever kinds it is used at: `Frame<S extends AnyFamily = SessionFamily>`
  with `message: S["Envelope"]`, `SessionFamily` the union of the session
  families of the world, and one binding argument per parameter,
  `Client.dial(url, probe.family, codex.family, …)`, whose validators then
  validate what fills each slot. Every family exports its `Family` descriptor
  and its `family` binding for this.
- Go has none, so a parameter becomes one type parameter per kind, named for
  the parameter and the kind: `S` gives `SE` and `SH`, and a type takes only
  the ones it uses — `Frame[SE any]`, `Attachment[SH any]`,
  `Both[SE, SH, TE any]`. `Frame[codexprotocol.Envelope]` validates what
  fills the slot through codex's codec; `Frame[json.RawMessage]` passes it
  through, which is what a relay wants.

  Nothing in Go relates `SE` to `SH`, so on their own they could be bound to
  one family's `Envelope` and another's `Handle` — a pairing no binding of
  the contract produces and no TypeScript peer can express. The entry points
  therefore take the binding as one argument per parameter, and every
  protocol package exports its own: `Dial(ctx, url, probeprotocol.Family, …)`
  infers `SE` and `SH` together from it, and asking for a pair it does not
  have is a compile error. A family's own declarations take both type
  parameters of every parameter for this, so a parameter used at one kind
  carries the other as a phantom; `runtime.Opaque()` binds a parameter to no
  family, for a relay. A consumer who builds a mismatched `runtime.Family`
  literal by hand can still mix them: Go offers no way to forbid that.

The two ways to a concrete package — binding the parameters into the contract
and rendering it plain, or rendering generically and instantiating — must
agree: `gen(bind(C, F)) ≅ gen(C)[F]`. The generator's fixture renders both
for a carrier of a probe family into one module and holds them equal: by
reflection in Go, field for field and method for method; by `Equals<>` under
`tsc` in TypeScript; and on the wire, a plain client against a generic server
and the reverse.

A binding need not be total: binding some parameters leaves the contract
generic in the rest, which is what lets a family be specialized a step at a
time.

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
