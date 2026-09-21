# Java runtime

Java 21 implements the `nightseam.duplex/1` core peer over the framed
[duplex seam](../../duplex/java). It preserves JSON member absence separately
from explicit null, validates scalar Unicode and exact decimal tokens, and
supports calls in both directions, events, cancellation, bounded queues,
metadata, trace propagation and declaration identity.

`Peer` owns a `Connection` and exposes `wire()` for relative-path access.
`Wires.forward` and `WirePair` provide local composition; path selection and
mounting live in the duplex package. Handler callbacks receive a
`RequestContext` containing received metadata, trace and cancellation.
Received metadata is never implicitly copied to a reverse call.

`PeerOptions.observer` receives payload-free maps with `type`, epoch-millisecond
`at`, actual byte counts, and nanosecond `duration` or `deadline` measurements.
Observation failures do not interrupt protocol work. A call uses one deadline
across admission and response, and follows its `RequestContext` cancellation.

Build and run native tests and an isolated jar consumer from the repository:

```sh
node conformance/java/build.mjs conformance/java/build --test
```

The equivalent Gradle build starts in `conformance/java` with `gradle check`.
The artifacts are `nightseam-duplex.jar` and `nightseam-runtime.jar` in the
chosen output directory. They require Java 21 and no third-party dependencies.
The private driver and shared fixture files are not runtime dependencies.

This is tier-4 core support. Generator, tunnel, live and observability adapters
are separate future lanes; the conformance matrix records the exact achieved
profiles. No package publication to Maven Central is claimed by this lane.
