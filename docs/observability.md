# Observing a peer

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
- **An event that concerns a frame carries that frame's trace and the family
  of the name it concerns**; a connection, a channel or a backpressure event
  concerns no frame and carries neither, stated rather than left null.
  `Options.Families` in Go and `PeerOptions.families` in TypeScript label a
  method or event name with the family it belongs to, which the generated
  install fills in; a name nobody labelled has no family rather than a
  guessed one.

## Taking one

```go
peer, _, err := runtime.Dial(ctx, url, runtime.DialOptions{
    Options: runtime.Options{Observer: slogobserver.New(logger)},
})
```

```ts
const peer = new DuplexPeer({ observer: consoleObserver() });
```

An adapter ships for each language and neither is required, an observer
being one interface with one method: `runtime/go/slogobserver` writes one
`slog` record per event, timed at the event's own instant rather than at the
moment the record is written, and `consoleObserver()` in `@nightseam/runtime`
writes one line per event, taking the four console methods and a clock so
that a test captures it with four functions. Both write an event of a type
they do not know — one a layer above them added — rather than dropping it.

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
