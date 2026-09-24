# Keep a child's budget through reconstruction

Keep an application-defined cart admission budget while rebuilding its parent.

## Run

From the Nightseam repository root, with Node.js 24+, pnpm 12.4.1 and Go 1.26:

```sh
node scripts/examples.mjs run guarded-child
node scripts/examples.mjs run guarded-child --language=go
node scripts/examples.mjs run guarded-child --language=ts
```

The first command runs both languages. The runner installs and builds the
checkout, packs its packages and runs independent consumer copies outside the
workspace. It checks types and exact output, and removes the temporary copies.
Add `--keep` to inspect the copies afterwards. Public dependency downloads need
network access; no credentials, service account or external server are required.

**Availability: unreleased source.** This example uses declared composition
from the checkout. Installing the current `0.6.0` dependencies directly does
not provide that API. The runner supplies packed TypeScript packages and a
separate Go rehearsal version from this source; the manifests stay aligned with
the repository's coordinated version for its eventual release.

## Model and approach

A wrapper allows two new `add` requests. The first uses one admission; the assembler then rebuilds the shop using the same guarded child. A second add uses the remaining admission, and a third is refused before delivery. `list` and control messages pass through unchanged.

The [model and example approach](../../docs/runtime/examples.md) explains the
domain, ownership and relationship to Bitwire. The complete implementations are
[Go](go/main.go) and [TypeScript](ts/main.ts); no generated code is required.

## Expected result

Each language prints the same result:

```text
remaining after first add: 1
remaining after rebuild and second add: 0
third add: refused
cart: book, pen, notebook, pencil
```

The runner also prints preparation and `PASS` lines. The example's output above
is held exactly by [expected.txt](expected.txt), including line order.

The checks hold the wrapper's identity, remaining budget, exact local refusal, and final cart contents through the old access. The budget counts accepted sends, not completed effects. This is a small consumer policy, not an authentication or authorization protocol. The cart handles requests; it exposes no event-based mutation in this example.

[All examples](../README.md)
