# Relative-path wires

A Wire is access to an origin. Its message is one of the profile's request,
response, event or cancel frames, and its path is relative to that origin.
The contract is [Bitwire](https://github.com/Bitspark/bitwire)'s, adopted at
v0.2.0. All eight ports import the upstream declarations directly. Applications
can supply Bitwire implementations without a Nightseam type shim.
Generated models use this interface for local access, sockets, prepared
tunnel channels, selected paths, mounts and forwarding. The host chooses
the carrier; the generated model does not inspect it.

The [record/follow composition](record.md) adds consumer-owned message storage
and an atomic replay-to-live handoff, with a bounded writer per subscriber.

## The surface

| responsibility | Go | TypeScript |
| --- | --- | --- |
| send a frame at a relative path | `Wire.Send(path, message) error` | `wire.send(path, message): void` |
| take an endpoint's one owning attachment | `Endpoint.Receive(receiver) (func(), error)` | `endpoint.receive(receiver): () => void` |
| end the endpoint | `Endpoint.Close(code, reason) error` | `endpoint.close(code?, reason?)` |
| own one attachment and route above it | `runtime.NewDispatcher(endpoint, options…)` | `createDispatcher(endpoint, options?)` |
| register a handler at a path | `Dispatcher.Register` / `RegisterPrefix` | `dispatcher.register` / `registerPrefix` |
| take a receiving view of that owner | `Dispatcher.Select(path)` | `dispatcher.select(path)` |
| select an origin, send only | `duplex.At(wire, path)` | `at(wire, path)` |
| mount children by one segment | `duplex.Mount(map[string]bitwire.Endpoint)` | `mount(ReadonlyMap<string, Endpoint>)` |
| compose an origin and complete child access | `duplex.ComposeDeclared(origin, children)` | `Declared.compose(origin, children)` |
| retain the origin and complete child access | `declared.Decompose()` | `declared.decompose()` |
| bind declared send access | `declared.Bind()` | `declared.bind()` |
| receive on an origin, send through composed access | `duplex.Through(origin, access)` | `through(origin, access)` |
| create a bounded local pair | `runtime.NewWirePair(options)` | `wirePair(options)` |
| use an existing peer | `peer.Wire()` | `peer.wire()` |
| forward both directions | `runtime.ForwardWire(left, right)` | `forwardWire(left, right)` |

`Wire` grants send access and nothing else; `Endpoint` adds the one owning
attachment and closure. A value that only needs to send takes `Wire`, and
`Endpoint` is required only where attachment or closure is genuinely needed —
a generated binding's disposal owns the attachment it created, never a
borrowed endpoint.

The duplex component implements path views using Bitwire types. Runtime supplies
local endpoints, peer access and the request/event helpers used by generated
adapters. A tunnel `Channel` already implements Wire. Its inner peer is
prepared once during channel acquisition, before reads begin. Raw tunnel
transport is available separately as `Connection`; one channel cannot be
claimed by both presentations.

The underlying definitions are public
[Bitwire v0.2.0](https://github.com/Bitspark/bitwire/releases/tag/v0.2.0):
Go imports `github.com/Bitspark/bitwire/wire/go` from module
`github.com/Bitspark/bitwire@v0.2.0`; TypeScript imports `@bitspark/bitwire@0.2.0`.
Generated Go and TypeScript adapters import those packages directly. Python
imports `bitwire` from `bitspark-bitwire==0.2.0`; Rust imports `bitwire` from
`bitspark-bitwire=0.2.0`; C++ includes `<bitwire/wire.hpp>` through
`Bitwire::wire`; Java imports `dev.bitspark.bitwire` from
`dev.bitspark:bitwire:0.2.0`; Swift imports `Bitwire` from the public SwiftPM
package; Haskell imports `Bitwire` from `bitspark-bitwire-0.2.0`. C++ and
Haskell build dependencies pin the same immutable upstream revision.

`Wire` is send-only in every port. `Endpoint` adds a single receive attachment
and closure. Path routing belongs to an explicit dispatcher: `Dispatcher` in
Python, Rust, C++, Java and Swift, or `Nightseam.Duplex.Dispatcher` in Haskell.
`at` accepts any Bitwire Wire; receiving selections come from a dispatcher.
Mounts accept Bitwire Endpoints and borrow their lifecycle.
Bitwire and Nightseam are maintained by Bitspark under Apache-2.0. The
[adoption evidence](../../conformance/bitwire/README.md) pins provenance and
distinguishes shared composition cases from Nightseam's profile/runtime suites.

`Send` returns on admission or refusal. It never waits for a destination
handler or a reply, and never executes application code on the sender's
stack. Go returns a refusal; TypeScript throws it. Request completion goes
to the message's local return capability. That capability and its private
received context are never serialized. Events create no response waiter.

## Paths and receivers

A path is `[]string` or `readonly string[]`. Every segment is an opaque
Unicode scalar string: `[]`, `[""]`, `["a/b"]` and `["a", "b"]` differ.
No normalization, separator parsing or permission inheritance occurs.
Physical peers use [canonical byte-length encoding](../wire/vocabulary.md#a-layers-own-vocabulary)
in the existing method/event field. There is no additional envelope field.

`At(At(w, a), b)` routes like `At(w, append(a, b...))`. Selecting `[]`
preserves behavior; it need not return the same language object. A mount
consumes one segment to choose its child. It has no destination at `[]`;
an empty string is a valid child key. Selection and mounting create views,
with no peer, channel or queue, including at the first call.

An endpoint has one active owning attachment. A second is refused without
replacing the first, detach is idempotent and permits a later attachment, and
there is no implicit broadcast. Handler registration, exact and prefix
matching, overlap precedence and duplicate-path refusal are not the endpoint's:
they belong to one reusable dispatcher composed above it.

## Declared composition

Go and TypeScript implement Bitwire's
[declared composites](https://github.com/Bitspark/bitwire/blob/fdc2ae99bbd4dcf1f887c5e32bbda2e315c890a1/docs/decisions/0006-declared-composites-realize-deixis-nodes.md).
Each immutable description retains an origin and complete named child access.
The origin is a Wire used only at the empty relative path. A nonempty path
selects one child by its first segment and delegates the remaining path to it.
Missing children refuse; the origin is never a fallback. `RefusingOrigin{}` /
`refusingOrigin` supplies an explicit grouping origin.

```ts
const child = Declared.compose(childOrigin, [['remote', remoteWire]]);
const root = Declared.compose(rootOrigin, [['child', child.bind()]]);
const selected = at(root.bind(), ['child']); // behaves like the complete child
const { origin, children } = root.decompose();
const rebuilt = Declared.compose(origin, children); // same capabilities and state
const extended = Declared.compose(origin, [...children, ['next', anotherWire]]);
```

Go accepts an origin `bitwire.Wire` and a slice of
`DeclaredChild{Key: name, Wire: childAccess}`. TypeScript uses
`DeclaredChild = readonly [string, Wire]`; its `DeclaredParts` contains
`origin` and `children`. Both constructors copy their input containers and
refuse duplicate keys, invalid scalar strings and missing capabilities,
including typed nils in Go. Decomposition returns copied containers in exact
UTF-8 key order. Empty keys, childless nodes and distinct Unicode spellings
remain meaningful.

A child can be any Wire: an endpoint, selected view, forwarder, guard or
another bound composite. Decomposition retains its complete access and
identity; it neither discovers nor unwraps an opaque child's structure.
Assemblers keep child descriptions separately when they need to rebuild
deeper parts. They reconstruct each affected ancestor from its original
origin and revised child entries. Previous bound views keep their original
structure, and shared capabilities retain their state.

Descriptions belong to the assembler. Callers receive only `Bind` / `bind`
and ordinary `At` / `at` selection. The facade exposes send only, with no
parts, receive attachment, close or unwrapping operation. Construction and
reconstruction borrow capabilities and acquire no endpoint ownership.

Policy is optional interception around ordinary access, outside the node's
value. A consumer can wrap an entire bound composite or an individual child
in a guard. Retaining that wrapper whole preserves its policy state. A
selected view through a guarded root includes that guard, so using such views
as children of another guarded root may repeat it. Use the assembler's
retained parts for exact reconstruction. A guard must respect the profile's
invocation lifecycle, including controls owed to an admitted request.

A generated model's origin is a namespace of one-segment operation paths,
which a send-only Wire cannot enumerate. Each generated side therefore emits
`Declared(access)` / `declared(access)`: a
[description of access to that model](../declaration/generated.md#the-binding-package)
with one child for each operation it receives and for the identity check.
Because a generated interpretation attaches, `Through(origin, access)` /
`through(origin, access)` gives it an endpoint that sends through the bound
access and receives on the origin. Closing that endpoint releases only its own
attachment.

Plain composition delegates requests, events, responses and cancels unchanged;
the destination and profile decide whether a message is valid. It retains the
original return capability and runtime associations and allocates no request
ledger. A caller cancels through the same captured access and path as its
request. Immutable composition delivers that cancel to the same child, and
the runtime's invocation machinery reaches the admitted destination. Replies
use the request's original return capability. Application dispatch remains
asynchronous at the destination endpoint. The
[independent acceptance suite](../../conformance/declared/README.md) records
production coverage and its limits.

This replaces the unreleased ADR0005 policy-bearing value, raw deep lookup
and attachment helpers. There is no compatibility wrapper or additional tree
codec. Adoption is currently Go and TypeScript; other language implementations
remain [pending](https://github.com/Bitspark/nightseam/issues/702). Existing
endpoint mounts and the native Bitwire interface are unchanged.

## The dispatcher

`NewDispatcher` / `createDispatcher` takes one endpoint's attachment and owns
it. Exact registration wins; otherwise the longest matching segment prefix
wins, and a duplicate path is refused. `Select` returns a receiving view of
that same owner — sibling and nested views share the one root attachment
rather than each claiming it — which prepends its prefix to what it sends and
strips it from what it delivers. Callbacks receive paths relative to the view
they registered on. Closing a dispatcher releases its own registrations and
leaves the borrowed endpoint usable; `DispatcherOptions{OwnEndpoint: true}` /
`{ownEndpoint: true}` is the explicit transfer of closure authority for an
endpoint the caller owns.

Detach prevents new dispatch while already admitted requests keep the
return and cancellation path they captured, which is [the invocation
lifecycle](#the-invocation-lifecycle) below.

## The invocation lifecycle

Returning from a receiver is not invocation completion, and a router cannot
infer from routing alone when an admitted request is finished. So the
invocation says it, publicly: **an admitted request's return capability is the
invocation, presented as a Wire.** Its empty path carries the outcome, as it
always has; its other paths carry the lifecycle, as ordinary events of the
profile — a layer's own vocabulary, the way `channel.` is the tunnel's.

| path | meaning |
| --- | --- |
| `["invocation.capture", id]` | claim one immutable routing decision for this traversal; the message's return address is the control sink |
| `["invocation.ready", id]` | the captured request has been delivered; a latched control reaches the sink now |
| `["invocation.release", id]` | drop the capture |
| `["invocation.begin", id]` | take one execution lease |
| `["invocation.done", id]` | the body actually finished |
| `["invocation.control"]` | relay a cancellation into the invocation, which latches it and pushes it to every ready capture once |

Admission is the answer: `Send` returns on admission or refusal, so a refusal
— retired, a bound reached, a return capability that carries no lifecycle — is
the error it returns. The participant mints the identifier, so no operation
needs a return value. The verbs never reach a peer root and never cross a
physical hop, so they take no built-in family and reserve no namespace there; a
return capability's path space is the invocation's alone. A return capability
refuses a path it does not implement, as every addressed receiver in this
profile does.

| responsibility | Go | TypeScript |
| --- | --- | --- |
| capture this traversal | `runtime.CaptureInvocation(message, control)` | `captureInvocation(message, control)` |
| say the request was delivered | `capture.Ready()` | `capture.ready()` |
| drop the capture | `capture.Release()` | `capture.release()` |
| take an execution lease | `runtime.BeginInvocationBody(message)` | `beginInvocationBody(message)` |
| report the body finished | `body.Done()` | `body.done()` |
| relay a control | `runtime.RelayInvocationControl(message)` | `relayInvocationControl(message)` |
| keep one admitted request's lifecycle | `runtime.NewInvocation(limits, onRetired)` | `new Invocation(limits, onRetired)` |
| answer the vocabulary | `invocation.Deliver(path, message)` | `invocation.deliver(path, message)` |
| fix the outcome, end the request's own delivery | `invocation.Settle()`, `DispatchDone()` | `invocation.settle()`, `dispatchDone()` |

An admitting runtime composes `Invocation` in its return capability, or
answers the same paths out of a ledger of its own; a participant needs the
Wire it was already handed and nothing else. Queue admission, invocation
admission, caller withdrawal, a fixed outcome, body completion, the
admitted-control drain and retirement stay distinct: an early answer to the
caller — a deadline, a withdrawal — never retires an invocation whose body is
still running, and retirement waits until the outcome is fixed, no capture is
undelivered, no body unfinished and no control still reaching one. `Retired` /
`retired` says when that happened.

Captures and leases are bounded per invocation, as totals, so neither depth nor
shallow fan-out grows what one invocation retains; the defaults are
`DefaultInvocationLimits` / `defaultInvocationLimits`. Each traversal takes a
capture of its own, so a dispatcher visited twice in one invocation has two.
A control is the invocation's to route: a dispatcher that receives one relays
it rather than resolving a route, which is what keeps a detach or a rebind from
retargeting an admitted request. A dispatcher refuses a request whose return
capability carries no lifecycle, rather than routing it with weaker
guarantees — generic addressed delivery remains usable without the facility.
It reports `busy` for a bound it recognizes as one and `invalid_message`
otherwise, so a refusal a facility did not spell in the agreed vocabulary is
reported as what it is: a request this dispatcher cannot route with the
guarantees it advertises.

## Ownership and bounds

A root owns bounded asynchronous dispatch, request capacity and closure.
Data uses the configured queue bound. An admitted request reserves room
for its cancellation without enlarging data capacity; unknown, duplicate
or completed cancels do not acquire another reservation. Deadline expiry
answers once. A handler that ignores cancellation still occupies its
active-work budget until it exits.

Closing a selected view releases that view's own route and leaves the
dispatcher and the endpoint beneath it usable; closure authority over a
borrowed endpoint is never inferred from a view of it. Closing a mount
detaches its attachments and leaves borrowed children usable. Forwarding
returns a detach function; detaching it also leaves both borrowed endpoints
usable. Forwarding preserves frame order and local return identity; it
does not inspect or translate references hidden in payloads.

Overflow, a stalled destination and send failure end the destination's own
carrier and notify its observer within the configured bound. For a tunnel
channel this leaves the underlying connection and sibling channels alive.
Transport admission does not promise that an application effect occurred.
A definite unpublished refusal and a failure after possible publication
remain different outcomes for value-conversion rollback.

## Models, values and context

Each generated side has methods and events and a model factory that takes
the opposite side's access. `ToWire` constructs access for a model factory;
`FromWire` returns the same kind of factory over existing access after a
bounded declaration-identity exchange. TypeScript `fromWire` returns a
promise. `ToWire` installs the identity responder before constructing its
model. Factories assemble implementations and do not initiate application
traffic during assembly. See [the generated surface](../declaration/generated.md).

### Preparing an interpretation

The generated preparation entry point separates receiver installation from
the exchange that needs a running carrier:

| step | Go | TypeScript |
| --- | --- | --- |
| prepare synchronously | `complete, cleanup, err := PrepareFromWire(wire, environment)` | `const prepared = prepareFromWire(wire, context)` |
| complete after the carrier is attached | `model, err := complete(ctx)` | `const model = await prepared.complete(options)` |
| bind the returned factory once | `access, err := model(opposite)` | `const access = model(opposite)` |
| detach this interpretation | `cleanup()` | `prepared.close()` |

For a generic family, append its required positional adapter or binding
arguments to the preparation call. Preparation registers the identity responder
and deferred model receivers on the supplied origin. In Go, run preparation
inside the host's `runtime.Options.Prepare`, then call `complete` after
`Dial`, `Accept` or `NewPeer` returns. Waiting for completion inside
`Options.Prepare` would wait for read/write loops that have not started.
In TypeScript, prepare on `peer.wire()` before `connect` or `attach`, then
await completion after the carrier is connected.

Completion sends `identity.check` as this interpretation's first request.
Only a successful check exposes the model factory; binding that factory
releases its deferred requests and events. An event received during setup
therefore waits for both checking and binding. A mismatch returns
`contract_mismatch` and runs none of that interpretation's model handlers.
Cleanup detaches its receivers, including the identity responder, while
leaving the borrowed carrier and unrelated registrations usable. The host
continues to own carrier closure.

The runtime's `PrepareIdentity` / `prepareIdentity` implements this boundary
without another receive queue. Its `Wire` / `wire` registers directly on the
source; `Check` / `check` completes the exchange, `Ready` / `ready` releases
dispatch, and `Close` / `close` abandons the interpretation. The generated
entry points supply these calls. `RequestTimeout` / `requestTimeoutMs` in
the adapter context's runtime options bounds the whole preparation, from
creation through binding, with a default of 30 seconds. A shorter completion
deadline can fail it sooner. Deferred requests also observe
`MaxConcurrentHandlers` / `maxConcurrentHandlers`; the root's existing
queue and backpressure limits remain in force. Expiry, cancellation or
failure detaches the preparation and releases held deliveries.

`FromWire` / `fromWire` combines preparation and completion for an already
active wire. It cannot recover an event delivered before it was called.
Use explicit preparation when handlers must be present before the first
frame, including when the remote host emits immediately on connection.

The check compares the nominal family path and its
[canonical declaration digest](../declaration/declaration-identity.md).
For a generic family, the supplied bindings determine the closed application
identity before setup begins. Missing declaration metadata or required
bindings fail preparation; the template digest is not substituted. Extending
a family composes declarations but does not make their different nominal
paths interchangeable. See [the wire identity rule](../wire/profile.md#declaration-identity-at-interpretation)
for absent digests and the `method_not_found` response.

An adapter receives explicit runtime options and, when values acquire live
bindings, a value environment. The environment selects the current owner
and supplies child lifetimes, conversion batches and publication. Reusable
value adapters carry validation and both conversions, not a permanent owner.
Standalone acquired conversions run inside an explicit environment batch.

Generated calls and events use the adapter's configured propagator and
observer. Calls retain its request timeout, with an explicit operation
deadline taking precedence. Local composition preserves the propagator's
trace context; a physical hop serializes only the profile trace fields and
extracts a new incoming context at the destination.

Verified request and event context survives local selection, mounting,
forwarding and pair dispatch. A physical outgoing hop sends only profile
fields and establishes a new incoming context. Received metadata is not
implicitly copied to a reverse call or event. Prefix selection supplies
routing, not authenticated resource ancestry or authority.

A live binding can be presented at `[binding]` after checked import, but
Wire closure is not binding release. Scope nonce, expected contract,
owner ledger and release-as-barrier remain the live layer's responsibilities.
The paired [construction tests](../../live/go/wire_construction_test.go)
hold this distinction. Generated converters, rather than raw forwarding,
translate live values between distinct scopes.
