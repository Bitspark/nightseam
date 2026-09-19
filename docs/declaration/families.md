# A family in tiers

A family of API is declared in tiers of JSON under `api/contracts/<family>/`
of the consuming checkout. This page is the reference for what may be
written there — the tiers, the types, the two sides, the governance of a
session, the per-target names. [A family generic in others](generics.md)
is the rest of the language; [the generator](generator.md) says how a
declaration is rendered, [the generated packages](generated.md) what comes
out, and the [README](../../README.md) is the short path.

## The files

A family `f` is a directory `api/contracts/f/` of the consuming checkout,
one file per tier and, where the convention is not enough, one per target:

```
api/contracts/probe/model.json       tier 1: the types
api/contracts/probe/protocol.json    tier 2: the two sides, the errors, the parameters
api/contracts/probe/session.json     tier 3: how a session is governed
api/contracts/probe/go.json          not a tier: what the Go rendering names otherwise than the convention does
api/contracts/probe/typescript.json  not a tier: the same for TypeScript (and spec.json for the specification)
```

The family and the tier come from the path; no file repeats them. Every
tier file may carry `types` and `imports`; a type is declared in the tier it
belongs to, and **a declaration refers to its own tier or a lower one, never
a higher one**: the tool refuses one that does. The tiers are one table
(`internal/model/tiers.go`), and a concern is a row in it. A file under
`api/contracts/` that is neither a tier file nor a target's override file
is refused, and so is a file where a family directory should be.

## model.json

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
case a family, and every family a declaration names is imported ([one
reference form](../decisions/one-reference-form.md)).

## protocol.json

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
its `result` any type; its `errors` codes the family declares, and a method
that names an error the family does not declare is refused. The public
errors reach both languages by name — [the generated
packages](generated.md#errors) say how.

Every family with a protocol carries two injected types it may not declare:
`Envelope`, one message of the profile, and `Handle`, a reference to a
channel that speaks it.

## session.json

```json
{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}
```

`decides` names the methods that need control to send; `asks` the client
methods — the ones the server sends — that raise a request the holder of
control must answer; `conversation` where the conversation id arrives. An
`extensions` member is carried through for other tools and read by nothing
here. A family with a session tier carries the `session` role, which a
parameter binds to. What a session does with `decides` and `asks` is [the
session](../wire/session.md).

## go.json and typescript.json

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
`cmd/nightseam/testdata/reserved`. The naming conventions themselves are
data both languages' tests read, `conformance/tables/naming.json`.

TypeScript's optional `Events` fields also reserve inherited `Object`
members, such as `toString`: otherwise an empty events object supplies a
built-in method as a callback and can fail the interface's type check. A
TypeScript override such as `{"names":{"to_string":"textChanged"}}`
keeps the wire event `to_string` while giving its callback a safe field name.
