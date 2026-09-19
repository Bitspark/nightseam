# A family generic in others

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
with a session tier carries ([a family in tiers](families.md#sessionjson)).
Two parameters never collapse into one.

## Referring to a generic type

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

## How each language instantiates it

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

A type drawn from a parameter is validated by the binding of the family
that fills it in TypeScript, and by that family's codec where the generic
type is instantiated in Go.

## The diagram commutes

The two ways to a concrete package — binding the parameters into the
declaration and rendering it plain, or rendering generically and
instantiating — must agree: `gen(bind(C, F)) ≅ gen(C)[F]`. The fixtures
render both into one module and hold them equal: by reflection in Go, by
`Equals<>` under `tsc` in TypeScript, and on the wire, a plain client
against a generic server and the reverse. `internal/oracle` is the left
path of that diagram, test support and nothing a consumer sees.
