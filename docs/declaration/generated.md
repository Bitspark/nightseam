# The generated packages

What comes out of the generator for a family, in each language: the names a
consumer writes against. This page is drawn from the probe family of the
generator's own corpus, whose exported Go surface is held file for file
under `cmd/nightseam/testdata/surface` and whose TypeScript packages are held
as goldens under `cmd/nightseam/testdata/golden`; a family of your own renders
the same shapes under its own names. [A family in tiers](families.md) is
the input, [the generator](generator.md) the tool, and the specification
beside them, `api/spec/<f>/README.md`, is the family's own reference.

## Declaration identity and preparation

The `identity` builtin describes the runtime's bootstrap exchange. It emits
types, validation and canonical declaration metadata, but no application-model
adapters; the runtime owns its receiver. The preparation below applies to all
other generated protocol families, including other builtins
([builtin references](builtins/README.md)).

Each family emits its canonical declaration and SHA-256 digest:
`WireDeclaration()` and `WireDigest()` in Go, `wireDeclaration` and `wireDigest`
in TypeScript. Generated validators retain that declaration alongside the
descriptor used to validate values. Operations, events and reachable imported
declarations participate in the digest; documentation and host-language names
do not. The [canonical declaration](declaration-identity.md) defines the bytes
and how supplied generic arguments produce a closed application identity.

Both generated sides check that identity where a wire becomes a model.
`FromWire(ctx, wire, environment, ...bindings)` in Go and
`await fromWire(wire, context, ...bindings)` in TypeScript complete an ordinary
`identity.check` request before returning the model factory. `ToWire` / `toWire`
installs its identity responder before constructing the supplied model.
Factories only assemble their implementation; application traffic starts
after binding.

When the host must receive the first frame, prepare before attachment:

| Go | TypeScript |
| --- | --- |
| `PrepareFromWire(wire, environment, ...bindings)` returns `complete func(context.Context) (Model, error)`, `cleanup func()` and `error` | `prepareFromWire(wire, context, ...bindings)` returns `{ complete(options?: WireCallOptions): Promise<Model>; close(): void }` |
| call `complete(ctx)` after the host attaches the carrier | await `prepared.complete(options)` after attachment |
| bind the returned factory once to its opposite implementation | bind the returned factory once to its opposite implementation |

`Model` denotes the generated side's `ServerModel` or `ClientModel`, with
its generic arguments where applicable. The exact positional bindings are
the same as `FromWire` / `fromWire`. Preparation registers deferred model
receivers synchronously; checking alone does not release them. Binding the
resolved factory releases delivery, including events received during setup.
The adapter context's `RequestTimeout` / `requestTimeoutMs` bounds the whole
preparation through binding. Cleanup detaches this interpretation without
closing the host's carrier. The [wire lifecycle](../runtime/wire.md#preparing-an-interpretation)
gives the host ordering and failure behavior.

The identity check compares the nominal path and any digests both sides
specify. Different nominal paths refuse even when one family extends the
other. A `method_not_found` response means the remote endpoint carries no
identity; other errors fail interpretation. A failed check exposes no model
and invokes none of that interpretation's model handlers. A digest identifies
a declaration, not a principal or permission.

## Go: three packages

`api/go/<f>-protocol`, `-binding` and `-client`, each owned wholesale. A
consumer imports the protocol package for its types and side models, and uses
either side's adapter to interpret a Wire. A family with only
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
Generic operation adapters take explicit value adapters alongside their type
arguments; direct boundary helpers take converters and type bindings.
An extended side includes its base operations with their bound types and
source naming overrides. It retains its own nominal family identity: the
base family's `FromWire` does not accept that different path implicitly.

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

## One model factory for each side

The protocol declares two complete side values. A side contains the methods
it implements and the events it receives from the opposite side. Methods and
events occupy separate facets, so their names may coincide.

For a server with `echo`, a client with `reverse`, a server event `changed`
and a client event `noticed`, Go emits:

```go
type ServerMethods interface { Echo(context.Context, Payload) (Payload, error) }
type ServerEvents interface { Noticed(context.Context, Seen) error }
type ClientMethods interface { Reverse(context.Context, Payload) (Payload, error) }
type ClientEvents interface { Changed(context.Context, Payload) error }
type Server struct { Methods ServerMethods; Events ServerEvents }
type Client struct { Methods ClientMethods; Events ClientEvents }
type ServerModel func(Client) (Server, error)
type ClientModel func(Server) (Client, error)
```

A model is a session factory. It receives the opposite side, captures it where
its implementation needs reverse calls or outgoing events, and returns its
own side. Construct the implementation without application traffic; invoke
methods and emit events after both sides are bound. A model's methods have
the same native signatures whether called directly or through a Wire.

### The binding package

The server adapter exports:

```go
func ToWire(model protocol.ServerModel, environment runtime.AdapterContext) (duplex.Wire, error)
func FromWire(ctx context.Context, wire duplex.Wire, environment runtime.AdapterContext) (protocol.ServerModel, error)
func PrepareFromWire(wire duplex.Wire, environment runtime.AdapterContext) (func(context.Context) (protocol.ServerModel, error), func(), error)
```

`ToWire` constructs a bounded local Wire pair, invokes the factory once with
the opposite proxy, registers the returned implementation and returns the
access end. It creates no physical peer or serialized frame connection.
`FromWire` checks identity, then returns a factory that binds the supplied
opposite implementation once and returns this side's proxy. A second bind
is refused. `PrepareFromWire` provides synchronous receiver installation
and a completion function for the exchange after carrier attachment.

Thus `FromWire(ToWire(model))` has the same model type. The host can perform
that round trip locally, or pass a socket peer's Wire, a prepared tunnel
channel, a selected view or a mount through the same adapter. The declaration's
operation name is one relative path segment; dotted names are not split.

### The client package

The client adapter is the mirror:

```go
func ToWire(model protocol.ClientModel, environment runtime.AdapterContext) (duplex.Wire, error)
func FromWire(ctx context.Context, wire duplex.Wire, environment runtime.AdapterContext) (protocol.ClientModel, error)
func PrepareFromWire(wire duplex.Wire, environment runtime.AdapterContext) (func(context.Context) (protocol.ClientModel, error), func(), error)
```

Both packages share the protocol's side and model types. Neither has
`Dial`, `Attach`, `Open`, `Serve`, `NewHandler`, `Remote` or a transport-owning
`Client` facade. `nightseam init` writes implementation stubs once; generated
packages remain owned wholesale by the generator.

## TypeScript: client and binding packages

`api/ts/<f>-client` contains shared declarations in `src/types.ts`, exported
as `@scope/<f>-client/types`. Its entry point adapts the declared client side;
`api/ts/<f>-binding` adapts the server side and imports that shared types
subpath. Both re-export the declarations. A model-only family emits types,
validation and value adapters in the client layout without a binding package
or protocol adapters. It needs no live runtime for ordinary data.

### Shared protocol declarations

Records become interfaces, optional fields become optional properties, nullable
values add `null`, enums become unions of string literals, timestamps are
strings and arbitrary JSON is `unknown`. A union carries its payload whole
under the declared value member: `{ kind: "some"; value: T }`. An empty
alternative carries its tag alone. Inline shapes become named declarations;
generic declarations retain their parameters and family associated types.

The shared `Family`, `family` and `validateWire` retain the declaration and
its validation scope. Type bindings carry `type`, `validate` and any
`slots`; family bindings keep the supplied family's validator. Conversion
and declaration validation remain separate responsibilities.

The same complete side values and factories are emitted:

```ts
export interface ServerMethods {
  echo(params: Payload, context?: WireModelContext): Payload | Promise<Payload>;
}
export interface ServerEvents {
  noticed(data: Seen, context?: WireModelContext): void | Promise<void>;
}
export interface ClientMethods {
  reverse(params: Payload, context?: WireModelContext): Payload | Promise<Payload>;
}
export interface ClientEvents {
  changed(data: Payload, context?: WireModelContext): void | Promise<void>;
}
export interface Server { methods: ServerMethods; events: ServerEvents }
export interface Client { methods: ClientMethods; events: ClientEvents }
export type ServerModel = (remote: Client) => Server;
export type ClientModel = (remote: Server) => Client;
```

A method with no declared request takes `Record<string, never>` in TypeScript;
Go omits that parameter. Methods return native values or promises.

### The binding package

```ts
export function toWire(model: ServerModel, context: AdapterContext): Wire;
export function fromWire(wire: Wire, context: AdapterContext): Promise<ServerModel>;
export function prepareFromWire(wire: Wire, context: AdapterContext): {
  complete(options?: WireCallOptions): Promise<ServerModel>;
  close(): void;
};
```

### The client package

```ts
export function toWire(model: ClientModel, context: AdapterContext): Wire;
export function fromWire(wire: Wire, context: AdapterContext): Promise<ClientModel>;
export function prepareFromWire(wire: Wire, context: AdapterContext): {
  complete(options?: WireCallOptions): Promise<ClientModel>;
  close(): void;
};
```

`toWire` invokes one model factory synchronously and returns its access Wire.
Await `fromWire`, then bind the returned factory once with the opposite side.
Reverse methods and incoming events are supplied together in that opposite side.

## Carrier assembly and context

The host owns authentication, upgrade, peer preparation and closure. In a
peer's `Prepare` / `prepare`, it can forward `peer.Wire()` / `peer.wire()`
to a model's local Wire with `runtime.ForwardWire` / `forwardWire`. Or it can
prepare an interpretation of that peer Wire with `PrepareFromWire` /
`prepareFromWire` before reads start, complete the identity exchange after
attachment, and bind the resolved factory. The preparation step is
synchronous in both languages; waiting for the exchange before a carrier
can read or write cannot complete. A tunnel's prepared channel has the same Wire
surface and accepts preparation options at acquisition. Views and mounts
reuse these roots without allocating another peer.

`runtime.AdapterContext` in Go and `AdapterContext` in TypeScript carry
`Options` / `options` and an optional `ValueEnvironment` /
`valueEnvironment`. Options govern a local pair's bounds, identity preparation
timeout and the adapters' observer. Carrier options remain with the host that
constructs the carrier.

Every operation validates request, response and event data in both
directions, including generic slots in their declaring family's scope.
Invalid incoming parameters are refused; a rejected incoming event closes
the affected Wire with code 1002. Generated operation observations carry the
declared family independently of a physical peer's labels.

Go passes `context.Context` through model operations. TypeScript uses
`WireModelContext`: cancellation, timeout, trace, received `meta`, explicit
`outgoingMeta`, and the received request id where present. Local composition
preserves verified context attached by the receiving runtime; path selection
does not confer authority. A handler explicitly chooses outgoing metadata
(`runtime.WithMeta` in Go, `outgoingMeta` in TypeScript); received metadata is
not implicitly copied to its subsequent calls.

## Value adapters and generic operations

A generic operation takes a reusable `runtime.ValueAdapter[T]` /
`ValueAdapter<T>` for each type slot. TypeScript also takes explicit family
bindings; Go retains drawn types through its type arguments and schema metadata.
Each value adapter retains the declaration binding, both conversion
directions and `NeedsContext` / `needsContext`. Each conversion receives the
active invocation context: `context.Context` in Go, opaque `unknown` in
TypeScript. It does not capture a permanent owner or principal.

Generated `AdapterX` / `adapterX` factories compose adapters through arrays,
maps, records, aliases and unions. `runtime.JSONAdapter[T]()` and
`jsonAdapter<T>(binding)` validate ordinary data both ways and require no
value environment. A generic model and its operation adapter can stay
unchanged when its type argument changes from scalar data to a fixed declared
callable or a nested callable-bearing value.

An acquiring conversion requires an explicit environment:
`runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}`,
or `{ valueEnvironment: valueEnvironment(scope) }` in TypeScript. The runtime's
`ValueEnvironment` supplies `Select`, `Child`, `Export`, `Import` and
`Publish` (lower camel case in TypeScript). The live wrapper selects the
current operation's owner from the given scope and supplies a batch view
throughout the whole conversion. Model-only and scalar-generic packages do
not import live to obtain this surface.

These adapters are primitive converters: acquiring conversions must run
inside the matching environment batch. A standalone consumer encloses the
entire walk and its validation in `Export`, `Import` or `Publish` and passes
the callback's active context through every nested adapter. There is no
global context registry and no factory-owned lifetime. The lower generated
conversion helpers below remain available for explicit native live work.

For a standalone export using an already chosen adapter and scope:

```go
environment := live.ValueEnvironment(scope)
raw, err := environment.Export(live.WithOwner(ctx, owner), func(active context.Context) (json.RawMessage, error) {
    return adapter.Export(active, value)
})
```

```ts
const environment = valueEnvironment(scope);
const raw = environment.export(owner, active => adapter.export(active, value));
```

Use `Publish` / `publish` instead when that same operation will attempt to
send the value, so only definite non-publication can unwind fresh exports.

### Associated type interpretations

A family-generic `Holder` drawing `S.Job` and `S.Progress` takes those two
interpretations through the same value-adapter surface. In Go, its generated
`ToWire`, `FromWire` and `PrepareFromWire` take `adapterSJob` and
`adapterSProgress`, each a `runtime.ValueAdapter` of its native type. The
existing `runtime.Of[STag]` constraint ties both types to one family.
`AdapterHeld(provider.AdapterJob(), provider.AdapterProgress())` composes a
record containing them without importing the provider into the generic package.

TypeScript supplies `FamilyBinding<S, "Job" | "Progress">`. Its `types`
dictionary contains both `ValueAdapter<S["Job"]>` and
`ValueAdapter<S["Progress"]>`; the generated provider's `family` value already
contains adapters for its closed types. For example,
`adapterHeld<provider.Family>(provider.family)` composes the same record.
The runtime's `familyTypeAdapter` lookup verifies both the selected member and
the complete bound source declaration. Descriptor-only `FamilyBinding<S>`
remains useful for validation metadata, but does not satisfy a constructor
requiring associated converters.

Missing converters and inconsistent source families, revisions or member names
fail before a model factory or a conversion effect runs. Conversion preserves
the active operation batch rather than storing a scope or owner in the family
dictionary. A protocol-only generic package therefore stays independent of
live while a supplied live record receives the host's explicit environment.

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
export type Report = (request: Percent, options?: { signal?: AbortSignal; owner?: LiveOwner }) => Promise<void>;
export type Cancel = (options?: { signal?: AbortSignal; owner?: LiveOwner }) => Promise<void>;
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
live type renders a pair that takes the owner its new bindings belong to:

```go
func ExportJob(owner *live.Owner, v Job) (json.RawMessage, error)
func ImportJob(owner *live.Owner, raw json.RawMessage) (Job, error)
```

```ts
export function exportJobUnchecked(owner: LiveOwner, value: Job): unknown;
export function importJobUnchecked(owner: LiveOwner, raw: unknown): Job;
```

Export walks the value, makes a binding of each local function and writes the
reference that names it in its place; import validates, attaches, and replaces
each reference with a typed proxy. Generated operation adapters call the
converters inside the explicit value environment supplied by their host.
The host creates the live scope before its physical peer reads.

Choose a lifetime with `scope.Owner().Child()` or `scope.owner().child()`.
For a generated model operation, Go selects it through
`live.WithOwner(ctx, owner)`; TypeScript supplies `valueContext: owner` in the
model context. With the live value environment, a supplied owner applies to
its own connection. Concrete callable aliases still take `owner?: LiveOwner`
in their native invocation options; they are the live-specific surface.
When it belongs to another connection, or none is supplied, conversion uses
the current connection's root owner. Thus a native proxy forwarded through
another connection still works; narrower ownership on the origin connection
requires an owner for that connection. A released owner of the same connection
is still the selected owner. This does not change the attachment through which
an already imported function is called, or the low-level refusal of foreign
native references.

Generated handlers that receive or return live values get a per-invocation
child owner: `live.OwnerOf(ctx)` in Go, `context.valueContext` in a TypeScript
model using the live environment. Concrete callable implementations receive
`options.owner`. Live event handlers receive a child too. Returned functions
are exported under that child. A handler
can retain the owner and release it later; RPC completion does not dispose of
it. Releasing an owner revokes its own new bindings and its descendants,
including every alias of those bindings, while borrowed attachments remain
under the owner that first acquired them. See
[choosing a lifetime](../runtime/live.md#choosing-a-lifetime).

A live type's own `MarshalJSON` **refuses**, and that is the semantics rather
than a gap: a reference means nothing outside the scope that minted its
binding, so a live value has no scope-free encoding. The refusal names the pair
that does have a scope. TypeScript needs no such refusal — its generated adapter
never hands a live value to the peer unconverted — but the same rule holds.

### Parameterized callable helpers

A declared `Function<A,B>` is a native generic function type. Its complete
argument adapters are supplied once, before a live reference is exported or
imported:

```go
func AdapterFunction[A, B any](a runtime.ValueAdapter[A], b runtime.ValueAdapter[B]) runtime.ValueAdapter[Function[A, B]]
func ContractFunction[A, B any](a runtime.ValueAdapter[A], b runtime.ValueAdapter[B]) (runtime.DeclarationIdentity, error)
func ExportFunction[A, B any](owner *live.Owner, value Function[A, B], a runtime.ValueAdapter[A], b runtime.ValueAdapter[B]) (json.RawMessage, error)
func ImportFunction[A, B any](owner *live.Owner, raw json.RawMessage, a runtime.ValueAdapter[A], b runtime.ValueAdapter[B]) (Function[A, B], error)
```

```ts
function adapterFunction<A, B>(a: ValueAdapter<A>, b: ValueAdapter<B>): ValueAdapter<Function<A, B>>;
function contractFunction<A, B>(a: ValueAdapter<A>, b: ValueAdapter<B>): DeclarationIdentity;
function exportFunction<A, B>(owner: LiveOwner, value: Function<A, B>, a: ValueAdapter<A>, b: ValueAdapter<B>): unknown;
function importFunction<A, B>(owner: LiveOwner, raw: unknown, a: ValueAdapter<A>, b: ValueAdapter<B>): Function<A, B>;
```

Captured family slots use the coherent associated interpretations described
above. The contract function checks closure and returns the canonical applied
path and digest; missing recipes or inconsistent interpretations fail before
acquisition. The ordinary nongeneric declared callable still has its constant
contract name. A source alias of a callable has its own `ContractAlias` /
`contractAlias` function and specialized conversion body, with the same nominal
identity as the original application. Callable alias helpers perform full
callable validation and have no `Unchecked` suffix.

Every callback invocation uses its current context. Reusable argument adapters
retain neither the connection nor the owner of the call that supplied a
callback. In Go, use `AdapterFunction(...).Export(ctx, value)` and the matching
import recipe under the explicit environment batch when the recipes need
additional context values; the owner-only lower helper supplies the owner.

### Generic boundary helpers

A generic data declaration can contain live values when applied in the live
tier: `Page<Job>` is supported. Its own generated package stays usable for
ordinary data and imports no live runtime. Instead, its conversion helpers
take a converter for each type parameter they use (or each associated type
drawn through a family parameter). The caller supplies the conversion; for
a live argument, that converter closes over the active owner batch view.

For example, the model-only `boxes` family's `Page<T>` emits these signatures
in [Go](../../cmd/nightseam/testdata/golden-families/api/go/boxes-protocol/types_generated.go)
and [TypeScript](../../cmd/nightseam/testdata/golden-families/api/ts/boxes-client/src/types.ts):

```go
func ExportPage[T any](v Page[T], convertT func(T) (json.RawMessage, error), typeT runtime.TypeBinding) (json.RawMessage, error)
func ImportPage[T any](raw json.RawMessage, convertT func(json.RawMessage) (T, error), typeT runtime.TypeBinding) (Page[T], error)
```

```ts
export function exportPageUnchecked<T = unknown>(value: Page<T>, convert_T_: (value: T) => unknown): unknown;
export function importPageUnchecked<T = unknown>(raw: unknown, convert_T_: (value: unknown) => T): Page<T>;
```

The same package emits `ExportBox`/`ImportBox` for a record,
`ExportChoice`/`ImportChoice` and `ExportResult`/`ImportResult` for unions,
and `ExportBatch`/`ImportBatch` for a nested generic alias; TypeScript uses
the corresponding `exportX`/`importX` names. Each forwards the supplied
converters through the declared container structure. Go pairs each converter
with a `runtime.TypeBinding`, whose `Schema` and `Type` retain the argument's
declaration context for validation. TypeScript's record, union and alias helpers
carry the `Unchecked` suffix when they only convert: the data-only helpers take
no binding argument and do not validate the whole value. The generated
operation validates before import and after export. A direct TypeScript
caller must perform that validation with the appropriate `TypeBinding`
slots too; conversion alone is not a validation API.

These complete functions show importing `Page<Job>` directly, with `boxes`
and `worker` naming their generated protocol packages/namespaces, `live`
and `runtime` the Go runtime imports, and `LiveOwner` the TypeScript live
runtime type. Both put the whole walk under the caller's owner so a later
failure releases earlier fresh attachments without invalidating borrowed aliases:

```go
func importJobs(owner *live.Owner, raw json.RawMessage) (boxes.Page[worker.Job], error) {
	var result boxes.Page[worker.Job]
	err := owner.ImportValue(func(batch *live.Owner) error {
		var err error
		result, err = boxes.ImportPage(raw, func(item json.RawMessage) (worker.Job, error) {
			return worker.ImportJob(batch, item)
		}, runtime.TypeBinding{Schema: worker.WireSchema(), Type: "Job"})
		return err
	})
	if err != nil {
		return boxes.Page[worker.Job]{}, err
	}
	return result, nil
}
```

```ts
function importJobs(owner: LiveOwner, raw: unknown): boxes.Page<worker.Job> {
  return owner.importValue((batch) => {
    boxes.validateWire("Page", raw, "", {
      T: { type: "Job", validate: worker.validateWire },
    });
    return boxes.importPageUnchecked(raw, (item) => worker.importJobUnchecked(batch, item));
  });
}
```

Export uses the opposite converter: `worker.ExportJob(batch, value)` in Go
or `worker.exportJobUnchecked(batch, value)` in TypeScript, followed by whole-value
validation. For direct data-helper exports, enclose the whole conversion and
validation in [an export build](../runtime/live.md#constructing-a-payload-before-publication)
and close the converters over its owner view, so an unpublished failure can
reclaim every newly allocated binding. The data helpers remain conversion
hooks; their caller supplies the owner and its disposal. An ordinary generated
operation supplies the converters and validation itself, so its consumer passes
native values.

A generic declaration that directly contains a callable also takes an owner.
The `combinator` family's `Bundle<T>` has a fixed `Unary` member beside its
generic metadata and emits:

```go
func ExportBundle[T any](owner *live.Owner, v Bundle[T], adapterT runtime.ValueAdapter[T]) (json.RawMessage, error)
func ImportBundle[T any](owner *live.Owner, raw json.RawMessage, adapterT runtime.ValueAdapter[T]) (Bundle[T], error)
```

```ts
export function exportBundleUnchecked<T = unknown>(owner: LiveOwner, value: Bundle<T>, slot_T: ValueAdapter<T>): unknown;
export function importBundleUnchecked<T = unknown>(owner: LiveOwner, raw: unknown, slot_T: ValueAdapter<T>): Bundle<T>;
```

The live helper receives complete adapters because a generic callable nested
inside the value needs both conversion directions when invoked later. Each
recipe receives the active owner view for nested acquisitions. Conversion of
the generic member and the fixed callable belongs to one batch; failure unwinds
fresh allocations and leaves borrowed aliases intact. Pure-data generic helpers
retain the value-only converter signatures shown for `Page<T>` above.

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
| A generic container applied to a live type, such as `Page<Job>` | Supported in the live tier; argument converters carry the owner dependency. |
| A callable declaration with its own parameters | Supported as a closed nominal application through complete argument adapters. Type arguments are fixed before export; an [unapplied callable](../../cmd/nightseam/testdata/invalid/unapplied-callable/diagnostics.txt) is refused. |
| A plain associated record containing live values, such as `S.Job` | Supported through complete family-supplied value adapters, with coherent declaration identity and operation-local ownership. Direct callable, alias and generic-member draws remain refused. |

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
