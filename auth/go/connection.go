package auth

import (
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// nonceSize is the size of a challenge nonce: 32 bytes of the server's
// entropy, one per challenge.
const nonceSize = 32

// proofSize is the size of a possession proof: one Ed25519 signature.
const proofSize = 64

// Context is what a protected connection has exactly one of: the subject —
// a key whose possession was proved on this connection — and the chain
// that key presented, kept as bytes, immutable for the connection's life;
// with the chain's effective validity at establishment and the decision
// time it was established at (zero when the chain needed none). A context
// is not a decision: the decision that a call is permitted is made at the
// call, against the chain, at that time.
type Context struct {
	Subject  [32]byte
	Chain    [][]byte
	Validity grant.Validity
	Since    uint64
}

// Connection is the per-connection exchange, a state machine the peer the
// layer is composed onto drives by name: auth.challenge is Challenge,
// auth.prove is Prove, and Context is what the connection has once the
// exchange has succeeded. NewConnection makes one.
type Connection interface {
	// Challenge takes the nonce the server minted — 32 bytes of its
	// entropy — keeps it as the connection's one pending challenge, and
	// answers it; a second challenge replaces the first. Refused
	// Unsupported on a connection with no audience, Established once a
	// context exists, Malformed for a nonce of the wrong size.
	Challenge(nonce []byte) ([]byte, *Refusal)
	// Prove takes the subject the client claims, its possession proof and
	// the chain it presents, and whatever the outcome consumes the pending
	// nonce: NoChallenge with none pending; PossessionInvalid when the
	// proof does not verify under the subject for this connection's
	// audience and nonce; ChainRefused, with the grant code and hop, when
	// the chain does not hold under grant.Inspect at now; SubjectMismatch
	// when the chain's leaf is not the subject; otherwise the context is
	// made and answered. Refused Unsupported and Established as Challenge
	// is, and Malformed for a proof of the wrong size, none of which
	// consumes the nonce.
	Prove(root grant.Root, subject [32]byte, proof []byte, chain [][]byte, now grant.Time) (*Context, *Refusal)
	// Context is the connection's context, nil until the exchange has
	// succeeded.
	Context() *Context
}

// NewConnection makes the exchange of one connection at audience, the
// service's own configuration. An empty audience is a presentation with no
// URL — an in-memory pipe — whose exchange is refused Unsupported; an
// audience outside the grammar is an error, since every proof made for it
// would fail closed.
func NewConnection(audience string) (Connection, error) {
	if audience != "" {
		if _, err := ConnectionBinding(audience); err != nil {
			return nil, err
		}
	}
	return &connection{audience: audience}, nil
}

// connection is the state machine: at most one pending nonce, at most one
// context, and the audience it is bound to. It is not safe for concurrent
// use; the peer serialises a connection's requests.
type connection struct {
	audience string
	pending  []byte
	context  *Context
}

func (c *connection) Challenge(nonce []byte) ([]byte, *Refusal) {
	if c.audience == "" {
		return nil, &Refusal{Code: Unsupported}
	}
	if c.context != nil {
		return nil, &Refusal{Code: Established}
	}
	if len(nonce) != nonceSize {
		return nil, &Refusal{Code: Malformed}
	}
	c.pending = append([]byte(nil), nonce...)
	return append([]byte(nil), nonce...), nil
}

func (c *connection) Prove(root grant.Root, subject [32]byte, proof []byte, chain [][]byte, now grant.Time) (*Context, *Refusal) {
	if c.audience == "" {
		return nil, &Refusal{Code: Unsupported}
	}
	if c.context != nil {
		return nil, &Refusal{Code: Established}
	}
	if len(proof) != proofSize {
		return nil, &Refusal{Code: Malformed}
	}
	if c.pending == nil {
		return nil, &Refusal{Code: NoChallenge}
	}
	nonce := c.pending
	c.pending = nil
	if !Verify(subject[:], c.audience, nonce, proof) {
		return nil, &Refusal{Code: PossessionInvalid}
	}
	held, refusal := grant.Inspect(root, chain, now)
	if refusal != nil {
		return nil, &Refusal{Code: ChainRefused, Grant: refusal}
	}
	if held.Subject != subject {
		return nil, &Refusal{Code: SubjectMismatch}
	}
	kept := make([][]byte, 0, len(chain))
	for _, env := range chain {
		kept = append(kept, append([]byte(nil), env...))
	}
	c.context = &Context{Subject: subject, Chain: kept, Validity: held.Validity, Since: now.Now}
	return c.context, nil
}

func (c *connection) Context() *Context {
	return c.context
}

// Call is the decision at a protected call, made where its effect or
// disclosure happens, against the context's chain, at that moment, in
// this order: a chain that does not hold under grant.Inspect is Denied
// with the grant code and hop; one that holds but does not end at the
// connection's proved subject is SubjectMismatch, decided before coverage
// so that a mismatched chain learns nothing about what it would have
// covered; one that holds and is the subject's but does not cover the
// request is Denied. No context is Unauthenticated: never a downgrade.
// Because the decision is made at use, expiry is enforced at use.
func Call(root grant.Root, ctx *Context, request grant.Request, now grant.Time) (grant.Verified, *Refusal) {
	if ctx == nil {
		return grant.Verified{}, &Refusal{Code: Unauthenticated}
	}
	held, refusal := grant.Inspect(root, ctx.Chain, now)
	if refusal != nil {
		return grant.Verified{}, &Refusal{Code: Denied, Grant: refusal}
	}
	if held.Subject != ctx.Subject {
		return grant.Verified{}, &Refusal{Code: SubjectMismatch}
	}
	verified, refusal := grant.Verify(root, ctx.Chain, request, now)
	if refusal != nil {
		return grant.Verified{}, &Refusal{Code: Denied, Grant: refusal}
	}
	return verified, nil
}
