# Declaration identity

The digest identifies the full wire-visible declaration, independently of
language and backend presentation. `render.Family.Declaration` is its
canonical graph. `WireDigest()` and `wireDigest` are the lowercase hexadecimal
SHA-256 of that graph's exact UTF-8 bytes. The family's validator descriptor,
`render.Family.Wire`, remains a separate artifact: its local type table does
not cover ordinary operations or referenced declarations.

This implements the coverage verdict of [#343](https://github.com/Bitspark/nightseam/issues/343)
and the graph/normalization rules of [#366](https://github.com/Bitspark/nightseam/issues/366).
Digest equality states declaration identity. It neither grants authority nor
proves that two different backend representations can read each other's bytes.

## Graph and selected closure

The graph is one JSON object with exactly three members:

```json
{"definitions":{},"root":{"primitive":"string"},"version":1}
```

`version` is the integer `1`. `root` is an expression. `definitions` maps
qualified declaration paths to definitions. A family path is its declaration
name (`worker`); a type path is `family/Type`; the two sides use
`family/$server` and `family/$client`. These paths contain declaration names,
never target-language overrides or generated inline-type names.

The family root is `{"ref":"worker"}`. Its definition has `kind:"family"`,
`parameters`, `types`, `server`, `client`, and `errors`. `types` maps every
locally declared type name to its normalized expression. `server` and `client`
reference the corresponding side definitions. `errors` is the set of declared
public error codes. Error descriptions are documentation and are excluded.
The profile's carried `Envelope` and `Handle` layouts are not local declared
types and do not enter this table.

A side definition has `kind:"side"`, `direction` (`server` or `client`),
`parameters`, `extends`, `methods`, and `events`. `extends` preserves inherited
side order, referring to the corresponding side of the source family; an
applied inheritance edge is an application node. `methods` maps names to
objects with `request`, `result`, and `errors`. `events` maps names to payload
expressions. Protocol and live operations both enter their declared direction.
A declared CRUD section, when present, maps each entity name to its type
expression and its set of operation names under `crud`.

A type definition has its declared `kind` and `parameters`. When it captures
family parameters, `captures` lists those parameters in family declaration
order. A parameter object is `{"name":N,"of":T}`, where `T` is the empty
string for a type slot or the declared tier for a family slot. An `extends`
array, when present, retains the ordered inheritance expressions and their
bindings. The kind adds these members:

| Kind | Members |
|---|---|
| record | `open`, ordered `fields` |
| entity | the record members and `key` |
| enum | the set `values` |
| union | `tag`, `value` (including the default `"value"`), and `variants` keyed by tag |
| callable | `request`, `result`, and the set `errors` |
| alias template | `type`, when a parameterized alias itself is selected without bindings |

Every field has `name`, `type`, `required`, `nullable`, and `unique`, including
the boolean defaults. Declared constraints add `min`, `max`, `length`, and
`pattern`; an absent constraint has no member. `length` contains any declared
`min` and `max`. Constraints retain exact numbers as strings, as specified
below. Descriptions and source locations are absent.

References select definition content, not a digest of another definition.
Selection follows normalized expressions transitively from the root. A
definition is entered once before following its edges, so recursive types and
mutually recursive imports form a finite graph. No definition contains another
definition's final digest, and generic applications are never expanded to a
recursive fixpoint. A normalization step may remove an unused alias argument;
the final graph then removes definitions unreachable from the normalized root.

Selecting a family includes all of its local declarations. Selecting one type
or application includes only that expression's reachable declarations. Importing
`shared.Payload` includes that type's content and its reachable dependencies,
not every other declaration in `shared`. Inheriting a side includes that side's
contract and reachable payloads, not all types of the source family.

## Expressions and normalization

| Expression | Canonical form |
|---|---|
| no request/result/payload | `{"empty":true}` |
| primitive | `{"primitive":"integer"}` |
| named declaration | `{"ref":"worker/Job"}` |
| type/family parameter | `{"parameter":"worker/T"}` or `{"parameter":"worker/Handler/T"}` |
| draw from a family parameter | `{"draw":<parameter expression>,"name":"Payload"}` |
| array, map, nullable | `{"array":E}`, `{"map":E}`, `{"nullable":E}` |
| literal string | `{"literal":S}` |
| entity-key reference | `{"entity":<entity expression>}` |
| inline shape | the corresponding type-definition object, without an invented nominal name |
| application | `{"apply":"worker/Handler","arguments":[E1,E2]}` |
| carried family projection | `{"family":{"ref":"worker"},"projection":"Envelope"}` (or `"Handle"`) |

Unqualified references become qualified paths. Pure aliases disappear into
their normalized target expression; a bound alias of an application therefore
has the same graph as the application itself. Callables keep the original
constructor's nominal path even when their signatures are identical. Entity-key
references retain entity meaning rather than becoming the key's primitive type.

Application arguments occur in parameter declaration order: captured family
parameters first, then the constructor's own parameters. The source `with`
object's key order is immaterial. Bindings are retained as expressions and
parameter nodes; an argument is normalized in its caller's lexical scope before
substitution into an alias. A type parameter with the same name in another
declaration cannot capture that argument. Family arguments reference their
family declaration graph. The unapplied family digest does not establish
identity of two applications.

A **selected composite root** scopes each expression-valued child separately.
Its children have the form `{"graph":G}`, where `G` is another complete
`{version,root,definitions}` document using these same rules. The constructor
of an application and its reachable template content occupy the outer
definitions. A child graph contains only that child's reachable content. This permits two slots
to hold two revisions of the same qualified path without a definition-key
collision. No final digest appears in either scope.

For example, a closed family application has this root:

```json
{"apply":"worker","arguments":[{"graph":{"definitions":{},"root":{"primitive":"integer"},"version":1}}]}
```

The scoped child positions are application `arguments`, container `array`,
`map`, and `nullable`, entity-reference `entity`, a draw's `draw` source,
a carried projection's `family`,
inline `fields[].type`, `variants` values, `extends` entries, alias `type`, and
callable `request` and `result`. This rule always applies at a selected root,
including a primitive-valued child; it is not conditional on a name collision.
A root which is itself `{"graph":G}` unwraps to `G`. References, primitives,
parameters, literals, and empty expressions have no child scopes. Consequently
an alias expanded to an anonymous record has the same graph as that record
selected directly, including when its fields contain multiple revisions of
one nominal path.

Expressions **inside definitions** retain ordinary lexical children rather
than embedding graphs recursively. Selecting a composite expression normalizes
its root children into scoped graphs. Selecting a recursive
named argument preserves the finite definition graph in its scope; it does not
re-expand every application occurring inside that definition.

`render.CanonicalApplication(template, arguments)` composes a normalized
constructor-reference graph with ordered argument graph strings. It selects
each graph's reachable content again and uses the exact same encoding as
`CanonicalExpression`. Source aliases normalize before this composition;
callable-template applications and their pure aliases produce identical roots.

## Printable callable applications

`CallableIdentity(TypeBinding)` in Go and `callableIdentity(TypeBinding)` in
TypeScript require a closed callable root and return its path and digest.
An application prints its qualified constructor path followed by ordered
argument names in angle brackets, separated by commas with no spaces:
`worker/Function<integer,string>`. Captured family parameters precede the
constructor's own parameters, just as in the canonical graph.

Argument names are rendered from normalized expression roots without expanding
referenced definitions. Scoped `graph` children contribute their root expression.

| Expression | Printable name |
|---|---|
| reference or primitive | its qualified path or primitive spelling |
| application | `path<A,B>` |
| array, map, nullable | `[T]`, `{T}`, `T?` |
| literal string | its canonical JSON string spelling |
| entity-key reference | `&T` |
| carried projection | the family name followed by `/Envelope` or `/Handle` |
| empty expression | `()` |
| anonymous shape | `shape(` followed by its canonical expression JSON and `)` |

An anonymous shape removes scoped graph wrappers recursively from its expression
before printing; it introduces no generated nominal name. Missing parameters,
unresolved draws and incomplete applications are errors. Referenced definition
content stays in the digest, so a changed revision may retain the same printable
name. Applied callables hash their selected canonical application graph;
nongeneric declared callables retain the digest of their declaring family.
Aliases select their original callable and retain both identities. The shared
[callable identity fixtures](../../conformance/tables/callable-identities.json)
hold these byte spellings and the separate nominal and revision comparisons.

References to carried `Envelope` or `Handle` select the associated abstract
family contract, not the current JSON envelope or channel identifier layout.
An empty protocol tier consequently does not change an otherwise equal family
declaration. A declared operation, event, or type still does.

Invalid/unresolved inputs are outside this contract. The renderer can retain
an `unresolved` marker for diagnostic tooling, but generated identity is emitted
only after neutral declaration checks pass.

## Exact bytes

The encoding uses UTF-8, with no byte-order mark, whitespace, indentation, or
trailing newline. All object keys are sorted by their UTF-8 byte sequence.
Arrays retain their order except mathematical sets: error codes, enum values,
and CRUD operation names are deduplicated and sorted by UTF-8 bytes. Field,
parameter, inheritance, and application-argument order is meaningful and kept.

Strings are JSON strings. Quote and backslash use `\"` and `\\`; backspace,
tab, newline, form feed, and carriage return use `\b`, `\t`, `\n`, `\f`, and
`\r`. Other U+0000–U+001F characters use lowercase four-digit `\u00xx` escapes.
`<`, `>`, and `&` use `\u003c`, `\u003e`, and `\u0026`; U+2028 and U+2029 use
`\u2028` and `\u2029`. Every other Unicode scalar is emitted directly as UTF-8.
There is no Unicode normalization. Declaration loading rejects invalid scalar
strings; surrogate code points are not an alternate accepted encoding.

Booleans are `true` or `false`; the version is `1`. Numeric type constraints are
JSON **strings**, never binary floating-point JSON numbers. Normalize their
exact decimal input by joining the mantissa digits, subtracting the fractional
digit count from the decimal exponent, dropping leading coefficient zeroes,
then dropping trailing coefficient zeroes and adding their count to the
exponent. Zero is `"0"`, including negative zero. Otherwise emit an optional
minus, the coefficient, and (only for a nonzero exponent) `e` followed by the
signed exponent without `+` or leading zeroes. Thus `1.00`, `10e-1`, and `1`
become `"1"`; `100` becomes `"1e2"`; `9007199254740993` retains every digit.
Length bounds are nonnegative decimal integer strings without leading zeroes.
This avoids the loss of integer precision caused by double-precision canonical
JSON schemes. The declaration language's literal expression currently accepts
strings; any future numeric literal must preserve exact numeric meaning too.

The encoder used here is Go's `encoding/json` over maps and these normalized
JSON values. Other languages must reproduce the rules above, including key
order and escaping, rather than reserialize with their default JSON settings.

## Shared proof

[`conformance/tables/declaration-digests.json`](../../conformance/tables/declaration-digests.json)
contains complete declaration inputs, selected roots, exact canonical strings,
SHA-256 values, and named equality/inequality controls. The rows cover ordinary
requests/results/direction, events, error contracts, imported and recursive
content, application arguments and nominality, documentation and target naming,
unreachable declarations, exact integers, inheritance, and live operations.
The runtime suites consume the same byte strings; generation tests hold the
Go and TypeScript exports to the renderer.

Each source row also supplies its normalized `wireExpression`, its `wire` and
`familyDeclaration`, and a `families` map of every source family's validator
descriptor, canonical declaration, digest, and imports. This lets both runtime
suites exercise identity selection through their real validators rather than
reimplement the source loader in a test. `application` rows compose earlier
named cases as a constructor and ordered arguments. `composite` rows replace
`{"case":N}` leaves with the earlier case's scoped graph, exercising multiple
revisions of one path inside an anonymous structure.

`go test ./internal/render -run TestCanonicalDeclarationCoverage
-update-declarations` rewrites the byte fixtures for an intentional graph
change. The ordinary test reads them without rewriting. A changed encoding is
a declaration-identity change and must be reviewed as such.
