# Contracts and representations

This is the home of the theory behind representing, transforming, navigating,
and composing contracts. The central question is what must stay the same when
one contract is expressed in another language, syntax, semantic realization,
or generated role. A graph makes the available transformations visible; paths
describe their compositions, and commuting diagrams state their laws.

The theory belongs to Nightseam's language-independent foundation. Its subjects,
keys, axes, and local values are supplied by an interpretation; it introduces
no consumer policy or new runtime primitive. TypeScript is used as a
**metalanguage** to make the definitions inspectable and their examples
checkable. This directory is a private documentation workspace, with no
published API.

## Read the theory

| Page                                     | Purpose                                                                                                                                                                      |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Foundations](foundations.md)            | The general model: opaque coordinates, representations, primitive transformations, paths, hierarchical contracts, whole-contract holes, substitution, and preservation laws. |
| [Nightseam interpretation](nightseam.md) | How to apply that vocabulary to declarations, generators, native types, adapters, and generic composition; links to implementation evidence and unresolved choices.          |

The [self-contained visual edition](index.html) renders the foundations with
interactive commuting squares, substitution stages, and embedded model sources.
Open it directly in a browser; it needs no server or network connection.

## Inspect and run the model

| File                                               | Purpose                                                                                                                             |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| [model.ts](model.ts)                               | Types and interfaces only; no predefined languages or generator roles.                                                              |
| [examples/coordinates.ts](examples/coordinates.ts) | A shape-only contract at language/form/role coordinates, named transformations, two routes, and an identity path.                   |
| [examples/hierarchy.ts](examples/hierarchy.ts)     | Nested and flat encodings, navigation, surface faithfulness, transparency, and counterexamples.                                     |
| [examples/composition.ts](examples/composition.ts) | Whole-contract substitution, repeated and root holes, staged composition, and navigation and transformation through inserted trees. |
| [model.typecheck.ts](model.typecheck.ts)           | Compile-only positive and negative assertions for the model's type constraints.                                                     |

From the repository root, using Node 22.12 or later:

```sh
pnpm install
pnpm --filter @nightseam/theory verify
pnpm --filter @nightseam/theory render
```

`verify` checks types, runs the examples, and refuses a stale visual edition.
`render` regenerates that edition after changing the foundations or sources.
The [renderer](render.mjs) derives the diagrams' examples from the checked
artifacts and embeds all four source files. Commit its output with its inputs.
The workspace's ordinary recursive checks, build, and tests include this
directory; the fast CI tier also runs its verification on both platforms.

## What each statement means

- **Definitions and required laws** state which objects and operations the
  theory admits. Opaque IDs and TypeScript signatures do not prove them.
- **Derived laws** explain consequences of those requirements, such as
  transparency of a composed coordinate path.
- **Executable examples** establish the stated equations for particular
  encodings and fixtures. Counterexamples distinguish independent requirements.
- **Implementation evidence** names existing code and tests. A proposed
  interpretation is not a claim that Nightseam already implements the entire
  theory or exposes these TypeScript types.

The documentation home was accepted in
[#635](https://github.com/Bitspark/nightseam/issues/635#issuecomment-5787314807)
and integrated by [#673](https://github.com/Bitspark/nightseam/issues/673).
Package extraction, a new generator SPI, and the outstanding domain-graph
choices remain their own design questions. This directory can develop the
theory without silently settling them.
