# Declared composition: production adoption

`node scripts/declared-conformance.mjs` runs the 39 independently authored
[Bitwire cases](https://github.com/Bitspark/bitwire/blob/fdc2ae99bbd4dcf1f887c5e32bbda2e315c890a1/conformance/declared/cases.json)
against Nightseam's production `ComposeDeclared` / `Declared.compose`, complete
parts, bound access and existing endpoints. It runs both Go and TypeScript on
local pairs and WebSockets in both directions: 234 case executions. The full
Go tier includes `TestDeclaredBitwireConformance`.

[upstream.json](upstream.json) pins the public upstream revision and SHA-256 of
the unmodified [cases.json](cases.json). Updating a pin is an explicit upstream
adoption, not permission to repair an expectation to fit an implementation.
The runner verifies the bytes before preparing inputs. Expected observations
are removed from inputs; a separate judge refuses missing, duplicate, extra,
unknown or mismatched observations. Script tests exercise those refusal paths.

The [Go driver](go/main.go) and [TypeScript driver](../ts/src/declared.ts) adapt
the upstream scenario vocabulary and supplied opaque guards. They retain no
replacement routing, composition or decomposition implementation. The assembler
keeps production descriptions separately from their send-only facades; opaque
children are retained whole. Reconstruction uses the production parts and
constructor. Lost or replaced origins, duplicated guards, reset guard state and
altered child maps remain negative controls with different expected behavior.

Coverage includes parent origin behavior, exact paths and refusals, complete
cuts, child substitution in shared state, opaque guard preservation, captured
views after replacement, borrowed endpoint usability, local return/context
identity, and a pending request's reply and cancellation after reconstruction.
The native duplex suites additionally exercise all four message kinds delegated
unchanged, typed nil refusal in Go, copied construction inputs and parts, exact
UTF-8 ordering, borrowed ownership and a facade which grants no parts. Paired
runtime tests check actual caller cancellation through bound, selected and
reconstructed access after assembler rebinding, including an opaque consumer
guard which passes controls through without another admission check.

The carrier comparisons assume connected, ordered, nonfaulting WebSockets and
use each direction separately; they are Go/Go and TypeScript/TypeScript, not
cross-language pairings. Local association markers are not authentication
evidence. Actual endpoint scheduling, invocation and carrier mapping remain
the runtime's. Plain composition delegates every message unchanged; destinations
and profiles decide validity, and the runtime owns captured invocation controls.

## Generated models across carriers

Bitwire's cases drive handwritten origins. The generated scenarios
[`wire-declared-local`](../scenarios/generated/wire-declared-local.json) and
[`wire-declared-carriers`](../scenarios/generated/wire-declared-carriers.json)
drive a generated Cell model through the same production facilities, one row
of the [declared composition table](../tables/declared-composition.json) at a
time:

- The caller builds a root, then `svc`, then the operation domain the generated
  `Declared` / `declared` spells. Counting consumer guards wrap the root's
  access and `svc`.
- The caller interprets the model with generated `FromWire` over
  `duplex.Through`, so its sends go through declared access and the server's
  callbacks and events reach it on the carrier.

| axis | values |
| --- | --- |
| access | the `svc` node itself; `svc` selected through the root; the same after rebuilding every description from its parts; the same through a local forwarding relay |
| slot | a string; a live unary function; a live factory that takes a callback |
| carrier | the model's own bounded local pair, in one process; a WebSocket; a prepared tunnel channel over a WebSocket |
| languages | Go and TypeScript, the server in either and the caller in either |

Each run holds these, in order:

1. Calls, and the server's callback into the caller.
2. Events both ways.
3. One refused message, after which the model, the access and the carrier are
   still usable.
4. A reply delayed across a complete rebuild and a rebind of `svc` to another
   prefix, while calls through the rebound tree reach the new target.
5. A caller's cancellation that reaches the body and then cancels the body's
   own callback, so it crosses the carrier both ways.
6. Metadata the caller established, observed at the server.
7. Guard checks: one per request or event at each guard crossed, none for a
   cancel.
8. Teardown that releases every live binding to zero and leaves the borrowed
   carrier usable for a fresh interpretation.

**Combinations exercised.**

- *CI* runs the Go star's pairings: Go/Go, Go/TypeScript and TypeScript/Go.
  Each pairing runs 12 local rows on its first side and 24 carrier rows in
  both roles: 60 runs per pairing, 180 in all.
- *Locally*, on 2026-09-24, TypeScript/TypeScript also passed its 60, with all
  four pairings green at 240 runs.
- *Nightly* matrix runs cover every pairing.
- *Not exercised:* the six other ports, which have no declared construction
  API yet (#702), and any carrier fault. The carriers are assumed connected,
  ordered and nonfaulting.
- *What is compared:* a local pair keeps the caller's return capability object.
  A physical hop correlates each reply and cancel with its own request, and the
  scenarios compare observations, not objects.

The structural contract follows Bitwire ADR0006. No tree codec or additional
package is embedded or imported here. Canonical serialization, verified
authority, richer interception, other runtime languages and
BitTree conversion are distinct work. The ordinary
[Bitwire adoption gate](../bitwire/README.md) remains unchanged. These are source
implementation checks; published-package acceptance is tracked separately in
[#703](https://github.com/Bitspark/nightseam/issues/703).
