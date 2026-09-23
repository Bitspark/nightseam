# The domain, as a graph

Nightseam is for typed access to models through wires, across languages
and carriers. This page models that domain — not the repository — as a
typed graph: what kinds of thing there are, which relations hold between
them, and which law governs each relation. The laws are the published
ones: Bitwire's [access contract](https://github.com/Bitspark/bitwire/blob/v0.2.0/docs/wire/contract.md) and
[composition laws](https://github.com/Bitspark/bitwire/blob/0f30b515694cb3005403229be0f4e4158baf41b9/docs/composition.md), and this
repository's own — [the diagram that commutes](declaration/generics.md#the-diagram-commutes)
for generic adaptation, [declaration identity](declaration/declaration-identity.md)
for what names a contract. Its counterpart is
[architecture.md](architecture.md), which draws the tree that realizes the
domain; one table below maps the one onto the other. It is a model
proposed on 2026-09-22, not a decision: the choices that change it are at
the end, filed for a verdict as [#640](https://github.com/Bitspark/nightseam/issues/640).

The [contract and representation theory](theory/README.md) distinguishes shape
`S`, specification `B`, contract `C = (S, B)`, native realization `T`, and an
instance `i` whose actual behavior may satisfy `B`. It also develops coordinate
paths, shape navigation and substitution, and their behavioral obligations.
Its [Nightseam interpretation](theory/nightseam.md) relates that model to this
domain without settling the choices below.

Its nodes come in three kinds. **Definitions** describe contracts and types;
they are not themselves live instances. A model contract can have several
selected model types in the same language. **Code** realizes a definition in a
language: an adapter binds a model type to the wire, or stubs the wire as a model
type. **Instances**
exist, act and are placed: model instances, wires, entries, carriers,
messages, references. A language is not a node — it is an opaque
identifier, an enum value naming one syntax and semantics, carried as an
attribute by model types, adapters and participants. Every instance node
is typed by a definition node, and the instance graph is generated from a
few rewrite rules whose laws are the model's commuting squares.

The [realization and satisfaction definitions](theory/foundations.md#3-native-realizations-instances-and-satisfaction)
qualify the tables below: a model type determines a structural carrier and
behavior interpretation, while satisfaction of the full contract is a separate
claim about an instance. A canonical declaration digest names only the content
the declaration includes; it does not establish unspecified behavioral laws.

## Node types

Fifteen node types in three kinds: five definitions, one kind of code,
nine instances. Contract, type and instance come in two triads —
ModelContract, ModelType, ModelInstance in a language; WireContract,
WireType, Wire on the wire — the triads meet at WireType, which reads a
model contract under the wire contract, and the adapters are the code that
joins them: `Adapter[T, U]` binds the selected model type `T` to the wire
realization `U`, and `Adapter[U, T]` stubs `U` as `T`.

| node | what it is | identity | in Nightseam today |
| --- | --- | --- | --- |
| ModelContract | a language-independent shape `S` (value structure or protocol operations, events and errors, with generic slots) together with its stated behavioral specification `B` | a declaration digest covers its canonical declared content; an application's declared identity is its constructor with ordered arguments | a family's tier files and built-in families describe the declared structure; behavioral laws need an explicit interpretation |
| WireContract | a statement of what every wire and its carrier satisfy: the *access* contract (send a message to a relative path; the selection and mounting laws) and the *invocation* contract that realizes it (request, response, event, cancel; correlation, refusal, ordering, acceptance) | a version string | Bitwire 0.2.0; `nightseam.duplex/1` |
| Side | a role of a model contract: what one party implements and the other calls — provider and caller | contract + role | client and server |
| ModelType | a selected realization `T` of a contract's shape in a language, with an interpretation of native values as behavior; several realizations can share a contract and language | contract + language do not identify `T` without a realization choice | the selected generated family in the language: its types, `ServerModel`, `ClientModel` |
| WireType | a model contract read on the wire, under the wire contract: `Wire[A]`, the shape of the messages that reach a wire of that type, and the token an entry carries beside its wire; one per contract and profile | contract + profile | the wire schema and its digest; the frames |
| Adapter | code that binds a selected model type `T` to a selected wire realization `U`, or stubs `U` as `T`; the round trip preserves the instance's observable behavior in the chosen domain | selected realizations + direction + implementation choice | the sides' adapters: `ToWire` is the binding, `FromWire` the stub |
| Participant | a process, in a language, that holds model instances, peers and entries; where policy lives and calls in from | address | the consumer's program |
| ModelInstance | a value of a selected model type, with relevant state/environment and actual behavior; structural compatibility makes it a candidate implementation, and satisfying `B` makes it lawful | its binding, once exported | a `ServerModel` value; a function |
| Wire | access to a destination by relative path, typed by a wire type and satisfying the wire contract: a connection at the empty path, a channel, a selection, a mount, a forward, a model instance presented | route from an origin, plus validity | `bitwire.Endpoint`: `peer.Wire()`, `at`, `mount`, `forward`, `ToWire` |
| Space and entry | a node mapping keys to (wire type, wire); an entry whose wire is a space again, or an end reaching a model instance | key within its node | a mount and a dispatcher's registrations; placement — which tree a mount belongs to — is a consumer's |
| Carrier and peer | what carries frames between two ends — a connection, or a channel over one; a peer is an end speaking the wire contract's invocation part: it mints correlation ids and serials, refuses, observes | connection instance + role | `duplex.Conn`, a tunnel channel; `runtime.Peer` |
| Message and invocation | request, response, event, cancel, at a path; an invocation is one request's life from capture to done, with its return capability | correlation id, per peer and direction | the profile's frames; `invocation.*` events |
| Reference | a model-valued position crossing a scope: a model instance exported as a binding in a scope on one side, imported as a model instance on the other, under an owner that supplies its lifetime; never a path string | binding identity with its nonce | `live.Over(peer)`: export, import, release; owners |
| Grant | authority: who may invoke what, decided at the effect boundary; never conferred by a digest, a mount or reachability | principal + issued authority | `auth`: grants, trust, guards |
| Observation | the record of one write by a peer at one layer, in one order, without the payload | peer + sequence | the observer |

## Edge types

Every edge is governed by a law or rule already stated — by Bitwire, by
this repository's pages or by its decisions; the graph adds none.

| edge | from → to | law or rule |
| --- | --- | --- |
| over | ModelContract → WireContract: a protocol over the access contract; within a WireContract, access over invocation; WireContract → Carrier | each layer speaks the one beneath it; everything on the wire belongs to exactly one layer |
| applies | ModelContract (an application) → ModelContract (its constructor), with ordered argument contracts | adaptation commutes with instantiation: `interpret(B[A, C]) ≃ interpret(B)[interpret(A), interpret(C)]` |
| extends · uses | Side → Side; ModelContract → ModelContract (a type refers to another, an import) | the digest covers reachable imported content, so identity follows the edge |
| of | Side → ModelContract (a protocol) | a protocol has two sides, and a model instance implements exactly one |
| presents | ModelType → ModelContract, in a language; WireType → ModelContract, under a WireContract | interpretation preserves composition |
| binds · stubs | `Adapter[T, U]` → ModelType T and WireType U; `Adapter[U, T]` → WireType U and ModelType T | `BehaviorOf_T(stub(bind(i))) ≈ BehaviorOf_T(i)` — wire transparency in the chosen observation domain, recursively through arguments and results |
| implements | ModelInstance → Side | structural conformance: a model instance's methods have the required signatures directly or through a wire; full contract satisfaction is a separate relation |
| typed by | ModelInstance → ModelType; Wire, Entry, Reference → WireType; Message → ModelContract, one of its operations | a type descriptor accompanies a wire; the transport never inspects payloads |
| satisfies | ModelInstance → ModelContract; Wire → WireContract | the instance's behavior satisfies the specified laws; the wire case includes selection and mounting, held by conformance |
| reaches | Wire → ModelInstance (an end wire) | a model instance and its presentation through a wire are interchangeable at the model level |
| at | Wire → Wire, with a path | `at(at(w, a), b) ≃ at(w, a ++ b)`; `at(w, []) ≃ w` |
| holds · names | Space → Entry; Entry → Wire, under a key | forwarding is local: select the key, hand the rest of the path to its wire |
| carries | Carrier → Wire at the empty path; Channel → Connection | the boundary is not the carrier |
| ends · speaks | Peer → Carrier; Peer → WireContract, its invocation part | correlation, refusal and cancellation are the peer's, never re-derived above it |
| on | Message → Wire, at a path | per-wire order survives every composition; a send is accepted or the wire is over |
| correlates | Response → Request; Cancel → Request; Invocation → Request | one answer per request; serials increase per direction |
| exports · imports | Scope → Binding → ModelInstance; Reference → Binding | reference transfer: a path string is never a portable reference |
| owns · releases | Owner → Binding | lifetime is supplied beside the model instance; release is a barrier |
| grants | Grant → Principal, and → the operations it may invoke | access carries no authority; enforced at the effect boundary |
| records | Observation → Peer, Message, layer | told at the write; never a payload |

## Rewrite rules and their laws

The instance graph is closed under ten rules: each takes nodes of the
types above and yields nodes of those types and no others, which is what
"recursively composable" means here.

| rule | takes → yields | law |
| --- | --- | --- |
| connect | a carrier → a wire at the empty path, with a peer at each end | a connection is a wire at `[]` |
| open a channel | a connection → a connection, so a wire | a channel is a connection of the seam |
| `at(w, p)` | a wire → a wire at a relative origin | selection composes; selecting nothing changes nothing |
| `mount({k: w})` | wires under keys → one wire forwarding by key | mount is the inverse of selection; a node needs only its own map |
| `forward(w1, w2)` | two endpoints → each other's traffic | the existing roots keep admission and correlation |
| `Adapter[T, U](i)` | a model instance of selected type T → a wire of selected type U for the same contract | the binding |
| `Adapter[U, T](w)` | a wire of selected type U → a model instance of selected type T | the stub; `BehaviorOf_T(stub(bind(i))) ≈ BehaviorOf_T(i)` |
| `export(m)` | a model instance in a scope → a binding, and a reference that can cross | a model-valued position crosses as a reference |
| `import(r)` | a reference → a model instance | it arrives as a model instance; transfer then adaptation agrees with adaptation then transfer |
| `instantiate(B, A)` | a constructor and an argument → `B[A]` with its adapters | the adapters of `B[A]` are B's adapters supplied with A's |

The laws are two commuting squares: the operation
square (perform then adapt equals adapt then perform) and the construction
square (instantiate then adapt equals adapt then instantiate), which
[the generics page](declaration/generics.md#the-diagram-commutes) draws. A
definition-level rule, instantiate, and the adapters, which are code, meet
at the second square, which is why a generic slot needs both adapters — and
why that square is a statement about two pieces of code agreeing, the
adapters of `B[A]` against B's adapters supplied with A's, which is the form
the generator can be tested against.

## Five identities, five graph elements

The five things that vary independently about one entry are five
different elements of the graph, which is what keeping them apart means.

| identity | what it names | graph element |
| --- | --- | --- |
| declared | the model contract: `(path, digest)` of the canonical declaration, covering operations, events and reachable imports | the ModelContract node |
| applied | a closed generic application by its constructor and ordered arguments, such as `worker/Handler<worker/Job>` | the application node and its *applies* edges |
| route | the key an entry sits under and the relative path that reaches it; mounting multiplies routes and touches nothing else | the path on *at* and *holds* edges |
| live validity | whether the access still works now | the state of the Wire or Reference instance |
| presentation | the language projection and the profile carrying it | the ModelType and WireType nodes |

Two further separations follow from the node kinds. A hash names content —
a value, immutable, retrievable — while a wire is access to something that
can act; a fact may carry a hash and never a wire. And access is not
authority: a *reaches* edge from a Wire to a ModelInstance implies no
*grants* edge, so reaching a target proves reachability and remounting it
manufactures nothing. A consumer may keep identities of its own beside
these five, such as a content hash; none of them is a second contract
identity.

## Where Nightseam's implementation sits

Nightseam realizes every node type; what it deliberately leaves out is
below the table.

| concept | Nightseam today |
| --- | --- |
| ModelContract | a family — `model.json`, `protocol.json`, `live.json` ([families](declaration/families.md)); built-in families for the tunnel, live, identity and auth vocabularies |
| WireContract | Bitwire 0.2.0 for access, named directly in every language, never copied, aliased or re-exported ([the wire](runtime/wire.md)); `nightseam.duplex/1` for invocation: request, response, event, cancel, the meta header, serials, coded refusals ([the profile](wire/profile.md)) |
| ModelType | the generated family in the language: its types, `ServerModel`, `ClientModel` ([generated code](declaration/generated.md)) |
| WireType | the wire schema and its digest ([declaration identity](declaration/declaration-identity.md)); the frames |
| Adapter | the sides' adapters: `<family>-binding` is `Adapter[T, U]` (`ToWire`), `<family>-client` is `Adapter[U, T]` (`FromWire`), for the selected model and wire realizations; the language identifier is the generator's target and the conformance matrix's row ([tiers](languages/tiers.md)) |
| Participant | the consumer's program: it implements a side and assembles transport, peer, layers and adapters ([the boundary](goals/boundary.md)) |
| ModelInstance | a `ServerModel` or `ClientModel` value — records of functions |
| Wire | `bitwire.Endpoint`: `peer.Wire()`, `at`, `mount`, `forward`, a `ToWire` endpoint, a wire pair |
| Space and entry | a mount and a dispatcher's registrations; no space tree — paths are local, never names |
| Carrier and peer | `duplex.Conn` over a WebSocket or a pipe, a tunnel channel ([the tunnel](runtime/tunnel.md)); `runtime.Peer`, `DuplexPeer` ([the peer](runtime/peer.md)) |
| Message and invocation | the four frame kinds; `invocation.capture`, `ready`, `begin`, `done`, `control` at the return capability |
| Reference | the live layer: scope, export, import, release, owners; `identity.check` on import ([the live layer](runtime/live.md)) |
| Grant | `auth`: grants as Archon envelopes, trust, guards, exposure ([the grant](auth/grant.md), [the exposure](auth/exposure.md)) |
| Observation | the observer, told at the write; the OpenTelemetry adapter ([the observer](runtime/observer.md)) |
| the laws | the conformance suite: scenarios and tables hold every language to them ([admission](admission.md)) |

What Nightseam does not realize is placement and policy: which tree a
mount belongs to, when an operation should run and what running it
establishes. Those are a consumer's, composed above the wire and calling
in from outside ([the boundary](goals/boundary.md)).

## Open choices

Seven choices change the model, and each needs a verdict before the graph
is written into any tree or tool. They are filed as [#640](https://github.com/Bitspark/nightseam/issues/640); the
verdict is copied there, this section is rewritten from it, and what
settles something becomes a [decision page](decisions/README.md).

| choice | options | recommendation |
| --- | --- | --- |
| WireContract | one node holding access and invocation; or two, AccessContract and InvocationContract | one — Bitwire states the access contract and names the profile that realizes it, so the pair is one contract with a shared part and a realization; the levels are an attribute |
| ModelType and WireType | nodes; or attributes on the adapters and on *typed by* | nodes — the fifth identity lives on them, and the adapters are typed by them |
| Adapter | a node of kind code; or an edge ModelType ↔ WireType | a node — rendered per language, with a direction, and what conformance exercises |
| Space and entry | in this graph, or only in a consumer's model | in the graph: a mount is an instance without placement rules; placement is a consumer's edges, not a different node |
| Grant | in this graph, or a separate graph joined at the effect boundary | in the graph, joined by one edge; the separation is the absence of any edge from Wire to Grant |
| Invocation | a node, or an edge between messages | a node — it has a life from capture to done; control and the return capability attach to it |
| Observation | a node, or an attribute stream on the peer | a node — questions are asked of it after the fact, in one order, from one place |

## Sources

- [Bitwire's contract](https://github.com/Bitspark/bitwire/blob/v0.2.0/docs/wire/contract.md) and [composition through Wire](https://github.com/Bitspark/bitwire/blob/0f30b515694cb3005403229be0f4e4158baf41b9/docs/composition.md) — the access contract adopted at v0.2.0, and its laws
- [generics.md](declaration/generics.md) — the diagram that commutes: adaptation and instantiation
- [declaration-identity.md](declaration/declaration-identity.md) — the canonical declaration, its digest and application identity
- [the goals](goals/README.md) — the directions the domain is measured against
- [admission.md](admission.md) — which concepts are primitives and which are compositions
- [architecture.md](architecture.md) — the tree that realizes the domain
