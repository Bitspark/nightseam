# The authenticated connection

How a connection comes to have a subject, how a subject's grant reaches
the calls made on it, and how a key that holds no grant gets its first one.
This is the packet [#351](https://github.com/Bitspark/nightseam/issues/351)
owes: the audience a connection binds to, the possession proof, the
exchange that establishes a connection's context, the decision made at
every protected call, the bootstrap over Archon's login scheme, its state
machine and its recovery — and the table both implementations are held to,
[`conformance/tables/auth-boot.json`](../../conformance/tables/auth-boot.json).

It builds on [the grant](grant.md) and adds no cryptography of its own. The
proofs are Archon's possession scheme in a domain of this page's; the
bootstrap is Archon's login scheme with this page's law admitting the
delegation it carries. It is a specification; the adapters
([#356](https://github.com/Bitspark/nightseam/issues/356)) and the verifiers
([#353](https://github.com/Bitspark/nightseam/issues/353),
[#354](https://github.com/Bitspark/nightseam/issues/354)) are held to it.

## What is being established, and what is not

A **context** is what a protected connection has exactly one of: the
**subject** — a key whose possession was proved on this connection — and the
**chain** that key presented, kept as bytes, immutable for the connection's
life. That is all. A context is not a decision: the decision that a call is
permitted is made **at the call**, against the chain, at that time, by the
resource owner's guard. A context is not portable: it belongs to the
physical connection whose proof made it, is inherited by the channels
tunnelled over that connection, and reaches no other connection, no
forwarded frame and no reconnection. **A different subject is a different
connection** — the initial construction [#351](https://github.com/Bitspark/nightseam/issues/351)
selected, and the whole of its replay and cross-user story.

The server side of a connection is identified by the **audience** the
client reached, authenticated by the transport it reached it over — the
TLS the browser or service already trusts. This packet proves the client to
the server; it does not prove a server key to the client. Calls the server
makes back into the client's handlers run on the same connection under the
same context.

## The audience

The audience is the connection's base URL as the service is configured to
know itself, and **it is never sent**: the server recomputes every binding
from its own configuration, and a proof made for one audience verifies at
no other. It is [Archon's audience grammar](grant.md#the-container-is-archons)
applied to the connection URL:

```
audience = scheme "://" host [ ":" port ] *( "/" segment )
```

`ws` folds to `http` and `wss` to `https`; the scheme and host lowercase;
a port that is the folded scheme's default (80, 443) is omitted; segments
keep their case and their percent-escapes, undecoded. Refused rather than
normalized: any byte outside printable ASCII, a scheme outside the four, a
query, a fragment, userinfo, an empty segment — which is what a trailing
slash is — a `.` or `..` segment, a malformed escape, a port with a leading
zero or outside 1..=65535. Archon's own derivation, given the same URL with a
login tail, yields the same audience; the table pins both.

A presentation with no URL — an in-memory pipe — has no audience and
**cannot run the exchange**. A peer over it may be given a context by
construction, which is trust, not authentication, and the page says so
where it is offered.

## The connection proof

Possession, in the domain **`nightseam-auth/1`**, of the key the client
claims, over a nonce the server minted for this connection and a binding
only this connection has:

```
binding = 0x01 ‖ u16(len audience) ‖ audience
proof   = possession.prove(seed, "nightseam-auth/1", nonce, binding)
```

`0x01` is the connection role. For the audience of the table:

```
01 0022 68747470733a2f2f6170692e6578616d706c652e746573742f6e696768747365616d
```

The signed bytes are Archon's — `0x01 ‖ u16(len nonce) ‖ nonce ‖
u16(len binding) ‖ binding`, Ed25519ph with the domain as context — so a
proof in `archon-login/1` or `nightseam-grant/1`, or over a bare nonce, or
raw, never verifies here. The nonce is 32 bytes of the server's entropy,
one per challenge.

What the binding buys is stated in Archon's terms: *a signed nonce alone is
relayable*. Bound to the audience, a proof captured by an attacker who
holds a connection to another service is worthless there; bound to the
nonce, a proof from an earlier connection is worthless on a later one. The
table holds both.

## The exchange

Two requests of the profile, under the reserved prefix **`auth.`**, in the
vocabulary of [the built-in family `auth`](../declaration/builtins/auth/README.md)
declared under `internal/model/builtin/` — as `live.` and `channel.` are —
with the peer dispatching them by name and knowing nothing of what they
mean. Bare Nightseam has no `auth.` handlers installed and refuses the
requests as any unknown method; a family may not declare an operation under
the prefix.

**`auth.challenge`** — a request from the client, no parameters. The server
mints a nonce, keeps it as the connection's one pending challenge, and
answers `{"nonce": "<64 hex>"}`. A second challenge replaces the first.

**`auth.prove`** — a request from the client:

```json
{
  "subject": "ed25519:<64 hex>",
  "possession": "<128 hex>",
  "chain": ["<envelope hex>", "…"]
}
```

The server, in this order, and whatever the outcome **consumes the pending
nonce** — one attempt per challenge:

1. no pending challenge → `auth.no_challenge`;
2. the possession does not verify under `subject` for this connection's
   audience and nonce → `auth.possession_invalid`;
3. the chain does not hold under [`grant.Inspect`](grant.md#the-interface)
   at the server's decision time → `auth.chain_refused`, carrying the grant
   code and hop, nothing more;
4. the chain's leaf subject is not `subject` → `auth.subject_mismatch`
   — a chain is usable by the key it was issued to and by no one else;
5. otherwise the context is made and the answer is `{"expires_at": n}` for a
   chain whose effective validity is finite, and `{}` for one that is
   unbounded — the declared `Proved`, whose one member is present exactly
   when there is an expiry.

Once a context exists, both requests are refused `auth.established`: a
context is immutable, and a change of subject is a new connection. On a
connection with no audience both are refused `auth.unsupported`. A malformed
parameter — a nonce or key of the wrong size, a chain that is not a list of
hex envelopes — is `auth.malformed`.

A protected operation called before a context exists is refused
`auth.unauthenticated`. That is a refusal, not a fallback: an operation is
**public** only where the exposure policy says so explicitly
([#337](https://github.com/Bitspark/nightseam/issues/337)), and nothing is
downgraded when the exchange has not run or has failed. Bounded
unauthenticated work — the two requests above, the public operations, and
nothing else — is what a connection may do before it has a context.

## The decision at the call

Every protected call is decided where its effect or disclosure happens,
against the context's chain, at that moment:

```
held    = grant.Inspect(root, context.chain, now)          — structure and time
          held.subject == context.subject                   — identity
verified = grant.Verify(root, context.chain, {domain, action, scope}, now)   — coverage
```

with the request the exposure policy derives for that member, in that
order: a chain that does not hold is `auth.denied` with the grant code and
hop; one that holds but **does not end at the connection's proved subject**
is `auth.subject_mismatch`, decided before coverage so that a mismatched
chain learns nothing about what it would have covered; one that holds and
is the subject's but does not cover the request is `auth.denied`. The
identity rule is what makes cross-user reuse of a connection, a pooled peer
serving two people, or a context installed by construction with the wrong
chain, fail at the call rather than succeed quietly.

Because the decision is made at use, **expiry is enforced at use**: a
connection established at `now < expires_at` whose chain expires while the
connection stays open sees its next protected call refused
`auth.denied` / `expired`. Nothing is revoked, nothing is released, the
connection is not closed — release, cancellation and expiry stay the three
things [the grant](grant.md) keeps apart. Whether the named resource is in a
state that permits the action is still the owner's, checked inside its own
transaction; `Verify` is the pre-filter, never the decision.

## The bootstrap

A key that holds no grant — a browser's ephemeral key, a CI job's, a
container's — gets its first one from a key that does, by
[Archon's login scheme](grant.md#the-container-is-archons): the holder
proves, unrelayably, that it agrees to let one key act at one service for
one stated scope and time, and hands that key a delegation whose meaning is
this page's to admit. The scheme's binding, its two proofs, its records and
its transport are Archon's, pinned by Archon's `login_*` vectors; this page
defines the **terms** a request carries, the **law** that admits an answer,
and the **state** the service keeps — and its state differs from Archon's
own login server in one deliberate way, said below.

### The terms

The login request's `scope` entries are the requested grant, rendered in a
tagged grammar the person reads verbatim before signing:

```
action:<a>      an action the key may use          (sorted, then)
delegable:<a>   an action it may grant onward       (sorted, then)
scope:<s>       a scope entry                       (sorted, then)
depth:<n>       the delegation budget, at most once, last; absent is 0
```

Each list strictly ascending by bytes, `delegable` within `action`, every
entry under [the grant's](grant.md#the-body) rules. A request whose entries
are not in that canonical order is refused at `begin`: one request has one
binding, and what the CLI shows is what is signed. `valid_for` is seconds,
1..=2592000.

### The law: `AdmitAuthority`

An answer's `authority` is a chain of grant envelopes, root first. Before
the answer is stored — and refused answers leave the request pending —
the server holds it to:

1. the login proof verifies under `principal` for the stored request and
   the server's own audience (Archon's `login_verify`; a proof over other
   terms, or for another audience, fails here);
2. the chain holds under `grant.Inspect` at the server's time;
3. the chain ends at the request's browser key;
4. **if the browser key is the principal** — the holder logging in as
   itself — no delegation is needed and none is admitted: the chain is the
   principal's own, and it must cover every requested `action` at every
   requested `scope` under `grant.Verify`. A same-principal login never
   requires permission to delegate;
5. otherwise the leaf was issued by `principal`, and it is **no wider than
   the request**: its actions within the requested actions, its delegables
   within the requested delegables, each scope entry covered by a requested
   one, its depth at most the requested depth;
6. and it is **bounded by the request**: finite, expiring no later than the
   server's time plus `valid_for` plus 60 seconds of issuer clock skew. An
   unbounded leaf is refused. A login always bounds.

A refusal is `invalid_grant` with the grant code and hop. The server never
signs: **credential admission cannot mint a grant** — it can only admit one
the principal already had the authority to issue.

### The state

One record per pending login, keyed by id, with a version the store
compares on every transition:

| state | entered by | leaves by |
|---|---|---|
| `pending` | `begin` — id (16 bytes) and nonce (32 bytes) minted by the server, the terms parsed, `expires_at = now + 300` | `answer` verified → `answered`; `expires_at` reached → gone |
| `answered` | `answer` — principal, possession and authority stored, `retained_to = now + 300` | first verified `collect` → `collected`; `retained_to` reached → gone |
| `collected` | `collect` | `retained_to` reached → gone |

`read` answers only a pending record. `answer` on a record that is not
pending is `invalid_request` (one issued result; HTTP 409); on a version
that is not the one read, `invalid_request` and the caller re-reads — that
is the compare-and-set, and it is what two replicas answering the same login
resolve by. `collect` verifies the collect proof first (`invalid_grant`
otherwise), then paces: a poll sooner than 5 seconds after the last
*verified* poll is `slow_down` without moving the reference time, so an
eager client is delayed and a stranger's polls delay nobody. A verified
collect on a pending record is `authorization_pending`; on an answered
record it moves it to collected and returns the answer; on a collected
record it **returns the same answer again**.

That last clause is the recovery, and the one place this state differs from
Archon's login server, which drops the record on first collect. Here a
collected answer stays until `retained_to`, so a client whose response was
lost recovers **that exact result** by an authenticated retry: the same
principal, possession and authority, no second grant, no change of key,
recipient or scope, and no restarting of the validity the leaf carries.
Two replicas collecting the same login make one transition and hand out
the same bytes. A recovery identifier alone recovers nothing — the collect
proof by the browser key does. After `retained_to` the record is gone and a
collect is `expired_token`; `sweep` drops what is gone. The store is the
consumer's — any store that offers a versioned compare-and-set — and
protocol replay state is not a revocation database: nothing here revokes,
and expiry of a login record says nothing about the grant it delivered.

Before authentication the service does bounded work: at most 4096 live
records (`slow_down` beyond), one id begun once while live, no protected
dispatch on any of it. The issuer's outage after it has answered changes
nothing — the answer is stored, and the grant it carries verifies with the
issuer offline.

## Forwarding and representation

There is no field that names a represented user, and the exchange carries
no authority from one connection to another. A service B calling a service
A does so on B's own connection, with B's own subject and B's own chain. If
B acts *for* a person, that is a chain whose ancestry passes through the
person's key — a grant the person issued to B, attenuated — and A sees it
as such at the call. Neither connectivity nor copied metadata delegates:
a broker with broad authority of its own that forwards a narrow caller's
request presents its own chain, and the resource owner decides for the
broker, not for the caller it did not hear from. Explicit downstream
representation is a grant, and nothing else.

## The interface

Go, in `github.com/Bitspark/nightseam/auth/go`:

```go
package auth

const Domain = "nightseam-auth/1"

func Audience(url string) (string, error)                     // the grammar above, or refused
func Binding(audience string) ([]byte, error)                 // 0x01 ‖ u16(len) ‖ audience
func Prove(seed []byte, audience string, nonce []byte) ([]byte, error)
func Verify(pubkey []byte, audience string, nonce, proof []byte) bool

type Context struct { Subject [32]byte; Chain [][]byte; Validity grant.Validity; Since uint64 }
type Refusal struct { Code Code; Grant *grant.Refusal }        // Grant set for chain_refused and denied
type Code string                                              // auth.unsupported … auth.malformed, as above

// The per-connection exchange, over the peer the layer is composed onto.
type Connection interface {
	Challenge(nonce []byte) ([]byte, *Refusal)
	Prove(root grant.Root, subject [32]byte, proof []byte, chain [][]byte, now grant.Time) (*Context, *Refusal)
	Context() *Context
}
// The decision at the call.
func Call(root grant.Root, ctx *Context, request grant.Request, now grant.Time) (grant.Verified, *Refusal)

// The bootstrap, over a consumer-supplied versioned store.
type Terms struct { Actions, Delegable, Scope []string; Depth uint8 }
func ParseTerms(entries []string) (Terms, error)
func RenderTerms(t Terms, withDepth bool) []string
type Record struct { /* version, state, request, terms, times, answer */ }
type Store interface { Get(id []byte) (Record, bool); Put(r Record, expectVersion int) bool; Sweep(now uint64) }
type Service struct { Root grant.Root; Audience string; Store Store }
func (s *Service) Begin(now uint64, id, nonce, browser []byte, scope []string, validFor uint32) (Record, LoginCode)
func (s *Service) Read(now uint64, id []byte) (Record, LoginCode)
func (s *Service) Answer(now uint64, id []byte, principal [32]byte, proof []byte, authority [][]byte, expectVersion int) (LoginCode, *grant.Refusal)
func (s *Service) Collect(now uint64, id, proof []byte) (Answer, LoginCode)
type LoginCode string  // invalid_request | invalid_grant | expired_token | authorization_pending | slow_down
```

TypeScript, in `@nightseam/auth`, module `connection`, the same names in
the language's spelling, with `Uint8Array` for bytes and `bigint` for time.
Both depend on `grant`, on Archon's `sdk` (`possession`, `login`) and on
nothing else; `Call`, `Verify` and the state transitions are pure over
their arguments, and the store is the only place state lives.

## The table

[`conformance/tables/auth-boot.json`](../../conformance/tables/auth-boot.json):
52 cases in six families, every entropy and every clock an input, over the
keys of [the grant table](grant.md#the-table) plus one browser key, with the
envelopes it shares with that table byte-identical to it.

- `boot_audience` — the grammar, accepted and refused; every accepted case
  agrees with Archon's derivation of the same URL with a login tail;
- `auth_binding`, `auth_prove` — the binding bytes; a proof that verifies,
  relayed to another audience, replayed against another challenge;
- `auth_connect` — scripts of challenges and proves: a delegated subject
  established; the principal itself; a copied chain without possession; a
  chain not ending at the subject; a proof for another audience; a proof
  over an earlier connection's challenge; prove before challenge; one
  attempt per challenge; an immutable context; a tampered chain; an expired
  chain; a bounded chain with no time; a pipe; a malformed nonce;
- `auth_call` — allowed; expired at use with the connection open; a scope
  the chain does not reach; cross-user reuse; an early protected call;
- `boot_login` — transcripts of `begin`, `read`, `answer`, `collect`,
  `sweep` with the record's state after each: the whole login and its
  recovery and its end; pending, pacing and an offline issuer; a proof over
  other terms; the wrong recipient; an authority wider than the request;
  one valid too long; one unbounded; a copied chain without possession; a
  second answer; a lost compare-and-set; an unanswered login expiring; a
  collect proof by another key; two collectors; the principal as itself,
  admitted and refused; and three refusals at `begin`.

In `boot_login` steps, keys are named, a chain is named by its envelopes,
`now` is the server's clock, and each step records what it answered and
the record's state afterwards. An implementation passes when it reproduces
every case in both languages. The table was produced by a reference written
from this page against Archon at `b8501fac`; the shipped implementations
are held to the data.

## What this packet does not decide

Which members of which exposure are guarded, public or denied, and what
`domain`, `action` and `scope` a call is held to, are
[#337](https://github.com/Bitspark/nightseam/issues/337)'s typed binding.
Whether the named resource permits the action in its current state is the
owner's. A mutual proof of a server key to a client, and an exchange over a
presentation with no audience, are not offered here; if either is wanted it
is a named obstruction, not an omission.
