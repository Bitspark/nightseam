# Concepts, laws, and evidence

This map connects the [foundations](foundations.md), [type model](model.ts),
and executable examples. The [Nightseam interpretation](nightseam.md) identifies
production evidence and the limits of that mapping. The [theory index](README.md)
contains the reading order and verification commands.

The tables cover every exported model type. Mathematical indices name individual
objects; a TypeScript type usually describes a set of possible objects. Keeping
the same type parameter does not prove runtime identity, denotation, or a law.

## Objects and denotation

| Concepts and laws | Types in model.ts | Executable evidence |
| --- | --- | --- |
| [Contract, behavior domain, and B1](foundations.md#2-shape-specification-and-actual-behavior) | `Proposition`, `BehaviorDomain`, `BehaviorSpecification`, `Contract` | [Behavior example](examples/behavior.ts): `cellDomain`, `cellContract`, and `satisfiesCell`; the predicate admits the two reference behaviors' equivalence classes. |
| [Selected native realization](foundations.md#3-native-realizations-instances-and-satisfaction) | `ModelType`, `ModelTypeIndex`, `NativeValue`, `ActualBehavior`, `ShapeOf`, `ContractFor` | [Behavior example](examples/behavior.ts): `methodType` and `functionType` realize the same shape with different native carriers. |
| [Instance, satisfaction, and I1](foundations.md#3-native-realizations-instances-and-satisfaction) | `ModelInstance`, `Satisfaction`, `ContractImplementation` | [Behavior example](examples/behavior.ts): `checkImplementation` checks the actual instance against its witness and rejects a mismatched witness. |
| [Adapter laws A1 and A2](foundations.md#5-adapters-generators-and-behavioral-preservation) | `AdapterSemantics` | [Behavior example](examples/behavior.ts): forwarding preserves behavior; replacement preserves satisfaction while changing the first read. |
| [Syntax and denotation](foundations.md#4-syntax-denotation-and-values-inside-an-interpreter) | `Syntax`, `ModelContractSyntax`, `ModelTypeSyntax`, `ImplementationSyntax`, `AdapterSyntax`, `GeneratorSyntax` | [Behavior example](examples/behavior.ts): stipulated declaration/native-type denotations, an adapter identifier resolved in an explicit environment, and the actual JavaScript generator reloaded in its host environment; [type assertions](model.typecheck.ts) reject mixing contract and type syntax. No Go parser or compiler is supplied. |
| [Dependent generator inputs and results](foundations.md#5-adapters-generators-and-behavioral-preservation) | `TypeGeneration`, `TypeGeneratorSemantics`, `AdapterGeneration`, `AdapterGeneratorSemantics` | [Behavior example](examples/behavior.ts): `generateType` and `generateAdapter` retain the contract and selected realization; [type assertions](model.typecheck.ts) reject the wrong realization or adapter output language. |

Mathematical `Syntax[X, L]` and TypeScript `Syntax<X, L>` use the same parameter
order. The mathematical index names a particular subject; a TypeScript type
describes a family of values. `ImplementationSyntax<T>` ranges over
`ModelInstance<T>`, with each artifact denoting its recorded initialized
instance. Use `Syntax<typeof i, L>` when retaining that instance's type matters;
even that type cannot prove exact runtime identity. A factory has a different
subject and needs an explicit initialization operation. `ModelContractSyntax<S, Beh, D>`
ranges over contracts in a domain; `Syntax<typeof C, D>` retains the selected
contract's type. These are families of syntax, not proofs of denotation.

## Coordinates and paths

| Concepts and laws | Types in model.ts | Executable evidence |
| --- | --- | --- |
| [Coordinate schema and E1](foundations.md#6-coordinates-and-cells) | `AxisId`, `AxisElementId`, `EmptyCoordinate`, `CoordinateValue`, `Coordinates`, `AxisElementEquality`, `CoordinateEquality`, `CoordinatesEqual` | [Coordinate example](examples/coordinates.ts): equality of key sets and axis values, including missing versus explicit empty slots. |
| [Exactly one changed axis, E2](foundations.md#7-elementary-transformations-and-coordinate-paths) | `DifferingAxes`, `ChangedAxis`, `Transformation`, `TransformationSignature`, `TransformationInstance` | [Type assertions](model.typecheck.ts): zero/multiple changes and non-singleton coordinates are refused; [coordinate example](examples/coordinates.ts): two functions share a signature. |
| [Fixed subject and P1](foundations.md#7-elementary-transformations-and-coordinate-paths) | `Representation`, `RepresentationMap`, `Stops`, `PathSteps`, `Path`, `PathMeaning` | [Coordinate example](examples/coordinates.ts): identity and two composed routes; [type assertions](model.typecheck.ts): mismatched steps and subject types are refused. |

The coordinate and behavior examples use two explicit schemas: ranked keys
such as `0:form` in one, and the minimal `{ form, language }` of `Syntax` in the
other. The axes are opaque, so these schemas are not interchangeable or
implicitly equal. A schema translation and a common subject would be needed
to connect their representation graphs.

## Shapes and composition

| Concepts and laws | Types in model.ts | Executable evidence |
| --- | --- | --- |
| [Shape trees](foundations.md#8-giving-the-shape-a-hierarchy) and [whole-shape holes](foundations.md#16-whole-shape-holes) | `ShapeKey`, `ShapePath`, `GenericId`, `Generic`, `ShapeNode`, `Shape` | [Hierarchy example](examples/hierarchy.ts): nine-node BooleanCell and an empty shape; [composition example](examples/composition.ts): repeated, root, and distinct holes. |
| [Partial navigation, N1 and N2](foundations.md#9-shape-navigation) | `Selection`, `ShapeNavigation`, `ShapeLocation` | [Hierarchy example](examples/hierarchy.ts): `shapeAt`, `locate`, valid splits and missing prefixes. |
| [Represented navigation and faithfulness, R1–R3](foundations.md#10-navigating-a-representation) | `RepresentationNavigation`, `ShapeNodeSurface`, `ShapeSurface`, `RepresentationObservation` | [Hierarchy example](examples/hierarchy.ts): local surfaces and path splits; dropping descendants or replacing values is rejected. |
| [Preservation and transparency, T1–T2 and Q1](foundations.md#11-transformation-families-and-the-transparency-square) | `RepresentationEquivalence`, `ShapeRepresentationMap`, `TransformationFamily`, `NavigationTransparency` | [Hierarchy example](examples/hierarchy.ts): `checkTransparency` checks all four primitive families. |
| [Prefix transparency T3 and composed paths P2](foundations.md#12-the-prefix-law-and-arbitrary-coordinate-paths); [optional coherence P3](foundations.md#13-transparency-does-not-force-a-unique-coordinate-path) | `FamilyPathSteps`, `FamilyPath`, `FamilyPathMeaning` | [Hierarchy example](examples/hierarchy.ts): `layoutFirst`, `orderFirst`, and every prefix split; equality of these two routes is specific evidence, not uniqueness of all paths. |
| [Simultaneous substitution, S1–S4](foundations.md#17-substitution-and-its-composition-laws) | `Substitution`, `ShapeSubstitution`, `SubstitutionApplication` | [Composition example](examples/composition.ts): `substitute`, `identity`, `compose`, and checked result witnesses. |
| [Navigation through replacements, S5–S6](foundations.md#18-navigation-through-composed-shapes) | `ShapeNavigation` | [Composition example](examples/composition.ts): selection at a hole and paths entering inserted shapes. |
| [Represented substitution and transparency, S7–S8](foundations.md#19-substitution-transparency-across-representations) | `RepresentedSubstitution`, `RepresentationSubstitution`, `SubstitutionTransparency` | [Composition example](examples/composition.ts): represented identity/composition and `checkSubstitutionTransparency` in nested and flat encodings. |

Whole-shape instantiation produces a shape `S[σ]`, not a native `ModelInstance<T>`.
The [behavioral lifting requirements](foundations.md#lifting-structural-operations-to-contracts-and-implementations)
explain what is still needed for whole-contract substitution and implementation
composition. The structural examples supply neither the resulting specification
`B_σ` nor an implementation constructor.

## Reading the evidence

The [limits of TypeScript checking](foundations.md#20-what-the-typescript-establishes)
apply to every row. The structural examples use opaque string values and concrete
finite trees. The behavioral example uses finite, pure, deterministic, total
state machines and an exact product exploration for that domain. Its 512-machine
enumeration checks forwarding and replacement for each two-state method machine;
it does not enumerate arbitrary programs or prove a production transport correct.

`Proposition` describes a statement. `Satisfaction` and the location/substitution
witnesses describe evidence obligations. Their records can be fabricated, so
TypeScript acceptance alone establishes none of those obligations.

## Documentation tooling

[render.mjs](render.mjs) generates [index.html](index.html) from the four Markdown
pages and the six primary TypeScript sources, with internal links to the included
pages and sources. References outside this document remain repository or external
links. [references.test.mjs](references.test.mjs) checks exported-type coverage,
source backlinks, and the rendered document's links and identifiers.
The [package scripts](package.json) run it with the examples; [tsconfig.json](tsconfig.json)
checks the model, compile-only assertions, and example types. Regenerate the HTML
after editing any included page or source.
