package grant

import (
	"github.com/Bitspark/archon/core/go/crypto"
)

// Inspect holds a chain — envelopes, root first — to its length, its
// ancestry, its domain, its attenuation and its time, with no request yet:
// steps 1, 2 and 4 of the evaluation, which is what establishing a
// connection and issuance need. It answers the leaf's subject and depth,
// the chain's effective validity and the hop count, or the first refusal.
func Inspect(root Root, chain [][]byte, now Time) (Verified, *Refusal) {
	verified, _, refusal := evaluate(root, chain, nil, now)
	return verified, refusal
}

// Verify holds a chain to a request: Inspect's evaluation with coverage
// between — every hop grants the request's action and covers its scope,
// and every hop but the last may delegate the action. A chain covers one
// concrete request at every hop or it covers nothing. Whether the
// presenter holds the leaf's key is not this function's question, and
// what the action means for the resource is the resource owner's.
func Verify(root Root, chain [][]byte, request Request, now Time) (Verified, *Refusal) {
	verified, _, refusal := evaluate(root, chain, &request, now)
	return verified, refusal
}

// evaluate runs the evaluation of docs/auth/grant.md in its order, the
// first failure being the answer: length; per hop, root first, the
// envelope, the body, the ancestry, the domain and the attenuation; then,
// with a request, coverage at every hop; then time, sampled once and
// applied to every finite hop. It also hands back the decoded grants, for
// issuance to hold a child against the leaf without opening it again.
func evaluate(root Root, chain [][]byte, request *Request, now Time) (Verified, []Grant, *Refusal) {
	if len(chain) == 0 {
		return Verified{}, nil, &Refusal{Code: Malformed, Hop: 0}
	}
	if len(chain) > maxChain {
		return Verified{}, nil, &Refusal{Code: ChainTooLong, Hop: 0}
	}
	grants := make([]Grant, 0, len(chain))
	for hop, env := range chain {
		issuer, g, code := Open(env)
		if code != "" {
			return Verified{}, nil, &Refusal{Code: code, Hop: hop}
		}
		if hop == 0 {
			if g.Parent != nil {
				return Verified{}, nil, &Refusal{Code: ParentMismatch, Hop: hop}
			}
			if issuer != root.Key {
				return Verified{}, nil, &Refusal{Code: RootMismatch, Hop: hop}
			}
		} else {
			previous := grants[hop-1]
			if g.Parent == nil || *g.Parent != Digest(chain[hop-1]) {
				return Verified{}, nil, &Refusal{Code: ParentMismatch, Hop: hop}
			}
			if issuer != previous.Subject {
				return Verified{}, nil, &Refusal{Code: IssuerMismatch, Hop: hop}
			}
		}
		if g.Domain != root.Domain {
			return Verified{}, nil, &Refusal{Code: DomainMismatch, Hop: hop}
		}
		if hop > 0 {
			if code := attenuated(g, grants[hop-1]); code != "" {
				return Verified{}, nil, &Refusal{Code: code, Hop: hop}
			}
		}
		grants = append(grants, g)
	}
	if request != nil {
		if request.Domain != root.Domain {
			return Verified{}, nil, &Refusal{Code: DomainMismatch, Hop: 0}
		}
		for hop, g := range grants {
			if !contains(g.Actions, request.Action) || !covered(request.Scope, g.Scope) {
				return Verified{}, nil, &Refusal{Code: NotCovered, Hop: hop}
			}
			if hop < len(grants)-1 && !contains(g.Delegable, request.Action) {
				return Verified{}, nil, &Refusal{Code: NotCovered, Hop: hop}
			}
		}
	}
	validity := Validity{}
	for hop, g := range grants {
		if !g.Validity.Finite {
			continue
		}
		if !now.Present {
			return Verified{}, nil, &Refusal{Code: TimeRequired, Hop: hop}
		}
		if now.Now >= g.Validity.ExpiresAt {
			return Verified{}, nil, &Refusal{Code: Expired, Hop: hop}
		}
		if !validity.Finite || g.Validity.ExpiresAt < validity.ExpiresAt {
			validity = g.Validity
		}
	}
	leaf := grants[len(grants)-1]
	return Verified{Subject: leaf.Subject, Depth: leaf.Depth, Validity: validity, Hops: len(grants)}, grants, nil
}

// attenuated holds a child to its parent: the parent's depth is above zero
// and the child's is strictly less; the child's actions are within the
// parent's delegable set; every scope entry of the child is covered by one
// of the parent's; a finite parent has a finite child expiring no later.
// Nothing is clipped: a child wider than its parent is refused, not
// narrowed.
func attenuated(child, parent Grant) Code {
	if parent.Depth == 0 || child.Depth >= parent.Depth {
		return WidenedDepth
	}
	if !subset(child.Actions, parent.Delegable) {
		return WidenedActions
	}
	if !allCovered(child.Scope, parent.Scope) {
		return WidenedScope
	}
	if parent.Validity.Finite && (!child.Validity.Finite || child.Validity.ExpiresAt > parent.Validity.ExpiresAt) {
		return WidenedValidity
	}
	return ""
}

// Issue seals child under the issuer's own chain — parents, root first,
// ending in a grant to the issuer — after holding that chain as Inspect
// does and the child to the same attenuation against its leaf. A root
// grant has an empty chain and is issued by the root key alone. The child's
// parent is the digest of the last envelope of parents; a supplied parent
// that names another is ParentMismatch at the child's hop. With inherit
// the child's validity resolves to the leaf's before signing — a root grant
// may not inherit, since it states its validity — and no inherit marker
// ever enters a grant. Issuance is deterministic: the same inputs seal the
// same bytes, so a retry renews nothing. A refusal is at the hop it arose
// at, the child's own being len(parents).
func Issue(root Root, parents [][]byte, issuerSeed []byte, child Grant, inherit bool, now Time) ([]byte, *Refusal) {
	hop := len(parents)
	var leaf *Grant
	if hop > 0 {
		_, grants, refusal := evaluate(root, parents, nil, now)
		if refusal != nil {
			return nil, refusal
		}
		leaf = &grants[hop-1]
	}
	if inherit {
		if leaf == nil {
			return nil, &Refusal{Code: Malformed, Hop: hop}
		}
		child.Validity = leaf.Validity
	}
	if leaf == nil {
		if child.Parent != nil {
			return nil, &Refusal{Code: ParentMismatch, Hop: hop}
		}
	} else {
		digest := Digest(parents[hop-1])
		if child.Parent != nil && *child.Parent != digest {
			return nil, &Refusal{Code: ParentMismatch, Hop: hop}
		}
		child.Parent = &digest
	}
	if _, err := Encode(child); err != nil {
		return nil, &Refusal{Code: Malformed, Hop: hop}
	}
	var issuer [32]byte
	if len(issuerSeed) == crypto.SeedSize {
		copy(issuer[:], crypto.PublicKeyFromSeed(issuerSeed))
	}
	if leaf == nil {
		if len(issuerSeed) != crypto.SeedSize || issuer != root.Key {
			return nil, &Refusal{Code: RootMismatch, Hop: hop}
		}
	} else if len(issuerSeed) != crypto.SeedSize || issuer != leaf.Subject {
		return nil, &Refusal{Code: IssuerMismatch, Hop: hop}
	}
	if child.Domain != root.Domain {
		return nil, &Refusal{Code: DomainMismatch, Hop: hop}
	}
	if leaf != nil {
		if code := attenuated(child, *leaf); code != "" {
			return nil, &Refusal{Code: code, Hop: hop}
		}
	}
	sealed, err := Seal(issuerSeed, child)
	if err != nil {
		return nil, &Refusal{Code: Malformed, Hop: hop}
	}
	return sealed, nil
}
