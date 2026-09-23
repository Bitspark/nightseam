# Interpreting the model in Nightseam

The theory distinguishes a contract `C = (S, B)`, a native realization `T`, an
instance `i`, and its actual behavior `b`. An interpretation chooses the behavior
domain and observations, how declarations describe `S` and any `B`, and how
native and Wire values realize them. This page connects those objects to the
representation calculus and names existing evidence. It does not introduce a
generator API or claim a production implementation of the complete theory.

Read this alongside the [foundations](foundations.md) and the
[concepts and evidence map](cross-references.md).

## Choose the subject before choosing its coordinates

`S` describes a declared interface and its parts: operations, arguments, result
types, and their relationships. `B : Behavior[S] -> Prop` states the admitted
behavior. A native type `T` realizes the structure; a value `i : Instance[T]`
implements the full contract exactly when `B(BehaviorOf_T(i))` holds. A method
signature does not establish that predicate for every value implementing it.

Two lawful BooleanCells can disagree on an initial read when the contract leaves
initialization unspecified. A write-ignoring cell has compatible method signatures
and violates the storing-cell specification. The
[finite behavior example](examples/behavior.ts) exhibits both distinctions and
two possible Go realizations. It models their semantics in TypeScript; it neither
runs Go nor claims these illustrative declarations are production Nightseam syntax.

The finite `Shape<O, G>` trees in [model.ts](model.ts) give one abstract basis for
hierarchy and substitution. Nightseam's
[declaration model](../../internal/model/family.go) and
[canonical declaration graph](../declaration/declaration-identity.md) carry
additional structure, including named references. An interpretation must
explain which information is an opaque local value, which becomes children,
and how references and generic contexts are represented. The examples do not
claim to encode the entire production declaration graph as a finite tree.

The [domain graph](../domain.md) distinguishes ModelContract, ModelType, and
ModelInstance. Here they correspond to `C`, `T`, and `i`, with satisfaction as
an explicit relation. The Wire side likewise needs a realization and an
interpretation of its actual interaction behavior. Runtime participants, carriers,
and events are not automatically representations of one interface contract.
A model type is an object; a coordinate cell can contain several artifacts
representing an object. These are different roles in the model.

## Choose an interpretation of the axes

The following assignments illustrate cells; their labels are not built into
the general model.

| Role           | Form                | Language                         | What an artifact can represent                                                                                 |
| -------------- | ------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Contract       | Syntax              | Declaration language             | A declaration denoting `C`, if its interpretation includes `B`; otherwise a shape `S`.                         |
| Contract       | Semantics           | Empty, when language-independent | The pair `(S, B)` in its selected behavior domain.                                                             |
| Model type     | Syntax              | Go or TypeScript                 | A declaration denoting `T`, also viewable as a representation of `S`.                                          |
| Model type     | Semantics           | Go or TypeScript                 | The native carrier with its correspondence to the abstract shape and behavior domain.                          |
| Adapter        | Syntax or semantics | A target language                | Code denoting, or a loaded function implementing, an adapter for the particular `T`.                           |
| Implementation | Syntax or semantics | A target language/runtime        | Source denoting an instance or factory, or an actual native instance `i`; satisfaction is a separate relation. |
| Interaction    | Runtime syntax      | A native or Wire surface         | An operation/request and its response, interpreted as effects against an instance.                             |

A transformation changing form alone can be an elementary edge when it preserves
the same subject. Rows in this table do not automatically share a subject. A
contract-indexed generation result can retain `C` alongside syntax denoting `T`;
a bare native interface generally retains only structural information. A generator
changing both language and role can be an unrestricted representation map when
that common interpretation is supplied, or a path through suitable intermediate
cells. Optional ranked axes
make absent context explicit; they do not manufacture a missing intermediate
representation or guarantee reachability.

## Generators are semantic functions, and can also be represented

A loaded Go generator implementation is an executable semantic function. Its
input and output can both be syntax. The language used to implement the
generator is separate from the languages of the artifacts it consumes and
produces.

The [semantic signatures](foundations.md#5-adapters-generators-and-behavioral-preservation)
expose the selected realization. Using the result packages defined there:

```text
TypeGen[D, L] : Π admitted C. Syntax[C, D] -> TypeResult[L, C]

AdapterGen[D, L, U_(-)] : Π admitted C. Π supported T : NativeRealization[L, shape(C)].
  (Syntax[C, D] × Syntax[T, L]) -> AdapterResult[L, C, T, U_C]
```

`U_C` is the target chosen for each `C`, such as an interpreted Wire surface,
in that contract's shape/behavior domain. The complete generated result retains
`C` and the selected `T`. Adapter syntax uses `T`'s language `L`; the admitted
domain states which contracts and realizations the generator supports.
An environment `E` can fix naming, binding, target, or strategy.
After fixing those choices and supplying the common-subject interpretation,
one can obtain `F_E,C : R_K(C) -> R_M(C)`. The unary map is a view of this richer
generation operation, not a reason to erase its dependencies.

`C` alone does not select a native realization. A generator emitting methods
does not establish satisfaction by arbitrary consumers. An implementation
generator must have enough specification or supplied behavior to construct a
lawful instance; a scaffold of unimplemented methods satisfies a weaker claim.

There are distinct objects at the generator level too: its shape/signature
`S_g`, behavioral specification `B_g`, and actual semantic function `g`.
The function implements `(S_g, B_g)` when its behavior satisfies the preservation
laws for every admitted input. `GeneratorSyntax<G, H>` describes syntax denoting
a function `g : G` in host language `H`. A generator loaded in Go can consume declaration syntax and
produce TypeScript syntax. Hosting, input language, and output language are
independent choices; the acted-upon contract `C` is not the generator's contract.

An ordinary program function is also a semantic function, but it belongs to a
fixed-subject transformation graph only when it preserves that subject and
satisfies the coordinate and observation laws. Function composition alone
does not imply preservation of `C`.

## What adapter transparency means

Let a selected binding map `i : Instance[T]` to a Wire realization `bind(i)`.
The behavior domain and environment must make the two sides comparable:

```text
BehaviorOf_Wire(bind(i)) ≈_S BehaviorOf_T(i)
BehaviorOf_T(stub(bind(i))) ≈_S BehaviorOf_T(i)
```

These are claims about that instance's behavior. The weaker implication
`i ⊨ C => bind(i) ⊨ C` permits replacing a true cell with a fresh false cell when
both initial values satisfy `C`. The finite example's forwarding adapter preserves
behavior; its replacing adapter preserves satisfaction and fails transparency.
This is observational equivalence, not equality of native instances. The
[fixed-subject interpretation](foundations.md#15-interpreting-generator-roles)
uses the common behavioral equivalence class when relating different realizations.

A production interpretation must account for the profile's observable failures,
lifecycle, ownership and concurrency, or state the environmental assumptions
under which they are abstracted away. The finite example deliberately has only
sequential, total read/write interactions. It supplies no evidence about transport
or invocation semantics. A Wire surface is an interaction language; a particular
request's interpretation acts on a particular instance.

Syntax held in a generator process is also a native host value, typically an AST
instance. Its host-level structure and its object-language denotation are two
views connected by an interpretation. Neither the runtime location of the AST
nor a `syntax` coordinate changes this relationship.

## What the current pipeline establishes

The [pipeline](../declaration/pipeline.md) loads and analyzes declarations,
checks them, constructs a render view, and invokes targets. The code exposes
these concrete points of comparison:

| Theory concept                    | Current code and evidence                                                                                                                                                         | Limit of the comparison                                                                                                                                                           |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Selected generator implementation | [Composition root](../../internal/compose/compose.go) and the [Target interface](../../internal/spi/spi.go).                                                                      | The SPI declares consumed concerns and output ownership and supplies `Check` and `Render`; it does not expose the complete coordinate signatures or law witnesses in this theory. |
| Syntax output for a contract      | `Target.Render` takes a render family and returns source files.                                                                                                                   | One target invocation may produce several roles and files; its full output is not automatically one primitive coordinate edge.                                                    |
| Implementation scaffolding        | `Scaffolder.Scaffold` in the same SPI.                                                                                                                                            | It emits stubs for the consumer to fill. It does not synthesize arbitrary contract behavior.                                                                                      |
| Generic instantiation             | [Generic declarations and applications](../declaration/generics.md).                                                                                                              | Parameters have type and family sorts, associated-type requirements, and declaration contexts. These restrictions must be supplied by an interpretation of opaque holes.          |
| Generate/instantiate interchange  | [TestGenericCompositionIndependentRoutes](../../cmd/nightseam/generic_composition_acceptance_test.go) and the [independent substitution oracle](../../internal/oracle/oracle.go). | These compare concrete routes through generated Go and TypeScript programs. They are evidence for those cases, not a universal proof for every coordinate or contract.            |
| Contract identity across targets  | [Canonical declaration identity](../declaration/declaration-identity.md).                                                                                                         | A digest identifies the specified declaration content. Equal digests alone do not prove artifact faithfulness, behavioral equivalence, or interoperability.                       |

The generic composition test renders a generic consumer and independently
specialized source routes, then compiles and exercises generated Go and
TypeScript programs. The oracle substitutes the model before rendering; the
generator does not depend on that oracle. This is a concrete instance of the
two-route question expressed by substitution transparency.

The abstract substitution model allows an unused ID in the permitted hole
context. Nightseam refuses unused declared parameters. Its type parameters
and family parameters also accept different sorts of arguments. Adapting the
general laws to declarations therefore requires an explicitly restricted
domain; treating every opaque hole as interchangeable would erase meaning.

The executable tree navigation and substitution laws concern shapes. To extend
them to `C = (S, B)`, define how laws restrict to a selected part, how contextual
laws are retained, and how substitution constructs a new specification `B_σ`.
A law linking `read` and `write` cannot be recovered from an isolated `read`
method signature. Composing implementations additionally needs a constructor
that preserves satisfaction, and behavioral equivalence for routes claiming to
preserve particular instances. Tree substitution or equal declaration digests
do not supply these arguments. The
[behavioral lifting obligations](foundations.md#lifting-structural-operations-to-contracts-and-implementations)
state what must be supplied beyond the structural laws.

## Where the remaining decisions belong

The general theory records what preservation and composition mean. A concrete
coordinate schema, decoding/observation rules, admissible generator inputs,
and evidence for each law would make a Nightseam interpretation precise.
Those choices must account for both navigation and substitution; naming
endpoint cells is only the signature of a transformation, not its proof.

The existing questions remain in
[type-model extraction #636](https://github.com/Bitspark/nightseam/issues/636),
[generator composition and SPI #637](https://github.com/Bitspark/nightseam/issues/637),
and [domain-graph choices #640](https://github.com/Bitspark/nightseam/issues/640).
The [language tiers](../languages/tiers.md) continue to define what each
implementation promises; this interpretation adds no new runtime promise.
