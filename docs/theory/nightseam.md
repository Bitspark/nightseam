# Interpreting the model in Nightseam

The theory describes a contract independently of its representations. An
interpretation chooses the contract's observable content, the coordinate
schema, the admitted artifacts, and the equivalence used to compare results.
This page gives a candidate interpretation and identifies where existing code
provides evidence. It does not introduce a generator API.

## Choose the subject before choosing its coordinates

For a shape-only interpretation, `C` can describe a declared interface and its
parts: operations, arguments, result types, and their relationships. A richer
interpretation includes behavioral laws. Every admitted transformation must
preserve the chosen content. A type declaration listing method signatures
does not, by itself, establish behavioral preservation.

The finite contract trees in [model.ts](model.ts) give one abstract basis for
hierarchy and substitution. Nightseam's
[declaration model](../../internal/model/family.go) and
[canonical declaration graph](../declaration/declaration-identity.md) carry
additional structure, including named references. An interpretation must
explain which information is an opaque local value, which becomes children,
and how references and generic contexts are represented. The examples do not
claim to encode the entire production declaration graph as a finite tree.

The [domain graph](../domain.md) also includes instances, participants,
carriers, and events. Those are not automatically representations of one
fixed interface contract. Their relationship to this theory requires a chosen
subject and observation rule. Likewise, a domain node for a model type and a
coordinate cell need not be the same object: a cell can contain several
artifacts representing the same `C`.

## Choose an interpretation of the axes

The following assignments illustrate cells; their labels are not built into
the general model.

| Role           | Form                | Language                         | What an artifact can represent                                      |
| -------------- | ------------------- | -------------------------------- | ------------------------------------------------------------------- |
| Contract       | Syntax              | Declaration language             | A declaration denoting `C`.                                         |
| Contract       | Semantics           | Empty, when language-independent | The interpreted contract.                                           |
| Model type     | Syntax              | Go or TypeScript                 | A native type declaration presenting `C`.                           |
| Model type     | Semantics           | Go or TypeScript                 | The native type's interpreted interface.                            |
| Adapter        | Syntax              | A target language                | Code exposing `C` through a selected binding.                       |
| Implementation | Syntax or semantics | A target language/runtime        | A program realizing the chosen shape and, where included, behavior. |

A transformation changing form alone can be an elementary edge. A generator
changing both language and role is an unrestricted representation map until
a path through suitable intermediate cells is supplied. Optional ranked axes
make absent context explicit; they do not manufacture a missing intermediate
representation or guarantee reachability.

## Generators are semantic functions, and can also be represented

A loaded Go generator implementation is an executable semantic function. Its
input and output can both be syntax. The language used to implement the
generator is separate from the languages of the artifacts it consumes and
produces.

For a selected environment `E`, write a generator family as:

```text
F_E,C : R_K(C) -> R_L(C)
```

The environment may include the selected native type, naming decisions, an
adapter binding, or an implementation strategy. Currying those choices gives
the unary representation map used by the core. An adapter generator taking
both a contract declaration and a chosen type must keep that choice explicit;
`C` alone does not select a native realization. An implementation generator
must have enough specification or supplied behavior to construct a lawful
implementation. A scaffold of unimplemented methods satisfies a weaker claim.

There are two distinct subjects here. Applying `F` transforms a representation
of `C`. Representing the generator itself uses `Representation<G, K>`, where
`G` is the generator's semantic function, including its signature and laws.
Generator source and a loaded generator can be representations of that `G`.
The same general model can describe this level without identifying `G` with
the contracts on which it acts.

An ordinary program function is also a semantic function, but it belongs to a
fixed-subject transformation graph only when it preserves that subject and
satisfies the coordinate and observation laws. Function composition alone
does not imply preservation of `C`.

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
