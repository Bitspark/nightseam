<!--
COLLABORATION.md has the rules this checklist is drawn from. One commit is
one change, and its message says what changed and why, in one sentence, from
the area it changes: `runtime/go: …`, `tunnel/ts: …`, `generator: …`,
`docs: …`.
-->

## What changed

<!-- The change, in the terms of the code, and why it was needed. -->

## Surface

<!-- The exported names added or changed, spelled out. "None" if there are none. -->

## Held by

<!-- The tests that hold it, named. -->

---

- [ ] Both tiers are green: `go test ./...`, `pnpm -r check && pnpm -r build && pnpm -r test`, and `go vet ./... && go test ./...` inside `otel/go`.
- [ ] The README's Languages table matches `conformance/matrix.json`: `node scripts/matrix-table.mjs --check`.
- [ ] **Parity** — a change to one language's runtime component carries its twin, and the shared suite says so; or the change touches neither runtime.
- [ ] **Goldens** — output-changing work is two commits, the logic and then `goldens: regenerate — feature: <what changed>`; `testdata/surface` changed only if the generated packages' exported surface did.
- [ ] No generated code was edited by hand.
- [ ] One lane: this carries no other lane's change.
