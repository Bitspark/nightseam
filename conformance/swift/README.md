# Swift testee

The driver-1 executable lives in
[`NightseamConformance`](NightseamConformance/Package.swift). It drives the native
Swift runtime and portable duplex adapter through the shared scenarios, with
seam/peer layers and listen, pipe, lazy-consumption and propagator features.
It does not implement the observer, generator, live or tunnel profiles.

The suite builds through `testee.json` into its own scratch directory. Swift 6.1.3
is installed in Linux CI. To build directly:

```sh
swift build --package-path conformance/swift/NightseamConformance
```

Stdout contains only flushed JSON answers. Payload members retain the driver's
original JSON bytes until handed to the runtime; results are decoded for the
driver's value comparisons. Reset closes peers, connections and listeners. The
recorded-wire witness is a test-only consumer composition of the production Wire
views and bounded follower queues.
