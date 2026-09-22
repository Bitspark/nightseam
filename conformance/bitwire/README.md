# Bitwire adoption evidence

All eight Nightseam language ports import the declarations of
[Bitwire v0.2.0](https://github.com/Bitspark/bitwire/releases/tag/v0.2.0), revision
`616a2fc5e3a0972f67f40331a9d9ca102bc9698d`. The public artifacts are Go module
`github.com/Bitspark/bitwire@v0.2.0` (package `wire/go`) and
`@bitspark/bitwire@0.2.0` on npmjs. Installation requires no sibling checkout
or private credentials. The [wire documentation](../../docs/runtime/wire.md) lists the other six
package coordinates. No port aliases or redeclares the shared Wire, Endpoint,
Message, Receiver or return capability. Native suites exercise their direct
use; the independent upstream observation gate below currently covers Go and
TypeScript.

Run `node scripts/bitwire-conformance.mjs`, or `go test ./conformance/go -run
TestPublishedBitwireConformance -v`. The full Go tier runs this gate. It resolves
the public Go dependency, refuses a replacement, verifies the immutable revision
and content sum, and reads the release's
[composition oracle](https://github.com/Bitspark/bitwire/blob/616a2fc5e3a0972f67f40331a9d9ca102bc9698d/conformance/reference/expected.json).
Expected behavioral results remain upstream. The drivers execute Nightseam's
production pair, peer, dispatcher, selected endpoint, mount and forwarder; they
contain no replacement delivery scheduler, endpoint, routing or correlation
implementation. Upstream reference implementations are never executed by this gate.

Both languages run all six observation groups locally and in each role direction
over real WebSockets. There are no carrier-specific case exclusions. The groups
hold single receive ownership; sibling and overlapping selected views; nested
selection, mounting and forwarding; opaque paths; view closure, detach and
rebind; and delayed captured replies after receiver teardown. Detachment and
view closure leave borrowed endpoints usable. Delivery barriers and bounded
waits use actual callbacks, rather than sleeps or assumed event-loop turns.

The one explicit profile instantiation is the reference's request identifier
`same-id`: it is outside Nightseam's existing `c:`/`s:` plus decimal grammar.
Both independent return capabilities therefore submit the identical valid
identifier `c:1`. The gate first asserts the upstream oracle's two `same-id`
labels and substitutes only these two echoed identifiers with `c:1`; every
other observation is compared unchanged. This is simultaneous independent
return-scope coverage, not evidence that reuse on one physical connection is
safe. The physical peer mints its own correlation identifiers.

Frame and return-capability preservation are measured around pure composition
boundaries: outgoing nested selection/mounting, the forwarding boundary and
receiving selection. The request includes nested payload, traceparent,
tracestate and metadata. A new carrier may remap correlation and return access;
the driver never claims object identity across such a boundary. The association
observation uses a test-owned marker keyed by the admitted Return capability;
it establishes preservation of that association, not authenticity of verified
runtime context. Go's asynchronous-delivery observation compares the callback's
goroutine with the sender's; concurrent delivery on another goroutine is valid.

The released Bitwire `conformance/cases/access.json` and historical Nightseam
driver are explicitly **0.1.0 historical evidence**. Their registration primitive
and selected-view-close semantics are not obligations on the 0.2 contract.
This gate replaces that baseline with the current released composition oracle
rather than relabeling the old cases or running a test-only reference as proof
of adoption.

These observations do not establish the full invocation lifecycle contract,
queued or newly arriving cancellation races, bounded retirement, independently
implemented profile facilities, verified context, generated generic substitution,
or live-reference release. Those require the runtime, generated-consumer and
ordinary full conformance suites. In particular, delayed replies here are not
signoff for the public lifecycle requirements of Nightseam #439 and Bitwire #20.

Those live elsewhere in this tree, and they are what #439 delivered: the
invocation lifecycle is [a vocabulary spoken at the request's own return
capability](../../docs/runtime/wire.md#the-invocation-lifecycle), held in
`runtime/go/invocation_test.go` and `invocation_experiment_test.go` and their
TypeScript twins — two independently authored same-profile endpoint
integrations and an opaque forwarding wrapper, through public facilities alone
and with no shared private ledger. The identity discipline the lifecycle
relies on at a carrier boundary is `conformance/tables/serials.json` and the
two scenarios that send it.
