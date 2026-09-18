# Releasing

A release is one version across every published TypeScript package under
the `@nightseam` scope, the version the generator writes into a generated
client's manifest (`DefaultRuntimeVersion` in
`internal/targets/typescript/target.go`), and the tag of the Go module and
of every Go module nested in it. They move together, and a test in the fast
tier (`cmd/nightseam`, `TestVersions…`) fails when they drift.

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
- **Go, nested**: a component that depends on what the core module may not
  is a module of its own, released by a second tag `<dir>/vX.Y.Z` cut beside
  `vX.Y.Z` — that is how the Go toolchain names a module in a subdirectory,
  and it is the whole of releasing one. `otel/go`, the OpenTelemetry
  adapter, is the first and at present the only one: the core module depends
  on nothing and that is what is published, so the backend a consumer opts
  into lives beside it rather than in it. Such a module requires the root
  module at the release's own number and carries a `replace` to this
  checkout, which is for the repository's own build and which a consumer
  ignores — a dependency's replace is not a consumer's. They are found
  rather than listed too: `scripts/version.mjs` rewrites the requirement in
  every `*/go/go.mod`, and `TestVersionsMoveInLockstep` globs the same
  paths.

## Cutting a release

1. Be on `main`, clean, with both tiers green: `go test ./...`,
   `pnpm -r check && pnpm -r test`, and `go vet ./... && go test ./...` in
   each nested Go module — `otel/go` — which the root module's `./...` does
   not enter.
2. Set the version everywhere: `node scripts/version.mjs 0.3.0`. It rewrites
   every manifest, the generator's constant and every nested module's
   requirement on the root module; since the constant is in
   the generated manifests' goldens, then run
   `go test ./cmd/nightseam -short -run Golden -update` and commit the two
   together: `release: 0.3.0`, with the changelog's *Unreleased* section
   moved under the version.
3. Tag and push, once for the root module and once for each nested one:
   `git tag v0.3.0 && git tag otel/go/v0.3.0 && git push origin main v0.3.0 otel/go/v0.3.0`.
   The second tag is what `go get github.com/Bitspark/nightseam/otel/go@v0.3.0`
   resolves, and it publishes nothing of its own.
4. The `release` workflow runs on `v*`, which is the first tag and not the
   second: it checks the versions against the tag, installs, runs both tiers
   and each nested module's, builds, publishes the packages with the
   repository's `NPM_TOKEN` secret, and creates the GitHub release with that
   version's changelog section as its notes.

To rehearse without publishing: `node scripts/release-prepare.mjs v0.3.0`
checks the versions and copies the license files, and
`pnpm -r publish --dry-run --no-git-checks` shows what each tarball would
hold.

## What a consumer does

- Go: `go get github.com/Bitspark/nightseam@v0.3.0` and
  `go get -tool github.com/Bitspark/nightseam/cmd/nightseam@v0.3.0`; a
  `replace` to a sibling checkout is for development only.
- Go, the OpenTelemetry adapter:
  `go get github.com/Bitspark/nightseam/otel/go@v0.3.0`, which brings
  OpenTelemetry with it — and brings none of it to a consumer that does not
  ask for it, which is why it is a module of its own.
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
