# A specification has a document, and renderers of it

**The question.** [A specification is rendered, not
written](a-specification-is-rendered-not-written.md): one Markdown page per
family, built by the `spec` target from the declaration alone. A reader
wants two more depths — what a type *is on the wire*, and what it *is in
their language* — and a second rendering, an interactive atlas of the whole
checkout. Who knows a language's spelling? Where does the document live?
How does a rendering become two, and how is one configured?

**Decided.** Five things, together.

1. **One document model**, `internal/doc`, built once per checkout from the
   declaration: every type with its fields, an example value of it, its
   place in every operation; every operation with its frames as the profile
   spells them; the errors, the governance, the weight of each exchange in
   fields; and, where a language target answers, that language's names and
   code. The document is the specification; a page is a rendering of it.
2. **Renderers implement `doc.Writer`** — a name, and files from the
   document — and an adapter makes each an `spi.Target`, composed in
   `internal/compose` like any target, so that the kernel's ownership,
   staleness, leftover removal and goldens hold a renderer without the
   renderer knowing them. Markdown is the first writer, the atlas the
   second; an HTML renderer can be many, each a package with a name and a
   layout of its own.
3. **A language target is the only source of its spelling**, through
   `spi.Speller`: how it spells a type expression, declares a type, and
   invokes an operation, answered by the emitter that writes the package.
   The document asks every target that answers and names no language.
4. **Renderer config is an opaque payload**, keyed by target name under
   `targets` in the checkout's `api/contracts/nightseam.json`, handed to the
   target raw and validated by it — the rule of the per-family override file
   lifted to the checkout. `disabled` is the one key the kernel reads.
5. **The checkout is a unit a renderer may render**, family `""`, the name
   the kernel already gives the checkout's own diagnostics. A writer declares
   whether its files are a family's or the checkout's.

**Why.** Each alternative was cheaper to start and dearer to keep. A
renderer with its own table of how Go and TypeScript spell things is a
second copy of every target's knowledge, and the copy nobody generates is
the one that drifts; reading the generated files back is blind to what the
target meant. A second page as a second target building its own document is
two documents diverging, which is the disease the first decision cured. A
flag per renderer per token puts the kernel in charge of what is not its,
and gives a consumer of the tool — who has `--module` and `--scope` and
nothing else — no way to shape a rendering short of editing the composition.
An HTML page per family has no families to switch between and no view across
them, which is what the atlas is for. And a document that named its
languages would date the day a third one joined, as the tool's own help did.

**Serves.** [Declarative](../goals/declarative.md) — what a family means is
stated once and every rendering reads it; [extensibility](../goals/extensibility.md)
— a renderer joins at `doc.Writer` and a language's spelling at
`spi.Speller`, by adding, with nothing that exists moving;
[configurability](../goals/configurability.md) — a renderer is shaped by
its own payload and the kernel reads one key of it;
[agnosticism](../goals/agnosticism.md) — the wire is the depth every
language agrees on, and the languages are a lens over it.

**Since.** #154, #155, #156, #157, decided 2026-09-19.
