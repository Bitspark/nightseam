# The holes in a declaration

A family, a record, a union and an alias may each declare `parameters` —
the holes in it — and a consumer fills them where the generated code is
used. One mechanism at every level, and a parameter is of one of two
**sorts**, which `of` names.

```json
"parameters": [
  {"name": "S", "of": "session", "description": "The family whose messages are carried."},
  {"name": "T", "description": "What a page holds."}
]
```

- **A family parameter** — it has `of`. It is filled by a **family**, one
  that carries the tier `of` names: `protocol`, `session`, whatever tier
  comes next ([a family in tiers](families.md#the-files) has the table). A
  type is drawn *through* it, `S.Envelope` — one message of the family bound
  to `S` — `S.Handle` a channel that speaks it, `S.Payload` any record or
  enum `Payload` of it, which every family that may bind `S` is then held to
  declare, plainly, checked across the world.
- **A type parameter** — it has no `of`. It is filled by a **type
  expression** and is written where a type is named: `{"array": "T"}`.

A family parameter is **not** a type parameter with a bound. A bound
narrows what a thing may be and leaves it the same kind of thing; a family
is not a type — nothing is an instance of it, and what a declaration takes
from it is a type *of* it. So `S` alone, where a type is named, is refused,
and `T.Payload` is refused too: a type has no types of its own to draw.
[One parameter mechanism, of two
sorts](../decisions/one-parameter-mechanism-of-two-sorts.md) is the record,
and the reason the answer is written here rather than left as two
mechanisms with a note.

```json
"parameters": [{"name": "S", "of": "session"}, {"name": "T", "of": "session"}],
"types": {
  "Frame": {"kind": "record", "fields": [
    {"name": "message", "type": "S.Envelope"},
    {"name": "back",    "type": {"nullable": "S.Handle"}},
    {"name": "heard",   "type": "T.Envelope"},
    {"name": "last",    "type": "S.Payload"}]},
  "Page": {"kind": "record", "parameters": [{"name": "Item"}], "fields": [
    {"name": "items", "type": {"array": "Item"}},
    {"name": "next",  "type": {"nullable": "string"}, "required": false}]},
  "Result": {"kind": "union", "parameters": [{"name": "Ok"}, {"name": "Err"}],
             "tag": "kind", "variants": {"ok": "Ok", "err": "Err"}}
}
```

Family parameters are declared in the protocol tier, since a tier a family
carries is a fact of that tier; a type's parameters are declared with the
type, in whatever tier declares it. A parameter's name is upper camel case,
is distinct within its declaration, may not be the name of a type of the
family or of a parameter of the family, and is refused if nothing names it.
Two parameters never collapse into one ([generic families render once and
commute](../decisions/generic-families-render-once-and-commute.md)).

## Filling them

One operation fills a parameter, at every level:

```json
{"apply": "Page",          "with": {"Item": "Payload"}}
{"apply": "Result",        "with": {"Ok": {"array": "Payload"}, "Err": "string"}}
{"apply": "carrier.Frame", "with": {"S": "probe"}}
{"apply": "carrier.Frame", "with": {"S": "B"}}
```

`apply` names a generic type of this family or of an imported one; `with`
maps each of that type's parameters to what fills it — a **type
expression** for a type parameter, a **family** for a family parameter, or
a family parameter visible at the application site, which keeps the result
generic here. That scope includes the enclosing family's parameters and
the containing record's, union's or alias's own parameters; inline shapes
and nested applications keep the same scope. A forwarded family parameter
must guarantee the tier the destination requires, just as a concrete
family must carry it. A type parameter cannot fill a family slot.

A family with exactly one family parameter may refer to a generic imported
type plainly only when every parameter the imported type needs is a family
parameter and that one bound guarantees every required tier. A session
bound can fill a protocol slot, since carrying a tier requires its lower
tiers too; a protocol bound cannot fill a session slot. A type slot always
needs an explicit application, whether the type declares it or captures it
from its family, including through other types or inline shapes. A mixed
declaration needs explicit arguments too. With any other number of caller
family parameters the plain generic reference is refused, and the
diagnostic names the application to write. Filling a family parameter with
a family that does not carry the tier is refused, and so is filling a type
parameter with a family.

## How each language instantiates it

Nightseam renders such a family once, generically, and a consumer
instantiates it:

- TypeScript has associated types, so one family parameter is one type
  parameter whatever it is drawn at: `Frame<S extends AnyFamily = SessionFamily>`
  with `message: S["Envelope"]` and `last: S["Payload"]`, the bound narrowed
  to `AnyFamily & { "Payload": unknown }` where a type beyond the ones every
  family carries is drawn, `SessionFamily` the union of the session families
  of the world, and one binding argument per parameter,
  `Client.dial(url, probe.family, codex.family, …)`, whose validators then
  validate what fills each slot.
- Go has none, so a family parameter becomes one type parameter per type
  drawn from it, named for both: `S` drawn at its `Envelope`, `Handle` and
  `Payload` gives `SEnvelope`, `SHandle` and `SPayload`, and a type takes
  only the ones it uses — `Frame[SEnvelope any]`, `Attachment[SHandle any]`.
  `Frame[codexprotocol.Envelope]` validates what fills the slot through
  codex's codec; `Frame[runtime.Raw]` passes it through, which is what a
  relay wants. Every record and enum of a protocol package returns the
  package's `Tag` from `Of`, and `Dial`, `Attach`, `Serve`, `NewHandler` and
  `Open` hold every type parameter drawn from `S` to `runtime.Of[STag]`, so
  an `Envelope` of one family beside a `Handle` of another does not compile.

A type parameter is the plainer case: both languages have one, and a type
that declares parameters renders as a type with type parameters.

A type drawn from a family parameter is validated by the binding of the
family that fills it in TypeScript, and by that family's codec where the
generic type is instantiated in Go.

## The diagram commutes

The two ways to a concrete package — binding the parameters into the
declaration and rendering it plain, or rendering generically and
instantiating — must agree: `gen(bind(C, F)) ≅ gen(C)[F]`. The fixtures
render both into one module and hold them equal: by reflection in Go, by
`Equals<>` under `tsc` in TypeScript, and on the wire, a plain client
against a generic server and the reverse. `internal/oracle` is the left
path of that diagram, test support and nothing a consumer sees.
