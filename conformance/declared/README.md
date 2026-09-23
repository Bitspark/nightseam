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

The structural contract follows Bitwire ADR0006. No tree codec or additional
package is embedded or imported here. Canonical serialization, verified
authority, richer interception, other runtime languages and
BitTree conversion are distinct work. The ordinary
[Bitwire adoption gate](../bitwire/README.md) remains unchanged. These are source
implementation checks; published-package acceptance is tracked separately in
[#703](https://github.com/Bitspark/nightseam/issues/703).
