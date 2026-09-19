# A family in tiers

A family of API is declared in tiers of JSON under `api/contracts/<family>/`
of the consuming checkout. This page is the reference for what may be
written there — the tiers, the types, the two sides, the governance of a
session, the per-target names. [The holes in a declaration](generics.md) is
the rest of the language; [the generator](generator.md) says how a
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
api/contracts/probe/typescript.json  not a tier: the same for TypeScript (and markdown.json for the specification)
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
    "Part": {"kind": "union", "tag": "type", "variants": {"text": "TextPart", "count": "integer"}},
    "Page": {"kind": "record", "parameters": [{"name": "T"}], "fields": [
      {"name": "items", "type": {"array": "T"}}
    ]},
    "Payloads": {"kind": "alias", "type": {"array": "Payload"}}
  }
}
```

A `record` has `fields`, may `extends` other records (their fields come
first, in wire order) and may be `open` (fields beyond the declared ones are
kept). An `enum` has `values`; an `alias` a `type`; a `union` a `tag` and
`variants` (below). An `entity` is a record with a `key`. A record, an
entity, a union and an alias may declare `parameters`, the holes in it —
[the holes in a declaration](generics.md).

A field is `required` unless it says otherwise and never null unless
`nullable`: presence is a fact of the member and nullness a fact of the
value, and a field's `nullable: true` is sugar for wrapping its type in
`{"nullable": T}` ([nullness is a fact of a
value](../decisions/nullness-is-a-fact-of-a-value.md)). `min`, `max`,
`length` and `pattern` constrain a field, and the validators enforce them;
`unique` says no two instances of the entity share the value, which is the
holder's to enforce and not the wire's — a validator sees one value.

A **`pattern`** is written in Nightseam's own regular-expression language:
ECMAScript Unicode syntax without lookaround and without backreferences,
matching code points with ECMAScript character classes and line terminators.
TypeScript uses `u` mode; Go translates the classes and dot to the same
sets. NBSP matches `\s`, one emoji matches one `.`, and dot excludes LF,
CR, U+2028 and U+2029. Unicode escape and range syntax is checked strictly. A
spelling only one engine reads — RE2's `(?P<name>`, inline flag groups,
`[[:alpha:]]`, `\A`, `\z`; ECMAScript's lookahead, lookbehind, `\1`,
`\k<name>` — is refused where it is written rather than found where it is
run ([a pattern has one dialect](../decisions/a-pattern-has-one-dialect.md)).

### Type expressions

A type expression is one of:

| written | is |
| --- | --- |
| `"string"` | a primitive: `string`, `boolean`, `integer`, `number`, `timestamp`, `json` |
| `"Payload"` | a name in scope: a parameter of this declaration or of the family, else a type of this family |
| `"identity.User"` | a type of an imported family |
| `"duplex.Envelope"` | a type this family carries from a built-in family (below) |
| `"S.Envelope"` | a type drawn through a family parameter |
| `{"array": T}` | an ordered list |
| `{"map": T}` | a string-keyed map |
| `{"nullable": T}` | a value that may be null |
| `{"literal": "text"}` | the type of one string value |
| `{"ref": "User"}` | a reference to an entity by its key |
| `{"apply": "Page", "with": {"T": "User"}}` | a generic type of this family, filled |
| `{"apply": "carrier.Frame", "with": {"S": "probe"}}` | a generic type of an imported family, filled |
| `{"kind": "record", "fields": […]}` | a shape written where a type is named |

There is one reference form: a qualifier in upper camel case is a
parameter, in lower case a family, and every family a declaration names is
imported or built in ([one reference
form](../decisions/one-reference-form.md)).

### Unions

```json
"Part": {"kind": "union", "tag": "type", "variants": {
  "text":  "TextPart",
  "image": {"kind": "record", "fields": [{"name": "url", "type": "string"}]},
  "count": "integer"
}},
"TextPart": {"kind": "record", "fields": [
  {"name": "type", "type": {"literal": "text"}},
  {"name": "body", "type": "string"}
]},
"RichPart": {"kind": "union", "extends": ["Part"], "tag": "type", "variants": {
  "table": {"kind": "record", "fields": [{"name": "rows", "type": {"array": {"nullable": "string"}}}]}
}}
```

A union is one of several variants, told apart by the member `tag` names.
Every payload is carried whole under `value`, including a record, map, JSON
or null: `{"type":"image","value":{"url":"…"}}` and
`{"type":"count","value":3}`. A union may rename the payload member with
`"value":"<member>"`; it must differ from the discriminator. A record's own
literal member stays inside that payload, with its own required/nullable
rules, and need not match the outer tag.

A variant declared as `{"empty":true}` has no payload and writes only the
tag: `{"type":"none"}`. This marker belongs only in `variants`. An empty
record and a nullable value remain payloads and write `"value":{}` and
`"value":null`, respectively.

A union may extend other unions, adding variants with the same tag and
value member. A base's tag cannot be redeclared; conflicting inherited tags
or bindings and inheritance cycles are refused. A base value validates
against the extended union; the reverse does not. String `enum` stays
unchanged. [A union is adjacently tagged](../decisions/a-union-is-internally-tagged.md)
records the carrier and why it replaced the original flat form.

### Inheritance arguments

An `extends` entry is a bare name for a nongeneric base, or an explicit
application for a generic base:

```json
"Child": {"kind":"record", "parameters":[{"name":"Item"}],
  "extends":[{"apply":"base.Box","with":{"T":{"array":"Item"}}}],
  "fields":[]}
```

The target is `Type` or `family.Type`. Every required parameter is bound
explicitly, including captured enclosing-family parameters and the base's
own parameters. This also applies to a local base. A binding can fix a type
(`"T":"string"`), forward a parameter (`"T":"Item"`), or fill a family
parameter with a family or an in-scope family parameter of a sufficient
tier. Same-spelled parameters do not bind automatically. Missing arguments,
unknown slots and incompatible kinds or tiers are diagnostics. Arguments
belong to the extending declaration's scope; inherited names retain their
original family's meaning.

### Shapes without a name

A record, an enum or a union may be written where a type is named — in a
field's type, in a variant, in an operation's request, result or event
type, and under any `array`, `map` or `nullable` of those. The generator
derives its name from the path to it:

> the upper camel of the declaration it sits in, then each step that
> **names** something — a field's name, a variant's tag, an operation and
> the role the shape plays in it — while the steps that name nothing,
> `array`, `map`, `nullable` and an application's slot, are passed over.

So the `note` member of `EchoRequest` is `EchoRequestNote`, a method's
inline request is `EchoRequest`, an event's data is `ChangedEvent`, and
`{"array": <shape>}` at `Message.parts` is `MessageParts`. The rule is
`internal/naming` and the `derived` rows of
`conformance/tables/naming.json`, so that every language spells it one way.

A shape is written inline where a *value's* type is declared and nowhere
else: an alias of one, an entity, and a shape filling a parameter are each
refused; so is a shape that declares parameters of its own, which nothing
could fill, and so is a derived name that collides with a declared type or
with another derived name. A diagnostic about a shape with no name points
at the path, which is what it has instead of a name. [A shape without a
name is named by where it
sits](../decisions/a-shape-is-named-by-where-it-sits.md) says what the rule
is being measured for.

## protocol.json

```json
{
  "profile": "nightseam.duplex/1",
  "imports": ["workbench"],
  "server": {
    "extends": ["workbench"],
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
client side is the reverse. A method's `request` is an object on the wire —
a record of this family or an imported one, a shape written inline, a
union, or a type drawn from a parameter — or absent; its `result` any type;
its `errors` codes the family declares, and a method that names an error the
family does not declare is refused. The public errors reach both languages
by name — [the generated packages](generated.md#errors) say how.

A generic side is extended with
`"extends":[{"apply":"workbench","with":{"T":"Item"}}]`; the target is
the family name and selects the same side. Every parameter declared by the
base family is explicitly filled. A bare family name selects a nongeneric
base only.

A side may **`extends`** another family's same side: its methods and events
arrive under their own names, with its errors, and with its governance
where the session tier is extended too. The extending family's surface is a
superset, so a consumer of the base may speak to it. The family it extends
is one this family imports; a side that extends its own family's, a chain
that returns, and a name that means two things across the join are each
refused ([a side may extend another
family's](../decisions/a-side-may-extend-another-familys.md)). Session governance
may name inherited operations on the selected sides, including through
another base. A conversation definition follows the side that carries its
event. Repeating the same event and path is compatible; inheriting or
declaring a different one is an `incompatible_governance` diagnostic. A
protocol-only family does not acquire a session tier by extending a side.

### What a tier brings

A tier's own vocabulary is declared as a **built-in family** — `duplex` for
the profile, `tunnel` for the tunnel, `session` for the session — written
in this same language, carried in the binary and held to the same shape
schemas. A family that has a tier file **imports** that tier's built-in,
with no `imports` line; naming one in `imports` is refused, since it is
already there.

The names `duplex`, `tunnel` and `session` are reserved for these built-ins;
a family of the checkout must use another directory name.

The protocol tier's built-in is **carried**: `duplex`'s `Envelope`, one
message of the profile, and `Handle`, a reference to a channel that speaks
it, are this family's own types, because a family's envelope is a message
of *that* family. A declaration names one by the built-in that declares it —
`duplex.Envelope` — and may not declare a type of that name itself. A
family's specification prints them with their members, and a diagnostic
that points into a built-in locates it as `nightseam:duplex/model.json`,
which is plainly not a file of the checkout. [A tier is a built-in
family](../decisions/a-tier-is-a-built-in-family.md) is the record, and
`internal/model/builtin/` the declarations.

## session.json

```json
{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}
```

`decides` names the methods that need control to send; `asks` the client
methods — the ones the server sends — that raise a request the holder of
control must answer; `conversation` where the conversation id arrives. An
`extensions` member is carried through for other tools and read by nothing
here. A family with a session tier carries the `session` tier, which a
family parameter binds to. What a session does with `decides` and `asks` is
[the session](../wire/session.md).

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
