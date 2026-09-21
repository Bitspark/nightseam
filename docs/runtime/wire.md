# Relative-path wires

A Wire is access to an origin. Its message is one of the profile's request,
response, event or cancel frames, and its path is relative to that origin.
Generated models use this interface for local access, sockets, prepared
tunnel channels, selected paths, mounts and forwarding. The host chooses
the carrier; the generated model does not inspect it.

## The surface

| responsibility | Go | TypeScript |
| --- | --- | --- |
| send a frame at a relative path | `Wire.Send(path, message) error` | `wire.send(path, message): void` |
| register a receiver and obtain an idempotent detach | `Wire.Receive(path, receiver)` | `wire.receive(path, receiver)` |
| end the endpoint | `Wire.Close(code, reason)` | `wire.close(code, reason)` |
| select an origin | `duplex.At(wire, path)` | `at(wire, path)` |
| mount children by one segment | `duplex.Mount(map[string]duplex.Wire)` | `mount(ReadonlyMap<string, Wire>)` |
| create a bounded local pair | `runtime.NewWirePair(options)` | `wirePair(options)` |
| use an existing peer | `peer.Wire()` | `peer.wire()` |
| forward both directions | `runtime.ForwardWire(left, right)` | `forwardWire(left, right)` |

The duplex component defines the interface and path views. Runtime supplies
local endpoints, peer access and the request/event helpers used by generated
adapters. A tunnel `Channel` already implements Wire. Its inner peer is
prepared once during channel acquisition, before reads begin. Raw tunnel
transport is available separately as `Connection`; one channel cannot be
claimed by both presentations.

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

Receivers match an exact path unless `Namespace`/`namespace` is true.
Exact registration wins; otherwise the longest matching segment prefix
wins. Callbacks receive paths relative to the Wire on which they registered.
Duplicate registrations fail. Detach prevents new dispatch while already
admitted requests retain the return/cancellation path they captured.

## Ownership and bounds

A root owns bounded asynchronous dispatch, request capacity and closure.
Data uses the configured queue bound. An admitted request reserves room
for its cancellation without enlarging data capacity; unknown, duplicate
or completed cancels do not acquire another reservation. Deadline expiry
answers once. A handler that ignores cancellation still occupies its
active-work budget until it exits.

Closing a selected view closes the endpoint it selects. Closing a mount
detaches its registrations and leaves borrowed children usable. Forwarding
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
`FromWire` returns the same kind of factory over existing access. TypeScript
`fromWire` is asynchronous. Bind the returned factory once before reads;
factories assemble implementations and do not initiate application traffic
during assembly. See [the generated surface](../declaration/generated.md).

An adapter receives explicit runtime options and, when values acquire live
bindings, a value environment. The environment selects the current owner
and supplies child lifetimes, conversion batches and publication. Reusable
value adapters carry validation and both conversions, not a permanent owner.
Standalone acquired conversions run inside an explicit environment batch.

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
