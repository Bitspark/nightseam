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
  `workspace:*` dependencies to the released version. Each is published
  with provenance, so the npm page names the workflow run, the commit and
  the repository the tarball was built from, and a consumer can check that
  rather than take it.
- **Go**: the module `github.com/Bitspark/nightseam` at the tag; nothing is
  uploaded, a tag is the release. `runtime/go`, `duplex/go`, `tunnel/go` and
  `live/go` are its importable packages, with their sub-packages — `duplex/go/ws`,
  `runtime/go/slogobserver`, and the suite `duplex/go/duplextest`.
  `cmd/nightseam` is what a consumer adds as a Go tool.
- **Go, nested**: a component that depends on what the core module may not
  is a module of its own, released by a second tag `<dir>/vX.Y.Z` cut beside
  `vX.Y.Z` — that is how the Go toolchain names a module in a subdirectory,
  and it is the whole of releasing one. `otel/go`, the OpenTelemetry
  adapter, is the first and at present the only one: the core module pulls
  in no telemetry backend and that is what is published, so the backend a
  consumer opts into lives beside it rather than in it. Such a module requires the root
  module at the release's own number and carries a `replace` to this
  checkout, which is for the repository's own build and which a consumer
  ignores — a dependency's replace is not a consumer's. They are found
  rather than listed too: `scripts/version.mjs` rewrites the requirement in
  every `*/go/go.mod`, and `TestVersionsMoveInLockstep` globs the same
  paths.

Nothing under `examples/` is published. `examples/probe` is a consumer of
what is — the getting-started of [examples/README.md](examples/README.md),
and the consumer both smokes below install — so it names the released
version of everything it depends on, in both languages, with no
`workspace:*` and no `replace`. `scripts/version.mjs` moves those spellings
with the rest, `scripts/release-prepare.mjs` holds them to the tag, and
`TestVersionsMoveInLockstep` holds them in the fast tier.

## Cutting a release

A pushed release tag is never moved or deleted: the organization's active
`release-tags-immutable` ruleset protects both `v*` and nested `**/v*` tags,
and a changed release gets a new version. The release workflow checks the
remote tag against its checkout before validation and again before publishing;
a rehearsal may use a name that has not been published, but cannot reuse one
that names another commit.

1. Start a release issue and its own worktree from `origin/main`, following
   [COLLABORATION.md](COLLABORATION.md). Keep the release scope fixed while
   preparing it; unfinished feature lanes may continue in their own
   worktrees. Hold the candidate to both tiers: `go test -short ./...`,
   `go test ./...`,
   `pnpm -r check && pnpm -r build && pnpm -r test`,
   `node scripts/matrix-table.mjs --check`, and `go vet ./... && go test ./...` in
   each nested Go module — `otel/go` — which the root module's `./...` does
   not enter. The conformance suite runs with the full tier and writes
   `conformance/matrix.json`; commit it with whatever moved it, since the
   release is weighed against the matrix the tag carries. *What a release
   refuses*, below, says what a red cell does.
2. Set the version everywhere: `node scripts/version.mjs 0.5.0`. It rewrites
   every manifest, the generator's constant, every nested module's
   requirement on the root module, and what the getting-started example
   depends on in both languages — a consumer checkout, so it names the
   version rather than linking to the tree, and a release whose own example
   asks for something else is not one; since the constant is in
   the generated manifests' goldens, then run
   `go test ./cmd/nightseam -short -run Golden -update`. Commit the version
   and release notes, then the generated goldens separately, following the
   golden discipline. Move the changelog's *Unreleased* entries under the
   version, describing the behavior at the release cutoff and identifying
   unfinished features. Land this release preparation through its pull
   request with auto-squash; never push `main` directly.
3. Record the merged release commit, verify it is on `origin/main`, and
   rehearse that exact commit through the release workflow before tagging.
   The rehearsal and publication both retain the release environment's
   reviewer gate. A later change to `main` does not change the release
   commit. Tag and push that commit once for the root module and once for
   each nested one, the root module's first:
   `git tag v0.5.0 <release-commit>` and `git push origin v0.5.0`, then
   `git tag otel/go/v0.5.0 <release-commit>` and
   `git push origin otel/go/v0.5.0`. The order is
   not a formality. A nested module requires the root module at the release's
   own number, so `go get github.com/Bitspark/nightseam/otel/go@v0.5.0`
   resolves only once `v0.5.0` is there to be fetched — the `replace` that
   makes the requirement resolve in this checkout is the repository's own and
   a consumer ignores a dependency's replace, getting what is required. The
   second tag is what that `go get` names, and it publishes nothing of its
   own.
4. The `release` workflow runs on `v*`, which is the first tag and not the
   second: it checks the versions and the conformance matrix against the tag,
   installs, runs both tiers and each nested module's, builds, checks what
   provenance needs, **installs what is about to be published** —
   `node scripts/smoke-packed.mjs`, described under *Rehearsing one* —
   publishes the packages with `--provenance` — every run of it waits in
   the `release` environment for its required reviewer — creates the GitHub
   release with that version's changelog
   section as its notes, and then **makes the round trip**:
   `node scripts/smoke-registry.mjs $TAG --open-issue` installs the same
   example again, this time from npm and from the module proxy with nothing
   laid for it and nothing overridden, `go get`s the root module at the tag
   and the adapter at its own, and runs the exchange.
   Before installing, it polls npm and the Go proxy with backoff for up to
   thirty minutes for every published package and Go module to propagate,
   with retry backoff capped at thirty seconds.
   The window includes cached Go proxy misses that can outlive publication
   by many minutes. It reports the wait and names anything still unavailable
   at the deadline.

   The round trip is the only step after the upload, and it is the only one
   whose failure cannot be answered by fixing the tree and tagging again: the
   tag is cut, the packages are on npm, and neither can be taken back. So its
   failure both fails the run and opens an issue naming the tag, because a
   red run on a tag nobody re-runs is silence, and the question it leaves —
   whether what is published is usable and the smoke is wrong, or a patch
   release is owed — is one somebody has to answer the next morning.
   Exercise that path once by hand, against a version that does not exist, so
   that the issue it opens has been seen before it is needed.

## What a release refuses

A release is a promise per language, and `docs/languages/tiers.md` says which promise.
`scripts/release-prepare.mjs` reads `conformance/matrix.json` — the standing
committed on the tag, which CI holds fresh, since a matrix that drifted fails
the README's table check — and applies the tier table to it before anything
is published:

- a red cell in a profile the language's tier **guarantees** is what that
  tier's `onFailure` says. For tiers 1 and 2 it is `stop`: the tag is
  refused. For tiers 3 and 4 it is `provisional`: the release ships and the
  language is named in the notes, under *Languages*, so a consumer reading
  the release learns it without opening the matrix.
- a red cell **elsewhere** is what the tier's `otherwise` says. Only tier 2
  has one, `stop-next`: the release ships, the notes say the lag has begun,
  and the next release is refused if the cell is still red. Whether it was
  red before is read from the previous `v*` tag's own
  `conformance/matrix.json`, which is why the workflow checks out the whole
  history — a shallow checkout carries no tag to read, and every second
  failure would pass as a first.
- a matrix with no row for a language `profiles.json` places at a tier is
  refused outright: it is the artifact of a run filtered by `-run`, and a tag
  weighed against one is weighed against a language nobody ran.

A first release, and a tag cut before the matrix was committed, both count as
nothing failing before — so the first red cell outside a tier 2 language's
guarantees always ships, and the second never does.

## Rehearsing one

The release workflow runs from its own page too — Actions → release → Run
workflow, with the version as its input. It does everything a tag does
except the two things that cannot be taken back: nothing is uploaded and no
release is created, `--dry-run` taking the publish as far as packing each
tarball and minting its provenance attestation. Run it before the first real
tag of a version, and whenever the release path itself has changed, because
each way it can fail — a version that drifted, a matrix cell the tier table
stops for, a tier that is red, a build that emits nothing, a repository that
is not public, an OIDC token the job may not mint — otherwise fails a run
that follows a tag which already exists.

A rehearsal also installs what it would publish. `node scripts/smoke-packed.mjs`
packs every published package, copies `examples/probe` — the getting-started,
and the one consumer both smokes use — out of the workspace, and resolves it
against the packed shape and nothing else: no `workspace:*` link, no
`replace` to this checkout. It then type-checks it, builds it, runs the
server and holds the client's exchange line by line: an RPC reply, an
event, a supplied callback the server invokes, and a function the server
returned. It is the only gate that asks whether what is published can be
*installed*; a `files` field that omits
`dist`, an `exports` entry naming a path the tarball does not hold, a
dependency a link satisfied and a registry would not, a Go package that only
ever resolved through a sibling checkout — each passes everything else here
and is given at a consumer's install, which is after the tag. It runs on
every pull request too, in `ci.yml`'s full job, so that a packaging change
fails the change rather than the release that carries it.

The smoke also *imports* what it would publish. The example imports the
packages its generated live client needs. Every package the release finds is
also imported from the copied consumer at every entry point its
`publishConfig.exports` declares — type
checked with library checking on, then loaded by Node — which is what asks the
questions only a real import answers: a `dist` that imports a package the
workspace link satisfied and a registry would not, an `exports` condition that
resolves to nothing under Node's own resolver, declarations that only ever
type-checked against a sibling's *source* rather than against its emitted
`.d.ts`, an entry point the build emitted nothing into. The published map is
what is imported and not the workspace one: inside the workspace a package may
expose helpers — `./conformance` — that are this repository's fixtures and not
entry points anybody installs, and `publishConfig` swaps the whole map at
publish time.

Before installing, the smoke checks each tarball for `dist/index.js`,
`dist/index.d.ts`, `README.md`, `LICENSE` and `NOTICE`. For a local run,
build the packages and copy the notices first:

```sh
pnpm -r build
node --input-type=module -e "import { copyNotices } from './scripts/packages.mjs'; copyNotices();"
node scripts/smoke-packed.mjs
```

The release preparation also copies the notices, except with `--dry-run`,
which reports that they were not copied.

It needs no registry and no tag, which is what lets it run while this
repository is still private. Two choices make that true, and each had an
alternative:

- **npm: a pnpm `overrides` map to the tarballs' paths**, rather than
  `pnpm add ./scratch/*.tgz`. The published packages depend on one another,
  so a direct install of them would satisfy exactly those and then go to the
  registry for the transitive `@nightseam/duplex` the tarballs already hold;
  an override reaches a transitive dependency and a direct one alike. Since
  pnpm 10 an override is a workspace setting, so the copy is given a
  `pnpm-workspace.yaml` of one project to carry them — written into the copy,
  never into the checkout, where what is committed names the versions a
  consumer of the registry names.
- **Go: a `file://` module proxy laid from the tree**, rather than a
  temporary tag on a scratch branch. A module zip is content-addressed —
  `go` hashes what it unpacks rather than trusting where it came from — so a
  zip written out of the working tree is what the proxy would serve for the
  tag, and it costs no tag to create, nothing to push and nothing to clean
  up afterwards. A temporary tag would have to be pushed to be fetched, which
  is the one thing a rehearsal may not do. The proxy carries Nightseam alone;
  everything else falls through to whatever `GOPROXY` the machine has.
  Its version is `v<version>-rehearsal.<commit>` (appended to an existing
  prerelease when needed), required only by the copied consumer; the source
  checkout's version stays unchanged. `GOMODCACHE` lives inside the smoke's
  scratch directory, with `-modcacherw` so it can be removed, and `GOSUMDB=off`
  is scoped to the smoke's Go commands. Rehearsal artifacts never enter the
  machine's shared module cache or impersonate a released version, and no
  global Go setting is changed.

What a rehearsal does not answer is the registry's own refusals: a name
already taken at that version, a token expired or without publish rights on
the scope. Those are given at the upload and nowhere before it, which is what
the round trip in *Cutting a release* is for — and that one runs on a tag
push and never on a rehearsal, since before the tag there is nothing on
either registry to install. What the packed smoke does not answer either is
anything about the tarball that only the registry decides: the name it is
served under, the files it keeps, the version it resolves `^` to.

By hand, and further from what CI does: `node scripts/release-prepare.mjs
v0.5.0 --dry-run` checks every spelling of the version and the matrix
against the tier table and writes nothing — without `--dry-run` it also
copies the license files and writes `release-notes.md` — and
`pnpm -r publish --dry-run --no-git-checks` shows what each tarball
would hold. Neither mints provenance — that needs the workflow's token.

## What a consumer does

What a consumer does is `examples/probe`, and it is checked rather than
asserted: both smokes install that example and run it, so the paragraph below
is the one the release itself walks. [examples/README.md](examples/README.md)
is the same thing written for the consumer.

- Go: `go get github.com/Bitspark/nightseam@v0.5.0` and
  `go get -tool github.com/Bitspark/nightseam/cmd/nightseam@v0.5.0`. The
  example requires the module and names the tool in its own `go.mod`, with no
  `replace`: a `replace` to a sibling checkout is for development only, and
  an example carrying one would be an example nobody had installed.
- Go, the OpenTelemetry adapter:
  `go get github.com/Bitspark/nightseam/otel/go@v0.5.0`, which brings
  OpenTelemetry with it — and brings none of it to a consumer that does not
  ask for it, which is why it is a module of its own. The round trip `go
  get`s it into a module of its own for the same reason: nothing the example
  does would resolve the second tag, and the second tag is the step that
  depends on the first already being fetchable.
- npm: the generated clients depend on `@nightseam/runtime` and
  `@nightseam/tunnel` at the version the generator that rendered them
  carries, and on `@nightseam/live` when their family has a live tier. The
  example depends on all three at that version, with no `workspace:*`.
  A workspace override to a sibling checkout is for
  development only, and comes out when the packages it stands in for exist.

## A name npm has not served

`scripts/packages.mjs` finds the published packages on the disk, so a
component added beside the others is released with the rest without being
named anywhere. What the disk cannot say is whether npm has ever served that
name — and that is what decides how the name authenticates. npm attaches a
trusted publisher to a package that **already exists**, so the first publish
of a name cannot be made by OIDC: it needs a granular token, read and write on
the `@nightseam` scope alone with one day's expiry, stored as the `NPM_TOKEN`
secret for that one run, exactly as the first names needed one.

`node scripts/first-publish.mjs` asks the public registry which of the
discovered names it already serves. It sends no credential, since whether a
name exists is public and it asks nothing else, and a name it cannot get an
answer about is a refusal rather than a new name. The release workflow runs it
before the install, on a rehearsal as well as on a tag, and on a tag with
`--require-credential`, which refuses the run when a name is new and no token
is set. That refusal is the whole point of asking early: `pnpm -r publish`
reaches a new name *after* the tag is pushed and after the names before it are
already uploaded, and neither of those is taken back.

A release that adds a package therefore goes:

1. Rehearse. The rehearsal names it — `first publish: @nightseam/…` — and
   publishes nothing.
2. Create the granular token and store it as the `NPM_TOKEN` secret.
3. Tag. The new name goes up by the token, the established ones by their
   trusted publishers.
4. Give the new package this repository's `release.yml` in the `release`
   environment as its trusted publisher, and delete the secret again. The next
   release publishes every name by OIDC, and `first-publish.mjs` says so.

## Once, before the first release

- The `@nightseam` organization exists on npm. The first publish of each
  package name is made with a granular access token — read and write on the
  `@nightseam` scope alone, one day's expiry — stored as the `NPM_TOKEN`
  secret for that one run, because npm's trusted publishing is configured on
  a package that already exists. Once they exist, each is given this
  repository's `release.yml` in the `release` environment as its trusted
  publisher, the secret is deleted, and every later publish authenticates
  by OIDC with no credential stored anywhere; that needs npm 11.5.1 and
  Node 22.14 or later on the runner.
- The GitHub environment `release` exists with the maintainer as its
  required reviewer, so that no run of the release workflow — a tag's or a
  rehearsal's — publishes without a person approving it.
- The repository is public. Three things wait on that one setting, and all
  three are done in the same sitting as the flip rather than remembered
  afterwards, because the documents that depend on them ship with the first
  release:
  - the Go module is fetched without credentials; a private one needs
    `GOPRIVATE=github.com/Bitspark/*` and Git credentials on the consumer's
    side;
  - npm provenance can be minted at all — the registry refuses it from a
    private repository, and the workflow refuses to publish without it;
  - **private vulnerability reporting** can be enabled, under Settings →
    Code security → Private vulnerability reporting. It is a checkbox, it is
    offered on public repositories only, and until it is ticked the *Report a
    vulnerability* form 404s — which is the form `SECURITY.md` sends a
    reporter to, `CODE_OF_CONDUCT.md` sends a conduct concern to, and
    `.github/ISSUE_TEMPLATE/config.yml` offers beside the issue types. Tick
    it and then follow the link from an account that is not a maintainer:
    being offered the form is the check.
