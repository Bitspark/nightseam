# The pipeline

How the generator renders, for whoever changes it: a consumer needs [the
generator](generator.md) and not this page. The generator is a pipeline of
small packages, each owning one level, and the package graph is held to
the seam between them by a test.

## How it renders

`load` reads a checkout's tier files into the typed `model`, holding each
file to its tier's shape schema; `analysis` gives a family its world —
imports resolved transitively, the types it carries from the built-in
family of each tier it has, inheritance flattened, the names derived for the
shapes it writes inline, and what is generic in it, computed once; `check` holds it to the model's
rules and each concern's; `render` presents it to the targets once, with
every fact they need and nothing target-specific; each target plans every
identifier it will declare — into the namespace it lands in, so that a
collision is a diagnostic and what is reserved is what is emitted — and
then emits, registering imports where it uses them; the `kernel` runs the
pipeline and refuses a rendered path outside the directories the target
owns. A family with any diagnostic is refused before a target renders.

Targets are composed in `internal/compose` and nowhere else; the seam
between them and the kernel is `internal/spi`, and a test holds the
package graph to that. The composition root is a package rather than the
command so that the conformance suite imports it too: what the suite
renders for a language's generated testee is what the tool renders, down
to each target's config, and a target added or defaulted differently
reaches the two together.

## The packages

```
cmd/nightseam/          the generator: generate, check, validate, init, version; the corpus and its goldens under testdata
internal/model/         the typed declaration of a family: the tiers, the sealed type-expression AST, the decoders
internal/model/builtin/ the families Nightseam declares of itself: duplex and tunnel, as tier files
internal/load/          files to families: the tier table, the shape schemas, the world of a checkout
internal/analysis/      a family within its world: imports resolved, inheritance flattened, what is generic in it
internal/check/         the rules, one function per tier and one for the override files
internal/render/        a family as a target sees it, computed once
internal/spi/           the seam between the kernel and a target
internal/doc/           the language-neutral specification document and writer adapter
internal/targets/       golang and typescript emit packages; markdown and atlas render the document
internal/kernel/        load, analyse, check, render
internal/compose/       the composition root: the only place a target is named, imported by the tool and by the conformance suite
internal/emit/          a writer, an import set, a namespace: what every target writes with
internal/naming/        the convention every target derives names by
internal/pattern/       pattern syntax checks shared by the declaration checker and the Go runtime; imports no generator package
internal/diag/          where a problem is: family, tier file, pointer, code
internal/oracle/        test support: the left path of the diagram a generic rendering commutes with
internal/surface/       exported Go declarations: shared by the surface goldens and cmd/nightseam-surface
```

## What holds it

`go test -short ./...` — the fast tier — runs every target's `Check` and
the corpora under `cmd/nightseam/testdata`: the families under `corpus` and
`families`, what every target renders for them held file for file under
`golden` and `golden-families`, the exported surface of every generated Go
package under `surface`, what each target reserves under `reserved`, and
one checkout per rule the tool refuses under `invalid` with what `validate`
says. A renderer or a diagnostic changes as a diff of those files, which is
what a review reads; [COLLABORATION.md](../../COLLABORATION.md) has the
golden discipline. A new target is `internal/targets/<name>`, composed in
`internal/compose`, held by its own goldens — [onboarding a
language](../languages/onboarding.md) is the order.

The atlas's pure JavaScript builders run under `node --test` in the full
tier, including the real proof family's document. Its Go tests hold the
embedded JSON, escaping and config; the corpus goldens hold the whole HTML
page. The browser layer only applies those views and handles navigation.
The page is one `text/template` execution over the embedded template,
stylesheet and modules, so the template escapes nothing and every field
put through it escapes its own: the title through `html.EscapeString`, and
the document through `encoding/json`, which escapes every `<` — including
one inside a `RawMessage` — so that no declaration can close the script
element that carries it. A field added there answers the same question
before it is added.
