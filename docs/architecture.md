# The shape of the tree

One figure of what the state pages describe one kind of thing at a time:
the stages a declaration is projected through until a peer of one language
is talking to a peer of another, and the interface at each stage. It is a
state page — it says what the tree is at 0.6.0, in the names the surfaces
have today — and it changes when they do. Where it is going is
[the goals](goals/README.md); why it has this shape is
[the decisions](decisions/README.md).

![The projection stages and their interfaces: one declaration rendered into a Go and a TypeScript realization, each a stack of the family in the language, the sides' adapters, the shared Bitwire contract, the wire realizations and the dispatcher, the layers over the peer, the peer and the seam, meeting at the frames of the profile](architecture.svg)

## Reading it, top to bottom

Every box names a surface a state page documents; the page is the
reference, the figure is the map.

| stage | what it is | Go | TypeScript | page |
| --- | --- | --- | --- | --- |
| declaration | one family in three tiers — data, RPC, live — with a nominal identity, `(path, digest)`, computed from its canonical rendering and checked wherever a wire is interpreted | `model.json`, `protocol.json`, `live.json` | the same files | [families](declaration/families.md), [the declaration proof](declaration/proof-findings.md) |
| rendering | the generator's target for the language; a specification is rendered, not written, and a stale derivation is a failure | `nightseam generate` | the same command | [the generator](declaration/generator.md), [generated code](declaration/generated.md) |
| the family in the language | the declaration realized natively, one package: the types and validators (the data tier); the two sides as records of native functions, `ClientModel` and `ServerModel` (the RPC and live tiers) — a callable is a function, a record of callables a record; the wire declaration and its digest (the identity); converters for live values. Rendered, not written — its provenance, not its name | `<family>-protocol`: `ClientModel`, `ServerModel`, `WireDeclaration`, `WireSchema`, `WireDigest` | `@scope/<family>-client/types`: `ClientModel`, `ServerModel`, `wireDeclaration`, `wireDigest` | [generated code](declaration/generated.md), [live values](declaration/generated.md#live-values), [the live layer](runtime/live.md) |
| the sides' adapters | the crossing between a model and a wire, one package per declared side: validation, the `identity.check` exchange, and value conversion under the owner the caller supplies through the adapter context; a recorded wire beside them. Test support — `familytest`, the `/test` subpath — composes these same adapters over local carriers and is no stage of the running system | `<family>-client`, `<family>-binding`: `ToWire(model, env)`, `PrepareFromWire(endpoint, env)`, `FromWire(ctx, endpoint, env)`, `Record`; `runtime.AdapterContext` | `@scope/<family>-client`, `-binding`: `toWire`, `prepareFromWire(…).complete()`, `record`; the value environment | [the wire](runtime/wire.md#models-values-and-context), [generated code](declaration/generated.md), [testing a family](declaration/generated.md#testing-a-consumer-family) |
| the access contract | **the second shared thing**, drawn as a band across both columns: Bitwire 0.2.0, owned by [Bitspark/bitwire](https://github.com/Bitspark/bitwire) and adopted here by alias in Go and re-export in TypeScript, never redefined — send-only `Wire { send(path, message) }`; `Endpoint extends Wire { receive(receiver) → detach, close(code, reason) }` with one owning receive attachment; `Receiver { message, closed }`; a `ReturnAddress` carrying send-only `Wire`. Registration, prefix routing and precedence are a composed dispatcher's, not the contract's | `bitwire.Wire`, `bitwire.Endpoint`, `bitwire.Receiver` (aliases) | `Wire`, `Endpoint`, `Receiver` (re-exports of `@bitspark/bitwire`) | [the wire](runtime/wire.md), [the adoption decision](decisions/the-reusable-foundation-lives-in-nightseam.md) |
| wire realizations, and the dispatcher | what each realization makes of the contract: the peer's own endpoint, a tunnel channel, a model endpoint from `ToWire`, a bounded local pair — each an `Endpoint`; and above them one dispatcher that registers, selects, mounts and forwards, with the invocation lifecycle spoken at a request's return capability | `peer.Wire()`, `runtime.NewWirePair`, `runtime.NewDispatcher`, `duplex.At`, `duplex.Mount`, `runtime.ForwardWire` | `peer.wire()`, `wirePair`, `createDispatcher`, `at`, `mount`, `forwardWire` | [the wire](runtime/wire.md) |
| layers over the peer | compositions a consumer could write and every consumer would write the same way: live bindings in a scope, and channels multiplexed over one peer | `live.Over` → `Scope`, `Owner`; `tunnel.Tunnel` | `liveOver` → `LiveScope`, `LiveOwner`; `Tunnel` | [the live layer](runtime/live.md), [the tunnel](runtime/tunnel.md), [admission](admission.md) |
| peer | the profile's primitives: correlated requests, events, cancellation, coded refusals, bounds, the `meta` header, trace context, and one observer told at the write | `runtime.Peer` | `DuplexPeer` | [the peer](runtime/peer.md), [the profile](wire/profile.md), [the observer](runtime/observer.md) |
| seam | a connection that sends, receives and closes frames — a WebSocket, a pipe, a tunnel channel; the floor, not Nightseam's to decompose | `duplex.Conn` | `FrameConnection` | [the profile](wire/profile.md#the-connection-beneath) |

Three things on the figure are shared, and the figure draws each across
both columns: the declaration at the top, the Bitwire contract at the wire
row, and the profile's frames at the bottom. Everything else is one
realization's. Between the two columns the figure names what each stage
*is* in the tree's own terms — the family natively, rendered not written;
the crossing, per side; realizations of the contract; compositions over
the peer; the envelope; any connection of the seam — and at the bottom what
crosses between two realizations: the profile's four frame kinds with their header and trace
context, the reserved vocabularies `channel.*`, `live.*` and `identity.*`
as ordinary frames, request serials that increase per direction, and the
declaration's digest at `channel.open`, at `identity.check` and on a
transferred reference. Nothing else crosses: no path member — the path is
the method name — and no lifecycle verb, which lives at a return
capability and never reaches a peer root.

## How the stages relate

- **Each layer speaks the one beneath it and knows nothing of those
  above.** A dispatcher takes an endpoint and asks nothing about what
  carried it; a peer runs over a channel as it runs over a socket; the
  live layer uses ordinary requests and events under its own prefix, as
  the tunnel does. [Layering](goals/layering.md) is the goal;
  [the vocabulary test](wire/vocabulary.md#the-test) is the rule that
  assigns a thing on the wire to its layer.
- **The crossing is one boundary, in both directions.** A model becomes a
  wire through `ToWire` and a wire becomes a model through `FromWire`;
  the same adapter serves a local pipe, a socket, a selected path, a
  mounted subtree and a forwarded route, which is the claim the
  [construction of #321](https://github.com/Bitspark/nightseam/issues/289)
  held. Ownership travels beside the model, never inside it: a live value
  is a function, and the caller supplies the lifetime it lives under.
- **The recurrence.** A connection is a wire at the empty path; a tunnel
  channel is a wire, being a connection of the seam; `at`, `mount` and
  `forward` yield a wire again. That is why the generated adapter has one
  entry point rather than one per carrier, and why a selected part and an
  assembled whole are used the same way — the
  [composability](goals/composability.md) goal at the unit
  [a layer takes a wire](decisions/the-session-runs-over-any-connection-of-the-seam.md)
  names.
- **One definition, held in one place.** Both columns are realizations of
  the same three things — the declaration, the Bitwire contract, the
  profile — and neither column is the home of any of them: the contract's
  home is Bitspark/bitwire, adopted here at v0.2.0 and never redefined; what a language promises is a tier, and the
  conformance suite holds every language to the same scenarios and the
  same tables ([tiers](languages/tiers.md),
  [agnosticism](goals/agnosticism.md)).
- **What is Nightseam's and what is the consumer's.** The two upper
  stages are the consumer's — the declaration is its family, the
  functions in its models are its logic, and the family in the language
  is rendered for it — and everything from the sides' adapters down is
  mechanism the tree owns; a policy calls in from above, never
  lives inside ([the boundary](goals/boundary.md),
  [admission](admission.md)).
