# OpenTelemetry is its own package

**The question.** The runtime leaves two hooks open — a propagator and an
observer — and OpenTelemetry is the obvious thing to bind them to. Does the
runtime bind them?

**Decided.** No. The adapter is a package of its own in TypeScript,
`@nightseam/otel`, and a Go module of its own, `otel/go` — the only nested
module — released by a second tag beside the root's. The four components
beneath depend on nothing on npm, and in Go on one third-party module, the
WebSocket transport. The two halves of the adapter are taken together, and
a consumer that chooses no backend installs nothing for one. [The
observer](../runtime/observer.md#the-opentelemetry-adapter).

**Why.** A backend is the consumer's choice, and a runtime that imported a
tracing library would have made it for every consumer, including the ones
that trace nothing and the ones that trace with something else. The
dependency-freedom of the four components is a thing they publish, and the
cost of keeping it is one more package and one more tag per release — paid
once, by the release procedure, rather than by every consumer's install.
The adapter minting a traceparent of its own wherever OpenTelemetry has
nothing to say is the same decision from the other side: the runtime
validates none of what a propagator returns, so the adapter is held to the
profile's form rather than the runtime being taught the adapter's.

**Serves.** Agnosticism — a peer assumes nothing about what consumes what
it reports.

**Since.** 0.3.0.
