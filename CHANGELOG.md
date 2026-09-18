# Changelog

Versions move in lockstep: the published TypeScript packages, the version the
generator writes into a generated client's manifest, and the Go module's tag
are one number. Entries are in the words of the commits that landed them.

## Unreleased

### Fixed

- An observer that gives up gives up alone in Go, as it already did in
  TypeScript: `Observe` is called on whichever goroutine the traffic ran on —
  the reader, a handler, a caller — and a panic there ended the connection and
  the process with it, where the TypeScript peer had always caught a throw and
  carried on. The Go peer now calls its observer from one place and recovers
  there: that event is lost and nothing else is. The rule is written down for
  both languages, in the `Observer` of each runtime and in
  [docs/observability.md](docs/observability.md).

## 0.2.0

The first published version. Everything below is in it.

### The declaration language and the generator

- A family is a directory of tier files under `api/contracts/<family>/`:
  `model.json`, `protocol.json`, `session.json`, and `go.json` and
  `typescript.json` for names the convention would spell otherwise. A
  declaration refers to its own tier or a lower one, never a higher one.
- A family declares the parameters it is generic in and a type draws on one
  — `S.Envelope`, `S.Handle`, `S.Payload` — and is rendered once,
  generically, in both languages; a parameter is bound where the generated
  code is instantiated, to any family that declares its role. A generic type
  of an imported family is applied explicitly, `{"apply": …, "with": …}`.
  The two ways to a concrete package — binding into the declaration, or
  rendering generically and instantiating — are held to agree by the
  fixtures, in Go by reflection and in TypeScript under `tsc`.
- `nightseam generate`, `check`, `validate` and `init`; the
  generated Go packages `-protocol`, `-binding` and `-client` and the
  TypeScript client; a `spec` target rendering each family's specification
  as Markdown; the public errors a family declares reaching both languages
  by name; the wire validator living in each runtime, held to one
  conformance table.
- A corpus of families with every target's output held as goldens, the
  exported Go surface held file for file, and one checkout per refused rule
  with its diagnostics.

### The runtimes

- `duplex`: the seam beneath every protocol — a frames duplex connection with
  an explicit close carrying a code and a reason — with the WebSocket
  transport, an in-memory pipe, and the conformance suite every transport is
  held to.
- `runtime`: the peer of the `nightseam.duplex/1` profile — requests,
  responses, events and cancellation, bounded queues and backpressure,
  presence types, the HTTP upgrade in Go, the wire validator — and W3C trace
  context carried on every kind of envelope and propagated: an incoming
  frame's trace reaches its handler's context, what the handler sends carries
  a child of it, a response its request's, with a propagator hook and a
  default that mints ids without a tracing library.
- `tunnel`: channels multiplexed over one peer, each a connection of the
  seam held to the same suite as the WebSocket and the pipe; per-channel
  credit; ids chosen by the opener, odd for the client and even for the
  server; the Go and TypeScript tunnels held to each other over a socket.
- `session`: a session as a thing that outlives connections — one up channel,
  any number of down channels in a role each, one holder of control, an ask
  routed to the holder and following a transfer, ids minted per session, a
  log replayed from a sequence before anything live — held to one suite in
  both languages.
- The generated code runs over any connection of the seam: `Attach` and
  `Serve` beside `Dial` and `NewHandler`, `Open` resolving a handle on a
  tunnel, `Client.attach` and `Client.open` in TypeScript.

### Observability

- One observer sees every layer. The peer takes an `Observer` of one method
  — `Options.Observer` in Go, `PeerOptions.observer` in TypeScript — and is
  told ten things about the traffic it carries: a connection opened and
  closed, a frame sent and received, a request started and ended with its
  duration and its outcome, an event emitted and delivered, backpressure, and
  a handler that threw. The tunnel adds five of its channels and the session
  ten of its conversation, emitted through the peer they run over rather than
  through an observer of their own, so a consumer chooses one and sees all
  three layers. A peer emits and never aggregates and chooses no backend, an
  absent observer costs nothing, and no event carries a payload — held by a
  sentinel in every payload a peer carries appearing in nothing any event
  renders.
- Every event that concerns a frame carries that frame's trace and the family
  of the name it concerns; the generated install labels each of a family's
  methods and events with the family, and a name nobody labelled has no
  family rather than a guessed one.
- Two adapters, one per language: `runtime/go/slogobserver` writes one `slog`
  record per event at the event's own instant, and `consoleObserver()` in
  `@nightseam/runtime` writes one line per event through four console methods
  a test can capture. Both write an event of a type they do not know.
- A layer of your own joins the same stream — in Go by implementing
  `ObserverEvent()`, in TypeScript by declaring into `ObserverEvents` so that
  a `switch (event.type)` stays exhaustive — and reaches the observer through
  `Channel.Peer()` in Go or `channel.observe(event)` in TypeScript.
- The session's registry reports the same ten facts as domain changes to a
  consumer that asks: `Registry.OnChange(fn)` in Go and `registry.onChange(fn)`
  in TypeScript, each handing back the stop that ends that registration alone,
  each change told before the frame it concerns is handed on.
