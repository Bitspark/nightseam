# Changelog

Versions move in lockstep: the published TypeScript packages, the version the
generator writes into a generated client's manifest, and the Go module's tag
are one number. Entries are in the words of the commits that landed them.

## Unreleased

### Added

- The conformance suite, `conformance/`: one Go runner drives a **testee** per
  language — a peer under remote control, speaking `DRIVER.md`'s JSON lines —
  through scenarios that are data, over a real socket, and holds every
  language to Go's on both sides of the wire. The Go and TypeScript testees
  serve the seam, the peer, the tunnel, the session and the generated
  packages; the six interop gates the runtimes carried became scenarios and
  were retired; the suite's first two catches were a TypeScript observer
  told less than Go's and a TypeScript session log that disagreed with Go's.
- Profiles and tiers, `docs/tiers.md` and `conformance/profiles.json`: the
  four promises a language can make, the five profiles the suite holds a
  language to, and the four tiers that say which a language guarantees and
  which red cell stops a release. The runner places every scenario in a
  profile, holds a testee to its tier, and writes `conformance/matrix.json`;
  the README renders it as the Languages table and CI holds the table to the
  matrix; the release refuses a tag whose matrix the tier table stops; the
  full matrix runs nightly and a failure becomes an issue against the
  scenario. Go and TypeScript are tier 1; Python, Rust, C++ and Haskell are
  the pilot languages, planned for tier 2 and pushed to every profile first
  — C++ and Haskell are in because they are the hardest — and Java and
  Swift follow at 4.
- A dial-only language is held in every role: a scenario no longer says who
  opens the socket but writes `pair.conns`, `pair.peers` or
  `pair.peer_and_conn` on the runner, which expands each from what the
  testees answered `hello` with — the natural side listening when it can,
  otherwise the other side listening a raw connection over which each takes
  a peer of its role. What a scenario needs is held per side, derived from
  each side's steps; `TestDialOnly` runs the star with TypeScript treated as
  unable to listen and allows no skip but that one; every run builds and
  renders under a directory of its own, since two runs may share one
  checkout; and `matrix.json` is written only by a run that held every
  profile for every language.
- `docs/layers.md`, the test for where something new on the wire belongs:
  the profile's only when the peer acts on it; a layer's own vocabulary as
  ordinary frames under a reserved prefix when one layer produces it and
  another reads it; a header when it is a consumer's fact about a call — and
  why a header does not propagate: delivering is the peer's one action on
  it, and a header is a fact about this call, not about the calls a handler
  makes in answering it.
- `meta`, the one header the profile has: a flat string map a request or an
  event may carry about the call rather than in it, delivered to the handler
  beside the payload and read into by nothing, never on a response, never in
  an observer event, `nightseam.`-prefixed keys reserved and refused. Both
  peers, both validators, the generated client and binding of both languages
  pass it through; the session relay forwards it verbatim. The suite holds
  the carriage across the wire and through the relay — both testees take a
  `meta` argument on `peer.call` and `peer.emit` — and the log is held to
  keeping a carriage as it keeps a payload.
- The WebSocket transport negotiates a subprotocol: `Subprotocols` on both
  sides, `SelectSubprotocol` on the server for a browser's ticket, what was
  selected readable on the peer, none offered or selected by default.
- A session binds and attaches over any connection of the seam — `Bind` and
  `Attach` take `duplex.Conn` / `FrameConnection`, a tunnel channel being one
  — so an in-process machine binds over a pipe; a registry-level observer
  serves an up side that runs over no peer.
- A durable log is bound at its head: `Bind` seats the relay's cursor by
  reading the log before the pump starts, so a consumer attaching to a
  session bound over a log that already holds frames is replayed them.
- The OpenTelemetry adapter, the one component a consumer opts into:
  `@nightseam/otel` at `otel/ts` and `github.com/Bitspark/nightseam/otel/go`
  at `otel/go`, a package and a Go module of their own so that the four
  components keep the dependency-freedom they publish and a consumer who
  chooses no backend installs nothing for one. Each binds the two hooks the
  runtime leaves open: a propagator over an OpenTelemetry text-map
  propagator — the W3C trace context one by default — whose every injection
  is a traceparent a peer accepts, minting one where OpenTelemetry has
  nothing to say; and an observer that opens a server span where a request
  came in and a client span where one went out, makes the connection a span
  and each event emitted or delivered a span of no duration, and records
  everything else the three layers tell as a span event, carrying only the
  fields that are a name, a count, a flag, a duration or a trace — so that no
  payload reaches a backend by this path either.
  [docs/observability.md](docs/observability.md) says what a trace of one
  call through a relay looks like.
- The Go module at `otel/go` is released by a second tag, `otel/go/vX.Y.Z`,
  cut after the root tag it requires; `RELEASING.md` says in what order, and
  `.github/dependabot.yml` names it, a `gomod` entry covering one directory
  and never a module nested in it.
- `nightseam version` prints the module version of the running tool — what a
  consumer's `go get -tool` resolved, or `(devel)` from a checkout, a build of
  a tree being exactly what that answer should say — and `nightseam --version`
  prints the same line. A tool built inside a consumer's own module is not
  that module's main package, so what is reported is the version the
  consumer's requirement resolved to rather than the consumer's own, and a
  `replace` pointing at a checkout reports `(devel)`. The generated files name
  no version and keep the header they have: one in it would rewrite every
  generated file of every consumer on every release, which the golden
  discipline would feel immediately, and would fail a `check` over output that
  is otherwise byte-identical — the failure this command exists to explain
  rather than one to add. [docs/generator.md](docs/generator.md) states the
  split.

### Changed

- No legacy: with no released consumer, a change is made directly and whole
  — both peers and the validators and the generator in one lane, emitting
  what they accept in the same commit, every caller changed in the same
  commit — and a design is chosen for being right, never for being cheap to
  roll out; `COLLABORATION.md` says so. It also gains the design issue — a
  question whose answer is the operator's to give, in a Design issue form
  under the `design` label, holding what exists today, the options lettered
  with what each costs, a recommendation marked as whose it is, and the
  verdict copied onto it verbatim before a lane is cut from it — and says
  which of the two ways to commit only one's own work applies when.
- One backpressure rule and one bound on outstanding calls, where the two
  peers had disagreed and `docs/profile.md` had described one of them as
  both: every queue is paced for one write deadline before its consumer is
  called stalled, in both runtimes and on both sides of the connection,
  because a burst that would drain in a second should not end a connection
  — the TypeScript peer paces its outgoing queue where it failed at once
  with `busy`, the Go peer paces its inbound event queue where it yielded
  once and gave up. Pacing an inbound queue is pacing the remote, so
  responses and cancellations on that connection wait behind a full event
  queue, which is what the deadline bounds. Go gains `MaxPendingRequests`,
  default 128, refusing the call past it with `busy` where it stands — no
  frame, no request an observer is told of — as TypeScript always has. The
  *Limits and backpressure* section is rewritten to the one rule, and three
  scenarios under `conformance/scenarios/peer` hold it with either language
  in either seat.
- The composition root is a package: the targets, their configs and their
  order live in `internal/compose`, which `cmd/nightseam` and the
  conformance runner both import, so that the suite renders with exactly the
  tool's targets — it had composed a kernel of its own and drifted to two
  targets where the tool has three, and would have gone on validating output
  the tool no longer produces with nothing saying so. `TestImportDirection`
  refuses a target named anywhere else.
- Every exported name a consumer meets first carries its doc comment, in
  `runtime/go` and in the published TypeScript packages — what pkg.go.dev
  and a `.d.ts` show a reader before anything else.
- The root module's Go directive and the nested module's move together;
  `otel/go` declares 1.26.0 as its dependencies require.
- The npm publish attaches provenance: the release workflow mints a
  short-lived OIDC token, so each package's npm page names the workflow run,
  the commit and the repository its tarball was built from. It refuses to
  publish without it, and the same workflow runs on demand without publishing
  anything — a rehearsal, so that a misconfiguration fails a run somebody
  asked for rather than one that follows a tag which cannot be taken back.

### Fixed

- The seam has a conformance suite in TypeScript too, and three transports run
  it: `duplex/ts/src/conformance.ts`, the twin of `duplex/go/duplextest`, run
  by the in-memory pipe and the WebSocket adapter in `duplex/ts` and by a
  channel in `tunnel/ts`, as the Go suite is run by `duplex/go`,
  `duplex/go/ws` and `tunnel/go`. `@nightseam/duplex` had no test file and no
  test script at all and so published untested, while `pnpm -r test` passed
  over it in silence — a package with no script looking exactly like one whose
  tests pass. The suite holds what a `FrameConnection` promises and nothing of
  the transport keeping it: frames in order and whole either way, every
  listener given every one of them and detaching one leaving the others, a
  close carrying its code and its reason to both sides and refusing what is
  sent after it, and `buffered` counting what a send left behind and nothing
  once the transport took it. Two promises of the Go suite are not the seam's
  in this language and the suite says so rather than asking for them: a
  receive limit, which is the peer's option and the tunnel's here and refused
  with 1009 by each, and an abort, a close being how a connection ends. The
  WebSocket adapter is held to its own mapping besides — the three shapes a
  socket delivers a message in, a Blob read as bytes with what followed it
  waiting behind it, `binaryType`, a close event with no code as the
  registry's 1005, and the detach that follows a close.
  `TestEveryPublishedPackageIsTested` now holds every published package to
  having a test script and to running every test file it has, so that the
  parity rule of COLLABORATION.md is run and not only read.

- An observer that gives up gives up alone in Go, as it already did in
  TypeScript: `Observe` is called on whichever goroutine the traffic ran on —
  the reader, a handler, a caller — and a panic there ended the connection and
  the process with it, where the TypeScript peer had always caught a throw and
  carried on. The Go peer now calls its observer from one place and recovers
  there: that event is lost and nothing else is. The rule is written down for
  both languages, in the `Observer` of each runtime and in
  [docs/observability.md](docs/observability.md).
- A receiver's own deadline answers `cancelled` on the wire in both runtimes:
  when a handler's deadline passed, the Go peer answered the caller
  `cancelled` and the TypeScript peer `request_timeout`, so the code a caller
  saw for one fact depended on which language served it. TypeScript now
  answers what the profile's error table has always said; `request_timeout`
  stays a caller's own error for its own deadline, never a frame anyone
  receives, and the *Requests* section of `docs/profile.md` says so.
- An observer's events are one order per peer, and the writer is the one
  place a send is told: the Go peer observed a frame sent after handing it
  to the writer, so a reply's `frame.received` could precede its request's
  `frame.sent` in an array the suite holds to one order; the send is now
  told by the goroutine that writes, immediately before the bytes leave, and
  the TypeScript peer moves the same way — it told the observer where it
  queued the frame, so a queue holding two had told both before either
  reached the transport. `docs/observability.md` gains an *Order* section
  stating the promise, its cost — one serialization point per direction —
  and what it does not promise; and its trace-and-family rule, wrong about
  two of the three layers where both runtimes had agreed all along, now says
  that every event names the thing its layer is about while an event
  concerning a frame carries that frame's trace.
- The TypeScript tunnel holds the receive window its Go twin holds: a
  channel kept what nobody had listened for in an array with no bound, so a
  sender that ignored the credit it was granted was served rather than
  refused and what a channel could take was the remote's to choose. A
  TypeScript channel now holds one window, the frame beyond it ends the
  channel with 1002, the two languages' reasons match word for word, and a
  channel refused while nobody was listening keeps the close for its first
  listener. Held by a case in each language and by a tunnel scenario that
  writes `channel.frame` on the peer the tunnel runs over, in both pairings.

## 0.2.0

The first version to be published; nothing has been tagged yet. Everything below is in it.

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
