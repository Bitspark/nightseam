# The generated packages

What comes out of the generator for a family, in each language: the names a
consumer writes against. This page is drawn from the probe family of the
generator's own corpus, whose exported Go surface is held file for file
under `cmd/nightseam/testdata/surface` and whose TypeScript client is a
golden under `cmd/nightseam/testdata/golden`; a family of your own renders
the same shapes under its own names. [A family in tiers](families.md) is
the input, [the generator](generator.md) the tool, and the specification
beside them, `api/spec/<f>/README.md`, is the family's own reference.

## Go: three packages

`api/go/<f>-protocol`, `-binding` and `-client`, each owned wholesale. A
consumer imports the protocol package for the types, serves the binding
package's `Handler`, and calls through the client package.

### The protocol package

One Go type per declared type, in wire order, with the two the family
carries from the built-in `duplex` family:

| declared | rendered |
| --- | --- |
| a record | a struct with `json` tags; a field not `required` is `runtime.Optional[T]`, one `nullable` is `runtime.Nullable[T]`, both where it is both; an `open` record keeps what it did not declare in `AdditionalFields map[string]json.RawMessage` |
| an enum | a string type with one constant per value, `StatusReady Status = "ready"` |
| an alias | a named type over the expression, `type Payloads []Payload` |
| a `map` | `map[string]T` |
| `duplex.Envelope`, `duplex.Handle` | one message of the profile, and a reference to a channel that speaks it, `Handle{Channel int64}` — carried, not declared ([a tier is a built-in family](../decisions/a-tier-is-a-built-in-family.md)) |

Every record marshals and unmarshals itself, keeping presence and nullness
apart on the way through, and every record and enum returns the package's
`Tag` from `Of()` — which is how a generic type of another family is held
to what fills it ([generics](generics.md#how-each-language-instantiates-it)).
A `timestamp` is `time.Time`, an `integer` `int64`, a `number` `float64`,
`json` `any`.

The family's public errors are a constant per error, `ErrorNotFound =
"not_found"`, the list `Errors`, and `IsError(err error, code string) bool`,
which answers for a `*runtime.PublicError` a call returned. The wire
description the validator reads is embedded here: `ValidateRaw(name, data,
at...)`, `ValidateExpressionRaw`, `ValidateValue` and `MustTypeExpression`
validate a value against a declared type, and are what the binding and the
client call on every frame.

### The binding package

What a server implements and serves:

```go
type Handler interface {
	Echo(ctx context.Context, remote *Remote, params protocol.Payload) (protocol.Payload, error)
	NoArgs(ctx context.Context, remote *Remote) (string, error)
	Seen(ctx context.Context, remote *Remote, params protocol.Seen) (protocol.Payloads, error)
}
type Remote struct{ Peer *runtime.Peer }
func (*Remote) Reverse(ctx context.Context, params protocol.Payload) (protocol.Payload, error)
func (*Remote) EmitChanged(ctx context.Context, data protocol.Payload) error
func (*Remote) OnNoticed(handler func(context.Context, protocol.Seen)) error
func NewHandler(handler Handler, options runtime.ServerOptions) (http.Handler, error)
func Serve(ctx context.Context, conn duplex.Conn, options runtime.Options, handler Handler) (*runtime.Peer, error)
```

`Handler` is the server side of the protocol — one method per server
method, each given the `Remote` — and `Remote` is the client side as the
server sees it: the client's methods to call, the server's events to emit,
the client's events to listen for. `NewHandler` serves the family at an
HTTP endpoint with the handlers installed before the peer reads its first
frame; `Serve` speaks it over any connection of the seam. `nightseam init`
writes a `Handler` with every method returning `unimplemented`, once, under
`api/impl/<f>`.

### The client package

```go
type Client struct{ Peer *runtime.Peer }
type Events struct{ Changed func(context.Context, protocol.Payload) }
func Dial(ctx context.Context, url string, options runtime.DialOptions, handler Handler, events Events) (*Client, error)
func Attach(ctx context.Context, conn duplex.Conn, options runtime.Options, handler Handler, events Events) (*Client, error)
func Open(ctx context.Context, t *tunnel.Tunnel, handle protocol.Handle, options runtime.Options, handler Handler, events Events) (*Client, error)
func (*Client) Echo(ctx context.Context, params protocol.Payload) (protocol.Payload, error)
func (*Client) NoArgs(ctx context.Context) (string, error)
func (*Client) EmitNoticed(ctx context.Context, data protocol.Seen) error
func (*Client) OnChanged(handler func(context.Context, protocol.Payload)) error
func (*Client) Close() error
type Caller interface{ Echo(…); NoArgs(…); Seen(…) }
type Handler interface{ Reverse(ctx context.Context, client *Client, params protocol.Payload) (protocol.Payload, error) }
func Decides(method string) bool
func Asks(method string) bool
var Conversation = struct{ Event, Path string }{…}
```

The mirror image: `Client` calls the server's methods, emits the client's
events and listens for the server's; `Handler` is what the server calls on
this client, required because a client serves what the server calls; and
`Caller` is the interface a consumer mocks. `Dial` over a socket, `Attach`
over any connection of the seam, `Open` over a handle resolved on a tunnel.
All three take `Events`, installed through `Prepare` before the first frame,
then run any caller-supplied `Prepare`. An empty `Events{}` handles none.
`OnX` is for later registration; it cannot recover already delivered events.
`Decides`, `Asks` and `Conversation` are the session tier's governance,
rendered as functions for a session's relay to be given
([the session's surface](../runtime/session.md)).

## TypeScript: one package

`api/ts/<f>-client`, an npm package under the consumer's scope, with
`types.ts` and `index.ts` and a `package.json` depending on
`@nightseam/runtime` and `@nightseam/tunnel`.

`types.ts` is one interface or type per declared type — a field not
`required` is optional, one `nullable` is `T | null`, an `open` record has
an index signature, an enum is a union of its string literals, an alias a
type alias, a `timestamp` a `string`, `json` `unknown` — plus `Envelope`
and `Handle`, and
three things the runtime binds by: the `Family` interface naming every type
of the family, `validateWire`, the family's validator built by the
runtime's `createValidator` from the embedded wire description, and
`family`, the binding a generic family's slot is filled with.

`index.ts` re-exports the types and `DuplexError`, and declares:

```ts
export interface Events { changed?: (data: Payload, context: EventContext) => void | Promise<void> }
export interface Handler { reverse(params: Payload, context: RequestContext): Payload | Promise<Payload> }
export interface Caller { echo(params: Payload, options?: CallOptions): Promise<Payload>; noArgs(options?: CallOptions): Promise<string>; … }
export const decides: ReadonlySet<string>;
export const asks: ReadonlySet<string>;
export const conversation: { event: "changed"; path: "text" };
export const errors: { denied: "denied"; notFound: "not_found" };
export type ErrorCode = (typeof errors)[keyof typeof errors];
export class Client implements Caller {
  static dial(url: string, options: PeerOptions, handler: Handler | undefined, events: Events): Promise<Client>;
  static attach(connection: FrameConnection, options: PeerOptions, handler: Handler | undefined, events: Events): Promise<Client>;
  static open(tunnel: Tunnel, handle: Handle, options: PeerOptions, handler: Handler | undefined, events: Events): Promise<Client>;
  echo(params: Payload, options?: CallOptions): Promise<Payload>;
  emitNoticed(data: Seen, options?: EmitOptions): Promise<void>;
  onChanged(handler: (data: Payload, context: EventContext) => void | Promise<void>): () => void;
  close(): void;
  readonly peer: DuplexPeer;
}
```

A `Client` validates every frame both ways against `validateWire` — a call's
params and result, a reverse call's, an event's data — and installs the
`Handler` and `Events` before the peer has a connection, so the server's first reverse call or event meets them. Pass `{}` for no event handlers;
use `onX` for later registration, before the event-producing flow begins. The `families` option is filled in for the observer, so a
frame event names the family.

## Errors

The public errors reach both languages by name and are the one vocabulary
of a family that crosses the wire as data: `IsError(err, protocol.ErrorNotFound)`
in Go; in TypeScript a call rejects with a `DuplexError` whose `code` is one
of `errors`, and `ErrorCode` is the union a consumer switches over. Names
follow the convention — `not_found` is `ErrorNotFound` and `errors.notFound`
— unless an override file says otherwise ([a family in
tiers](families.md#gojson-and-typescriptjson)).

## Generic families

A family generic in others renders once, generically, in both languages,
and a consumer instantiates it where it binds the parameters — one type
parameter per parameter in TypeScript, one per type drawn from a parameter
in Go — as [generics](generics.md#how-each-language-instantiates-it) says.
