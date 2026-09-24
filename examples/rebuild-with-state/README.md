# Expand the shop without resetting its cart

Add recommendations to a shop without losing its existing cart or parent behavior.

## Run

From the Nightseam repository root, with Node.js 24+, pnpm 12.4.1 and Go 1.26:

```sh
node scripts/examples.mjs run rebuild-with-state
node scripts/examples.mjs run rebuild-with-state --language=go
node scripts/examples.mjs run rebuild-with-state --language=ts
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

The application owns a cart initialized with a book and a pen. The assembler retains the shop's `Declared` description, decomposes its origin and complete child access, then composes another description with an extra `recommendations` child. It does not reconstruct the cart's internals.

The [model and example approach](../../docs/runtime/examples.md) explains the
domain, ownership and relationship to Bitwire. The complete implementations are
[Go](go/main.go) and [TypeScript](ts/main.ts); no generated code is required.

## Expected result

Each language prints the same result:

```text
before: book, pen
same cart capability: true
shop: Bookshop
after: book, pen, notebook
recommendations: pencil
```

The runner also prints preparation and `PASS` lines. The example's output above
is held exactly by [expected.txt](expected.txt), including line order.

The program checks capability identity, parent behavior, both old and new access to the same mutated cart, and the new recommendations. Retention here is an in-process reference to live access; this is not persistence, a snapshot, or a serialization round trip.

[All examples](../README.md)
