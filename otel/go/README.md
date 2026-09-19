# github.com/Bitspark/nightseam/otel/go

```sh
go get github.com/Bitspark/nightseam/otel/go@latest
```

The OpenTelemetry adapter for Nightseam's Go runtime: a `Propagator` over
an OpenTelemetry text-map propagator — the W3C trace context one by default
— whose every injection is a `traceparent` a peer accepts, and an `Observer`
that opens a server span where a request came in and a client span where
one went out, makes the connection a span and each event a span of no
duration, and records what the three layers tell as span events carrying
only names, counts, flags, durations and traces, never a payload.

It is a module of its own so that `github.com/Bitspark/nightseam` pulls in
no telemetry backend: a consumer who chooses none installs nothing for one.
It requires the root module at its own version and is released by a tag of
its own, `otel/go/vX.Y.Z`, cut beside `vX.Y.Z`.

```go
import (
    "github.com/Bitspark/nightseam/otel/go"
    "github.com/Bitspark/nightseam/runtime/go"
)

peer, err := runtime.NewPeer(ctx, conn, runtime.ClientRole, runtime.Options{
    Propagator: otel.Propagator(nil), // nil: the W3C trace context propagator
    Observer:   otel.Observer(tracer),
})
```

[docs/runtime/observer.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/observer.md)
says what a trace of one call through a relay looks like.

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
