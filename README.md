# Nightseam

Nightseam is a declaration language for duplex APIs and the runtime those
APIs run on. A family of API is declared in tiers of JSON — its model, the
protocol over it, how a session of it is governed — and rendered into Go
and TypeScript packages that bind to one runtime: the `nightseam.duplex/1`
profile, JSON frames carrying requests, responses, events and cancellations
over a frames duplex connection, a WebSocket today.

One directory per component, one subdirectory per language: a third
language's runtime is `runtime/<lang>` and nothing else moves.

```
duplex/go/              the seam in Go: Conn, Pipe, the close codes; duplex/go/ws, a WebSocket as a Conn; duplex/go/duplextest, the conformance suite
duplex/ts/              @nightseam/duplex: FrameConnection and the WebSocket adapter
runtime/go/             the Go peer of the profile: envelope, request correlation, cancellation, backpressure, presence, HTTP upgrade, the wire validator
runtime/ts/             @nightseam/runtime, the TypeScript peer for the browser and Node, no third-party dependency, the wire validator
runtime/testdata/       the conformance table both validators are held to
tunnel/go/              channels multiplexed over one peer, each a Conn: the third transport, with per-channel credit
tunnel/ts/              @nightseam/tunnel, the same over a DuplexPeer, each channel a FrameConnection
session/go/             a session over a tunnel's channels: the relay, the registry, the holder of control, the in-memory log; session/go/sessiontest, the suite both languages are held to
session/ts/             @nightseam/session: a session over a tunnel's channels — the relay, the registry, the holder of control, the in-memory log
cmd/nightseam/          the generator: generate, check, validate, upgrade; the corpus and its goldens under testdata
internal/model/         the typed declaration of a family: the tiers, the sealed type-expression AST, the decoders
internal/load/          files to families: the tier table, the shape schemas, the world of a checkout
internal/analysis/      a family within its world: imports resolved, inheritance flattened, what is generic in it
internal/check/         the rules, one function per tier and one for the override files
internal/render/        a family as a target sees it, computed once
internal/spi/           the seam between the kernel and a target
internal/targets/       golang, typescript and spec: each renders a family, names the others never
internal/kernel/        load, analyse, check, render
internal/emit/          a writer, an import set, a namespace: what every target writes with
internal/naming/        the convention every target derives names by
internal/diag/          where a problem is: family, tier file, pointer, code
internal/oracle/        test support: the left path of the diagram a generic rendering commutes with
internal/upgrade/       layer files of the previous language into the directory form
```

## A family in tiers

A family `f` is a directory `api/contracts/f/` of the consuming checkout,
one file per tier:

```
api/contracts/probe/model.json       tier 1: the types
api/contracts/probe/protocol.json    tier 2: the two sides, the errors, the parameters
api/contracts/probe/session.json     tier 2: how a session is governed
api/contracts/probe/go.json          tier 3: what the Go rendering names otherwise than the convention does
api/contracts/probe/typescript.json  tier 3: the same for TypeScript
```

The family and the tier come from the path; no file repeats them. Every
tier file may carry `types` and `imports`; a type is declared in the tier it
belongs to, and **a declaration refers to its own tier or a lower one, never
a higher one**: the tool refuses one that does. The tiers are one table
(`internal/model/tiers.go`), and a concern is a row in it.

### model.json

```json
{
  "nightseam": 2,
  "imports": ["identity"],
  "types": {
    "Payload": {"kind": "record", "extends": ["Base"], "description": "…", "fields": [
      {"name": "count", "type": "integer"},
      {"name": "note", "type": "string", "required": false, "nullable": true}
    ]},
    "Status": {"kind": "enum", "values": ["ready", "done"]},
    "Payloads": {"kind": "alias", "type": {"array": "Payload"}}
  }
}
```

A `record` has `fields`, may `extends` other records (their fields come
first, in wire order) and may be `open` (fields beyond the declared ones are
kept). An `enum` has `values`; an `alias` a `type`. A field is `required`
unless it says otherwise and never null unless `nullable`: presence and
nullness are two facts. An `entity` is a record with a `key`; `unique`,
`min`, `max`, `length` and `pattern` constrain a field, and the validators
enforce them.

A type expression is one of: a primitive (`string`, `boolean`, `integer`,
`number`, `timestamp`, `json`); a type of this family, `"Payload"`; a type
of an imported family, `"identity.User"`; a type drawn from a parameter,
`"S.Envelope"`; `{"array": T}`; `{"map": T}`; `{"ref": "User"}`, a reference
to an entity by its key; `{"apply": "carrier.Frame", "with": {"S": "B"}}`, a
generic type of an imported family with its parameters filled. There is one
reference form: a qualifier in upper camel case is a parameter, in lower
case a family, and every family a declaration names is imported.

### protocol.json

```json
{
  "profile": "nightseam.duplex/1",
  "server": {
    "methods": {"echo": {"request": "Payload", "result": "Payload", "errors": ["denied"]}},
    "events": {"changed": {"type": "Payload"}}
  },
  "client": {
    "methods": {"reverse": {"request": "Payload", "result": "Payload"}}
  },
  "errors": {"denied": "The caller is denied."}
}
```

A side is an interface: the methods it implements and the events it emits.
The server side is implemented by the server and called by the client; the
client side is the reverse. A method's `request` is a record, or absent;
its `result` any type; its `errors` codes the family declares. The public
errors reach both languages by name: the Go protocol package declares a
constant per error, `ErrorNotFound = "not_found"`, the list `Errors`, and
`IsError(err, code)`; the TypeScript client exports `errors`, an object with
a member per error, `errors.notFound`, and the `ErrorCode` union of them.

Every family with a protocol carries two injected types it may not declare:
`Envelope`, one message of the profile, and `Handle`, a reference to a
channel that speaks it.

### session.json

```json
{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}
```

`decides` names the methods that need control to send; `asks` the client
methods — the ones the server sends — that raise a request the holder of
control must answer; `conversation` where the conversation id arrives. A
family with a session tier carries the `session` role, which a parameter
binds to.

### go.json and typescript.json

```json
{"names": {"work.get": "GetWorkItem", "Item.url": "Link"}}
```

Names are derived by convention — upper camel case with initialisms in
capitals for Go (`work_item_id` → `WorkItemID`), lower camel for TypeScript
members (`workItemId`) — and an override file replaces the convention where
it must, by path: `Type`, `Type.field`, `Enum.value`, a method or event,
`errors.code`. An override file may only override: a key that names nothing
the family declares is refused, and so is a name the generated code
declares of itself — what each target reserves is held under
`cmd/nightseam/testdata/reserved`.

## A family generic in others

A family declares the parameters it is generic in, and a type draws on one:

```json
"parameters": [{"name": "S", "of": "session"}, {"name": "T", "of": "session"}],
"types": {
  "Frame": {"kind": "record", "fields": [
    {"name": "message", "type": "S.Envelope"},
    {"name": "back",    "type": "S.Handle"},
    {"name": "heard",   "type": "T.Envelope"},
    {"name": "last",    "type": "S.Payload"}]}}
```

`S.Envelope` is one message of the family bound to S, `S.Handle` a channel
that speaks it, `S.Payload` any record or enum `Payload` of it — which every
family that may bind `S` is then held to declare, plainly, checked across
the world. A parameter is bound where the generated code is instantiated,
to any family that declares its role; today `session`, the role a family
with a session tier carries. Two parameters never collapse into one.

A family that refers to a **generic** type of an import says what fills
each of that type's parameters:

```json
{"apply": "carrier.Frame", "with": {"S": "B"}}
```

`with` maps the imported type's parameters to this family's — which keeps
the result generic there — or to named families, which does not. A family
with exactly one parameter may refer to such a type plainly and fill it
with that one; with any other number the plain reference is refused rather
than guessed, and the diagnostic names the application to write.

Nightseam renders such a family once, generically, and a consumer
instantiates it:

- TypeScript has associated types, so one parameter is one type parameter
  whatever it is drawn at: `Frame<S extends AnyFamily = SessionFamily>` with
  `message: S["Envelope"]` and `last: S["Payload"]`, the bound narrowed to
  `AnyFamily & { "Payload": unknown }` where a type beyond the two every
  family carries is drawn, `SessionFamily` the union of the session families
  of the world, and one binding argument per parameter,
  `Client.dial(url, probe.family, codex.family, …)`, whose validators then
  validate what fills each slot.
- Go has none, so a parameter becomes one type parameter per type drawn
  from it, named for both: `S` drawn at its `Envelope`, `Handle` and
  `Payload` gives `SEnvelope`, `SHandle` and `SPayload`, and a type takes
  only the ones it uses — `Frame[SEnvelope any]`, `Attachment[SHandle any]`.
  `Frame[codexprotocol.Envelope]` validates what fills the slot through
  codex's codec; `Frame[runtime.Raw]` passes it through, which is what a
  relay wants. Every record and enum of a protocol package returns the
  package's `Tag` from `Of`, and `Dial`, `Attach`, `Serve`, `NewHandler` and
  `Open` hold every type parameter drawn from `S` to `runtime.Of[STag]`, so
  an `Envelope` of one family beside a `Handle` of another does not compile.

The two ways to a concrete package — binding the parameters into the
declaration and rendering it plain, or rendering generically and
instantiating — must agree: `gen(bind(C, F)) ≅ gen(C)[F]`. The fixtures
render both into one module and hold them equal: by reflection in Go, by
`Equals<>` under `tsc` in TypeScript, and on the wire, a plain client
against a generic server and the reverse.

## How it renders

The generator is a pipeline of small packages, each owning one level:
`load` reads a checkout's tier files into the typed `model`, holding each
file to its tier's shape schema; `analysis` gives a family its world —
imports resolved transitively, the injected types, inheritance flattened,
and what is generic in it, computed once; `check` holds it to the model's
rules and each concern's; `render` presents it to the targets once, with
every fact they need and nothing target-specific; each target plans every
identifier it will declare — into the namespace it lands in, so that a
collision is a diagnostic and what is reserved is what is emitted — and
then emits, registering imports where it uses them; the `kernel` runs the
pipeline and refuses a rendered path outside the directories the target
owns. A family with any diagnostic is refused before a target renders.
Targets are composed in `cmd/nightseam/v2.go` and nowhere else; the seam
between them and the kernel is `internal/spi`, and a test holds the
package graph to that.

The generated packages own their directories wholesale —
`api/go/<f>-protocol`, `-binding`, `-client` and `api/ts/<f>-client` — and
a human writes nothing there: behavior is written against the `Handler`
interfaces they declare, in files of the consumer's own. What a target
owns and nothing renders any more — a file of a family that was removed,
or one a target no longer writes — `check` reports and `generate` removes,
along with a directory it leaves empty; what a package manager installs
beside a client, `node_modules`, is nobody's and stays.

The wire validator lives in each runtime, once, and reads the family's
wire description the protocol package embeds; both are held to
`runtime/testdata/validator-cases.json`. A type drawn from a parameter is
validated by the binding of the family that fills it in TypeScript, and by
that family's codec where the generic type is instantiated in Go.

### The specification

A third target, `spec`, renders each family's specification as Markdown at
`api/spec/<family>/README.md` — its types with their fields and
constraints, the two sides with their operations and errors, the parameters
it is generic in, the governance of a session of it — from the declaration
alone, so that the document is never behind it. It reserves nothing and
refuses nothing.

## Using it

Nightseam is developer tooling, never a runtime dependency of the
generator's own: the generated packages depend only on the protocol types
and the runtime. A consumer runs it as a Go tool:

```
go get -tool github.com/Bitspark/nightseam/cmd/nightseam
go tool nightseam validate            # every diagnostic of every family
go tool nightseam generate [family]   # render what is stale
go tool nightseam check               # fail if the checked-in output is stale
go tool nightseam upgrade             # rewrite layer files of the previous language into the directory form
go tool nightseam init <family>       # write the handlers a consumer implements, once, into api/impl/<family>
```

`validate` prints each diagnostic as `family/file#pointer: message [code]`.
The Go packages are rooted at the checkout's module, read from its `go.mod`
or given as `--module`; the TypeScript package is named under an npm scope,
`--scope`, the module's last element unless given, and depends on
`@nightseam/runtime`. The runtime the Go packages bind to is
`github.com/Bitspark/nightseam/runtime/go`; in Go, a consumer requires this
module.

A checkout declared in the previous language's layer files —
`<f>.dto.json`, `<f>.rpc.json`, `<f>.sess.json` — is refused with the
command that converts it: `upgrade` rewrites them into the directory form,
sides in place of directions, one reference form in place of the slot
objects, and a hand-spelled name into an override only where the
convention would spell it otherwise; `upgrade --file <contract.json>`
converts a contract declared in one file, under the name it carries.

Behavior is written into slots: `init <family>` writes the Go server's
`Handler` — a type implementing the binding package's interface, every
method returning an `unimplemented` error, generic in what the family is
— and the TypeScript client's handler of what the server sends, as stubs
under `api/impl/<family>` (or `--dir`), for the consumer to fill in. It
writes once and never rewrites: the directory is the consumer's.

## Development

```
go test ./...
pnpm install && pnpm -r check && pnpm -r test
```

The tests are in two tiers. `go test -short ./...` is the fast one and
needs Go alone: every package's own tests, every target's `Check`, and the
corpus under `cmd/nightseam/testdata` — families written in tier files as a
consumer writes them under `corpus` and `families`, what every target
renders for them held file for file under `golden` and `golden-families`,
the exported surface of every generated Go package under `surface`, what
each target reserves under `reserved`, and under `invalid` one checkout per
rule the tool refuses, with what `validate` says held in its
`diagnostics.txt`. The corpus is also what `upgrade` makes of the previous
language's corpus under `corpus-v1`, held byte for byte. A change to a
renderer or a diagnostic shows up as a diff of those files, which is what a
review reads; when the change is meant,
`go test ./cmd/nightseam -short -run 'Golden|Invalid|Surface|Reserved|Upgrade' -update`
rewrites them from the current output. A new family in the corpus, or a new
case under `invalid`, needs only its files and one `-update`.

The full tier, `go test ./...`, is the fixtures: they compile and run the
generated packages in both languages, so they need Go, Node 22.12 or later,
and the TypeScript compiler pnpm installs — and fail, rather than skip, when
one is missing. The Go fixture resolves this module to the checkout, so the
runtime under test is the real one. The fixture bodies were written against
the previous generator and pass unchanged against this one; a next
generation is held to them the same way.

## Lineage

The runtime and the generator were copied from Nightshift into Nighthall
and grew there — the seam beneath the profile, imports and slots, the three
layers — before they were extracted into this repository as their own tool,
where the generic rendering was added; Nighthall's `docs/DECISIONS.md`
records the steps: D-001, D-007, D-010, D-013, D-014 and D-015. The
declaration language was then redesigned into its tiers and the generator
rebuilt beneath it — `docs/V2_MIGRATION.md` records how — as the prototype
of a general one, whose model is written up as bitlink.
