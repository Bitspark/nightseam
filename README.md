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

The `errors` of the `rpc` layer are the public errors a family declares, by
code, and reach both languages by name: the Go protocol package declares a
constant per error, `ErrorNotFound = "not_found"`, the list `Errors`, and
`IsError(err, code)`; the TypeScript client exports `errors`, an object with
a member per error, `errors.notFound`, and the `ErrorCode` union of them. A
handler returns one as a `*runtime.PublicError` with the code, and a caller
tells them apart without spelling it.

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
    {"name": "heard",   "type": {"envelope": "T"}},
    {"name": "last",    "type": "S.Payload"}]}}
```

A slot draws a type from another family: `{"envelope": "S"}` is S's
`Envelope`, one message of it, `{"connection": "S"}` is S's `Handle`, a
channel that speaks it, and `"S.Payload"` is any record or enum `Payload` of
S. A slot target in upper camel case is a parameter, in lower case a family:
`{"envelope": "codex"}` is one message of codex and the generated code refers
to codex's own `Envelope`. A parameter is bound where the generated code is
instantiated, to any family that declares its role — today `session`, the
role a family with a `sess` layer carries — and a slot of `S.Payload` holds
every such family to declaring `Payload`, plainly, which the tool checks
across the world. There is no limit on how many parameters a family declares,
and two parameters never collapse into one: a consumer may bind `S` to one
session family and `T` to another.

A method's `request` is a type expression like any other, so it may hold a
slot; a declared parameter nothing names is reported, as is a slot naming a
parameter the family does not declare.

A family that imports another and refers to a **generic** type of it says
what fills each of that type's parameters, since they are not its own:

```json
{"apply": "carrier.Frame", "with": {"S": "B"}}
```

`with` maps the imported type's parameters to this family's — which keeps
the result generic there — or to named families, which does not. A family
with exactly one parameter may refer to such a type plainly and fill it with
that one; with any other number the plain reference is refused rather than
guessed, and the diagnostic names the application to write.

Nightseam renders such a family once, generically, and a consumer
instantiates it:

- TypeScript has associated types, so one parameter is one type parameter
  whatever it is drawn at: `Frame<S extends AnyFamily = SessionFamily>` with
  `message: S["Envelope"]` and `last: S["Payload"]`, the bound narrowed to
  `AnyFamily & { "Payload": unknown }` where a type beyond the two every
  family carries is drawn, `SessionFamily` the union of the session families
  of the world, and one binding argument per parameter,
  `Client.dial(url, probe.family, codex.family, …)`, whose validators then
  validate what fills each slot. Every family exports its `Family` descriptor,
  listing every plain type it declares, and its `family` binding for this.
- Go has none, so a parameter becomes one type parameter per type drawn from
  it, named for both: `S` drawn at its `Envelope`, `Handle` and `Payload`
  gives `SEnvelope`, `SHandle` and `SPayload`, and a type takes only the ones
  it uses — `Frame[SEnvelope any]`, `Attachment[SHandle any]`,
  `Both[SEnvelope, SHandle, TEnvelope any]`.
  `Frame[codexprotocol.Envelope]` validates what fills the slot through
  codex's codec; `Frame[runtime.Raw]` passes it through, which is what a
  relay wants.

  Nothing in a type declaration relates `SEnvelope` to `SHandle`, so the
  entry points do: every record and enum of a protocol package returns the
  package's `Tag` from `Of`, and `Dial`, `Attach`, `Serve`, `NewHandler` and
  `Open` hold every type parameter drawn from `S` to `runtime.Of[STag]`. The
  compiler infers them all from the handler, or from the ones a caller
  spells — `Dial[probe.Envelope, probe.Handle](…)`, the tag never written —
  and an `Envelope` of one family beside a `Handle` of another, or a type of
  no family at all, does not compile. `runtime.Raw` carries a tag of its own
  for the relay. What remains is a `Client` literal built by hand, which
  bypasses the entry points and is a deliberate act.

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
