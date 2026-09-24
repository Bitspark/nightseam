# Reuse a selected cart under another parent

Select the existing cart and mount that access as `basket` under another parent.

## Run

From the Nightseam repository root, with Node.js 24+, pnpm 12.4.1 and Go 1.26:

```sh
node scripts/examples.mjs run select-and-remount
node scripts/examples.mjs run select-and-remount --language=go
node scripts/examples.mjs run select-and-remount --language=ts
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

`at(shop, ['cart'])` / `At(shop, []string{"cart"})` creates relative access. The checkout borrows that whole capability as a child. Adding a notebook through `basket/add` therefore changes the same cart read through the original shop.

The [model and example approach](../../docs/runtime/examples.md) explains the
domain, ownership and relationship to Bitwire. The complete implementations are
[Go](go/main.go) and [TypeScript](ts/main.ts); no generated code is required.

## Expected result

Each language prints the same result:

```text
original cart: book, pen, notebook
remounted cart: book, pen, notebook
```

The runner also prints preparation and `PASS` lines. The example's output above
is held exactly by [expected.txt](expected.txt), including line order.

Both paths must return the same three items. Selection grants send access; it does not copy the cart, enumerate its operations, or transfer endpoint ownership.

[All examples](../README.md)
