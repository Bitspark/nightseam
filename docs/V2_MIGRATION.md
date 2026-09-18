# The v2 migration

Nightseam is being redesigned: a v2 declaration language (a directory per
family with tier files, two sides instead of a direction, one reference
form, names by convention with per-target override files) and a new
internal architecture (a typed type-expression AST, small single-purpose
packages behind SPIs, a render model between analysis and targets, the
validator interpreter in the runtimes). The model it implements is written
up in `../bitlink/MODEL.md`; the profile and the runtime packages stay as
they are, and the name stays Nightseam until v2 is stable.

This page is the working agreement while the two generators coexist.

## The freeze

The v1 generator lives under `internal/legacy/` — `contract`, `kernel`,
`spi`, `languages` — and the CLI runs it until the switch. **Nothing under
`internal/legacy/` changes except its deletion.** Its goldens under
`cmd/nightseam/testdata/golden`, the `diagnostics.txt` of every invalid
case and the surfaces under `testdata/surface` hold it byte for byte; a
diff there before the switch is a regression, not a change.

v2 grows beside it under `internal/` — `diag`, `naming`, `emit`, `model`,
`load`, `analysis`, `check`, `render`, `spi`, `targets/`, `kernel`,
`oracle`, `upgrade` — and lands on `main` milestone by milestone with the fast tier
green. Before cutting and before landing a milestone:

```
git fetch origin; git log --oneline HEAD..origin/main; git status --short
```

## The invariant ladder

Bytes are not the invariant across v1 → v2; these are, in order of strength:

| | invariant | held by |
|---|---|---|
| I0 | v1 output byte-identical to its goldens until v1 is deleted | `TestCorpusRendersGolden`, `TestInvalidCorpusIsRefused`, `TestCorpusSurfaceIsGolden` |
| I1 | v2 unit invariants per package | each package's tests |
| I2 | **surface equivalence**: the exported Go surface of `render_v2(upgrade(F))` equals that of `render_v1(F)`, identifier for identifier; the TS `.d.ts` equal modulo whitespace | `TestUpgradeIsSurfaceEquivalent`, `TestUpgradeIsDeclarationEquivalent` |
| I3 | **behavioral equivalence**: every hand-written fixture body (`goIntegrationFixture`, `goTunnelFixture`, `goDiagramFixture`, `tsDiagramFixture`, the `.mjs` scripts) passes **unchanged** against v2 output | the slow fixtures, parametrised on a renderer |
| I4 | the invalid corpus is refused with equivalent codes, pointers relocated to tier files | `TestInvalidCorpusIsRefused` on the v2 corpus |
| I5 | after the switch, v2 goldens are the byte oracle | `TestCorpusRendersGolden` |

The golden bytes are expected to change at the switch for exactly these
reasons, each named in the commit that regenerates them: ordering by name
within a side; direction words gone; the embedded descriptor spelling
slots as `"S.Envelope"`; diagnostics carrying a tier file; validator files
shrunk to one `MustSchema` call. Anything else in a golden diff is a bug.

## Golden discipline

An output-changing change is two adjacent commits: the logic, then
`goldens: regenerate — <naming|ordering|formatting|feature|fix>: <line>`
touching only `cmd/nightseam/testdata`. Regenerate with

```
go test ./cmd/nightseam -run 'Golden|Invalid|Surface|Reserved' -update
git diff --stat -- cmd/nightseam/testdata
```

A diff under `testdata/surface` or `testdata/reserved` is a change to the
public API of the generated packages, or to what a consumer may name, and
gets its own sentence in the commit.

## Milestones

| | lands | proves |
|---|---|---|
| M0 | v1 under `legacy/`, the surface oracle, LF discipline, this page | I0 |
| M1 | `diag`, `model` — the typed AST | I1 |
| M2 | `load` with the tier table, `nightseam upgrade`, the v2 corpus | the corpus converts mechanically |
| M3 | `analysis`, `check`, the v2 kernel; the v2 invalid corpus | I4 |
| M4 | `render`, `emit`, `spi`, the Go validator in the runtime, the Go target | I2, I3 for Go |
| M5 | the TS validator in the runtime, the TypeScript target | I2, I3 for TS |
| M6 | the CLI switches; fixtures move; Windows in CI | I5 |
| M7 | `internal/legacy` deleted | — |
| M8 | README, related-repo pages, `MODEL.md` amended | — |
