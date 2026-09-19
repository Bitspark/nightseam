# @nightseam/otel

```sh
npm install @nightseam/otel
```

The OpenTelemetry adapter for Nightseam: the propagator that puts a trace on
the wire and the observer that turns what a peer sees into spans. It is a
package of its own so that `@nightseam/duplex`, `@nightseam/runtime`,
`@nightseam/tunnel` and `@nightseam/session` stay free of every dependency —
a backend is the consumer's choice, and a consumer that makes none installs
nothing for it.

```ts
import { DuplexPeer } from '@nightseam/runtime';
import { observer, propagator } from '@nightseam/otel';
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('my-service');
const peer = new DuplexPeer({ propagator: propagator(), observer: observer(tracer) });
```

Every peer takes both: the propagator decides what a frame carries, the
observer decides what a backend is shown, and a generated client passes them
through its `dial`, `attach` and `open` like any other peer option.

## The two

`propagator(textMap?)` reads an incoming frame's `traceparent` and
`tracestate` onto the request context its handler runs with, and writes the
span a call was made under onto every frame the call sends.
`W3CTraceContextPropagator` from `@opentelemetry/core` is the default and is
the profile's own form; replace it only to carry a vendor's members beside the
two standard ones.

`observer(tracer)` opens a span per request — a server span where the request
came in, a client span where it went out — names it for the method, ends it at
the outcome, and records everything else as a span event on the span it
belongs to: the frames of an exchange, an event emitted or delivered, a
connection closed with its code, and the tunnel's and the session's events
where the peer has the span they concern. An observer belongs to one peer, as
it does in the runtime, so the tunnel and the session running over that peer
reach the same tracer without being given one.

## What a span carries

The attributes of a span are the fields of the events it was made from, under
`nightseam.`, and only those that are a string, a number or a boolean:
`nightseam.method`, `nightseam.id`, `nightseam.family`, `nightseam.bytes`,
`nightseam.outcome`, `nightseam.errorCode` and the rest. A payload reaches no
observer by any path and so reaches no span; a structure is never written at
all, which is the one shape one could have arrived in from a layer declared
after this package. How a request ended is the span's status — `OK`, or
`ERROR` carrying the error code, or the outcome where there is none.

## What the propagator guarantees

**Every injection carries a `traceparent` the remote peer accepts:**
`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`, the one form of the
profile. The runtime does not validate what a propagator returns, and a frame
whose member is anything else is refused by the decoder at the other end — so
where there is no span to carry, where the span context is invalid, where
tracing is suppressed, or where a text-map propagator writes nothing or writes
some other form, this adapter mints a trace of its own rather than returning
nothing. The span it opens for such a frame belongs to that trace, so a call
made outside every span is still one trace end to end.

An injection made from no request context reads `context.active()`, which
means what your own spans mean only where a context manager keeps it: register
one — `AsyncLocalStorageContextManager` on Node — as you would for any other
instrumentation.

## The shape of a trace

The runtime asks the propagator what an outgoing frame carries before it tells
the observer the request began, so what a frame names is the span the call was
made under: the client span of a call and the server span of the handler that
serves it are siblings under it, and the nesting is between hops. One call
from a consumer through a session relay to a machine handler that asks the
holder of control, answered:

```
consumer.call                     the application's own span
├── echo    CLIENT                the consumer's call
└── echo    SERVER                the machine's handler, over the relay
    ├── reverse CLIENT            the ask the handler raises
    └── reverse SERVER            the holder's answer
```

The relay forwards `traceparent` and `tracestate` byte for byte while
re-minting request ids, so one trace spans every hop of a session.

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
