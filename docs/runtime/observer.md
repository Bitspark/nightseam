# The observer

One observer sees everything a connection carries, at every layer above it.
The peer of the profile takes it; the tunnel and the session declare events
of their own and emit them through the peer they run over, so a consumer
chooses an observer once and is told about frames, channels and sessions
alike. Neither the tunnel nor the session takes an observer of its own,
because that would be a second place to choose one.

What holds of every event, at every layer:

- **It emits and never aggregates, and chooses no backend.** Counting,
  sampling and rate limiting are the observer's, not the peer's.
- **It carries names, ids, sizes, durations, outcomes and close codes, and
  never a payload.** What a method was called, how big its frame was and how
  it ended are observable; what it said is not. The suites hold this by
  putting a sentinel in every payload a peer carries and finding it in
  nothing any event renders.
- **An absent observer is guarded at every call site and costs nothing.**
- **An observer that gives up gives up alone.** It is called on whichever
  goroutine or task the traffic ran on — the reader, a handler, a caller —
  none of them the consumer's to catch on, so a panic in Go and a throw in
  TypeScript are contained where they happen: that event is lost and nothing
  else is, the connection carries on, and the peer reports the failure
  nowhere, having no logger of its own to report it to.
- **Every event names the thing its layer is about, and an event that
  concerns a frame carries that frame's trace.** A frame event names its
  family, a channel event names its family, a session event names its
  session; a connection event and a backpressure event concern no frame and
  name nothing beyond themselves, stated rather than left null.

  | events | names | trace |
  | --- | --- | --- |
  | `ConnectionOpened`, `ConnectionClosed`, `Backpressure` | — | — |
  | the runtime's `FrameSent`, `FrameReceived`, `RequestStarted`, `RequestEnded`, `EventEmitted`, `EventDelivered`, `HandlerPanic` | its family | ✓ |
  | the tunnel's five channel events | its family | — |
  | the session's `AskRaised`, `AskRouted`, `AskAnswered`, `FrameAppended`, `Refused` | its session | ✓ |
  | the session's `SessionBound`, `SessionUnbound`, `SessionAttached`, `SessionDetached`, `ControlChanged` | its session | — |

  A channel is opened *for* a family, so a family is the one thing every
  channel event knows; a session event names the session and never a family,
  the family being the session's own. `Options.Families` in Go and
  `PeerOptions.families` in TypeScript label a method or event name with the
  family it belongs to, which the generated install fills in; a name nobody
  labelled has no family rather than a guessed one.

## Order

An observer's events are **one order per peer**, and each is told before the
effect it names has left the peer:

- `frame.sent` before the bytes reach the transport;
- `frame.received` after the frame is parsed and before it is dispatched;
- `request.started` before the handler runs, and `request.ended` after it
  returns and before the response is queued;
- `event.emitted` before the frame that carries it, and `event.delivered`
  before the listeners run.

What this asks of a runtime is one serialization point per direction: the
writer tells the observer of each send immediately before it writes, and the
reader tells it of each receipt immediately after parsing. A peer that told
the observer where the frame was *queued* instead would have no order at all
— the writer is free to write a queued frame, have it answered and have the
answer read before the queueing goroutine says anything, so a reply could be
observed received before the request that drew it was observed sent. That is
not a theoretical window: it was seen once in a conformance run, and rarely
enough to be worse than often, since a gate that fails on a schedule nobody
can predict invites the loosening of the gate.

So a frame observed sent is one the peer handed to the transport, and a
connection that fails with frames still queued never observed those sent.

The promise is one peer's. Two peers' observers are two orders, and nothing
relates them but a trace.

## Taking one

```go
peer, _, err := runtime.Dial(ctx, url, runtime.DialOptions{
    Options: runtime.Options{Observer: slogobserver.New(logger)},
})
```

```ts
const peer = new DuplexPeer({ observer: consoleObserver() });
```

An observer is one interface with one method, so writing one is short and
none of the adapters is required. Two ship inside the runtime, one per
language: `runtime/go/slogobserver` writes one `slog` record per event, timed
at the event's own instant rather than at the moment the record is written,
and `consoleObserver()` in `@nightseam/runtime` writes one line per event,
taking the four console methods and a clock so that a test captures it with
four functions. Both write an event of a type they do not know — one a layer
above them added — rather than dropping it. A third ships beside the runtime
rather than in it, because it carries a dependency: the OpenTelemetry
adapter, below.

## The OpenTelemetry adapter

`@nightseam/otel` and `github.com/Bitspark/nightseam/otel/go` bind the two
hooks a peer leaves open to OpenTelemetry. It is a package of its own in
TypeScript and a Go module of its own — the only one — so that the four
components above keep the dependency-freedom they publish and a consumer who
chooses no backend installs nothing for one.

```go
import otelns "github.com/Bitspark/nightseam/otel/go"

peer, _, err := runtime.Dial(ctx, url, runtime.DialOptions{Options: runtime.Options{
    Propagator: otelns.Propagator(nil),
    Observer:   otelns.Observer(tracer),
}})
```

```ts
import { observer, propagator } from '@nightseam/otel';

const peer = new DuplexPeer({ propagator: propagator(), observer: observer(tracer) });
```

The two halves are the two hooks and they are taken together. `Propagator`
puts an incoming frame's trace on the context its handler runs with and
writes the span a call was made under onto every frame that call sends,
minting a traceparent of its own wherever OpenTelemetry has nothing to say —
a context with no span, a span that does not record, a span context that is
invalid — because the runtime validates none of what a propagator returns and
the peer at the other end closes the connection on a traceparent that is not
of the profile's form. `Observer` opens a server span where a request came in
and a client span where one went out, named for the method and parented at
what the frame carries, and ends it where the peer says the request ended,
with the outcome as the status and the error code as an attribute; a
connection is a span from the peer taking it over to the close that ended it,
an event emitted or delivered is a span of no duration, and everything else
the three layers tell — frames, backpressure, a handler that gave up, the
tunnel's five and the session's ten — is a span event on the span it belongs
to.

What reaches a backend is what the events carry and no more: a field that is
a name, a count, a flag, a duration or a trace becomes an attribute and a
field of any other kind is dropped rather than rendered, so an event of a
layer the adapter has never heard of reaches a span without a payload
reaching one.

Because the runtime asks a propagator what an outgoing frame carries before
it tells an observer that a request began, a call's client span and the
server span that serves it are siblings, both children of the span that
caused the call; what nests across hops is the work — a handler runs under
its own server span, so what the handler calls is a child of the request that
ran it. Nesting a call under a handler and a handler under the call that
reached it is the whole of what one trace shows.

## The events

The runtime's ten, of the traffic a peer carries:

| Go | TypeScript | what it says |
| --- | --- | --- |
| `ConnectionOpened` | `connection.opened` | a connection is open, and which role this peer took |
| `ConnectionClosed` | `connection.closed` | it ended, under a code and a reason, and whose close it was |
| `FrameSent` | `frame.sent` | a frame went out: its kind, the name it concerns, its size, its id |
| `FrameReceived` | `frame.received` | one arrived, the same way |
| `RequestStarted` | `request.started` | a request began, in either direction |
| `RequestEnded` | `request.ended` | it ended, with its duration, its outcome — ok, error, cancelled or timeout — and the code it failed with |
| `EventEmitted` | `event.emitted` | an event's frame went out |
| `EventDelivered` | `event.delivered` | an event reached the listeners |
| `Backpressure` | `backpressure` | a frame met a full pipe, and again where the queue or a deadline gave out |
| `HandlerPanic` | `handler.panic` | a handler threw, carrying what it threw and never its params |

The tunnel's five, of the channels over one peer:

| Go | TypeScript | what it says |
| --- | --- | --- |
| `ChannelOpened` | `channel.opened` | a channel exists, told once on the side that opened it and once on the side it was opened to |
| `ChannelAccepted` | `channel.accepted` | a channel the other side opened was taken here |
| `ChannelClosed` | `channel.closed` | a channel ended, under its code and reason |
| `CreditStall` | `credit.stall` | a frame found the other side's window exhausted and waits, and how many wait |
| `OpenRefused` | `open.refused` | an open did not become a channel, here or at the side it reached |

The session's ten, of the conversation over a tunnel's channels:

| Go | TypeScript | what it says |
| --- | --- | --- |
| `SessionBound` | `session.bound` | a session is bound on the registry |
| `SessionUnbound` | `session.unbound` | it ended, with the code and reason it ended under |
| `SessionAttached` | `session.attached` | a consumer attached, in its role and from the sequence it held |
| `SessionDetached` | `session.detached` | a consumer detached, however it went |
| `AskRaised` | `ask.raised` | the machine sent what the family asks |
| `AskRouted` | `ask.routed` | it reached a holder of control — and again wherever control moves while it is open |
| `AskAnswered` | `ask.answered` | whoever held control then answered it |
| `ControlChanged` | `control.changed` | control moved to a holder, or to nobody |
| `FrameAppended` | `frame.appended` | a frame took its place in the log, under the consumer that sent it or under none |
| `Refused` | `session.refused` | a consumer's frame was refused, with `not_controlling` or with `busy` |

A refusal is a change and never a frame, because the machine never saw it.

## Observing from a layer of your own

A layer built over a channel reaches the same observer the consumer chose,
rather than being given one: in Go a channel hands back the peer that
carries it, `Channel.Peer()`, and in TypeScript it forwards an event to it,
`channel.observe(event)`. A layer adds its own events the way the tunnel and
the session do — in Go by implementing `ObserverEvent()` on its event types,
in TypeScript by declaring them into `ObserverEvents`:

```ts
declare module '@nightseam/runtime' {
  interface ObserverEvents {
    'work.claimed': { type: 'work.claimed'; at: Date; id: string };
  }
}
```

so that a consumer's `switch (event.type)` stays exhaustive over every layer
it imports.

## The session's changes, told twice

A session's ten events are also the ten changes its registry reports to a
consumer that asks, through `Registry.OnChange(fn)` in Go and
`registry.onChange(fn)` in TypeScript, each handing back the stop that ends
that registration alone. The two are the same facts in two shapes: an
observer is what a peer tells whoever watches its traffic, and a `Change` is
what a registry tells a consumer building its own events on top of a domain
it holds. A change is told before the frame it concerns is handed on, so
nobody watching a consumer's channel sees a refusal or a close ahead of the
change that says it, and a `Change` carries no message and has nowhere to
put one.
