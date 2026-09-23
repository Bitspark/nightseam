# Documentation

The [README](../README.md) is the short path: what Nightseam is, how it is
installed and what a family looks like. The reference is here, in sets by
who reads it — each page one kind of thing, and each kind in one place.
Three of the sets stand in one relation: the **goals** say where the tree
is going, the **state** pages (`wire/`, `runtime/`, `declaration/`,
`languages/`) say what it is, and the **decisions** say what it was chosen
over and why.

The [repository-home decision](decisions/the-reusable-foundation-lives-in-nightseam.md)
keeps runtime, generator, declaration/identity and optional rooted-grant authority
implementation in Nightseam, with Bitwire as the home of the shared Wire
contract, adopted at v0.2.0 in 0.6.0 under
[#421](https://github.com/Bitspark/nightseam/issues/421) and
[#439](https://github.com/Bitspark/nightseam/issues/439). The state pages
describe today's implementation; public contracts, builds and examples remain
usable without private repository access.

## The whole, in one figure — `architecture.md`

Before the sets: one picture of the projection stages — the declaration,
its rendering, the generated packages, the native model, the adapter
boundary, the wire and the dispatcher, the layers over the peer, the peer
and the seam — as two realizations meeting at the frames, with the
interface at each stage and the page that documents it.

| page | what |
| --- | --- |
| [architecture.md](architecture.md) | the figure, read stage by stage: what each stage is, its Go and TypeScript surface, where it is described, and how the stages relate |

## The domain it targets, as a graph — `domain.md`

Beside the figure: the domain Nightseam is for — typed access to models
through wires, across languages and carriers — modeled as a typed graph
over the published laws, with one table mapping each concept to what the
tree realizes today and the choices that still need a verdict.

| page | what |
| --- | --- |
| [domain.md](domain.md) | the graph: ModelContract → ModelType → ModelInstance and WireContract → WireType → Wire, adapters as code with their law, the edges and the law behind each, the rewrite rules that close the instance graph, the five identities as graph elements, the mapping onto the tree, the open choices |

## Contracts and representations — `theory/`

The [theory index](theory/README.md) separates the general model from its
Nightseam interpretation and implementation evidence. It defines coordinate
cells and transformation paths, shapes, behavioral contracts, realizations,
instances and satisfaction. Structural navigation and whole-shape substitution
have commuting laws; lifting them to behavior requires further interpretation.
It includes a TypeScript metalanguage, a concept/law cross-reference, checked
examples, and a self-contained visual edition. These are developing definitions and laws;
the state pages below continue to describe the implemented interfaces.

## Where it is going — `goals/`

The north stars, abstract and never done: what Nightseam is for, in one
respect each, at the limit — so that a reviewer can take one page and the
tree at any point and say where the tree falls short and how to get closer.
[goals/README.md](goals/README.md) says what a goal page is, the two tests
that keep it abstract, and how a review is asked for and what it returns.

| page | at the limit |
| --- | --- |
| [boundary.md](goals/boundary.md) | Nightseam holds exactly what is the same for every consumer; every policy calls in from outside |
| [layering.md](goals/layering.md) | each layer speaks the one beneath and knows nothing of those above |
| [composability.md](goals/composability.md) | every part is usable alone, stacks on any part beneath, and is replaced without any part beside it knowing |
| [agnosticism.md](goals/agnosticism.md) | of no host, no transport, no backend and no language; each language one realization of a thing defined outside all of them |
| [declarative.md](goals/declarative.md) | what two of anything would each hold a copy of is stated once as data, and a stale derivation is a failure |
| [configurability.md](goals/configurability.md) | every bound is the consumer's to set under one name everywhere; nothing that decides a policy is a setting |
| [extensibility.md](goals/extensibility.md) | a language, a target, a transport, a layer, an adapter joins at one named point, and nothing that exists moves |
| [observability.md](goals/observability.md) | any question about what a peer did can be answered after the fact, at every layer, in one order, from one place, without seeing what was said |

## What is admitted — `admission.md`

Between the goals and the state: the test a concept passes to be
Nightseam's — in scope at its level and not composable from the justified
primitives beneath it — the four classes a concept ends in, and every concept
the tree has or has proposed in its class. A design issue that admits a
concept supplies this page's six answers.

| page | what |
| --- | --- |
| [admission.md](admission.md) | the test: scope, the basis one level down with nothing grandfathered, the composition attempt, what an obstruction is and is not, the smallest primitive, the alternatives; primitive, composition, domain semantics, unresolved candidate; the session's concepts, what stays beneath them, the data level and the live layer, each classified |

## What crosses the wire — `wire/`

What a peer of any language sends, accepts and refuses, in the wire's own
terms and nobody's runtime. A runtime for another language needs these
pages and [the driver protocol](../conformance/DRIVER.md), and nothing else.

| page | what |
| --- | --- |
| [profile.md](wire/profile.md) | `nightseam.duplex/1`: the seam beneath, the subprotocol, the envelope, ids and correlation, requests and their errors, events, limits and backpressure, trace context, request metadata, close codes, a frame on the wire |
| [tunnel.md](wire/tunnel.md) | channels over one peer: the four operations, ids by parity, credit, limits and closes |
| [live.md](wire/live.md) | callable values across one connection: a binding, a scope and a reference, `live.invoke` and `live.release`, what is refused, and why a binding is not a channel |
| [vocabulary.md](wire/vocabulary.md) | how a layer speaks on the wire: the test that decides whether something new is the profile's, a layer's own vocabulary or a header, and the rules a layer's vocabulary follows |

## What a consumer calls — `runtime/`

The surface of each runtime component, Go and TypeScript side by side: each
fact once, with both spellings.

| page | what |
| --- | --- |
| [wire.md](runtime/wire.md) | relative-path frame access: local pairs, peer and channel origins, selection, mounting, forwarding, bounds, context and ownership |
| [record.md](runtime/record.md) | opaque Wire recording, consumer storage, atomic replay-to-live handoff, bounded subscriber isolation and preserved reference scope |
| [peer.md](runtime/peer.md) | the peer: the seam beneath, making one, the order it starts in, options and limits, the subprotocol surface, the server's hooks, errors, request metadata, the propagator, the validator |
| [tunnel.md](runtime/tunnel.md) | the tunnel: making one and when, the surface, options, credit in each language |
| [live.md](runtime/live.md) | the live layer: making a scope and when, the surface, a reference that is minted and never constructed, the rules a consumer relies on, bounds |
| [observer.md](runtime/observer.md) | the observer across the layers: the rule, order, taking one, the console and `slog` adapters, the OpenTelemetry adapter, every event in both languages, a layer of your own |
| [compositions.md](runtime/compositions.md) | what a consumer composes out of them: a callback supplied and an interface returned, the rules that construction keeps and who keeps them, the three cancellations, interface, stream, cell and topic side by side, forwarding, and where the basis stops short |

## What a consumer declares — `declaration/`

The input side: the tier files, the tool, and what comes out.

| page | what |
| --- | --- |
| [families.md](declaration/families.md) | a family in tiers: the files, the tier rule, `model.json`, `protocol.json`, the per-target names |
| [generics.md](declaration/generics.md) | the holes in a declaration: parameters of two sorts at every level, `apply` and `with`, how each language instantiates them, the diagram that commutes |
| [declaration-identity.md](declaration/declaration-identity.md) | the canonical declaration graph, its exact digest bytes, reachable content and application identity |
| [generator.md](declaration/generator.md) | the commands and their flags, which version rendered this, what the packages own, the specification |
| [generated.md](declaration/generated.md) | what the generated packages export in each language: the protocol, binding and client packages in Go, the client and binding packages in TypeScript, the errors |
| [proof-findings.md](declaration/proof-findings.md) | what the proof family established across the wire: the settled forms held in both languages, the generated roles each target renders and their scenario coverage, and the limits of bounded example synthesis |
| [pipeline.md](declaration/pipeline.md) | how the generator renders and the packages it is made of — for whoever changes it |

## The languages — `languages/`

| page | what |
| --- | --- |
| [tiers.md](languages/tiers.md) | languages, profiles and tiers: the four promises, the profiles the conformance suite holds them to, which tier guarantees what and when, how the suite enforces it |
| [onboarding.md](languages/onboarding.md) | how a language joins: the lanes in order, the testee, what a language promises before it is in the table |
| [rust.md](languages/rust.md) | Rust core crates, checkout use, Cargo packaging and the outside WebSocket consumer smoke |
| [python.md](languages/python.md) | Python 3.11+, installing the source or wheel, using an asyncio peer and checking the installed distribution |

## What an authority proves — `auth/`

The optional rooted-grant profile: what a consumer that opts in signs,
carries and checks, in bytes both verifiers are held to. Bare data and RPC
need none of it. The contracts are specified here before either verifier
is written; a page is the packet its issue owes, and its table is what the
implementations are held to.

| page | what |
| --- | --- |
| [grant.md](auth/grant.md) | the grant: an Archon envelope in the `nightseam-grant/1` domain around a canonical body — subject, parent digest, scope, actions, delegable, depth, validity — the chain, the order it is held in and each refusal, issuance and inherit, the bounds, the interface both verifiers export, and the table of 72 cases |
| [connection.md](auth/connection.md) | the authenticated connection: the audience a connection binds to and never sends, the possession proof in `nightseam-auth/1`, `auth.challenge` and `auth.prove` and the one immutable context they make, the decision at every protected call and expiry at use, the bootstrap over Archon's login scheme — its tagged terms, the law that admits an answer, the recoverable state machine — forwarding as a grant and nothing else, the interface, and the table of 52 cases |
| [exposure.md](auth/exposure.md) | the exposure: one protected instance of a declared surface and its policy — a treatment per member, guarded, public or denied — bound whole to the surface at construction or not at all, the target a request names as a selector and never as authority, the decision at dispatch and the decision again at the owner's effect, what an exported callable carries, an emission decided per recipient, the broker as itself, the interface, and the table of 35 cases held to the eight named acceptance cases |
| [adapters.md](auth/adapters.md) | the adapters: the profile over a peer — the exchange installed beside `live.*`, the one context reachable from every handler of the connection, a context by construction for a pipe — the guard an implementation calls at dispatch and at its effect, the surface read off the generated declaration, and what holds them |

## Why it is this way — `decisions/`

The record of what was decided and why, one page per decision: the
question, what was decided, what the alternative cost, the goal it serves,
and since when. A state page keeps the rule and one sentence of why and
points here for the argument; a review reads this record before proposing
what was already tried. [decisions/README.md](decisions/README.md) indexes
them.

## Around the repository

| page | what |
| --- | --- |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | the short path in: what to run, how a change is cut |
| [COLLABORATION.md](../COLLABORATION.md) | how work is organized: the boundary rule, no legacy, parity, the two tiers, goldens, lanes, how a change lands |
| [conformance/DRIVER.md](../conformance/DRIVER.md) | the conformance suite: the protocol a language's testee speaks to the runner, every op, and how a language joins |
| [examples/README.md](../examples/README.md) | the getting-started: one family, generated and committed, a Go server and a TypeScript client a consumer installs and runs |
| [RELEASING.md](../RELEASING.md) | what is published and how a release is cut |
| [CHANGELOG.md](../CHANGELOG.md) | what landed, by version |
| [SECURITY.md](../SECURITY.md) | reporting a vulnerability, and what counts as one |
| [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) | what is expected of everyone here, and how a concern is raised |
