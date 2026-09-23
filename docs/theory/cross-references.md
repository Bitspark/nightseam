# Concepts, laws, and evidence

Return to the [theory index](README.md) for the reading order and commands.

The definitions live in the foundations; the following map connects them to
the names in [model.ts](model.ts) and the checks that exercise them. Source
comments link back to these definitions. Each example states its admitted
domain; passing it does not prove a universal law.

| Definition or law | Model types | Evidence |
| ----------------- | ----------- | -------- |
| [Shape, specification, and behavior; B1](foundations.md#2-shape-specification-and-actual-behavior) | `BehaviorDomain`, `BehaviorSpecification`, `Contract` | [Finite cell behaviors and their specification](examples/behavior.ts) |
| [Realizations, instances, and satisfaction; I1](foundations.md#3-native-realizations-instances-and-satisfaction) | `ModelType`, `ModelInstance`, `Satisfaction`, `ContractImplementation` | [Two realizations, unlawful values, mismatched witness](examples/behavior.ts); [negative type assertions](model.typecheck.ts) |
| [Syntax and denotation](foundations.md#4-syntax-denotation-and-values-inside-an-interpreter) | `Syntax`, `ModelContractSyntax`, `ModelTypeSyntax`, `ImplementationSyntax`, `AdapterSyntax`, `GeneratorSyntax` | [Declared syntax interpretations and contextual host expression](examples/behavior.ts) |
| [Adapters and generators; A1–A2](foundations.md#5-adapters-generators-and-behavioral-preservation) | `AdapterSemantics`, `TypeGeneration`, `AdapterGeneration`, `TypeGeneratorSemantics`, `AdapterGeneratorSemantics` | [Forwarding/replacement and generator applications](examples/behavior.ts); [selected realization and language assertions](model.typecheck.ts) |
| [Coordinates and equality; E1](foundations.md#6-coordinates-and-cells), [transformations and paths; E2, P1](foundations.md#7-elementary-transformations-and-coordinate-paths) | `Coordinates`, `Representation`, `ChangedAxis`, `Transformation`, `TransformationSignature`, `TransformationInstance`, `Path` | [Coordinate examples](examples/coordinates.ts); [literal-coordinate and path assertions](model.typecheck.ts) |
| [Shape hierarchy](foundations.md#8-giving-the-shape-a-hierarchy), [navigation; N1–N2](foundations.md#9-shape-navigation), [represented navigation; R1–R3](foundations.md#10-navigating-a-representation) | `Shape`, `ShapeNavigation`, `ShapeLocation`, `RepresentationNavigation`, `RepresentationObservation` | [Every surface and path, missing paths, forged witness](examples/hierarchy.ts) |
| [Families and transparency; T1–T2, Q1](foundations.md#11-transformation-families-and-the-transparency-square), [composition; T3, P2](foundations.md#12-the-prefix-law-and-arbitrary-coordinate-paths), [coherence; P3](foundations.md#13-transparency-does-not-force-a-unique-coordinate-path) | `ShapeRepresentationMap`, `TransformationFamily`, `FamilyPath`, `NavigationTransparency`, `RepresentationEquivalence` | [Two routes, all path splits, and independent counterexamples](examples/hierarchy.ts) |
| [Holes](foundations.md#16-whole-shape-holes), [substitution; S1–S4](foundations.md#17-substitution-and-its-composition-laws), [navigation; S5–S6](foundations.md#18-navigation-through-composed-shapes), [represented substitution; S7–S8](foundations.md#19-substitution-transparency-across-representations) | `Generic`, `Substitution`, `ShapeSubstitution`, `SubstitutionApplication`, `RepresentedSubstitution`, `RepresentationSubstitution`, `SubstitutionTransparency` | [Open/closed, repeated/root holes, staged plugging and its two routes](examples/composition.ts) |
| [Behavioral lifting](foundations.md#lifting-structural-operations-to-contracts-and-implementations) | Additional interpretation obligations; not a built-in operation on `Contract` | [Limits of the production comparison](nightseam.md#what-the-current-pipeline-establishes); no general constructor or proof is supplied |

The [Nightseam interpretation](nightseam.md) links the theory to current code,
tests, and unresolved design choices. The [domain graph](../domain.md) records
the corresponding domain vocabulary; its remaining choices are still proposals.
