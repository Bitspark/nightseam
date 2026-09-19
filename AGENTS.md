# Working here as an agent

Read these, in this order, before the first change:

1. [COLLABORATION.md](COLLABORATION.md) — the boundary rule, no legacy,
   parity between the languages, the two tiers of tests, the golden
   discipline, and **how a change lands**: a worktree, a branch, a pull
   request, a squash merge that lands itself when green. `main` takes no
   direct push and nobody is exempt.
2. The issue you hold — its **What**, **Surface**, **Held by** and
   **Touches** are the lane; what it does not name is not yours.
3. [docs/layers.md](docs/layers.md) before anything on the wire;
   [docs/tiers.md](docs/tiers.md) before anything about a language;
   [conformance/DRIVER.md](conformance/DRIVER.md) before a scenario or a
   testee.

The lifecycle, whole:

```
node scripts/worktree.mjs add issue-<N>-<slug>     # your own checkout
cd .worktrees/issue-<N>-<slug>
…work, commit…
go test -short ./... && go test ./... && pnpm -r check && pnpm -r build && pnpm -r test
git push -u origin issue-<N>-<slug> && gh pr create --fill
gh pr merge --auto --squash --delete-branch          # lands when green; do not wait for it
```

Commit messages are one prose sentence prefixed by the area (`runtime/go:`,
`generator:`, `docs:`), no trailer, no attribution line. A file a lane holds
is in its issue; a rule of the repository is in COLLABORATION.md; what a
document promises is checked against the code, not repeated from another
document. When a design question has no written answer, it is an issue with
the `design` label and a verdict from the operator — not a guess in code.
