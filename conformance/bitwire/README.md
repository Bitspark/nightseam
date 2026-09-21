# Bitwire adoption evidence

Nightseam's Go and TypeScript Wire surfaces use
[Bitwire v0.1.0](https://github.com/Bitspark/bitwire/releases/tag/v0.1.0), revision
`9f45a2e0e9dc576db34237e5ad3aaaa0266a276b`. Bitspark maintains the shared
contract and both implementations; the code is Apache-2.0. The public artifacts
are Go module `github.com/Bitspark/bitwire@v0.1.0` (package `wire/go`) and
`@bitspark/bitwire@0.1.0` on npmjs. Installation requires no sibling checkout
or private credentials.

Go's `duplex.Wire`, `Message`, `Receiver`, `ReturnAddress`, profile data and
`Code` are aliases of the shared types. TypeScript re-exports the shared types.
The generator's existing duplex references therefore target these actual
declarations. Nightseam owns the implementations, carriers, profile, declaration
identity, value converters, optional authentication and scoped-reference rules.

Run `node scripts/bitwire-conformance.mjs`, or `go test ./conformance/go -run
TestPublishedBitwireConformance -v`. The full Go tier runs this gate. The runner
resolves the versioned Go module, refuses a replacement, checks the revision
and content sum, and reads its independent `conformance/cases/access.json`.
It requires the exact case inventory and compares every emitted observation
with the upstream expected object. Expected results are not copied into this
repository. The drivers are adapted from that release with provenance in each
source file; their types come from the adopted public contract.

Both languages run all ten cases over bounded local pairs, and nine cases in
each role direction over real WebSockets. Those cases exercise selection,
mounting, receiver precedence, return identity, detach and borrowed ownership,
and forwarding through the production runtime. TypeScript uses the production
WebSocket adapter; Go uses its public HTTP handler and dialer.

The explicit physical exception is `distinct-opaque-paths`. Its first unused
registration is an exact `[]` receiver, which a physical Nightseam peer refuses;
the profile requires a nonempty root request/event path. This does not make
`[]` and `[""]` equivalent. The entire unchanged case remains mandatory locally;
the runner permits exactly this physical exception and reports it. Nightseam's
existing physical opaque-path tests separately hold supported segment encoding.

This follows the published contract's
[path admission boundary](https://github.com/Bitspark/bitwire/blob/9f45a2e0e9dc576db34237e5ad3aaaa0266a276b/docs/wire/contract.md#paths)
and [preservation laws](https://github.com/Bitspark/bitwire/blob/9f45a2e0e9dc576db34237e5ad3aaaa0266a276b/docs/wire/contract.md#preservation-laws):
empty selection preserves the peer root's existing request/event refusal.
The exact receiver-registration refusal is the existing Nightseam profile
interpretation, not a universal Bitwire registration requirement. The upstream
[baseline](https://github.com/Bitspark/bitwire/blob/9f45a2e0e9dc576db34237e5ad3aaaa0266a276b/conformance/README.md#what-remains-distinct)
uses local pairs and leaves remote-carrier coverage separate. This gate keeps
that entire mandatory baseline and reports the additional physical coverage's
limit; it introduces no new exception to the shared path or composition laws.

These 56 observations establish the shared composition cases after adoption,
not all profile or live-reference obligations. The ordinary full suite retains
prepared tunnel, checked context, admission/cancellation, live scope and release
coverage. The combined generic acceptance under #370 also retains independent
construction, both converters, callbacks, rollback, guards and installed
data-only consumers. The packed smoke installs the public Bitwire dependency
transitively and checks emitted TypeScript declarations with library checking
enabled; Go rehearsal uses an isolated module cache and a public dependency
proxy.
