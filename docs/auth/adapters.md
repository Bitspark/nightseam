# The adapters

How the profile meets a peer: where the exchange of [the authenticated
connection](connection.md) is installed, where a handler finds the context
the exchange made, and what an implementation calls so that [the
exposure](exposure.md)'s decision is made before its body runs and again at
its effect. This is what [#356](https://github.com/Bitspark/nightseam/issues/356)
owes on the three packets, in both languages, and it adds nothing to the
runtime: the exchange is two named handlers beside `live.invoke`, the context
lives with the layer over the peer, and a guard is a call the implementation
makes.

## The layer over a peer

`live.Over` is the shape. The profile is composed onto a peer once it is
built and before it reads — in `Prepare`, where a server installs its model —
and from then on the peer carries the connection's one context:

```go
// Go, in github.com/Bitspark/nightseam/auth/go/over — the one package of the
// module that composes onto the runtime; auth and grant stay pure
layer, err := over.Over(peer, over.Options{
	Root:     root,                  // the configured trust root and domain
	Audience: audience,              // as the service knows itself; "" for a pipe
	Now:      clock,                 // func() grant.Time — the decision time, read at each decision
	Nonce:    entropy,               // func() [32]byte — one per challenge
})
ctx := over.ContextOf(handlerCtx)   // *auth.Context, or nil before the exchange
```

```ts
// TypeScript, in @nightseam/auth/over
const layer = authOver(peer, { root, audience, now, nonce });
const ctx = contextOf(context);     // Context | undefined, from a handler's context
```

The layer registers `auth.challenge` and `auth.prove` on the peer and keeps
the connection state with it. `Now` and `Nonce` are the caller's: the layer
reads no clock and mints no entropy of its own, which is what keeps every
decision it makes reproducible from its inputs.

**Where a handler finds the context.** In Go the connection's context is
on the `context.Context` every handler of the connection runs with — the
one `Authenticate` returned for a socket, the one `NewPeer` was built with
for a pipe, inherited by every channel tunnelled over the connection — and
`over.ContextOf` reads it there. The place is prepared before the peer is
built: a server returns `over.Prepare(r.Context())` from `Authenticate`, a
pipe's constructor builds its peer with it, and `Over` refuses a peer whose
context was not prepared, since no handler of it could ever find a context. In TypeScript every handler's context
carries the peer of the connection that brought the frame in, and
`contextOf` reads the layer over that peer. An exported callable's closure
runs under the context of the connection that *invokes* it, in both
languages, which is why a returned `Job`'s `status` is decided for whoever
holds it and never for whoever made it.

**A presentation with no audience** — a pipe, a model wire accessed with no
peer between — runs no exchange: `auth.challenge` is refused
`auth.unsupported`. Such a peer is given a context by construction
(`over.Trust(ctx, context)` in Go, `trusted` in the TypeScript options),
which is trust, not authentication, and the layer refuses the exchange on
it as `auth.established`.

## The guard

The implementation of an exposed side calls the guard where the packet says
the decision is made:

```go
surface, _ := over.SurfaceOf("worker", "server", protocol.WireDeclaration(), protocol.WireDigest())
binding, refused := auth.Bind(surface, policy)   // whole, or every gap named
guard, _ := over.NewGuard(binding, root, clock)

func (w *worker) Start(ctx context.Context, params protocol.StartRequest) (protocol.Job, error) {
	decision, err := guard.Decide(ctx, "method:start", over.Payload(params))   // before the body
	if err != nil { return protocol.Job{}, err }                             // a *runtime.PublicError with the code
	w.mu.Lock(); defer w.mu.Unlock()                                          // the owner's transaction
	if err := guard.Effect(ctx, decision, w.condition); err != nil { return protocol.Job{}, err }
	…
}
```

```ts
const surface = surfaceOf('worker', 'server', wireDeclaration, wireDigest);
const bound = bind(surface, policy);
const g = guard(bound, { root, now });
list(params, context) {
  const decision = g.decide(context, 'method:list', payloadOf(params));   // throws a DuplexError with the code
  …
  g.effect(context, decision, condition);
}
```

`SurfaceOf` reads the side's members and the family's callables off the
declaration the generator emitted, with the top-level names of each request
payload — never a hand-written list — so that a member the declaration
gains fails `Bind` until the policy names it. `Payload` is the request's
top-level members as the wire spells them, which is what a scope template's
holes are filled from.

A guard's refusal crosses the wire as the profile's public error: the code
as the packets spell it (`auth.unauthenticated`, `auth.denied`,
`auth.member_denied`, `auth.selector_invalid`, `auth.subject_mismatch`), and
where a chain refused, the grant's code and hop as its data.

The rest of the guard follows the exposure packet by name: `Export` records
what a returned callable is to this exposure, `Invoke` decides it for
whoever invokes it, `Emit` decides an event per recipient before it leaves.

## What is held where

The layer and the guard are composition: they add no member to any frame,
no condition to the live scope, and no reading of `meta`. Their behavior
is the packets', held by the tables through the verifiers they compose, and
by the profile's own tests over a pipe of two peers with the tables' keys.
The generated Go↔TypeScript fixture under `cmd/nightseam/testdata/` runs the
exchange over real sockets in both directions and every route the synthetic
witness of [#355](https://github.com/Bitspark/nightseam/issues/355) ran, with
real proofs and a clock the test moves.
