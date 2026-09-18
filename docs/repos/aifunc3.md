# aifunc3 — aifunc-mono

| | |
|---|---|
| Local checkout | `C:\Users\julia\Development\aifunc3` |
| Remote | https://github.com/Bitspark/aifunc-mono (`origin`) |
| Branch | `main` |
| Last commit seen | `e6edab7f9` · 2026-04-13 |
| Agent contract | `AGENTS.md` (mirrored by `CLAUDE.md`) |

Back to the [index](../RELATED_REPOS.md). Its type models, generator and
protocol, compared to Nightseam: [aifunc3-models.md](aifunc3-models.md).

## What it is

The third generation of the aifunc monorepo: a large, agent-driven workspace
that holds the aifunc runtime, its research system, and a family of
code-generation and typing tools. Earlier generations are `aifunc`
(`gitlab.bitspark.com/bitspark/aifunc-mono`, also pushed to
`github.com/Bitspark/aifuncref`) and `aifunc2`; this checkout is the live
one.

## Layout

The repository is organised as *units* — small, independently buildable
packages sorted by role — with *systems* composing them:

```
units/            api, app, data, docs, impl, lib, meta, packs, proto, scripts, stack, svc, test
systems/          api, app, bind, comp, lib, proto, tools
metatype/         the type model the units are described in
rune-codegen/     code generation from that model (RUNE_CODEGEN*.md track its status)
rune-tester/      end-to-end harness for generated code (RUNE_E2E_*.md)
phantom/          a server + UI pair; run logs land in .meta/run/logs/
delveorb/         code-graph tooling (see also code-graphs/)
sdks/             client SDKs
research/         the research system: JSON state under research/state/NNNN/, rendered to research/docs/
docs/, units-docs/, research-docs/, flow-docs/
```

Top-level `*.md` files are working documents from the agent sessions that
built the repo — `RUNE_*.md`, `RESEARCH*.md`, `GLYPH_*.md`, `SYMMETRY.md`,
`FORGE_KERNEL.md` — rather than a curated docs set.

## Working conventions

`AGENTS.md` sets the rules for agents in this repo and they differ from
Nightseam's:

- Work is decomposed into parallel waves of agents ("single instruction,
  multiple agents"); the plan → contracts → waves → validate loop.
- `research/docs/` and `research/state/` are never edited directly; every
  mutation goes through the `research` CLI, which owns the JSON state and
  renders the markdown views.
- Agents do not touch git at all in this repo — no commits, no status checks.
- No stubs, no partial work, no "remaining steps" summaries.

## Relation to Nightseam

aifunc3 is where the Glyph / Rune / Mark stack was built — a declaration
DSL with a full type algebra (`units/data/glyph`), a nine-stage generator
with per-language backends for TypeScript and Rust (`units/lib/rune`), and
a value model whose `Arrow` and `Location` sorts carry callbacks and
handles over a symmetric `{c, h, p}` session protocol (`units/lib/mark`).
The elevation model derives an operation's topology from its signature;
`rune-tester` and `units/test/rune/protocol-interop` test the generator
end to end and across languages and transports. It is the nearest sibling
to Nightseam's generator and the richest source of ideas for it; the
stack was later ported into bitmachine's kernel. All of it, with paths,
in [aifunc3-models.md](aifunc3-models.md).
