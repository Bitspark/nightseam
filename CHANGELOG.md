# Changelog

Versions move in lockstep: the four TypeScript packages, the version the
generator writes into a generated client's manifest, and the Go module's tag
are one number. Entries are in the words of the commits that landed them.

## Unreleased

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
