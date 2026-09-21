// Package grant is the rooted, attenuating grant of the authority profile:
// an Archon envelope in the nightseam-grant/1 domain around a canonical
// body, the chain rules and their refusals, issuance with inherit, and the
// pure evaluator — Inspect, Verify, Issue — that both verifiers export.
// What it is is specified in docs/auth/grant.md and held to the 72 cases of
// conformance/tables/auth-grant.json, byte for byte. It depends on Archon's
// core and sdk for the envelope, domain signing and key derivation, and on
// nothing else: no I/O, no clock of its own, no state.
package grant
