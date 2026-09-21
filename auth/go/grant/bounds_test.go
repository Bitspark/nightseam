package grant_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/Bitspark/archon/core/go/crypto"
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// TestChainBoundBeforeOpening holds that seventeen envelopes are refused
// before any is opened — sixteen of the same garbage are refused at the
// first envelope, seventeen at the length — so the work of Verify is
// bounded by the packet before it is bounded by the input.
func TestChainBoundBeforeOpening(t *testing.T) {
	tb := loadTable(t)
	root := tb.root(t, jsonRoot{Key: "root", Domain: "example.test"})
	garbage := bytes.Repeat([]byte{0xff}, 300)
	sixteen := make([][]byte, 16)
	for i := range sixteen {
		sixteen[i] = garbage
	}
	if _, refusal := grant.Inspect(root, sixteen, grant.Time{}); refusal == nil || refusal.Code != grant.EnvelopeInvalid || refusal.Hop != 0 {
		t.Fatalf("sixteen garbage envelopes: %v", refusal)
	}
	seventeen := append(sixteen, garbage)
	if _, refusal := grant.Inspect(root, seventeen, grant.Time{}); refusal == nil || refusal.Code != grant.ChainTooLong || refusal.Hop != 0 {
		t.Fatalf("seventeen garbage envelopes: %v", refusal)
	}
	if _, refusal := grant.Issue(root, seventeen, tb.seed(t, "root"), grant.Grant{Domain: "example.test"}, false, grant.Time{}); refusal == nil || refusal.Code != grant.ChainTooLong {
		t.Fatalf("issuing under seventeen envelopes: %v", refusal)
	}
}

// TestDeepChain issues a chain of sixteen hops — the root grant at depth
// 15 down to a leaf at depth 0, each inheriting its parent's validity —
// verifies the leaf's request through all of them, and holds that the leaf
// can issue nothing more: a seventeenth hop is refused at the leaf's
// depth, and a seventeenth envelope at the length.
func TestDeepChain(t *testing.T) {
	root := grant.Root{Domain: "deep.test"}
	seeds := make([][]byte, 17)
	keys := make([][32]byte, 17)
	for i := range seeds {
		seeds[i] = bytes.Repeat([]byte{byte(i + 1)}, 32)
		copy(keys[i][:], crypto.PublicKeyFromSeed(seeds[i]))
	}
	root.Key = keys[0]
	var chain [][]byte
	for hop := 0; hop < 16; hop++ {
		child := grant.Grant{
			Domain:    "deep.test",
			Subject:   keys[hop+1],
			Scope:     []string{"a"},
			Actions:   []string{"read"},
			Delegable: []string{"read"},
			Depth:     uint8(15 - hop),
			Validity:  grant.Validity{Finite: true, ExpiresAt: 2000000000},
		}
		env, refusal := grant.Issue(root, chain, seeds[hop], child, hop > 0, grant.Time{Present: true, Now: 1000})
		if refusal != nil {
			t.Fatalf("hop %d: refused %s at hop %d", hop, refusal.Code, refusal.Hop)
		}
		chain = append(chain, env)
	}
	verified, refusal := grant.Verify(root, chain, grant.Request{Domain: "deep.test", Action: "read", Scope: "a/b"}, grant.Time{Present: true, Now: 1000})
	if refusal != nil {
		t.Fatalf("refused %s at hop %d", refusal.Code, refusal.Hop)
	}
	if verified.Hops != 16 || verified.Depth != 0 || verified.Subject != keys[16] || verified.Validity != (grant.Validity{Finite: true, ExpiresAt: 2000000000}) {
		t.Fatalf("verified %+v", verified)
	}
	seventeenth := grant.Grant{Domain: "deep.test", Subject: keys[0], Scope: []string{"a"}, Actions: []string{"read"}, Depth: 0}
	if _, refusal := grant.Issue(root, chain, seeds[16], seventeenth, true, grant.Time{Present: true, Now: 1000}); refusal == nil || refusal.Code != grant.WidenedDepth || refusal.Hop != 16 {
		t.Fatalf("the leaf at depth 0 issued: %v", refusal)
	}
	if _, refusal := grant.Inspect(root, append(chain, chain[0]), grant.Time{Present: true, Now: 1000}); refusal == nil || refusal.Code != grant.ChainTooLong {
		t.Fatalf("seventeen envelopes: %v", refusal)
	}
}

// TestBodyBounds holds the decoder to the packet's bounds before it does
// the work the input asks for: a list count or an entry length over its
// bound is refused as it is read, a body over 65535 bytes before it is
// read, and the encoder refuses what would not fit.
func TestBodyBounds(t *testing.T) {
	tb := loadTable(t)
	base, _ := tb.grant(t, jsonGrant{Domain: "example.test", Subject: "alice", Validity: []byte(`"unbounded"`)})
	body, err := grant.Encode(base)
	if err != nil {
		t.Fatal(err)
	}
	// The scope list starts right after the parent kind byte.
	scopeAt := 1 + 1 + len(base.Domain) + 32 + 1
	claimed := append([]byte(nil), body...)
	binary.BigEndian.PutUint16(claimed[scopeAt:], 65535)
	if _, code := grant.Decode(claimed); code != grant.Malformed {
		t.Fatalf("a claimed count of 65535 entries decodes as %q", code)
	}
	claimed = append([]byte(nil), body[:scopeAt]...)
	claimed = binary.BigEndian.AppendUint16(claimed, 1)
	claimed = binary.BigEndian.AppendUint16(claimed, 1025)
	claimed = append(claimed, bytes.Repeat([]byte{'a'}, 1025)...)
	claimed = append(claimed, body[scopeAt+2:]...)
	if _, code := grant.Decode(claimed); code != grant.Malformed {
		t.Fatalf("an entry of 1025 bytes decodes as %q", code)
	}
	if _, code := grant.Decode(append(append([]byte{0x01}, body[1:]...), make([]byte, 65535)...)); code != grant.Malformed {
		t.Fatalf("a body over 65535 bytes decodes as %q", code)
	}
	if _, code := grant.Decode(append([]byte{0x02}, make([]byte, 70000)...)); code != grant.UnsupportedVersion {
		t.Fatalf("a body of another version is %q", code)
	}

	wide := base
	wide.Scope = entries(65, 1)
	if _, err := grant.Encode(wide); err == nil {
		t.Fatal("65 entries encoded")
	}
	wide.Scope = entries(1, 1025)
	if _, err := grant.Encode(wide); err == nil {
		t.Fatal("an entry of 1025 bytes encoded")
	}
	wide.Scope = entries(64, 1024)
	if _, err := grant.Encode(wide); err == nil {
		t.Fatal("64 entries of 1024 bytes fit no body")
	}
	wide.Scope = entries(63, 1024)
	body, err = grant.Encode(wide)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, code := grant.Decode(body); code != "" || len(decoded.Scope) != 63 {
		t.Fatalf("63 entries of 1024 bytes: %q, %d entries", code, len(decoded.Scope))
	}
	wide.Scope = nil
	wide.Domain = strings.Repeat("d", 256)
	if _, err := grant.Encode(wide); err == nil {
		t.Fatal("a domain of 256 bytes encoded")
	}
	wide.Domain = "example.test"
	wide.Validity = grant.Validity{Finite: false, ExpiresAt: 1}
	if _, err := grant.Encode(wide); err == nil {
		t.Fatal("an unbounded validity carrying an expiry encoded")
	}
}

// entries are n distinct entries of size bytes each, in ascending byte
// order: one printable byte tells them apart, and the rest is filler.
func entries(n, size int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%c", '0'+i)+strings.Repeat("x", size-1))
	}
	return out
}

// TestIssueDerivesTheParent holds what the table does not: the child's
// parent is the digest of the last envelope of parents, and a supplied
// parent that names another is ParentMismatch at the child's hop — as a
// root grant carrying any parent is at hop 0; a seed that is no key is no
// issuer; and a child in another trust domain is DomainMismatch at its
// hop.
func TestIssueDerivesTheParent(t *testing.T) {
	tb := loadTable(t)
	root := tb.root(t, jsonRoot{Key: "root", Domain: "example.test"})
	parents := tb.chain(t, []string{"root_alice"})
	child, _ := tb.grant(t, jsonGrant{Domain: "example.test", Subject: "bob", Scope: []string{"projects/7"}, Actions: []string{"read"}, Delegable: []string{"read"}, Depth: 2, Validity: []byte(`"unbounded"`)})
	other := grant.Digest(tb.envelope(t, "alice_bob"))
	child.Parent = &other
	if _, refusal := grant.Issue(root, parents, tb.seed(t, "alice"), child, false, grant.Time{}); refusal == nil || refusal.Code != grant.ParentMismatch || refusal.Hop != 1 {
		t.Fatalf("a foreign parent: %v", refusal)
	}
	expected := grant.Digest(parents[0])
	child.Parent = &expected
	env, refusal := grant.Issue(root, parents, tb.seed(t, "alice"), child, false, grant.Time{})
	if refusal != nil {
		t.Fatalf("the derived parent, supplied: %v", refusal)
	}
	child.Parent = nil
	again, _ := grant.Issue(root, parents, tb.seed(t, "alice"), child, false, grant.Time{})
	if !bytes.Equal(env, again) {
		t.Fatal("the supplied and the derived parent seal different bytes")
	}
	rootChild, _ := tb.grant(t, jsonGrant{Domain: "example.test", Subject: "alice", Validity: []byte(`"unbounded"`)})
	rootChild.Parent = &expected
	if _, refusal := grant.Issue(root, nil, tb.seed(t, "root"), rootChild, false, grant.Time{}); refusal == nil || refusal.Code != grant.ParentMismatch || refusal.Hop != 0 {
		t.Fatalf("a root grant carrying a parent: %v", refusal)
	}
	rootChild.Parent = nil
	if _, refusal := grant.Issue(root, nil, []byte("short"), rootChild, false, grant.Time{}); refusal == nil || refusal.Code != grant.RootMismatch {
		t.Fatalf("a seed that is no key, at the root: %v", refusal)
	}
	if _, refusal := grant.Issue(root, parents, nil, child, false, grant.Time{}); refusal == nil || refusal.Code != grant.IssuerMismatch || refusal.Hop != 1 {
		t.Fatalf("a seed that is no key, under a parent: %v", refusal)
	}
	child.Domain = "other.test"
	if _, refusal := grant.Issue(root, parents, tb.seed(t, "alice"), child, false, grant.Time{}); refusal == nil || refusal.Code != grant.DomainMismatch || refusal.Hop != 1 {
		t.Fatalf("a child in another trust domain: %v", refusal)
	}
}
