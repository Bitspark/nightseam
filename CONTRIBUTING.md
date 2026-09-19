# Contributing

How work is organized in this repository — the boundary rule, parity between
the languages, the two tiers of tests, the golden discipline, lanes, and how a change
lands — is in **[COLLABORATION.md](COLLABORATION.md)**. Read
that first; this page exists so that GitHub links to it.

In short:

- `main` takes no direct push: a change is a branch in a worktree of its own
  (`node scripts/worktree.mjs add issue-<N>-<slug>`), a pull request, and
  `gh pr merge --auto --squash --delete-branch`, which lands it when the
  checks are green — no approval is required, so a green PR does not wait.
- Both tiers green before a change lands: `go test ./...`,
  `pnpm install && pnpm -r check && pnpm -r build && pnpm -r test`,
  `go vet ./... && go test ./...` inside `otel/go` (a module of its own,
  which the root's `./...` does not enter), `node scripts/matrix-table.mjs
  --check` and `node scripts/links.mjs` — what CI runs, which runs the
  example and the packed-install smoke besides. The full tier needs Go, Node
  22.12 or later and the TypeScript compiler `pnpm install` brings, and fails
  rather than skips when one is missing.
- A runtime change exists in both languages and is held to the shared suite.
- Generated code is never edited by hand; behavior is written against the
  interfaces the generated packages declare.
- Output-changing work is two commits: the logic, then the regenerated
  goldens.
- A vulnerability is reported the way [SECURITY.md](SECURITY.md) says, not in
  an issue.

[RELEASING.md](RELEASING.md) has what is published and how a release is cut.
