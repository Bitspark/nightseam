# bitmachine

| | |
|---|---|
| Local checkout | `C:\Development\bitspark\bitmachine` |
| Remote | https://gitlab.bitspark.com/bitmachine/bitmachine (`origin`) |
| Branch | `main` |
| Last commit seen | `f64b23c992` · 2026-05-17 · "Tighten Graphe runtime contract parity and browser stubs" |
| Agent contract | `AGENTS.md` (mirrored by `CLAUDE.md`) |

Back to the [index](../RELATED_REPOS.md).

## What it is

The Bitspark platform monorepo: a kernel of named subsystems, the apps built
on it, and BitDev, the tool that orchestrates the registered development
stacks in Docker. It is the parent of [plexis](plexis.md), which lives on a
branch of this same repository.

## Layout

```
kernel/           the platform kernel, one directory per subsystem:
                  archon, browser, chora, deixis, eidos, email, energeia, glyph, graphe,
                  leukos, logos, metis, noesis, nomos, ontos, opsis, sandbox, sema,
                  skopos, syntaxis, telos, tools
apps/             mimos, opsis-demo, plexis, pragma, pragma-experiments, research
bitdev/           BitDev: manifest-first checks, builds, tests, declaration drift, Docker templates
dev/              dev/bitdev.ps1 and other entry scripts
schemas/          shared schemas (including schemas/bitmachine)
components/, lib/ shared code
docs/             apis/, archon/, bitdev/, csi/, integration/, metis/, testing/ and platform notes
                  (PLATFORM_BOUNDARIES.md, LOCAL_RUNTIME_REQUIREMENTS.md, ...)
bitmachine2/, bitmachine3/, bitmachine3-e/   earlier generations kept in-tree
epics/, milestones/, issues/, handoffs/       planning and hand-over material
```

Top-level working documents: `CONVENTIONS.md`, `SYMMETRY.md`,
`irreducible-kernel-and-gateways.md`, `declarative-machine-semantics.md`,
`RUNTIME-DEPENDENCIES.md`, `heraldry.md`.

## Working conventions

From `AGENTS.md`:

- **No legacy compatibility code.** When an internal surface is superseded,
  update every caller and remove the old surface in the same change. The only
  permitted adapters are boundary adapters to external systems (host
  bindings, file formats, browser APIs, GPU backends, third-party services),
  named as such.
- **BitDev owns Docker.** The registered stacks — Plexis, Mimos, Research,
  Leukos — are run through `dev/bitdev.ps1 <verb> <stack>` or
  `pnpm --dir bitdev bitdev <verb> <stack>`, never raw `docker compose`.
  `docs/bitdev/REPO_CAPABILITIES.md` describes the manifest-first checks.

## Relation to Nightseam

The kernel subsystems talk to their apps and to each other over duplex seams
of exactly the kind Nightseam describes — requests, events and cancellation
over a WebSocket. The Graphe runtime contract work on `main` (browser stubs
in parity with the runtime) is the closest analogue to Nightseam's Go /
TypeScript pairing, and the kernel's per-subsystem contract files are the
natural place for `urn:nightseam:contract:1` families should bitmachine
adopt the generator.
