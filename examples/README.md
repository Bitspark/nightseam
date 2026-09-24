# Runnable examples

Start with a use case, run it, then read its complete consumer code. Go and
TypeScript examples share the same observable result.

| Example | What you can see | Availability |
| --- | --- | --- |
| [service-tree](service-tree/README.md) | A parent has its own behavior and complete children | Unreleased source |
| [select-and-remount](select-and-remount/README.md) | Reuse the same cart as a different parent's child | Unreleased source |
| [rebuild-with-state](rebuild-with-state/README.md) | Add recommendations without resetting a cart | Unreleased source |
| [guarded-child](guarded-child/README.md) | A child's admission budget survives parent reconstruction | Unreleased source |
| [cancel-pending-request](cancel-pending-request/README.md) | Cancel the original invocation after replacing the assembler's route | Unreleased source |
| [probe](probe/README.md) | Generated Go server and TypeScript client: requests, reverse calls, events and live callbacks over WebSockets | Released example; use a release tag for registry installs |

## One command to run

Clone Nightseam, open the repository root, and install Node.js 24+, pnpm 12.4.1
and Go 1.26. The runner handles dependency installation and package preparation:

```sh
node scripts/examples.mjs list
node scripts/examples.mjs run rebuild-with-state
node scripts/examples.mjs run rebuild-with-state --language=go
node scripts/examples.mjs run rebuild-with-state --language=ts
node scripts/examples.mjs run probe
node scripts/examples.mjs check --all
```

The five composition examples run in either language, or both by default.
Probe requires both languages: its Go server and TypeScript client communicate
over an ephemeral loopback port. The runner starts and stops that server.

Every run uses **this working tree's packaged code**, with consumer copies
outside the repository, and exits nonzero if compilation or a behavioral check
fails. Each composition example compares its stdout with `expected.txt`;
probe's existing smoke checks its complete exchange and package imports.
A successful run prints `PASS` for each selected example/language.

Use `--keep` on `run` or `check` to retain the consumer copies for inspection.
Their location is printed; normal runs remove temporary consumers and caches.
The runner does not edit the committed consumer manifests. It does install
workspace dependencies and build packages in the checkout.

Public dependency downloads require network access. No tokens, private sibling
checkouts, external services or generator installation are needed. An uncached
first run takes longer because it prepares packages and downloads dependencies.

## Understand and extend

The [model and approach](../docs/runtime/examples.md) explains the shopping
cart, the separate assembler and caller surfaces, retained state and ownership,
and how runnable examples are organized and held in CI. Each example has its
own README, consumer manifests, source and expected result.

The composition API is implemented in source but is not in the published
Nightseam 0.6.0 packages. The source runner gives it a Go rehearsal version and
uses TypeScript tarballs. That proves packaged source works; it does not prove
a release has been published. For probe's direct registry installation, use
its README from a release tag.

The [Bitwire use-case catalogue](https://github.com/Bitspark/bitwire/blob/main/examples/README.md)
connects these runtime examples to the shared contract. Contract conformance,
runtime examples and domain-specific integrations have different owners.
