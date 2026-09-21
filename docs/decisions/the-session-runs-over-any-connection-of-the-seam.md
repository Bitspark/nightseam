# A layer takes a wire

**The question.** Can local model access, a socket, a prepared tunnel
channel and a mounted or forwarded origin expose one interface without
reimplementing correlation or losing the guarantees of live references?

**Decided.** A Wire carries the profile's four frame kinds at a relative
path. Selection prefixes the path; mounting chooses a child by one opaque
segment; forwarding preserves the message and its local return capability.
They allocate no peer or channel, including on first use. Generated
`ToWire`/`FromWire` and `toWire`/`fromWire` interpret the same per-side model
factory on this interface. Transport construction belongs to the host.

The wire's message is a frame; request/response is the peer's, not the
adapter's; the abstract `void send` is realized by the peer's correlation
for a request and by nothing for an event. A local runtime endpoint owns
the equivalent bounded dispatch and completion rules without serializing
bytes or constructing a peer. Send reports admission or refusal without
executing application code on the caller's stack.

**Why.** A generated transport facade made a family choose how it was
carried. A Wire lets the same generated interpretation receive an existing
origin. The path is encoded in the profile's existing method/event name,
so routing requires no envelope extension. The peer continues to own its
ids, cancellation and refusals. Bounds and carrier isolation stay with
the destination, including a channel whose failure leaves its underlying
connection and sibling channels usable.

The live construction imports a reference with an expected contract and
active owner before exposing its invocation at `[binding]`. Its nonce,
contract checks, guard, release barrier and ownership ledger survive
selection and mounting. This retains live interpretation and lifetime
state. Closing the selected Wire ends an admitted reply but does not
release the scope's binding; releasing its owner revokes later calls while
allowing an admitted result to settle. The two operations cannot replace
one another. A path under an origin alone therefore does not supply the
full live-reference contract. Expected type, native scope association and
allocation owner remain explicit inputs. The construction establishes
common access over this retained state, not elimination of the live layer.

Routing also supplies no authority. Incoming verified context remains
attached through local composition; a physical downstream connection
establishes its own context. Forwarding request metadata is an explicit
consumer action. A local shortcut never unwraps an application guard.

**Serves.** Composability: one model interpretation works over each Wire
presentation. Agnosticism: scalar interpretation requires no live or
tunnel component. The retained live state is chosen when values acquire
bindings; a value factory itself captures no permanent owner.

**Since.** #289 and its executed construction #321, with #290's send laws
and #291's order law. This replaces in place #48's decision that a session
took any transport connection. The session itself was removed by #196 and
#200; the transport-independent rule now applies to relative-path access.
