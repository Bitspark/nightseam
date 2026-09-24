# Cancel the original invocation after replacing access

Replace an assembler's `run` route while a request through previously bound access is still pending.

## Run

From the Nightseam repository root, with Node.js 24+, pnpm 12.4.1 and Go 1.26:

```sh
node scripts/examples.mjs run cancel-pending-request
node scripts/examples.mjs run cancel-pending-request --language=go
node scripts/examples.mjs run cancel-pending-request --language=ts
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

A handler signals that it has started and waits for cancellation. The assembler creates a new description pointing `run` at a replacement handler. Cancelling the original caller must still reach the original invocation; a fresh call through the new description must reach the replacement.

The [model and example approach](../../docs/runtime/examples.md) explains the
domain, ownership and relationship to Bitwire. The complete implementations are
[Go](go/main.go) and [TypeScript](ts/main.ts); no generated code is required.

## Expected result

Each language prints the same result:

```text
original request: cancelled
new request: replacement
```

The runner also prints preparation and `PASS` lines. The example's output above
is held exactly by [expected.txt](expected.txt), including line order.

The program waits for deterministic handler signals with deadlines. It checks cancellation at both caller and original handler, then calls the replacement. Composition forwards the cancellation unchanged. The runtime owns invocation tracking, and the application retains ownership of the physical endpoints.

[All examples](../README.md)
