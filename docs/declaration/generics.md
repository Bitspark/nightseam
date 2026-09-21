# The holes in a declaration

A family, a record, an entity, a union, an alias and a callable may each declare `parameters` —
the holes in it — and a consumer fills them where the generated code is
used. One mechanism at every level, and a parameter is of one of two
**sorts**, which `of` names.

```json
"parameters": [
  {"name": "S", "of": "protocol", "description": "The family whose messages are carried."},
  {"name": "T", "description": "What a page holds."}
]
```

- **A family parameter** — it has `of`. It is filled by a **family**, one
  that carries the tier `of` names: `protocol`, and whatever tier comes next
  ([a family in tiers](families.md#the-files) has the table). A
  type is drawn *through* it, `S.Envelope` — one message of the family bound
  to `S` — `S.Handle` a channel that speaks it, `S.Payload` any record or
  enum `Payload` of it. The family actually supplied for `S` must declare
  every required associated type plainly; unrelated families in the world
  need not declare those members. Source applications check their selected
  family, and generated construction checks the supplied runtime binding.
  The generic consumer derives without loading a provider.
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
"parameters": [{"name": "S", "of": "protocol"}, {"name": "T", "of": "protocol"}],
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
the containing record's, entity's, union's, alias's or callable's own parameters; inline shapes
and nested applications keep the same scope. A forwarded family parameter
must guarantee the tier the destination requires, just as a concrete
family must carry it. A type parameter cannot fill a family slot.

A family with exactly one family parameter may refer to a generic imported
type plainly only when every parameter the imported type needs is a family
parameter and that one bound guarantees every required tier. A bound of a
higher tier can fill a slot of a lower one, since carrying a tier requires
its lower tiers too, and not the other way about. A type slot always
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
  parameter whatever it is drawn at: `Frame<S extends AnyFamily = AnyFamily>`
  with `message: S["Envelope"]` and `last: S["Payload"]`, the bound narrowed
  to `AnyFamily & { "Payload": unknown }` where a type beyond the ones every
  family carries is drawn and the parameter defaulting to that same bound,
  and one `FamilyBinding<S, K>` argument per family parameter on `toWire` and
  `fromWire`, where `K` is the set of associated types that consumer uses.
  Its `types` dictionary supplies a `ValueAdapter<S[P]>` for every `P` in `K`.
  The generated provider's `family` value carries these recipes for its closed
  members. A type parameter instead takes a `ValueAdapter<T>`, carrying its
  declaration binding and both native/wire conversions. The validators use
  those bindings to check what fills each slot.
- Go has none, so a family parameter becomes one type parameter per type
  drawn from it, named for both: `S` drawn at its `Envelope`, `Handle` and
  `Payload` gives `SEnvelope`, `SHandle` and `SPayload`, and a type takes
  only the ones it uses — `Frame[SEnvelope any]`, `Attachment[SHandle any]`.
  `Frame[codexprotocol.Envelope]` validates what fills the slot through
  codex's codec; `Frame[runtime.Raw]` passes it through, which is what a
  relay wants. Every record and enum of a protocol package returns the
  package's `Tag` from `Of`, and `ToWire` and `FromWire` hold every type
  parameter drawn from `S` to `runtime.Of[STag]`, so
  an `Envelope` of one family beside a `Handle` of another does not compile.
  Each drawn type also takes a `runtime.ValueAdapter[T]` argument, including
  carried envelopes and handles. `runtime.JSONAdapter[T]()` supplies ordinary
  data; a provider's generated `AdapterJob()` supplies a live record.

A type parameter is the plainer case: both languages have one, and a type
that declares parameters renders as a type with type parameters.

A type drawn from a family parameter receives its declaration, validation and
both conversions together. Construction checks that all supplied members
belong to one complete bound family interpretation, including its revision.
A missing recipe or a mismatched member fails before the model factory runs.

## Conversion at a generic operation

Each generated side converts between Wire and the same session-factory model
type. It renders once for its generic declaration; the consumer supplies the
slot adapters when constructing `ToWire` / `FromWire` or `toWire` / `fromWire`.
Changing a slot from scalar data to a declared callable or a nested
callable-bearing record changes that supplied value adapter, not the model
implementation or a carrier-specific bridge.

`runtime.ValueAdapter[T]` / `ValueAdapter<T>` retains the type binding,
conversion in both directions and `NeedsContext` / `needsContext`. Generated
`AdapterX` / `adapterX` factories compose it through declared containers.
`runtime.JSONAdapter[T]()` / `jsonAdapter<T>(binding)` validates ordinary data
without a live dependency or a value environment.

An acquiring adapter receives the current operation's context; it does not
retain an owner or principal in its factory. `runtime.AdapterContext` supplies
the conversion environment. For live values this is
`live.ValueEnvironment(scope)` / `valueEnvironment(scope)`, which selects the
operation owner and provides child lifetimes and export/import/publication
batches. A standalone consumer must enclose the entire walk in the matching
environment batch and pass its active callback context into every adapter.
The [generated surface](generated.md#value-adapters-and-generic-operations)
shows both languages and the lower direct conversion helpers.

This covers type arguments containing fixed declared callables and plain
associated records containing them, including callables that take or return
other callables. A generic operation drawing `S.Job` remains in
`protocol.json`: its supplied interpretation decides whether a live context is
needed. Its generated package imports neither a concrete provider nor live.
Direct draws of aliases, callable declarations and generic members remain
refused; a plain associated record can contain a callable.

## Declared callable applications

A callable declares its type parameters in `live.json` and uses the ordinary
application syntax to fix them before export:

```json
"Function": {"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],
             "request":"A","result":"B"},
"IntToText": {"kind":"alias","type":{"apply":"Function",
              "with":{"A":"integer","B":"string"}}}
```

Its native type is `Function[A, B]` in Go and `Function<A, B>` in TypeScript.
`AdapterFunction(a, b)` / `adapterFunction(a, b)` composes complete value
adapters for its arguments. Either argument may itself be a closed callable
application or a container containing one. An exported implementation imports
its request and exports its result; an imported proxy does the reverse, so
both recipes are required even by a helper used initially in only one direction.

The closed alias above has the same nominal application as those supplied
adapters. Its generated conversion body is specialized directly from the
source signature and does not delegate to the generic callable helper. Neither
route invents a new callable constructor. A separately declared callable with
an identical signature remains a different contract.

Call arguments do not choose type arguments. Anonymous callables, higher-rank
polymorphism and unfilled applications remain outside this grammar. A returned
callable may outlive the call that supplied it, within its owner's lifetime;
each later invocation supplies its own context and acquisition batch.

## Identity of a bound application

A generated generic adapter renders once. Its supplied family and type
bindings determine the [closed declaration identity](declaration-identity.md)
when `ToWire` / `toWire` or `PrepareFromWire` / `prepareFromWire` is called.
The constructor and ordered arguments retain their nominal declaration
paths and reachable content, including nested applications. Changing an
argument's declaration can therefore change the application digest even
when the generated generic adapter itself is unchanged.

Missing required bindings or canonical declaration metadata fail before
model construction or dispatch. A custom codec supplies a `TypeBinding`
with the declaration used to interpret its wire values. Validation and
export/import functions alone do not establish that identity. Generated
`WireType()` metadata in Go and the TypeScript validator's declaration
metadata preserve it; a Go reflection shape or host type-name hash is not
a declaration identity. The family template's digest is never substituted
for a closed application whose arguments are missing.

Go's bound schema exposes `BoundDeclaration()` and `DeclarationDigest()`;
TypeScript exposes `boundDeclaration(validator, slots)` and
`declarationDigest(validator, slots)`. `TypeBinding.Declaration()` in Go and
`typeDeclaration(binding)` in TypeScript select an argument's declaration
while retaining its binding scope. These use the shared canonical graph,
so supplying bindings at runtime produces the same identity in either
language. Identity grants no authority and performs no compatibility
negotiation between distinct declarations.

`CallableIdentity(binding)` / `callableIdentity(binding)` selects a closed
callable's printable path and digest from that same graph. An applied path
includes its ordered arguments, such as `worker/Function<integer,string>`;
reversing those arguments changes the path even when a reference omits its
optional digest. Its digest covers the selected application and reachable
argument content. A nongeneric declared callable retains its declaring
family's digest. Pure aliases preserve both identities. The exact spellings
are held by the shared [callable identity fixtures](../../conformance/tables/callable-identities.json).

## The diagram commutes

The two ways to a concrete package — binding the parameters into the
declaration and rendering it plain, or rendering generically and
instantiating — must agree: `gen(bind(C, F)) ≅ gen(C)[F]`. The fixtures
render both into one module and hold them equal: by reflection in Go, by
`Equals<>` under `tsc` in TypeScript, and on the wire, a plain client
against a generic server and the reverse. `internal/oracle` is the left
path of that diagram, test support and nothing a consumer sees.

The [combined acceptance findings](proof-findings.md#combined-generic-construction-and-retained-values)
hold both generic forms through an unchanged Cell model, independent source
substitution, real connection scopes and installed consumer packages. The
failure fixtures check nested rollback and retained values under a mutable
consumer guard; they do not claim exhaustive authentication coverage.
