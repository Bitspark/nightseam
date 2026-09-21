# The grant

The bytes of a rooted, attenuated, expiring grant — the authority profile
[#345](https://github.com/Bitspark/nightseam/issues/345) selected — and the
one pure function that holds a chain of them to a request. This is the
contract packet [#348](https://github.com/Bitspark/nightseam/issues/348)
owes: the layout, the rules, the refusals, the interface both languages
declare, and the table both are held to,
[`conformance/tables/auth-grant.json`](../../conformance/tables/auth-grant.json).

It is a specification. No verifier ships with it; the Go and TypeScript
verifiers ([#353](https://github.com/Bitspark/nightseam/issues/353),
[#354](https://github.com/Bitspark/nightseam/issues/354)) are held to this
page and its table, and neither is written from the other.

## The container is Archon's

A grant is a **body** sealed in an **Archon envelope**, and nothing of the
signing is this packet's to define. The envelope is
`"arcn" ‖ 0x01 ‖ u8(len domain) ‖ domain ‖ pubkey[32] ‖ signature[64] ‖ payload`,
where the signature is Ed25519ph over `0x02 ‖ payload` with the domain as
the RFC 8032 context; `open` takes the domain the *verifier* expects and
refuses an envelope that claims another. That container carries, by
design, no expiry, no issuer field, no audience and no key id — each being
*"either policy or a second spelling of the key"* — which is exactly why it
fits: the grant body carries what a grant means, and the envelope carries
who said it.

The grant domain is **`nightseam-grant/1`**. A signature made in any other
domain, or raw, verifies as nothing here; a grant signature can never be
replayed as a possession proof or as any other envelope of this profile.

The **issuer** of a grant is the envelope's public key. The body does not
repeat it.

## The body

Version-tagged, fixed order, length-prefixed, big-endian; one encoding per
grant.

```
body      = 0x01                                   the body version
          ‖ u8(len domain) ‖ domain                the trust domain, 1..=255 bytes
          ‖ subject[32]                            the key the grant is issued to
          ‖ parent                                 0x00, or 0x01 ‖ digest[32]
          ‖ list(scope)                            what the subject may reach
          ‖ list(actions)                          what the subject may do there
          ‖ list(delegable)                        which of those it may pass on
          ‖ u8(depth)                              how many further hops it may pass them
          ‖ validity                               0x00, or 0x01 ‖ u64(expires_at)

list(xs)  = u16(count xs) ‖ ( u16(len x) ‖ x )*
```

| member | rule |
|---|---|
| `domain` | the trust domain the scope lives in — one configured root per domain. UTF-8, no control characters (U+0000–U+001F, U+007F), 1..=255 bytes. |
| `subject` | an Ed25519 public key. The key is the principal; there is no other. |
| `parent` | absent for a root grant. Otherwise the SHA-256 of the **whole envelope** of the parent grant, so a child names one exact signed parent and no other. |
| `scope` | entries of 1..=1024 bytes, UTF-8, no control characters, at most 64, in **strictly ascending byte order** — canonical and duplicate-free by construction. An entry is a path; `a/b` covers `a/b` and everything under `a/b/`. |
| `actions` | the same rules; the actions the subject may use. |
| `delegable` | the same rules; a subset of `actions`; the actions the subject may grant onward. Using and delegating are different permissions. |
| `depth` | 0..=15. A child's depth is strictly less than its parent's; a grant at depth 0 has no children. |
| `validity` | explicit: `0x00` unbounded, or `0x01` followed by an absolute expiry in seconds since the Unix epoch, UTC. A body with neither is malformed; nothing is an implicit infinity. |

`Encode` refuses a body outside these rules; `Decode` is total and answers
a code, never a panic. A trailing byte, a truncated field, an unknown
parent or validity kind, a list out of order, or a `delegable` outside
`actions` is `malformed`; a first byte other than `0x01` is
`unsupported_version`. A body is at most 65535 bytes.

### A body, read

The child grant `alice_bob` from the table — `alice` grants `bob` `read` and
`write` under `projects/7`, delegable `read`, two more hops, until
1800000000:

```
01                                   version
0c 6578616d706c652e74657374          domain "example.test"
ed4928c6…9e00                        subject: the key `bob` (32 bytes)
01 a2231b39…9e                       parent: the digest of the root_alice envelope
0001 000a 70726f6a656374732f37        scope: ["projects/7"]
0002 0004 72656164 0005 7772697465   actions: ["read", "write"]
0001 0004 72656164                   delegable: ["read"]
02                                   depth 2
01 000000006b49d200                  finite, expires_at 1800000000
```

126 bytes. Sealed by `alice` in `nightseam-grant/1`, the envelope is 245
bytes, and its SHA-256 is what the grant from `bob` to `carol` names as its parent.

## The chain, and what holds it

A **chain** is a list of envelopes, root first. The verifier is configured
with one **root** — a public key and a trust domain — and takes a
**request** — a domain, an action and a scope entry — and, where any hop is
bounded, one **decision time**.

Evaluation runs in this order, and the first failure is the answer:

1. **Length.** An empty chain is `malformed`; more than 16 envelopes is
   `chain_too_long`, refused before any is opened.
2. **Per hop, root first** — open the envelope in the grant domain
   (`envelope_invalid`), decode the body (`malformed`,
   `unsupported_version`), then:
   - **ancestry** — hop 0 has no parent and its issuer is the configured
     root (`parent_mismatch`, `root_mismatch`); every later hop's parent is
     the SHA-256 of the previous envelope and its issuer is the previous
     grant's subject (`parent_mismatch`, `issuer_mismatch`). A skipped hop,
     a leaf from another chain and a grant signed by a key that was not
     granted to are each refused here, before their contents are read.
   - **domain** — every grant's domain is the configured one
     (`domain_mismatch`).
   - **attenuation**, against the previous grant: its depth is above zero
     and this one's is strictly less (`widened_depth`); this grant's actions
     are within its **delegable** set (`widened_actions`); every scope entry
     is covered by one of its entries (`widened_scope`); if it is finite,
     this one is finite and expires no later (`widened_validity`). Nothing
     is clipped: a child wider than its parent is refused, not narrowed.
3. **Coverage, at every hop.** The request's domain is the configured one
   (`domain_mismatch`). Every hop grants the request's action and covers
   the request's scope, and every hop but the last has the action in its
   delegable set (`not_covered`, at the first hop that does not). A chain
   covers one concrete request at every hop or it covers nothing.
4. **Time.** If any hop is finite, a decision time is required
   (`time_required`), it is sampled once and applied to every hop, and it
   must be strictly before each finite expiry (`expired`) — half-open, no
   grace, no clock read from the presenter. An entirely unbounded chain
   needs no clock.

What comes back is the leaf's **subject**, its **depth**, the chain's
effective **validity** — the earliest finite expiry, or unbounded — and the
hop count. `Inspect` is the same evaluation without step 3 — a chain held
to its length, ancestry, domain, attenuation and time, with no request yet
— which is what [establishing a connection](connection.md#the-exchange)
and issuance need; every refusal of the table that arises in steps 1, 2 or
4 holds `Inspect` as it holds `Verify`. **Whether the presenter holds that subject's key is not this
function's question**; possession is [#351](https://github.com/Bitspark/nightseam/issues/351)'s,
and what the action means for the resource is the resource owner's.

A refusal is a **code and a hop index** and nothing else — no member of any
grant is echoed. `Open` on an envelope whose body is refused answers the
code alone: the issuer that sealed a malformed body is not handed out. The codes are the closed set above; a program branches on
them, as it does on every refusal of the profile.

## Issuance

`Issue` signs a child under the issuer's own chain — root first, ending in a
grant *to* the issuer — after verifying that chain exactly as above, minus
coverage, and holding the child to the same attenuation against its leaf.
The issuer must be the leaf's subject (`issuer_mismatch`). A root grant has
an empty chain and is issued by the root key alone (`root_mismatch`
otherwise). `Issue` sets the child's parent — the digest of the chain's
last envelope, none for a root grant — and a child that arrives naming
another is `parent_mismatch`. A refusal of the child itself carries the
child's hop, one past the chain's last: hop 0 for a root grant. The child
is held to attenuation and to nothing else: its own expiry is not compared
with the decision time, and a finite root grant needs no time to issue.

Validity may be given as **inherit** at issuance: it resolves to the
parent's validity — finite or unbounded — *before* signing, and no inherit
marker ever enters a grant. A root grant may not inherit; it states its
validity. An earlier expiry than the parent's is permitted; a later one, or
an unbounded child under a finite parent, is refused. Issuing under a finite
parent needs the decision time and refuses an expired parent.

Issuance is deterministic: the same inputs seal the same bytes. A retry
therefore renews nothing — it yields the grant it already yielded, with the
absolute expiry it already carried.

## Bounds

| bound | value | why |
|---|---|---|
| depth | 15 | so a chain is at most 16 hops, and a hop is one signature |
| chain | 16 envelopes | refused before opening; the work of `Verify` is linear in it |
| entries per list | 64 | |
| bytes per entry | 1024 | |
| body | 65535 bytes | the envelope's payload is unbounded; the body is not |
| domain | 255 bytes | the envelope's own bound |

No network, no membership query, no revocation, no status lookup, no
freshness epoch: the verifier reads its arguments and answers.

## The interface

What both verifiers export, by these names. Go:

```go
package grant // github.com/Bitspark/nightseam/auth/go/grant

const Domain = "nightseam-grant/1"

type Validity struct { Finite bool; ExpiresAt uint64 }          // seconds since the epoch, UTC
type Grant struct {
	Domain    string
	Subject   [32]byte
	Parent    *[32]byte
	Scope     []string
	Actions   []string
	Delegable []string
	Depth     uint8
	Validity  Validity
}
type Root struct { Key [32]byte; Domain string }
type Request struct { Domain, Action, Scope string }
type Time struct { Present bool; Now uint64 }
type Verified struct { Subject [32]byte; Depth uint8; Validity Validity; Hops int }
type Refusal struct { Code Code; Hop int }
type Code string  // the codes above, as constants

func Encode(g Grant) ([]byte, error)
func Decode(body []byte) (Grant, Code)
func Seal(issuerSeed []byte, g Grant) ([]byte, error)
func Open(envelope []byte) (issuer [32]byte, g Grant, code Code)
func Digest(envelope []byte) [32]byte
func Inspect(root Root, chain [][]byte, now Time) (Verified, *Refusal)   // steps 1, 2 and 4: the chain held without a request
func Verify(root Root, chain [][]byte, request Request, now Time) (Verified, *Refusal)
func Issue(root Root, parents [][]byte, issuerSeed []byte, child Grant, inherit bool, now Time) ([]byte, *Refusal)
```

TypeScript, in `@nightseam/auth`, module `grant`:

```ts
export const DOMAIN = "nightseam-grant/1";
export type Validity = { finite: false } | { finite: true; expiresAt: bigint };
export interface Grant { domain: string; subject: Uint8Array; parent?: Uint8Array; scope: string[]; actions: string[]; delegable: string[]; depth: number; validity: Validity }
export interface Root { key: Uint8Array; domain: string }
export interface Request { domain: string; action: string; scope: string }
export type Time = { present: false } | { present: true; now: bigint };
export interface Verified { subject: Uint8Array; depth: number; validity: Validity; hops: number }
export interface Refusal { code: Code; hop: number }
export type Code = "malformed" | "unsupported_version" | "envelope_invalid" | "chain_too_long" | "domain_mismatch" | "root_mismatch" | "parent_mismatch" | "issuer_mismatch" | "widened_actions" | "widened_scope" | "widened_depth" | "widened_validity" | "not_covered" | "time_required" | "expired";

export function encode(g: Grant): Uint8Array;                 // throws on a body outside the rules
export function decode(body: Uint8Array): Grant | Code;
export function seal(issuerSeed: Uint8Array, g: Grant): Uint8Array;
export function open(envelope: Uint8Array): { issuer: Uint8Array; grant: Grant } | Code;
export function digest(envelope: Uint8Array): Uint8Array;
export function inspect(root: Root, chain: Uint8Array[], now: Time): Verified | Refusal;
export function verify(root: Root, chain: Uint8Array[], request: Request, now: Time): Verified | Refusal;
export function issue(root: Root, parents: Uint8Array[], issuerSeed: Uint8Array, child: Grant, inherit: boolean, now: Time): Uint8Array | Refusal;
```

Both depend on Archon's `core` and `sdk` for the envelope, domain signing
and key derivation, and on nothing else. `Verify` and `Issue` are pure: no
I/O, no clock of their own, no state.

## The table

[`conformance/tables/auth-grant.json`](../../conformance/tables/auth-grant.json)
holds 72 cases, each naming its family — `grant_encode`, `grant_decode`,
`grant_seal`, `grant_open`, `grant_verify`, `grant_issue` — over six keys
derived from fixed seeds and twenty named envelopes, with every byte
deterministic: Ed25519 signatures are, and the encoding is canonical. Each
case states its inputs and its exact output — bytes, a verified result, or
a code and a hop — independently of any implementation. Among them: the
canonical bytes of the worked example; a flipped byte, a foreign signing
domain and a body of another version; a wrong root, a root grant carrying a
parent, a skipped hop, a leaf from another chain, a forged issuer; each
widening; exactly-at-expiry refused; a bounded chain with no time refused;
an all-unbounded chain needing none; one decision time across the chain;
inherit resolving to the parent's validity; a retry sealing the same bytes;
seventeen envelopes refused before one is opened.

In the table, keys are named and a parent is named by the envelope whose
digest it is; a `now` of `null` is no trusted time; and a child whose
`validity` is `"inherit"` in a `grant_issue` case is issued with `inherit`
set — the one `grant_encode` case that offers `"inherit"` to `Encode`
records that no body can carry it.

A verifier passes the packet when it reproduces every case in both
languages. The table was produced by a reference encoder and evaluator
written from this page against Archon at `b8501fac`, and is pinned as
data; the shipped verifiers are held to the data, not to that program.

## What this packet does not decide

The AUTH exchange that carries a grant, the possession proof that binds a
subject to a connection, and the bootstrap that issues the first grant are
[#351](https://github.com/Bitspark/nightseam/issues/351)'s. Which members of
which exposure require which action is
[#337](https://github.com/Bitspark/nightseam/issues/337)'s typed binding,
and whether the named resource is in a state that permits the action is the
resource owner's, checked at the effect. A grant declared as a value in a
family — a Grant record carrying subject, actions and expiry as data — is a
declaration like any other and puts no authority into any type's identity.
