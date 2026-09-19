# The peer

The peer of the profile is what a generated package binds to and what a
consumer holds when it speaks the profile without one: `runtime.Peer` of
`runtime/go` and `DuplexPeer` of `@nightseam/runtime`. This page is its
surface in both languages — each fact once, with both spellings — and what
a consumer needs to know about the order it does things in. What crosses
the wire is [the profile](../wire/profile.md); this page names no rule of
the wire that page does not state.

## The seam beneath

A peer runs over a connection of the seam, `duplex.Conn` in Go and
`FrameConnection` in TypeScript — ordered frames both ways, an explicit
close with a code and a reason, and nothing else. Three transports ship:
the WebSocket adapter (`duplex/go/ws`; `webSocketConnection(socket)` of
`@nightseam/duplex`, which adapts a browser or Node socket), an in-memory
pipe (`duplex.Pipe(limit)`; `pipe()`, two connected ends for tests) and a
tunnel channel ([the tunnel](tunnel.md)). Every transport of a language is held to
the seam's conformance suite — `duplex/go/duplextest` and
`duplex/ts/src/conformance.ts`, each run by the pipe, the WebSocket adapter
and the tunnel channel — so a peer runs over any of them unchanged. To run
one over a transport of your own, implement the seam: in TypeScript
`state`, `buffered`, `send`, `close` and `listen`, the peer sending its next
frame only once `buffered` reads zero.

## Making one

| | Go | TypeScript |
| --- | --- | --- |
| over a socket, as the client | `runtime.Dial(ctx, url, runtime.DialOptions{…})` → `(*Peer, *http.Response, error)` | `const peer = new DuplexPeer(options); await peer.connect(url)` |
| behind an HTTP server | `runtime.NewHandler(runtime.ServerOptions{…})` → an `http.Handler`; or `runtime.Accept(w, r, options)` inside one of your own | — a browser is never the server; Node serves through `attach` below |
| over any connection of the seam | `runtime.NewPeer(ctx, conn, role, options)` | `await peer.attach(connection)`, with `role: 'server'` in the options where this side is the server |
| ending it | `peer.Close()`; `peer.Done()` closes and `peer.Err()` says why | `peer.close()`; `peer.onClose(listener)` |

A peer speaks and serves at once: `Call` and `Emit` go out, `Handle` and
`HandleEvent` (Go) — `call` and `emit`, `handle` and `onEvent` (TypeScript)
— take what comes in. A handler in Go is `func(ctx, *Peer, json.RawMessage)
(any, error)`; in TypeScript it is given `(params, context)` where the
context carries `signal`, `peer`, `requestId`, `trace` and `meta`. A
`dispatch` option in TypeScript is the fallback for methods with no handler.
In TypeScript a peer that already has a connection refuses another —
`attach` and `connect` reject `already_connected` — rather than listening
twice.

## When a peer starts reading

A peer reads the connection from the moment it exists, so what it must serve
is installed before it exists, not after. Both Go constructors run in one
order: the options are taken, **`Prepare` runs on the peer**, the read and
write loops start, and only then does the caller get the peer — `Accept`
calls `OnConnect`, `Dial` returns. `Prepare` is `runtime.Options.Prepare`,
reached through the embedded `Options` of `ServerOptions` and `DialOptions`,
and it holds a peer nothing has reached: `Handle`, `HandleEvent` and a
`tunnel.New` over the peer cannot miss a frame there. An error from it fails
the construction — no peer is returned, and a socket the handshake already
answered is closed with 1008 rather than left open in silence.

`OnConnect` is the other half and means what it always meant: the peer is
live, has read frames and may have answered them. A handler installed there
is installed on a peer the other side may already have called, which is
`method_not_found` for the first request of a consumer that opens a channel
the moment it sees the `101`. Install in `Prepare`, use in `OnConnect`.

TypeScript has the same order by construction rather than by a hook: a
`DuplexPeer` is made, `handle` and `onEvent` register on it, and `attach` or
`connect` gives it a connection — handlers first, frames second.

Generated clients take a typed `Events` value at `Dial`, `Attach` and `Open`
(TypeScript `dial`, `attach` and `open`). Go installs it in `Prepare`, before
running a caller-supplied `Prepare`; TypeScript installs it in the client
constructor before connecting. An empty value leaves events unhandled.
`OnX`/`onX` remain available for later registration, but a handler registered
after the flow producing its events began may miss earlier events.

## Options and limits

Every bound of [the profile](../wire/profile.md#limits-and-backpressure) is
one option under one name in both languages:

| Go | TypeScript | default | what it bounds |
| --- | --- | --- | --- |
| `MaxConcurrentHandlers` | `maxConcurrentHandlers` | 64 | requests being handled at once; the one past it is answered `busy` |
| `MaxPendingRequests` | `maxPendingRequests` | 128 | calls outstanding at once; the one past it is refused `busy` where it stands |
| `QueueCapacity` | `queueCapacity` | 128 | outgoing frames, and events waiting for their handlers |
| `MaxFrameBytes` | `maxFrameBytes` | 1 MiB | a received frame; a larger one ends the connection |
| `RequestTimeout` | `requestTimeoutMs` | 30s | a call's own deadline, past which it fails `request_timeout` |
| `WriteTimeout` | `writeTimeoutMs` | 10s | how long a full queue is paced before its consumer is stalled |
| `ConnectTimeout` | `connectTimeoutMs` | 30s | the handshake; a dial past it is refused `connect_timeout` |

The first six are the peer's — `runtime.Options` in Go, `PeerOptions` in
TypeScript — and the last is the dial's, `runtime.DialOptions` in Go and the
same `PeerOptions` in TypeScript, a browser peer having no context to carry
it. Go takes a `time.Duration` where TypeScript counts milliseconds, and a
zero in Go selects the default where TypeScript's options take positive
integers; a negative one is refused in either. `peer.MaxFrameBytes()` reads
the one bound a layer above needs to know.

Beside the bounds, `runtime.Options` carries `Handlers` and `Events` (what
to serve, installed before the first frame), `Prepare`, `Propagator`,
`Observer` and `Families`; `PeerOptions` carries `role`, `dispatch`,
`webSocketFactory` (a socket of the platform's own), `subprotocols`,
`propagator`, `onError`, `observer` and `families`. `Families` labels a
method or event name with the family it belongs to, for the observer's
sake; the generated install fills it in. Cancellation in TypeScript aborts a
handler's signal but cannot interrupt running JavaScript, so a cancelled
handler keeps its slot until it settles.

## The subprotocol

The surface is the transport's, on both sides: `ServerOptions.Subprotocols`
and `DialOptions.Subprotocols` in Go, `PeerOptions.subprotocols` in
TypeScript, and what was selected is `peer.Subprotocol()` and
`peer.subprotocol`, `""` when none was. A ticket is not a list, so the
server's selection may be a function of the request:
`ServerOptions.SelectSubprotocol` answers with the one token to select out
of what that request offered, `""` for none, which is how a ticket comes
back unchanged. What the profile says about offering and selecting — one
decision, taken on both sides together — is [the wire's](../wire/profile.md#the-subprotocol).

## The server's hooks

`ServerOptions` in Go carries what the upgrade decides before the profile
begins, and two of them are **required** — a server is refused rather than
made without explicit policies: `Authenticate func(*http.Request)
(context.Context, error)`, whose context is the peer's and whose error
answers the request 401 and upgrades nothing; `CheckOrigin
func(*http.Request) bool`, whose refusal upgrades nothing either; and
`OnConnect`, above. The profile has no authentication of its own — a
connection is authenticated by whatever opened it — and a browser client's
ticket travels as a subprotocol token, which `SelectSubprotocol` reads off
the request like everything else.

## Errors

A handler's own error crosses the wire only when it is a public one:
`*runtime.PublicError{Code, Message, Data}` in Go, `new DuplexError(code,
message, data?)` in TypeScript. Any other error a handler returns or throws
becomes `internal` on the wire, its message withheld. The family's declared
errors are these, by name, in the generated packages — a constant per error
and `IsError(err, code)` in Go, `errors.<name>` and the `ErrorCode` union in
TypeScript ([the generated packages](../declaration/generated.md)). The
peer's own codes — `method_not_found`, `busy`, `cancelled`,
`request_timeout`, `internal`, and `connect_timeout` and
`already_connected` which never cross the wire — are the
[profile's](../wire/profile.md#requests).

## Request metadata

A sender says what a frame carries: `runtime.WithMeta(ctx, map[string]string)`
in Go and `{ meta }` on a `call` or an `emit` in TypeScript say what the next
frame takes, and `runtime.MetaFrom(ctx)` and a handler's `context.meta` are
what arrived — `MetaFrom` returns a copy. A handler's own calls carry none of
it unless the handler says so — `WithMeta(ctx, MetaFrom(ctx))`, or `{ meta:
context.meta }`. A reserved key given to either is dropped rather than sent;
the form, and why a header does not propagate, are [the
wire's](../wire/profile.md#request-metadata).

## Trace context and the propagator

An incoming request's or event's trace is placed on the context its handler
runs with — `runtime.TraceOf(ctx)` in Go, `context.trace` in TypeScript — and
a call or emit made from that context carries a child of it. The default
propagator mints W3C ids with the platform's random source and needs no
tracing library; `Options.Propagator` in Go and `PeerOptions.propagator` in
TypeScript replace it with an adapter for one, of which [the OpenTelemetry
adapter](observer.md#the-opentelemetry-adapter) is the one that ships.

## The validator

The wire validator lives in each runtime, once, and reads the family's wire
description the protocol package embeds: `runtime.NewSchema(wire, imported)`
and `MustSchema` in Go, with `ValidateRaw`, `ValidateExpressionRaw` and
`ValidateValue`; `createValidator(types, imported)` in TypeScript, which
validates calls, replies, reverse calls and events alike. Both are held to
one conformance table, `conformance/tables/validator.json`. `Optional[T]` and
`Nullable[T]` in Go carry presence and nullness as the two facts the
declaration keeps apart; `Raw` passes a payload through unread, which is
what a relay wants.

## Observing it

`Options.Observer` in Go and `PeerOptions.observer` in TypeScript take one
observer — an interface of one method — and tell it ten things about the
traffic the peer carries, never a payload. A layer over the peer reaches the
same observer through `peer.Observer()` and `peer.Observe(event)` in Go, and
by declaring its events into `ObserverEvents` in TypeScript. [The
observer](observer.md) has the rule, the adapters and every event of every
layer.
