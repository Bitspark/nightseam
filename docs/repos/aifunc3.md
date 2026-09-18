# aifunc3 — aifunc-mono

| | |
|---|---|
| Local checkout | `C:\Users\julia\Development\aifunc3` |
| Remote | https://github.com/Bitspark/aifunc-mono (`origin`) |
| Branch | `main` |
| Last commit seen | `e6edab7f9` · 2026-04-13 |
| Agent contract | `AGENTS.md` (mirrored by `CLAUDE.md`) |

Back to the [index](../RELATED_REPOS.md).

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

aifunc3 is where the vocabulary of units, protos and binds that Nightseam's
contract layers echo was first worked out (`units/proto`, `systems/bind`,
`metatype`). It is a candidate consumer of the `nightseam.duplex/1` profile
for its server ↔ UI seams (phantom, the research system), and its
`rune-codegen` pipeline is the nearest sibling to Nightseam's generator.
