# The peer

The peer carries the profile over a physical frame connection: `runtime.Peer` of
`runtime/go` and `DuplexPeer` of `@nightseam/runtime`. This page is its
surface in both languages — each fact once, with both spellings — and what
a consumer needs to know about the order it does things in. What crosses
the wire is [the profile](../wire/profile.md); this page names no rule of
the wire that page does not state.

## The seam beneath

Outgoing values must satisfy the profile's
[Unicode string domain](../wire/profile.md#the-envelope), including keys
and custom JSON output. Go's `runtime.MarshalJSON(value)` applies that
check during encoding; generated codecs use it before their type checks.
`Schema.ValidateValue` also uses it. TypeScript validates the serialized
frame before sending, and generated validators refuse unpaired UTF-16
units in in-memory values and descriptors. A malformed handler response
becomes an `internal` error response rather than a changed value.

A peer runs over a connection of the seam, `duplex.Conn` in Go and
`FrameConnection` in TypeScript — ordered frames both ways, an explicit
close with a code and a reason, and nothing else. Three transports ship:
the WebSocket adapter (`duplex/go/ws`; `webSocketConnection(socket)` of
`@nightseam/duplex`, which adapts a browser or Node socket), an in-memory
pipe (`duplex.Pipe(limit)`; `pipe()`, two connected ends for tests) and a
tunnel's raw `Connection` ([the tunnel](tunnel.md)). Every transport of a language is held to
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

TypeScript supports `PeerOptions.prepare`, run before reads begin, and the
same explicit construction order: a
`DuplexPeer` is made, `handle` and `onEvent` register on it, and `attach` or
`connect` gives it a connection — handlers first, frames second.

Generated adapters use [wire preparation](wire.md#preparing-an-interpretation)
to install receivers before reading while checking identity over the live
carrier. Call `PrepareFromWire` inside Go's `Prepare`, retain its completion
and cleanup functions, then complete after the peer constructor returns.
In TypeScript, call `prepareFromWire(peer.wire(), context, ...bindings)`
before `attach` or `connect`, and await its `complete()` afterwards. Bind
the returned model factory once to release its deferred requests and events.
Preparation cleanup leaves the peer's lifetime with the host. Calling the
combined `FromWire` / `fromWire` after attachment cannot recover an event
that reached the peer before the generated receivers were installed.

## Structured Wire access

`peer.Wire()` / `peer.wire()` returns the peer's root `duplex.Wire` / `Wire`.
The same root shares its queues, correlation and carrier lifetime. Generated
models consume this surface, a prepared tunnel channel, or a local Wire pair.
The complete routing contract is [relative-path wires](wire.md).

| Operation | Go | TypeScript |
| --- | --- | --- |
| send a structured frame | `wire.Send(path, message)` | `wire.send(path, message)` |
| register a receiver | `wire.Receive(path, receiver)` | `wire.receive(path, receiver)` |
| select a relative origin | `duplex.At(wire, prefix)` | `at(wire, prefix)` |
| mount child origins | `duplex.Mount(children)` | `mount(children)` |
| make a bounded local pair | `runtime.NewWirePair(options)` | `wirePair(options)` |
| forward two origins | `runtime.ForwardWire(left, right)` | `forwardWire(left, right)` |
| call, emit, register handlers | `runtime.CallWire`, `EmitWire`, `RegisterWire` | `callWire`, `emitWire`, `registerWire` |

Paths are arrays of Unicode strings. Selection prefixes a path; mounting
consumes one segment to choose a child. Neither allocates a peer, including
on first use. Exact receivers win; a receiver with `Namespace: true` /
`namespace: true` otherwise matches a segment prefix, with the longest match
winning. Callback paths are relative to the Wire on which the receiver was
registered. Registration returns a detach function.

`ForwardWire` / `forwardWire` installs namespace receivers in both directions
and returns a detach function. It preserves the frame and local return
capability. It adds no channel, serialization or peer. The native profile
frames and their physical path encoding are [the profile's](../wire/profile.md#relative-paths-on-a-wire).

A local pair uses the existing structured request and return machinery. Its
bounded queues drain asynchronously; TypeScript awaits each event callback,
and Go drains event callbacks serially. Pending and handler reservations last
through response completion, including a cancelled handler that has not
settled. It creates no frame connection or `Peer`. `Send` admits or refuses
without running an application body in the caller. A return from an event
send acknowledges admission only. Ordering belongs to each Wire root; mounting
independent roots does not impose a shared scheduler or a global order.

Closing a selected view closes its underlying Wire. Closing a mount detaches
its registrations and leaves its children open. A detach from forwarding
leaves both roots open. These carrier operations do not release a live owner.
Keep the host's explicit lifecycle alongside the model session.

Verified request and event context accompanies local delivery through
selection, mounting, forwarding and a local pair. It is not reconstructed
from payload or metadata and does not cross a physical outgoing hop as local
authority. Consumer checks still decide whether an effect is allowed.

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
`webSocketFactory` (a socket of the platform's own), `subprotocols`, `prepare`,
`propagator`, `onError`, `observer` and `families`. `Families` labels a
method or event name with the family it belongs to, for the observer's
sake. Generated Wire adapters label their own operation observations with the
declared family independently of a physical host's labels. Cancellation in TypeScript aborts a
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
description the protocol package embeds: `runtime.NewSchema(wire, digest, imported)`
and `MustSchema` in Go, with `ValidateRaw`, `ValidateExpressionRaw` and
`ValidateValue`; `createValidator(description, digest, imported)` in TypeScript, which
validates calls, replies, reverse calls and events alike. Both are held to
one conformance table, `conformance/tables/validator.json`. `Optional[T]` and
`Nullable[T]` in Go carry presence and nullness as the two facts the
declaration keeps apart; `Raw` passes a payload through unread, which is
what a relay wants.

The descriptor is `{"types": {...}, "parameters": [...]}`: the types keep
the declaration's expressions and each parameter keeps its `name` and
optional `of`. Imports are `map[string]*runtime.Schema` in Go and a map of
validators returned by `createValidator` in TypeScript. They retain the
declaring family, so an argument to an imported generic resolves names in
the caller's scope. Only the family parameters used by that imported type
need arguments; a local application fills the type's own parameters.

Both validators read nonempty string literals, nullable expressions, inline shapes,
adjacently tagged unions and nested applications of either parameter sort.
Every payload, including a record, map, arbitrary JSON or null, is carried
whole under the union's `value` member; a payload-free `{empty: true}` arm
carries the tag alone. An empty record is a payload and remains wrapped.
An extending union accepts its base's
variants, and the base refuses the added variants. Nullable values do not
make required fields optional.
An inheritance edge uses a plain name for a nongeneric base or an explicit
`{apply, with}` expression for a generic base. Arguments keep their lexical
scope, so a base parameter can be fixed, renamed or forwarded without
binding two declarations merely because their parameters share a name.

Go's `schema.Bind(types, families)` supplies bindings for raw validation and
returns a new schema. TypeScript's fourth validator argument is a `Slots`
map: a family binding has `name` and `validate`, while a type binding has
`type` and `validate`, the validator of the family where that expression
belongs. An explicit application validates its arguments in both runtimes.
An unbound family slot in a generated Go generic codec is checked by the
instantiated Go type during marshal or unmarshal; TypeScript requires its
runtime binding.

Go's `TypeArgument[T]()` supplies an instantiated type automatically. Bind
its result under the declaration's parameter name, or under a drawn name
such as `S.Envelope`. Drawn bindings also supply the family scope needed
when an expression forwards that parameter to another family. A
`TypeBinding{Schema, Type}` retains an expression's original family;
`WireType() TypeBinding` metadata retains a named declaration's literals
and constraints. Discovery is lazy, so recursive generated types need no
codec registry. Reflection stays inside the runtime; generated callers
only bind their concrete Go type arguments. Primitive and container
arguments retain their ordinary wire kinds, and pointers and `Nullable[T]`
retain nullness. A nil map or slice does not become nullable merely because
Go's JSON decoder accepts null into it.

A runtime also checks a descriptor's pattern syntax, including constraints
inside inline shapes and application arguments. An absent optional member
or an empty collection cannot hide a forbidden pattern. The Go declaration
checker and runtime share the Unicode parser and engine translation; both
runtimes read the same syntax and value conformance rows. TypeScript uses
ECMAScript `u` mode. Go gives `\s`, `\d`, `\w`, their complements, word
boundaries and dot the same meanings, including NBSP whitespace and
code-point matching for emoji. The full tier compares the translation to
Node's Unicode engine over both syntax refusals and matching values.

## Observing it

`Options.Observer` in Go and `PeerOptions.observer` in TypeScript take one
observer — an interface of one method — and tell it ten things about the
traffic the peer carries, never a payload. A layer over the peer reaches the
same observer through `peer.Observer()` and `peer.Observe(event)` in Go, and
by declaring its events into `ObserverEvents` in TypeScript. [The
observer](observer.md) has the rule, the adapters and every event of every
layer.
