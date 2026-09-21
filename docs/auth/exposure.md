# The exposure

What it costs to call each member of a protected exposure, said once by the
application that owns the resources, held to the declared surface at
construction, and decided at every call — at dispatch, and again at the
effect. This is the packet
[#337](https://github.com/Bitspark/nightseam/issues/337) owes after its
recorded decision: the typed **exposure-policy binding**, its exhaustiveness
and refusals, how a request names its target, the recheck at the owner's
boundary, what a returned callable carries, what an emission is, and the
table both adapters are held to,
[`conformance/tables/auth-exposure.json`](../../conformance/tables/auth-exposure.json).

It builds on [the grant](grant.md) and [the authenticated
connection](connection.md), and it is where the three contracts #337 keeps
distinct meet without merging: the **model** says what exists; the
**authority profile** says what a chain permits; the **binding** says what
each member of *this* exposure requires. None of the three learns the
others' vocabulary.

## What an exposure is

One protected instance of a declared surface: a family's side, served by one
implementation, at one origin. The same declaration, digest and generated
adapter serve many exposures — a project's workers for its members, the same
workers for an auditor with other rights — and an exposure is the unit that
has a policy. The type has none: two exposures of one unchanged declaration
have two policies and one digest, and a value that travels between them
keeps the treatment of the exposure that exported it
([#367](https://github.com/Bitspark/nightseam/issues/367), rule 6: *authority
is not in the type*).

## The surface

An exposure's **members** are the things it can be asked to do:

| member key | what it is |
|---|---|
| `method:<name>` | a method of the exposed side |
| `event:<name>` | an event the exposed side emits — a disclosure toward each recipient |
| `callable:<Type>` | an invocation of a callable type the exposure exports — the `status`, `result` and `cancel` a returned `Job` carries, the `report` a supplied `ProgressSink` receives |

A member carries the top-level names of its request payload — a method's
params, an event's data, a callable's request — which are what a scope
template may name. Both sides have surfaces: a client exposing a callback
to the server is an exposure of the client's, decided by the client's
policy under the server's context — a reverse call meets the guard of the
side that owns the thing called.

The surface is identified by the family and its declaration digest
([#292](https://github.com/Bitspark/nightseam/issues/292)), so that a policy
written for one revision cannot be bound to another. Route prefixes and
target-language names are not members and never appear in a policy.

## The policy

A treatment per member, and nothing else:

```json
{
  "family": "worker",
  "digest": "sha256:…",
  "treatments": {
    "method:start":       {"kind": "guarded", "action": "write", "scope": "projects/{projectId}"},
    "method:list":        {"kind": "guarded", "action": "read",  "scope": "projects/{projectId}"},
    "event:progress":     {"kind": "guarded", "action": "read",  "scope": "projects/{projectId}"},
    "callable:JobStatus": {"kind": "guarded", "action": "read",  "scope": "{export}"},
    "callable:JobResult": {"kind": "guarded", "action": "read",  "scope": "{export}"},
    "callable:JobCancel": {"kind": "guarded", "action": "write", "scope": "{export}"}
  }
}
```

| kind | means |
|---|---|
| `guarded` | the caller's chain must grant `action` over the scope the template renders, at this call, now |
| `public` | callable with no context at all — an explicit choice, never a default |
| `denied` | refused for everyone; a member the exposure does not offer |

A `guarded` treatment carries an action and a scope template; `public` and
`denied` carry neither. The **scope template** is a grant scope entry with
holes: `{field}` names a member of the request payload, and `{export}` — on
a callable only — names the scope the owner recorded when it exported the
reference. A constant template names one scope for every call.

The policy is the consumer's, is data, and lives beside the declaration —
never in it. Nothing in `protocol.json` or `live.json` says `requires`, and
the generator renders nothing of a policy. The declaration is read by both
peers, every target and the specification; a fact that only one exposure's
guard interprets belongs where its interpreter lives.

## Construction

`Bind(surface, policy)` holds the policy to the surface **whole**, and
constructs nothing on refusal:

| refusal | when |
|---|---|
| `auth.contract_mismatch` | the policy names another family or digest — a policy for another revision |
| `auth.member_unbound` | a declared member has no treatment — including a member the declaration gained since the policy was written |
| `auth.member_undeclared` | a treatment names a member the surface does not declare |
| `auth.template_invalid` | a hole names no field of its member; `{export}` on a method or event; an action or scope on a `public` or `denied` treatment; a control character |

Each refusal names every member it is about, so one construction reports
every gap of its kind; where gaps of several kinds coexist, the first kind
in the table above is reported with all its members. **Omission never
creates a usable partial exposure**: a new member is a construction
failure, not a public route. An explicit `denied` is valid and deliberate.

A constructed binding's **routes** are exactly the declared members, each
once. Registered dispatch, a local self-reference, a reverse call, a returned
or supplied callable, a mount, a forward, and every presentation the backend
advertises reach the implementation through the binding or not at all; the
raw implementation is not a route. That is the property
[#355](https://github.com/Bitspark/nightseam/issues/355) demonstrates and
[#357](https://github.com/Bitspark/nightseam/issues/357)'s
`AUTH-EXPOSURE-005` holds; the table pins that the *decision* is the same on
every route, which is what makes a route inventory checkable.

## The target

The scope a call is held to comes from the request, syntactically, before
any authority is consulted:

```
scope = render(template, payload, export)
```

A hole is filled from the payload member it names — a string, or an
integer spelled in decimal — and the value must be a scope segment: no
separator, no control character, not empty. A value carrying `/` is
`auth.selector_invalid`, so a caller cannot traverse to a sibling or a parent
by the text it sends; a missing or non-scalar field is the same refusal. The
rendered scope is then what the grant's coverage rule decides on:
`projects/7` covers `projects/7/ledger` and does not cover `projects/70` or
`projects/8`, whatever prefix the call arrived under.

That is the whole of what the *request* contributes: **a selector, never
authority**. Whether project 7 exists, whether it is archived, whether it is
the caller's — the request cannot say, and the guard does not ask. The
owner resolves the actual target when it acts, inside its own transaction,
and a caller authorized for `projects/7` learns nothing about `projects/9999`
from a refusal, because the refusal is decided before the owner looks.

## The decision at dispatch

For a call to member `m` with payload `p` on a connection with context `c`
at time `t`:

1. `m` not in the surface → `auth.unknown_member`; no handler.
2. `denied` → `auth.member_denied`.
3. `public` → admitted, with `c`'s subject if there is one and none if not.
4. `guarded` → no context is `auth.unauthenticated`, never a downgrade;
   render the scope (`auth.selector_invalid`); then
   [`Call(root, c, {domain, action, scope}, t)`](connection.md#the-decision-at-the-call)
   — the chain held, its leaf the context's subject
   (`auth.subject_mismatch`), the request covered at every hop
   (`auth.denied` with the grant code and hop).

The result is a **decision**: the member, its kind, the subject, and for a
guarded member the action and the rendered scope. A decision admits a
handler to *start*.

## The decision at the effect

A decision at dispatch is a pre-filter. What admits the **effect** — the
write, the disclosure, the emission — is the same decision re-made at the
owner's boundary, at the effect's own time, inside the owner's transaction:

1. for a guarded member, the decision's subject held to the context's
   (`auth.subject_mismatch` — a decision made under another connection is
   nobody's here), then `Call` again with the effect time — so authority
   that expired between dispatch and effect refuses the effect
   (`auth.denied` / `expired`), with the connection still open and nothing
   revoked;
2. then the owner's own condition over the resolved target — the predicate
   only the owner can evaluate: the project exists, is not archived, is in a
   state that permits `write` — refused with the owner's own reason under
   the prefix `owner:`, and no effect.

The recheck boundary is the owner's transaction or lock, wherever it draws
it; the packet says only that the check and the effect meet there, so that a
state or authority that changed after an early check cannot authorize a
changed mutation. A gateway check is never the decision. Admitted in-flight
work is admitted — the packet neither rolls back a prior committed effect
nor adds a revocation lookup — and a *later* disclosure of that work is a
new decision.

## What a returned callable carries

When a handler returns a value holding callables — a `Job` with `status`,
`result` and `cancel` — the owner **exports** each reference under the
callable's member and the scope it assigns: `Export(ref, "callable:JobStatus",
"projects/7")`. A record is written once: exporting the same reference
under the same member and scope again is nothing, and under another is
refused — a record is never rewritten. The export record is what the reference *is* to this
exposure; an invocation is decided against it, with `{export}` rendering to
the recorded scope, under **the invoking connection's** context, at the
invocation's time. So:

- authorization to create a job is not authorization to use it: `status`,
  `result` and `cancel` each carry their own action, decided when called,
  and a chain that expires with the connection open refuses the next
  `result` (`AUTH-EXPOSURE-006`);
- a reference is decided for whoever holds it: a job started by one caller,
  its `status` invoked by another, is decided against the other's chain;
- a reference this exposure did not export is `auth.reference_unknown` to
  it — a type round trip, a conversion, a local optimization cannot
  manufacture a guarded reference's record or unwrap it, because the record
  is keyed by the reference, never by the declared type
  ([#367](https://github.com/Bitspark/nightseam/issues/367), rule 6);
- two exposures export the same declared type under two policies, and each
  reference keeps the treatment of the exposure that exported it.

A supplied callable — the client's `ProgressSink` — is the same thing from
the other side: the client exports it under its own exposure's policy, and
the server's `report` into it is decided under the server's context by the
client's guard.

## What an emission is

An event is a **disclosure toward each recipient**, and is decided per
recipient at emission time, against that recipient's connection context:
delivered where admitted, dropped where refused. A dropped recipient is not
told — a refusal is not a disclosure either — and the owner is, through the
observer. A subscriber whose authority expires while subscribed stops
receiving at the next emission, with nothing revoked and the subscription
intact. Continued disclosure — a followed stream, a replayed log, an
artifact read — is the same rule applied at each disclosure, never inherited
from the moment of subscribing.

## Forwarding and representation

Nothing here names a represented caller, and no metadata is read. A broker
is decided as itself, on its own connection, under its own chain — the same
decision with or without a `meta` entry claiming to act for someone
(`AUTH-EXPOSURE-007`). A broker that presents the represented caller's
chain is refused `auth.subject_mismatch` before coverage is considered,
whether or not the chain would have covered. To act *for* a person is to
present a chain the person issued — attenuated, ending at the broker — and
the decision then says the broker's subject with the person's grant in its
ancestry; a person whose grant carries no delegable action cannot be
represented at all, which is the correct default.

## The interface

Go, in `github.com/Bitspark/nightseam/auth/go`:

```go
package auth

type Member struct { Key string; Fields []string }
type Surface struct { Family, Digest string; Members []Member }
type Kind string                                    // "guarded" | "public" | "denied"
type Treatment struct { Kind Kind; Action, Scope string }
type Policy struct { Family, Digest string; Treatments map[string]Treatment }

type ConstructionError struct { Code Code; Members []string }
func Bind(s Surface, p Policy) (*Binding, *ConstructionError)
func (b *Binding) Routes() []string

func Render(template string, payload map[string]any, export string) (string, error)

type Decision struct { Member string; Kind Kind; Subject *[32]byte; Action, Scope string; Grant *grant.Verified }
type Refusal struct { Code Code; Grant *grant.Refusal }
func (b *Binding) Decide(root grant.Root, member string, payload map[string]any, export string, ctx *Context, now grant.Time) (*Decision, *Refusal)
type Condition func(scope string) (ok bool, reason string)
func (b *Binding) Effect(root grant.Root, d *Decision, ctx *Context, now grant.Time, cond Condition) *Refusal

func (b *Binding) Export(ref, member, scope string) error
func (b *Binding) Invoke(root grant.Root, ref string, payload map[string]any, ctx *Context, now grant.Time) (*Decision, *Refusal)

type Recipient struct { Name string; Ctx *Context }
type Delivery struct { Recipient string; Refused *Refusal }
func (b *Binding) Emit(root grant.Root, event string, data map[string]any, to []Recipient, now grant.Time) []Delivery
```

TypeScript, in `@nightseam/auth`, module `exposure`, the same names in the
language's spelling. The generated adapter of an exposed side is what calls
`Decide` before dispatch and `Effect` at the owner's boundary; the surface
it binds is derived from the generated model, never written by hand, which
is what makes a newly declared member fail construction rather than pass
unguarded.

## The table

[`conformance/tables/auth-exposure.json`](../../conformance/tables/auth-exposure.json):
35 cases over the `worker` surface — `start`, `list`, `progress`, and the
three callables a `Job` carries — and a `worker-client` surface exposing a
`ProgressSink`, with eight named policies, and contexts whose chains are
those of [the grant table](grant.md#the-table) plus a service key; the
shared envelopes are byte-identical to both earlier tables.

- `exposure_bind` — construction: bound, and each refusal with the members
  it names;
- `exposure_template` — rendering: fields, integers, constants, `{export}`;
  a missing field, a separator, an empty value, a non-scalar, a control
  character;
- `exposure` — scripts of `bind`, `call`, `export`, `invoke`, `emit`, each
  step recording its decision and, where an effect time is given, the
  decision at the effect. Held to #357's cases by name: complete surface and
  default denial (`001`); the same declaration under two policies (`002`);
  trusted resource binding — siblings, traversal, prefixes (`003`); state
  and time at the owning boundary (`004`); every route meeting the same
  decision (`005`); `Worker.start` and the returned `Job`'s callables under
  their own actions, after expiry, for another caller (`006`); the broker
  as itself, with copied metadata ignored and the represented caller's chain
  refused (`007`); public and denied treatments, and no downgrade (`008`).

An adapter passes when it reproduces every case in both languages. The
table was produced by a reference written from this page; the shipped
adapters are held to the data.

## What this packet does not decide

What a resource's state permits is the owner's condition, evaluated by the
owner. How a surface is derived from a generated model, and the exact shape
of the adapter that calls `Decide` and `Effect`, are
[#356](https://github.com/Bitspark/nightseam/issues/356)'s, on this
interface. Whether a policy may be loaded from a file, and in what format,
is a consumer's convenience the packet neither offers nor forbids — a
`Policy` is data, and its only rule is that it binds whole or not at all.
