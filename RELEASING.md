# Releasing

A release is one version across every published TypeScript package under
the `@nightseam` scope, the version the generator writes into a generated
client's manifest (`DefaultRuntimeVersion` in
`internal/targets/typescript/target.go`), and the Go module's tag. They move
together, and a test in the fast tier (`cmd/nightseam`, `TestVersions…`)
fails when they drift.

## What is published

- **npm**: every package under `*/ts`, in the `@nightseam` organization,
  public. They are found rather than listed — `scripts/packages.mjs` reads
  them off the workspace, and `TestVersionsMoveInLockstep` globs the same
  paths — so a component added beside the others is released with them.
  Each is built to `dist/` — ES modules with declarations and source maps
  that carry the sources — by `pnpm -r build`; the tarball holds `dist`,
  the package's README, and a copy of `LICENSE` and `NOTICE`. In the
  repository the packages export their TypeScript source, so the workspace
  and a consumer that links them need no build; `publishConfig` swaps the
  entry points to `dist` at publish time and pnpm rewrites the
  `workspace:*` dependencies to the released version.
- **Go**: the module `github.com/Bitspark/nightseam` at the tag; nothing is
  uploaded, a tag is the release. `runtime/go`, `duplex/go`, `tunnel/go`
  and `session/go` are its importable packages, and `cmd/nightseam` is
  what a consumer adds as a Go tool.

## Cutting a release

1. Be on `main`, clean, with both tiers green: `go test ./...` and
   `pnpm -r check && pnpm -r test`.
2. Set the version everywhere: `node scripts/version.mjs 0.3.0`. It rewrites
   every manifest and the generator's constant; since the constant is in
   the generated manifests' goldens, then run
   `go test ./cmd/nightseam -short -run Golden -update` and commit the two
   together: `release: 0.3.0`, with the changelog's *Unreleased* section
   moved under the version.
3. Tag and push: `git tag v0.3.0 && git push origin main v0.3.0`.
4. The `release` workflow runs on the tag: it checks the versions against
   the tag, installs, runs both tiers, builds, publishes the packages
   with the repository's `NPM_TOKEN` secret, and creates the GitHub release
   with that version's changelog section as its notes.

To rehearse without publishing: `node scripts/release-prepare.mjs v0.3.0`
checks the versions and copies the license files, and
`pnpm -r publish --dry-run --no-git-checks` shows what each tarball would
hold.

## What a consumer does

- Go: `go get github.com/Bitspark/nightseam@v0.3.0` and
  `go get -tool github.com/Bitspark/nightseam/cmd/nightseam@v0.3.0`; a
  `replace` to a sibling checkout is for development only.
- npm: the generated clients depend on `@nightseam/runtime` and
  `@nightseam/tunnel` at the version the generator that rendered them
  carries; a workspace override to a sibling checkout is for development
  only, and comes out when the packages it stands in for exist.

## Once, before the first release

- The `@nightseam` organization exists on npm; an automation token for it
  is stored as the `NPM_TOKEN` secret of this repository.
- The repository is public if the Go module is to be fetched without
  credentials; a private module needs `GOPRIVATE=github.com/Bitspark/*`
  and Git credentials on the consumer's side.
