# Contracts, implementations, and representations

This is the home of the theory behind contracts, their implementations, and
their representations. A contract pairs shape `S` with a behavioral specification
`B`; an instance has actual behavior `b` that may satisfy `B`. Representing a
contract and satisfying it are separate relations. Adapters preserving a
particular instance's behavior make a stronger promise than producing some
lawful implementation.

The representation model describes what stays fixed across languages and forms.
Paths compose transformations; structural navigation and substitution have their
own commuting laws. Lifting those laws to behavior requires explicit observation,
restriction, and composition rules.

The theory belongs to Nightseam's language-independent foundation. Its subjects,
keys, axes, and local values are supplied by an interpretation; it introduces
no consumer policy or new runtime primitive. TypeScript is used as a
**metalanguage** to make the definitions inspectable and their examples
checkable. This directory is a private documentation workspace, with no
published API.

## Read the theory

| Page                                     | Purpose                                                                                                                                                                            |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Foundations](foundations.md)            | Shape/specification/behavior, native realizations and satisfaction, syntax and denotation, adapters and generators, representation paths, structural navigation, and substitution. |
| [Nightseam interpretation](nightseam.md) | How to apply that vocabulary to declarations, generators, native types, adapters, and generic composition; links to implementation evidence and unresolved choices.                |

The [self-contained visual edition](index.html) renders the foundations with
an interactive BooleanCell, commuting squares, substitution stages, and embedded model sources.
Open it directly in a browser; it needs no server or network connection.

## Inspect and run the model

| File                                               | Purpose                                                                                                                                                                                         |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [model.ts](model.ts)                               | Semantic and representation types, including realizations, instances, satisfaction, and generator dependencies; no production API.                                                              |
| [examples/coordinates.ts](examples/coordinates.ts) | A shape at language/form/role coordinates, named transformations, two routes, and an identity path.                                                                                             |
| [examples/hierarchy.ts](examples/hierarchy.ts)     | Nested and flat encodings, navigation, surface faithfulness, transparency, and counterexamples.                                                                                                 |
| [examples/composition.ts](examples/composition.ts) | Whole-shape substitution, repeated and root holes, staged composition, and navigation and transformation through inserted trees.                                                                |
| [examples/behavior.ts](examples/behavior.ts)       | Two native realizations, lawful and unlawful cells, satisfaction witnesses, transparent and replacing adapters, and modeled generator applications; exhaustive checks over 512 finite machines. |
| [model.typecheck.ts](model.typecheck.ts)           | Compile-only positive and negative assertions for the model's type constraints.                                                                                                                 |
| [render.mjs](render.mjs) | Generates the HTML, diagrams, and embedded sources from the foundations and checked examples; preserves Markdown heading anchors. |
| [references.test.mjs](references.test.mjs) | Checks source-to-theory links, the file index, and links and anchors in the generated edition. |
| [package.json](package.json) and [tsconfig.json](tsconfig.json) | Private workspace commands and strict, no-emit type checking for the model and examples. |

From the repository root, using Node 22.12 or later:

```sh
pnpm install
pnpm --filter @nightseam/theory verify
pnpm --filter @nightseam/theory render
```

`verify` checks types, runs the examples and cross-reference checks, and refuses
a stale visual edition.
`render` regenerates that edition after changing the foundations or sources.
The [renderer](render.mjs) derives the diagrams' examples from the checked
artifacts and embeds the model, its compile-only assertions, and all four examples.
Commit its output with its inputs.
The workspace's ordinary recursive checks, build, and tests include this
directory; the fast CI tier also runs its verification on both platforms.

## Concepts, laws, and evidence

The [cross-reference map](cross-references.md) connects definitions and laws to
their model types, checked examples, and production evidence. Source comments
link back to the definitions.

## What each statement means

- **Definitions and required laws** state which objects and operations the
  theory admits. Opaque IDs and TypeScript signatures do not prove them.
- **Derived laws** explain consequences of those requirements, such as
  transparency of a composed coordinate path.
- **Executable examples** establish the stated equations for particular
  encodings and fixtures. Counterexamples distinguish independent requirements.
  The behavior example decides all finite interaction traces of its finite,
  deterministic, total machines; it does not infer behavior of arbitrary programs.
- **Implementation evidence** names existing code and tests. A proposed
  interpretation is not a claim that Nightseam already implements the entire
  theory or exposes these TypeScript types.

The documentation home was accepted in
[#635](https://github.com/Bitspark/nightseam/issues/635#issuecomment-5787314807)
and integrated by [#673](https://github.com/Bitspark/nightseam/issues/673).
The implementation/behavior extension follows the operator's
[verdict](https://github.com/Bitspark/nightseam/issues/635#issuecomment-5788001881)
and [#675](https://github.com/Bitspark/nightseam/issues/675).
Package extraction, a new generator SPI, and the outstanding domain-graph
choices remain their own design questions. This directory can develop the
theory without silently settling them.
