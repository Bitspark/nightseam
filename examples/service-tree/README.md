# Give a parent its own behavior and complete children

Compose a parent that answers at the empty path and a cart child that answers at `cart/list` and `cart/add`.

## Run

From the Nightseam repository root, with Node.js 24+, pnpm 12.4.1 and Go 1.26:

```sh
node scripts/examples.mjs run service-tree
node scripts/examples.mjs run service-tree --language=go
node scripts/examples.mjs run service-tree --language=ts
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

Calls to `[]` return the shop name. Calls under `cart` delegate to the complete cart access with that first segment removed. The dispatcher and application own the handlers and their state.

The [model and example approach](../../docs/runtime/examples.md) explains the
domain, ownership and relationship to Bitwire. The complete implementations are
[Go](go/main.go) and [TypeScript](ts/main.ts); no generated code is required.

## Expected result

Each language prints the same result:

```text
shop: Bookshop
cart: book, pen
```

The runner also prints preparation and `PASS` lines. The example's output above
is held exactly by [expected.txt](expected.txt), including line order.

The result checks both parent behavior and the child's initial contents. It uses a local asynchronous Wire pair; it does not demonstrate networking.

[All examples](../README.md)
