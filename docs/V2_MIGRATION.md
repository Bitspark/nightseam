# The v2 migration

**Done.** Every milestone below landed on `main` between `df4ad3c` and the
commit that added this line; the v1 generator is deleted, the CLI runs v2,
and the README describes the language as it now is. The page stays as the
record of how the redesign was held to the old generator.

Nightseam was redesigned: a v2 declaration language (a directory per
family with tier files, two sides instead of a direction, one reference
form, names by convention with per-target override files) and a new
internal architecture (a typed type-expression AST, small single-purpose
packages behind SPIs, a render model between analysis and targets, the
validator interpreter in the runtimes). The model it implements is written
up in `../bitlink/MODEL.md`; the profile and the runtime packages stay as
they are, and the name stays Nightseam until v2 is stable.

This page was the working agreement while the two generators coexisted.

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

## What the redesign decided beyond the plan

- Every tier file may carry `types` and `imports`; a type is declared in
  the tier it belongs to. The corpus needed it: `carrier.Frame` holds
  `S.Envelope`, a protocol-tier type.
- One reference form means one rule: every family a declaration names is
  imported, including one whose `Envelope` fills a slot and one that fills
  an application's parameter. The old `nested_import` rule went with it;
  imports resolve transitively and an import cycle is refused instead.
- Targets own whole directories; the kernel refuses a rendered path
  outside them. A per-family `Place` in a target's config puts a family
  elsewhere, and a family that refers to it finds it there too — which is
  what the commuting diagram's right path needs, and what the old
  `importPath` did by accident.
- The validator interpreter moved into both runtimes, held to one
  conformance table, and reads `apply`, `ref` and the field constraints.
  A type drawn from a parameter is the TypeScript binding's to validate,
  and passed through in Go, whose generated codecs validate it where the
  generic type is instantiated; the table has no case that depends on the
  difference.
- The runtimes read the previous language's slot spellings,
  `{"envelope": X}` and `{"connection": X}`, as `X.Envelope` and
  `X.Handle`, so a hand-written caller of a validator keeps working.
- The Go surface of every package v2 renders for the converted corpus is
  identical to v1's — the surface goldens taken from v1 in M0 hold against
  v2 unchanged — and the TypeScript modules export what v1's do, plus
  `TypeExpression`.

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
