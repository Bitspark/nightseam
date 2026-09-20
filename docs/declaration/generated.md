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
package's `Handler`, and calls through the client package. A family with only
a model generates the protocol package's types and validator; binding and
client helpers require a protocol.

### The protocol package

One Go type per declared type, in wire order, with the two the family
carries from the built-in `duplex` family:

| declared | rendered |
| --- | --- |
| a record | a struct with `json` tags; a field not `required` is `runtime.Optional[T]`, one `nullable` is `runtime.Nullable[T]`, both where it is both; an `open` record keeps what it did not declare in `AdditionalFields map[string]json.RawMessage` |
| an enum | a string type with one constant per value, `StatusReady Status = "ready"` |
| an alias | a Go alias of the expression, `type Payloads = []Payload`, retaining the aliased type's codecs and schema metadata |
| a `map` | `map[string]T` |
| a literal | a defined string type and typed constant, such as `LiteralReady` and `LiteralReadyValue`; equal literals in one family share them |
| a nullable expression | `runtime.Nullable[T]`, including inside arrays, maps and applications |
| a type parameter | a Go parameter constrained by `any`; it can sit beside the types drawn through a family parameter |
| an inline shape | a declaration under its derived name, retaining the enclosing type parameters it uses |
| a union | a concrete struct with one exported pointer per alternative, a typed `Kind()` result and its own JSON codec; exactly one pointer must be selected |
| `duplex.Envelope`, `duplex.Handle` | one message of the profile, and a reference to a channel that speaks it, `Handle{Channel int64}` — carried, not declared ([a tier is a built-in family](../decisions/a-tier-is-a-built-in-family.md)) |

Every record marshals and unmarshals itself, keeping presence and nullness
apart on the way through, and every record and enum returns the package's
`Tag` from `Of()` — which is how a generic type of another family is held
to what fills it ([generics](generics.md#how-each-language-instantiates-it)).
A `timestamp` is `time.Time`, an `integer` `int64`, a `number` `float64`,
`json` `any`.

A union carries every payload whole under its declared value member. A
record alternative reuses its record type; another payload uses a generated
`<Union><Variant>Value` wrapper with a `Value` field. An empty alternative is
a selected `*struct{}` and writes only the tag. An extending union reuses the
base's payload types and pointers, including imported and applied bases.
`WidenRichPartFromPart` includes a base value; `NarrowRichPartToPart` returns
the base value and a boolean, false for an extended-only or invalid selection.
An imported base's family prefixes its name in these helpers. The precise
names and collision rules are held by `conformance/tables/naming.json`.

Generated records, enums, unions and literal types expose `WireType()` for
automatic runtime validation of Go type arguments. Codecs bind ordinary
parameters and family draws before validation, so nested generic values
retain literal constraints, nullness and their declaring family's schema.
Ordinary generated Go client calls take type arguments without caller-supplied
codecs or registration. Direct use of the generic boundary helpers below is
different: those helpers explicitly take converters and type bindings.
An extended side includes its base operations with their bound types and
source naming overrides; a base client can call the extended binding.

The family's public errors are a constant per error, `ErrorNotFound =
"not_found"`, the list `Errors`, and `IsError(err error, code string) bool`,
which answers for a `*runtime.PublicError` a call returned. The wire
description the validator reads is embedded here: `ValidateRaw(name, data,
at...)`, `ValidateExpressionRaw`, `ValidateValue` and `MustTypeExpression`
validate a value against a declared type, and are what the binding and the
client call on every frame.
`WireSchema()` exposes that descriptor and its imports to other generated
families, so validation of an imported application retains its parameter
bindings and the family that declared each expression.

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

## TypeScript: one package

`api/ts/<f>-client`, an npm package under the consumer's scope, with
`types.ts` and `index.ts` and a `package.json` depending on
`@nightseam/runtime` and `@nightseam/tunnel`.

The TypeScript target emits no server-binding package. Its `Handler`
implements operations declared on the **client** side, called by the server;
it is not the counterpart of Go's binding `Handler`. The runtime can serve
requests and export callables in either peer role, but those capabilities
do not supply generated server bindings. See the
[generated-role evidence](proof-findings.md#generated-roles-and-skips) for
the roles the conformance suite executes and skips.

A family with only a model tier emits its types, validator and family
binding in the same package layout. It depends on the runtime alone and
declares no client or protocol helpers.

`types.ts` is one interface or type per declared type — a field not
`required` is optional, one `nullable` is `T | null`, an `open` record has
an index signature, an enum is a union of its string literals, an alias a
type alias, a `timestamp` a `string`, `json` `unknown` — plus `Envelope`
and `Handle`, and
three things the runtime binds by: the `Family` interface naming every type
of the family, `validateWire`, the family's validator built by the
runtime's `createValidator` from the embedded wire description, and
`family`, the binding a generic family's slot is filled with.

A union is a discriminated TypeScript union. Every variant with a payload
keeps it whole under the declared value member (`value` by default), even
a record: `{ kind: "some"; value: T }`. A no-payload variant declared with
`{ "empty": true }` is `{ kind: "none" }`; an empty record still has its
`value: {}` payload. An extending union includes its inherited variants
with the base's explicit type and family arguments applied. Literal types
stay literals, and nullable expressions work inside arrays, maps and type
arguments as well as on fields. Inline shapes become named declarations
at their derived names and capture the parameters they use.

A type parameter becomes a TypeScript parameter with an `unknown` default;
a family parameter exposes associated types such as `S["Envelope"]`.
Both can appear on the same declaration. A generated generic client takes
one runtime binding per parameter: `FamilyBinding<S>` for a family, or
`TypeBinding` containing `{ type, validate }` for a type interpreted in the
supplied validator's declaration scope. For example, a string binding is
`{ type: "string", validate: probe.validateWire }`. An explicit TypeScript
type argument supplies the matching consumer type. Imported type names
follow the source family's overrides; associated-type keys retain their
declaration names. Extended sides retain their source operation names and
validate their fixed or forwarded parameter bindings on calls and events.

`nightseam init` writes a handler whose method types come from its `Handler`
annotation. For a generic family, choose concrete arguments on that annotation
when implementing the handler; the initial stub uses the interface's defaults.

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

## Live values

A family with a live tier renders one more thing in each language: a callable
is a **function value**, and a record of callables is an ordinary record whose
members are functions. A consumer writes one where it means to be called and
receives one where it means to call, and never sees a reference.

```go
type Report = func(ctx context.Context, params Percent) error
type Cancel = func(ctx context.Context) error
type Rename = func(ctx context.Context, params Ticket) (Ticket, error)
type ProgressSink struct{ Report Report }
const ContractReport = "worker/Report"
```

```ts
export type Report = (request: Percent, options?: { signal?: AbortSignal }) => Promise<void>;
export type Cancel = (options?: { signal?: AbortSignal }) => Promise<void>;
export interface ProgressSink { "report": Report }
export const contractReport = "worker/Report";
```

These aliases follow their host language's function assignability: two
declared callables with the same native signature can be assigned to each
other. Exporting that value uses the destination helper's contract, not an
identity attached to the original function. The compiled Go and TypeScript
[`TestGeneratedCallableNominality`](../../cmd/nightseam/callable_nominality_test.go)
holds this assignment and stamping, then verifies that the resulting wire
descriptor is refused under the other contract. This is
[nominal wire checking](../decisions/a-callable-is-a-declared-kind.md#native-assignment-and-contract-evolution),
not nominal host typing or a proof of compatible signature revisions.

The conversion is at the boundary, so a handler is handed native values. Each
live type renders a pair that takes the scope its bindings belong to:

```go
func ExportJob(scope *live.Scope, v Job) (json.RawMessage, error)
func ImportJob(scope *live.Scope, raw json.RawMessage) (Job, error)
```

Export walks the value, makes a binding of each local function and writes the
reference that names it in its place; import validates, attaches, and replaces
each reference with a typed proxy. The generated client and binding call these
for an operation that carries callables, and install the scope over the peer in
`Prepare`, before it reads — as a tunnel is made.

A live type's own `MarshalJSON` **refuses**, and that is the semantics rather
than a gap: a reference means nothing outside the scope that minted its
binding, so a live value has no scope-free encoding. The refusal names the pair
that does have a scope. TypeScript needs no such refusal — its generated client
never hands a live value to the peer unconverted — but the same rule holds.

### Generic boundary helpers

A generic data declaration can contain live values when applied in the live
tier: `Page<Job>` is supported. Its own generated package stays usable for
ordinary data and imports no live runtime. Instead, its conversion helpers
take a converter for each type parameter they use (or each associated type
drawn through a family parameter). The caller supplies the conversion; for
a live argument, that converter closes over the appropriate connection scope.

For example, the model-only `boxes` family's `Page<T>` emits these signatures
in [Go](../../cmd/nightseam/testdata/golden-families/api/go/boxes-protocol/types_generated.go)
and [TypeScript](../../cmd/nightseam/testdata/golden-families/api/ts/boxes-client/src/types.ts):

```go
func ExportPage[T any](v Page[T], convertT func(T) (json.RawMessage, error), typeT runtime.TypeBinding) (json.RawMessage, error)
func ImportPage[T any](raw json.RawMessage, convertT func(json.RawMessage) (T, error), typeT runtime.TypeBinding) (Page[T], error)
```

```ts
export function exportPage<T = unknown>(value: Page<T>, convert_T_: (value: T) => unknown): unknown;
export function importPage<T = unknown>(raw: unknown, convert_T_: (value: unknown) => T): Page<T>;
```

The same package emits `ExportBox`/`ImportBox` for a record,
`ExportChoice`/`ImportChoice` and `ExportResult`/`ImportResult` for unions,
and `ExportBatch`/`ImportBatch` for a nested generic alias; TypeScript uses
the corresponding `exportX`/`importX` names. Each forwards the supplied
converters through the declared container structure. Go pairs each converter
with a `runtime.TypeBinding`, whose `Schema` and `Type` retain the argument's
declaration context for validation. TypeScript's conversion helpers take
no binding argument and do not validate the whole value: the generated
operation validates before import and after export. A direct TypeScript
caller must perform that validation with the appropriate `TypeBinding`
slots too; conversion alone is not a validation API.

These complete functions show importing `Page<Job>` directly, with `boxes`
and `worker` naming their generated protocol packages/namespaces, `live`
and `runtime` the Go runtime imports, and `LiveScope` the TypeScript live
runtime type. Both use the caller's existing scope:

```go
func importJobs(scope *live.Scope, raw json.RawMessage) (boxes.Page[worker.Job], error) {
	return boxes.ImportPage(raw, func(item json.RawMessage) (worker.Job, error) {
		return worker.ImportJob(scope, item)
	}, runtime.TypeBinding{Schema: worker.WireSchema(), Type: "Job"})
}
```

```ts
function importJobs(scope: LiveScope, raw: unknown): boxes.Page<worker.Job> {
  boxes.validateWire("Page", raw, "", {
    T: { type: "Job", validate: worker.validateWire },
  });
  return boxes.importPage(raw, (item) => worker.importJob(scope, item));
}
```

Export uses the opposite converter: `worker.ExportJob(scope, value)` in Go
or `worker.exportJob(scope, value)` in TypeScript, followed by whole-value
validation. For direct data-helper exports, enclose the whole conversion and
validation in [an export build](../runtime/live.md#constructing-a-payload-before-publication)
and close the converters over its scope view, so an unpublished failure can
reclaim every newly allocated binding. These are conversion hooks, not independent ownership or
disposal handles; binding lifetimes remain those of the
[live scope](../runtime/live.md). An ordinary generated operation supplies
the converters and validation itself, so its consumer passes native values.

A generic declaration that directly contains a callable also takes a scope.
The `combinator` family's `Bundle<T>` has a fixed `Unary` member beside its
generic metadata and emits:

```go
func ExportBundle[T any](scope *live.Scope, v Bundle[T], convertT func(*live.Scope, T) (json.RawMessage, error), typeT runtime.TypeBinding) (json.RawMessage, error)
func ImportBundle[T any](scope *live.Scope, raw json.RawMessage, convertT func(json.RawMessage) (T, error), typeT runtime.TypeBinding) (Bundle[T], error)
```

```ts
export function exportBundle<T = unknown>(scope: LiveScope, value: Bundle<T>, convert_T_: (scope: LiveScope, value: T) => unknown): unknown;
export function importBundle<T = unknown>(scope: LiveScope, raw: unknown, convert_T_: (value: unknown) => T): Bundle<T>;
```

The live helper passes its active export scope view to each export converter.
Use that argument for nested exports, rather than closing over the original
scope: conversion of the generic member and the fixed callable then belongs to
one unpublished build. Import converters keep their value-only signatures.

The [generic-live scenario](../../conformance/scenarios/generated/live-generic-containers.json)
executes the generated operation with an imported generic record containing
a supplied callable and a returned nested alias/union/container holding
`Bundle<Count>`. The [Go](../../conformance/go/generated/combinator.go.tmpl)
and [TypeScript](../../conformance/ts/generated/combinator.ts) consumers invoke
the returned function after the supplying RPC has completed. The
[family reference](families.md#what-is-live-and-where-it-may-be-written)
keeps three cases separate:

| Form | Current support |
| --- | --- |
| A generic container applied to a live type, such as `Page<Job>` | Supported in the live tier; argument converters carry the scope dependency. |
| A callable declaration with its own parameters | Temporarily refused as [`callable_parameters`](../../cmd/nightseam/testdata/invalid/callable-parameters/diagnostics.txt); applied callable identities are not defined by the current contract. This does not rule out future generic callables. |
| A live type drawn through a family parameter, such as `S.Job` | Separately refused as [`live_draw`](../../cmd/nightseam/testdata/invalid/live-draw/diagnostics.txt); the family-binding contract does not supply its live boundary converter. This is a missing conversion surface, not a consequence of nominal identity or a requirement that all generic containers remain data-only. |

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
