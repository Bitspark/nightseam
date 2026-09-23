# Declared composition: production adoption

`node scripts/declared-conformance.mjs` runs the 27 independently authored
[Bitwire cases](https://github.com/Bitspark/bitwire/blob/671b61d4513c0ba31e8d2f1c813a0552ab0f816a/conformance/declared/cases.json)
against Nightseam's production `ComposeDeclared` / `Declared.compose`, complete
parts, bound access and existing endpoints. It runs both Go and TypeScript on
local pairs and WebSockets in both directions: 162 case executions. The full
Go tier includes `TestDeclaredBitwireConformance`.

[upstream.json](upstream.json) pins the public upstream revision and SHA-256 of
the unmodified [cases.json](cases.json). Updating a pin is an explicit upstream
adoption, not permission to repair an expectation to fit an implementation.
The runner verifies the bytes before preparing inputs. Expected observations
are removed from inputs; a separate judge refuses missing, duplicate, extra,
unknown or mismatched observations. Script tests exercise those refusal paths.

The [Go driver](go/main.go) and [TypeScript driver](../ts/src/declared.ts) adapt
the upstream scenario vocabulary and supplied quota policies. They retain no
replacement routing, composition or decomposition implementation. Policies are
consumer-supplied admission checks; their quota semantics are fixture inputs.
Reconstruction uses the production parts and constructor. Deliberately incorrect
assembly operations remain negative controls: unguarded raw-child access,
duplicate guards, replaced policy state and changed child maps are observed as
different behavior, not silently accepted as equivalent.

Coverage includes parent origin behavior, exact paths and refusals, complete
cuts, child substitution in shared state, policy occurrence order, captured
views after replacement, borrowed endpoint usability, local return/context
identity, and a pending request's reply and cancellation after reconstruction.
The native duplex suites additionally exercise exact fresh-child attachment,
copied construction inputs and concurrent use of a caller-synchronized Go policy.

The carrier comparisons assume connected, ordered, nonfaulting WebSockets and
use each direction separately; they are Go/Go and TypeScript/TypeScript, not
cross-language pairings. Local association markers are not authentication
evidence. Actual endpoint scheduling, invocation and carrier mapping remain
the runtime's. Generic responses/cancellation are refused by the new-admission
entry and use the already captured invocation facilities.

The structural contract follows Bitwire ADR0005. No tree codec or additional
package is embedded or imported here. Canonical serialization, verified
authority, richer interception, other runtime languages and
BitTree conversion are distinct work. The ordinary
[Bitwire adoption gate](../bitwire/README.md) remains unchanged.
