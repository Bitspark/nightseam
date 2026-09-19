# What the type-language proof showed

The [proof family](../../cmd/nightseam/testdata/families/api/contracts/proof)
puts the settled forms in one declaration and imports the corpus's `probe`.
It is rendered with the other families, compiled in both languages, and
driven over sockets by the [generated conformance scenarios](../../conformance/scenarios/generated).
These are the findings of [#105](https://github.com/Bitspark/nightseam/issues/105).

## The same values, different native forms

| Form | Go | TypeScript | Evidence |
| --- | --- | --- | --- |
| Tagged union | A struct of alternative pointers, `Kind()`, and checked codecs; scalar payloads have a wrapper | A discriminated union narrowed by a switch on its tag | [Union calls and events](../../conformance/scenarios/generated/proof-unions.json) |
| Extended union | Explicit widening and checked narrowing helpers | Structural inclusion of the base alternatives | The same scenario sends a base value to both bindings and refuses the added `table` variant at the base binding |
| Optional and nullable | `Optional[Nullable[T]]`, with presence separate from the null bit | An optional member with `T \| null` | The image payload keeps `alt: null`; the table event keeps null inside `rows` |
| Generic type | Native generic structs and union wrappers | Generic interfaces and type aliases | [Page of parts and page of strings](../../conformance/scenarios/generated/proof-generics.json) |
| Literal | A named scalar with a checked codec | A literal type | The text payload retains its own `type: "text"` inside the union's complete `value` |

The Go union is usable but not a native sum type: construction can select
zero or several pointers, so its codec must check the selection. The
TypeScript union narrows naturally in a switch. Both keep the complete
payload under `value`, even when it is a record or an integer; neither
needs to merge the payload's fields into its discriminator object.

The generated Go binding dispatches decoded alternatives. Both generated
clients receive and dispatch typed events; a refused call is followed by
a successful call on the same connection. The TypeScript target currently
emits clients, not bindings. Mirrored scenarios exercise Go/TypeScript in
both ordered pairings with Go serving, and report the unsupported reverse
binding role as a skip in the matrix. No hand-written TypeScript binding
stands in for an output the target does not produce.

## A family parameter and a type parameter stay distinct

`Carried<S, Item>` contains `S.Envelope`, nullable `S.Handle`, and
`Page<Item>`. The [mixed diagram fixture](../../cmd/nightseam/proof_diagram_test.go)
substitutes `S = probe` and `Item = string` in source JSON independently of
the generator, then compares that plain rendering with the generic one.
It checks Go structure and codecs, TypeScript type equality and refusal
messages, and both languages' plain and generic clients against the
opposite Go binding over real sockets.

“A family parameter is a type parameter with a bound” held at the
declaration level. Its native realization differs: Go draws separate
associated types constrained at entry points by the same family tag; TypeScript uses a
family interface plus a runtime validator binding. The independent `Item`
argument remains string on both paths. An integer item, malformed family
envelope, or malformed handle is refused on both paths.

## Path-derived names are readable and do move

`PartImage`, `RichPartTable`, `PartsRequest`, and `OptionNone` read as the
places that own their shapes. The [name scenario](../../conformance/scenarios/generated/proof-names-and-domain.json)
holds references to those actual emitted types in both compiled testees.

The [churn fixture](../../cmd/nightseam/proof_churn_test.go) moves the
unchanged inline `image` payload from `Part.variants` to
`RichPart.variants`. Its [measured output](../../cmd/nightseam/testdata/golden-proof-churn/move-image.json)
records `PartImage` becoming `RichPartImage` in both languages. Two Go
protocol files, one TypeScript types file, and the specification change;
the client and binding method files do not. The other three inline names
stay the same, and both moved packages still compile.

This move also deliberately removes the alternative from the base union:
the extended union still accepts the same image value, while the base now
refuses it. The rename is therefore measurable public API churn, alongside
that wire-domain change. Naming a shape explicitly remains appropriate
when its identity should survive a move. This measurement supplies the
evidence requested by [#60](../decisions/a-shape-is-named-by-where-it-sits.md);
it does not reverse the inline-shape ruling.

## Extending a side works at the binding boundary

The [base-client scenario](../../conformance/scenarios/generated/proof-side-extends.json)
dials the proof binding with an unchanged generated `probe` client.
Inherited requests retain `probe`'s payload types and wire names. An
extended side is enough to provide those operations; it does not turn the
proof family into a session family.

`TestProofInheritanceKeepsGovernanceOnItsTierAndSide` adds governance to
the base in a copy of the fixture. Proof has no session until its own
session tier is added. Then `decides: echo` and the server-side conversation
arrive with the extended server side; `asks: reverse` from the unextended
client side does not. This is held in the same [diagram fixture](../../cmd/nightseam/proof_diagram_test.go).

## Refusal is part of the proof

The [domain scenario](../../conformance/scenarios/generated/proof-names-and-domain.json)
compares the same pattern-dialect refusal message and the driver's
`invalid` code in both languages. That code belongs to the testee protocol;
it does not add a validator API error type. The scenario also holds the
[Unicode scalar ruling](../decisions/strings-are-unicode-scalars.md): a
valid emoji pair and ordinary U+FFFD pass; an unpaired surrogate, including
one hidden by a duplicate member, is refused before decoding loses it.
The proof found no further contract decision to reopen.
