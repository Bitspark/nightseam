# plexis

| | |
|---|---|
| Local checkout | `C:\Development\bitspark\systems\plexis` |
| Remote | https://gitlab.bitspark.com/bitmachine/bitmachine (`origin`) — the same repository as [bitmachine](bitmachine.md) |
| Branch | `plexis-main` (tracks `origin/plexis-main`) |
| Last commit seen | `5eb8b254f` · 2026-05-25 |
| Divergence from `origin/main` | 266 commits ahead, 92 behind, as of this page |
| Workspace | `apps/plexis/` |
| Agent contract | `AGENTS.md` (mirrored by `CLAUDE.md`), the bitmachine one with a "local v2 branch" note |

Back to the [index](../RELATED_REPOS.md).

## What it is

Plexis is the successor runtime workspace inside bitmachine: the control
plane, management API and ingress/egress services that front sandboxed
workloads, plus an operator dashboard. There is no separate Plexis remote;
the `systems\plexis` checkout is a second clone of bitmachine held on the
long-running `plexis-main` branch so that Plexis work can proceed
independently of `main`. `claude-gateway2/` is its reference-only
predecessor.

Other branches present in this checkout: `codex/research-main-port-lab`,
`codex/research-runtime-main-port`, `codex/research-runtime-main-port-clean`.

## Layout of `apps/plexis/`

Role-first units, composed into services:

```
units/data/*          domain records
units/surface-*/*     inbound contracts, protocols, stubs and binds
units/storage-*/*     persistence contracts and implementations
units/impl/*          the application core
units/app/*           composition roots and operator tooling
units/proto/*         transport adapters
units/design/*        shared UI assets
deploy/, docker/, work/   operational assets and generated runtime artifacts
infra/, secrets/          provider stacks, public-edge automation, local secret material
```

Canonical services and binaries:

| Service | Binary |
|---|---|
| `control-plane` | `plexis-control-plane` |
| `management-api` | `plexis-management-api` |
| `edge-proxy` | `plexis-edge-proxy` |
| `managed-ingress` | `plexis-managed-ingress` |
| `egress-runtime` | `plexis-egress-runtime` |
| `management-ui` (dashboard, served apart from the API) | — |

Sandbox provisioning is not a Plexis service: Plexis consumes the bitmachine
Sandbox kernel contract, with BitDev `sandboxd` injected locally and a
production implementation behind the same API in deployment. Design notes
live beside the workspace in `ARCHITECTURE_MODEL.md`, `STORAGE.md`,
`TRACES.md` and `UNITS_REORGANIZATION_PROPOSAL.md`.

The branch also carries `apps/mimos`, `apps/eidos`, `apps/eidos2`,
`apps/megaron`, `apps/skopos`, `apps/access` and `apps/research`, several of
which are not on `main`.

## Working conventions

Same as [bitmachine](bitmachine.md): no legacy compatibility code, and Docker
only through BitDev (`dev/bitdev.ps1 doctor research-stack` and the like).

## Relation to Nightseam

Plexis' `units/surface-*` and `units/proto/*` are exactly the layers
Nightseam splits a contract into — data, operations, and the transport that
governs a session of them. The control-plane ↔ management-ui seam, and the
management-api ↔ operator tooling seam, are duplex APIs of the shape
`nightseam.duplex/1` targets, and are the first candidates for a generated
Go server / TypeScript client pair.
