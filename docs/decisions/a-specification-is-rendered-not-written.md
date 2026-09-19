# A specification is rendered, not written

**The question.** A family needs a document a human reads — its types, its
two sides, its errors, how a session of it is governed. Who writes it?

**Decided.** A third target, `spec`, renders it as Markdown at
`api/spec/<family>/README.md` from the declaration alone, beside the Go and
TypeScript targets, and it is held as a golden like any other output. It
reserves nothing and refuses nothing. [The
generator](../declaration/generator.md#the-specification).

**Why.** A document written by hand is behind the declaration the moment
the declaration moves, and nobody notices until a reader is misled; one
rendered from the declaration is never behind it, and `check` fails the
pull request that would have let it be. Making it a target rather than a
side effect of the tool is what proved that a target need not be a
language: the same pipeline, the same `render`, a different emitter — the
example every later target is measured against.

**Serves.** Declarative — what two documents would each hold a copy of is
stated once, and the copy nobody edits is the one that is right.

**Since.** 0.2.0.
