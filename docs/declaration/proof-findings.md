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

Both generated bindings dispatch decoded alternatives. Both generated
clients receive and dispatch typed events; a refused call is followed by
a successful call on the same connection. Mirrored scenarios exercise
each language serving through its generated binding and calling through
its generated client.

## Generated roles and skips

The checked-in [matrix](../../conformance/matrix.json) records the current
results, including generic live containers and higher-order forwarding.
[`TestGenerated`](../../conformance/go/conformance_test.go) runs Go/Go,
Go/TypeScript and TypeScript/Go. Each pair runs the generated scenario set;
`mirror: true` runs a scenario again with driver sides exchanged. The
TypeScript target renders both roles, and its testee serves its generated
binding over an accepted socket. Socket hosting and canned application
handlers live in the testee; validation, conversion, typed reverse calls
and events come from generated packages.

| Driver pair (`a` / `b`) | Generated code exercised |
|---|---|
| Go / Go | Go client and Go server binding, including mirrored roles |
| Go / TypeScript | Go binding with TypeScript client; mirrored scenarios also exercise TypeScript binding with Go client |
| TypeScript / Go | TypeScript binding with Go client; mirrored scenarios also exercise Go binding with TypeScript client |

The TypeScript row combines both cross-language pairs. The names/domain
case runs generated names and validators on both sides without a binding.
The server operations below exercise the same declarations in both
languages; none substitutes a handwritten server protocol adapter.

| Generated server operation and TypeScript testee | Scenarios |
|---|---|
| `gen.serve` in [testee.ts](../../conformance/ts/generated/testee.ts) | [round-trip](../../conformance/scenarios/generated/round-trip.json), [validation-refuses](../../conformance/scenarios/generated/validation-refuses.json), both mirrored |
| `gen.proof_serve` in [proof.ts](../../conformance/ts/generated/proof.ts) | [proof-generics](../../conformance/scenarios/generated/proof-generics.json), [proof-side-extends](../../conformance/scenarios/generated/proof-side-extends.json), [proof-unions](../../conformance/scenarios/generated/proof-unions.json), all mirrored |
| `gen.live_serve` in [live.ts](../../conformance/ts/generated/live.ts) | [live-callback-and-result](../../conformance/scenarios/generated/live-callback-and-result.json), [live-higher-order](../../conformance/scenarios/generated/live-higher-order.json), [live-nested-values](../../conformance/scenarios/generated/live-nested-values.json) |
| `gen.combinator_serve` in [combinator.ts](../../conformance/ts/generated/combinator.ts) | [live-higher-order-callables](../../conformance/scenarios/generated/live-higher-order-callables.json), [live-generic-containers](../../conformance/scenarios/generated/live-generic-containers.json) |
| `gen.forwarding_serve` in [forwarding.ts](../../conformance/ts/generated/forwarding.ts) | [live-higher-order-forwarding](../../conformance/scenarios/generated/live-higher-order-forwarding.json), mirrored |
| `gen.owners_serve` in [owners.ts](../../conformance/ts/generated/owners.ts) | [live-owners](../../conformance/scenarios/generated/live-owners.json), mirrored, with four exports and imports per connection |

The forwarding scenario uses generated endpoint bindings in either
language, with a test-only retain route calling generated
`ImportToolkit`/`importToolkitUnchecked`. A Go or TypeScript intermediary runs
generated converters across two connections. The retain route remains
driver plumbing; it is not a new declared operation.

The [tier policy](../languages/tiers.md) defines required coverage and the
meaning of a skip. Counts change with the scenario set; this inventory
describes the roles, not a fixed expected total.

## A family parameter and a type parameter stay distinct

`Carried<S, Item>` contains `S.Envelope`, nullable `S.Handle`, and
`Page<Item>`. The [mixed diagram fixture](../../cmd/nightseam/proof_diagram_test.go)
substitutes `S = probe` and `Item = string` in source JSON independently of
the generator, then compares that plain rendering with the generic one.
It checks Go structure and codecs, TypeScript type equality and refusal
messages, and both languages' plain and generic clients against the
opposite Go binding over real sockets.

The declaration language keeps family parameters and type parameters as
[two sorts](generics.md): `S` is filled by a family, `Item` by a type.
Go draws separate associated types constrained at entry points by the same
family tag; TypeScript uses a family interface plus a runtime validator
binding. The independent `Item` argument remains string on both paths. An
integer item, malformed family envelope, or malformed handle is refused
on both paths.

## Combined generic construction and retained values

The [combined corpus](../../conformance/corpora/generic-composition) is the
acceptance fixture for [#363 B](https://github.com/Bitspark/nightseam/issues/363)
and [#370](https://github.com/Bitspark/nightseam/issues/370). It declares
`Function<A,B>`, a source alias `IntegerFunction`, a higher-order `Factory`,
and `Holder<S>` drawing both `S.Job` and `S.Progress`. The two providers put
integer and string callable applications inside those associated records.
An unrelated live declaration need not implement the selected family's
associated types; the actual supplied family must satisfy every draw.

The [shared table](../../conformance/tables/generic-composition.json) supplies
five slots to the existing, unchanged `Cell<T>` model. Every row performs a
put/get, retains the first value, replaces it, reads the second, exercises
the reverse method and both event facets, and invokes the retained value
after the supplying calls have returned. Nullable arrays, maps, the empty
union arm and the progress label are checked during observation. A
higher-order observation verifies that its supplied callback runs once.

| Evidence | Construction held |
| --- | --- |
| [GEN-COMPOSE-LOCAL](../../conformance/scenarios/generated/generic-composition-local.json) | Five slots through local, nested mounted and forwarded model Wires |
| [GEN-COMPOSE-CARRIERS](../../conformance/scenarios/generated/generic-composition-carriers.json) | The same slots and presentations through real sockets and prepared tunnel channels |
| [GEN-COMPOSE-BRIDGE](../../conformance/scenarios/generated/generic-composition-bridge.json) | The whole model returned by `FromWire(origin)` is supplied directly to `ToWire(destination)` over two connections and four live scopes |
| [GEN-ID-ROUTES](../../conformance/scenarios/generated/generic-composition-identity.json) | Source-specialized `IntegerFunction` and supplied `Function<integer,integer>` recipes at opposite ends, in both construction orders |
| [`TestGenericCompositionIndependentRoutes`](../../cmd/nightseam/generic_composition_acceptance_test.go) | Both targets compile and execute source-substituted `Holder` declarations and the unchanged generic declaration with the two supplied provider recipes |
| [`TestGenericCompositionFailures`](../../cmd/nightseam/generic_composition_failures_test.go) | A nested failure after fresh acquisition, borrowed and unrelated child survival, concurrent independent scopes, and mutable synthetic guard preservation |
| [Packed consumer](../../scripts/smoke-generic-composition.mjs) | The installed candidate generator builds both forms outside the workspace; scalar Go and TypeScript consumers have no live dependency |

`TestGenerated` runs every shared row in Go/Go, Go/TypeScript and
TypeScript/Go, with mirrored roles. The table names the exact binding counts
before release and expects zero after explicit owner release while carriers
remain open. Allocation observations distinguish carrier setup from view
construction and first use; selection adds no peer or channel. The bridge
contains no per-method or per-slot forwarding implementation.

The independent Holder route substitutes the source model with
`internal/oracle` before either target resolves or renders it. The generic
route renders the original declaration and supplies value recipes in the
consumer. They share parsing, target lowering and runtime mechanisms, but
not the substitution route or generated native types. Callable aliases
retain the constructor's nominal origin and ordered arguments while their
specialized helpers emit their own bodies. A new `OtherFunction` with the
same signature is the negative nominal control. The paired
[callable identity table](../../conformance/tables/callable-identities.json)
additionally covers argument order, nesting, reachable revisions and aliases;
it does not equate a template identity with an applied one.

The synthetic guard belongs to a consumer exposure and rereads mutable
policy at each protected effect, including returned callbacks and
local/self-reference and forwarded values. Revoking one exposure leaves
another exposure of the same reusable type usable. This proves converter
plumbing; it does not implement the exhaustive exposure binding of #356,
authenticate a principal, or validate signatures, expiry or grants. The
fixtures exercise a finite grammar and do not prove higher-rank polymorphism
or structural callable identity.

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
extended side is enough to provide those operations and brings nothing of
the base beyond them.

A second finding, that a tier of the base did not travel with an extended
side, was measured against the governed session tier and went with it in
0.5.0 ([#196](https://github.com/Bitspark/nightseam/issues/196)); side
inheritance carries a side's operations and errors and nothing else.

## Refusal is part of the proof

The [domain scenario](../../conformance/scenarios/generated/proof-names-and-domain.json)
compares the same pattern-dialect refusal message and the driver's
`invalid` code in both languages. That code belongs to the testee protocol;
it does not add a validator API error type. The scenario also holds the
[Unicode scalar ruling](../decisions/strings-are-unicode-scalars.md): a
valid emoji pair and ordinary U+FFFD pass; an unpaired surrogate, including
one hidden by a duplicate member, is refused before decoding loses it.
The proof found no further contract decision to reopen.

## Constraints the proof establishes

The proof exercises selected declarations and values. Its successful round
trips do not establish compatibility for arbitrary later changes to those
declarations. The table separates chosen wire and API commitments from
current implementation limits; neither classification is a promise that a
future release can change the behavior without affecting consumers.

| Commitment or limit | Consequence for a consumer | Executable evidence |
| --- | --- | --- |
| **Wire commitment: adjacent-tagged unions.** The discriminator and complete payload occupy separate members; an empty arm has no payload. | Adding an alternative widens the extended union only. A reader of the base declaration still refuses the new tag, even when the new payload resembles an old one. | [proof-unions](../../conformance/scenarios/generated/proof-unions.json) accepts `table` at `classify_rich` and refuses it at `classify`; the [validator table](../../conformance/tables/validator.json) covers payload forms and invalid tags. |
| **Go API choice: alternative-pointer unions.** Go emits a struct with one pointer per alternative; TypeScript emits a discriminated union. | Go construction can select zero or several alternatives. The generated codec enforces exactly one; the Go type alone cannot. TypeScript narrowing also does not validate bytes received from the wire. | [TestGoProofRenderingAndSurfaceGolden](../../cmd/nightseam/proof_test.go) records the exported Go form. [TestConcreteUnionCodecCompilesAndRoundTrips](../../internal/targets/golang/union_codec_test.go) runs `TestSelectionAndTransactionalDecode`, refusing zero/multiple selections and preserving the receiver after failed decoding. |
| **Naming commitment: an inline shape takes its name from its path.** Both targets use that derived name. | Moving an unchanged shape may rename public types. Declare a named shape when its API name should survive relocation; the measured move also changes which union accepts the alternative. | [TestProofInlineMoveChurnGolden and TestProofInlineMoveStillCompiles](../../cmd/nightseam/proof_churn_test.go), the [recorded diff](../../cmd/nightseam/testdata/golden-proof-churn/move-image.json), and [proof-names-and-domain](../../conformance/scenarios/generated/proof-names-and-domain.json). |
| **Grammar commitment: inline shapes have specific positions.** Records, enums and unions may appear in fields, variants, operation requests/results/events and their array/map/nullable containers. | Inline entities, aliases of inline shapes, parameterized inline shapes and shapes used as generic arguments are refused. Use a named intermediate declaration in those positions. | The [inline-shape invalid fixture](../../cmd/nightseam/testdata/invalid/inline-shape/diagnostics.txt) holds alias/argument refusals; [the expression checker](../../internal/check/check.go) checks the admissible position and kind. |
| **Binding commitment: generic inheritance takes explicit arguments.** A generic base's own and captured family parameters must be filled, including for a local base. | Same-spelled parameters do not bind themselves. Type and family slots remain different, and kind/tier checks still apply to the supplied arguments. | [The inheritance checks](../../internal/check/inheritance.go), [TestInheritanceKeepsExplicitBindingsAndTheirLocations](../../internal/model/inheritance_test.go), [TestProofMixedDiagramCommutes](../../cmd/nightseam/proof_diagram_test.go), and [proof-generics](../../conformance/scenarios/generated/proof-generics.json). The diagram proves its mixed instantiation, not every possible binding. |
| **Side-inheritance commitment: the same side's operations and errors are inherited.** Names and bindings must remain unambiguous. | Extending a side does not inherit a family's lifecycle, ownership policy or unrelated semantics. | [proof-side-extends](../../conformance/scenarios/generated/proof-side-extends.json) uses a base client against the extended binding; the [extended-side-collision fixture](../../cmd/nightseam/testdata/invalid/extended-side-collision/diagnostics.txt) refuses conflicting inherited names. |
| **Representation commitment: JSON data and object-shaped ordinary RPC requests.** The primitives are `string`, `boolean`, `integer`, `number`, `timestamp` and `json`; maps have string keys. | Native map keys outside strings require a representation choice. Ordinary method requests, including methods declared in `live.json`, must be object-shaped or absent; a callable's own request may be a general type expression, including a scalar. | [Type-expression definitions](../../internal/model/expr.go), [TestRules/operations](../../internal/check/check_test.go), the [protocol/callable checks](../../internal/check/check.go), [live method checks](../../internal/check/live.go), and the [shared validator table](../../conformance/tables/validator.json). |
| **Portable value-domain commitment: Unicode scalar strings and one restricted pattern dialect.** Native string types can hold values outside that domain. | Invalid UTF-8 and unpaired surrogates are refused. Patterns use the supported ECMAScript Unicode subset without lookaround or backreferences; arbitrary host regex syntax is not portable declaration syntax. | [proof-names-and-domain](../../conformance/scenarios/generated/proof-names-and-domain.json), the [validator table](../../conformance/tables/validator.json), and the [malformed-Unicode](../../cmd/nightseam/testdata/invalid/malformed-unicode/diagnostics.txt) and [pattern-dialect](../../cmd/nightseam/testdata/invalid/pattern-dialect/diagnostics.txt) invalid fixtures. |
| **Implementation limit: example synthesis is bounded.** A generic example displays the concrete bindings it demonstrates, or an explicit unavailability reason. | `unavailable` with kind `limit` does not prove that no value exists. A validated example establishes that value under those bindings; it is not an exhaustive proof of the declaration or every instantiation. | [TestDocumentExamplesTable](../../cmd/nightseam/document_examples_test.go) derives [examples.json](../../conformance/tables/examples.json); the [Go](../../runtime/go/document_examples_test.go) and [TypeScript](../../runtime/ts/src/document-examples.test.ts) tests validate its concrete rows. [TestUnavailableExamplesAreAccountedForWithoutInventedJSON](../../internal/doc/validated_examples_test.go) covers bounded synthesis, including large and recursive declarations. |

The [family reference](families.md), [generic reference](generics.md) and
[generated surface](generated.md) give the syntax and APIs behind these
constraints. Generated language-role coverage, callable contract identity
and generic live conversion have their own boundaries; this data-language
proof does not establish those guarantees. Version coordination and the
current clean-break release policy are described in
[COLLABORATION.md](../../COLLABORATION.md#releases) and
[RELEASING.md](../../RELEASING.md), rather than a permanent compatibility
policy inferred from these fixtures.
