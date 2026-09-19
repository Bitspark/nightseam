# The declaration language

A family of API is declared in tiers of JSON under `api/contracts/<family>/`
of the consuming checkout. This page is the reference for what may be
written there — the tiers, the types, the two sides, the governance of a
session, the per-target names — and for what a family generic in others
means in each language.

[The generator](generator.md) says how a declaration is rendered and what
the rendered packages own; the [README](../README.md) says how to run it.

## A family in tiers

A family `f` is a directory `api/contracts/f/` of the consuming checkout,
one file per tier:

```
api/contracts/probe/model.json       tier 1: the types
api/contracts/probe/protocol.json    tier 2: the two sides, the errors, the parameters
api/contracts/probe/session.json     tier 3: how a session is governed
api/contracts/probe/go.json          not a tier: what the Go rendering names otherwise than the convention does
api/contracts/probe/typescript.json  not a tier: the same for TypeScript
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
nullness are two facts. An `entity` is a record with a `key`; `min`,
`max`, `length` and `pattern` constrain a field, and the validators enforce
them; `unique` says no two instances of the entity share the value, which is
the holder's to enforce and not the wire's — a validator sees one value.

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
control must answer; `conversation` where the conversation id arrives. An
`extensions` member is carried through for other tools and read by nothing
here. A family with a session tier carries the `session` role, which a
parameter binds to.

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

