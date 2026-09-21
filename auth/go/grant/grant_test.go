package grant_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/auth/go/grant"
)

// table is conformance/tables/auth-grant.json as the packet describes it:
// keys derived from fixed seeds, named envelopes, and cases each naming a
// family, its inputs and its exact output.
type table struct {
	Domain    string            `json:"domain"`
	Keys      map[string]key    `json:"keys"`
	Envelopes map[string]string `json:"envelopes"`
	Cases     []json.RawMessage `json:"cases"`
}

type key struct {
	Seed   string `json:"seed"`
	Pubkey string `json:"pubkey"`
}

// jsonGrant is a grant as the table spells it: a subject as a key name or
// hex, a parent as an envelope name or a hex digest, a validity as
// "unbounded", {"expires_at": n} or — at issuance only — "inherit".
type jsonGrant struct {
	Domain    string          `json:"domain"`
	Subject   string          `json:"subject"`
	Parent    *string         `json:"parent"`
	Scope     []string        `json:"scope"`
	Actions   []string        `json:"actions"`
	Delegable []string        `json:"delegable"`
	Depth     uint8           `json:"depth"`
	Validity  json.RawMessage `json:"validity"`
}

type jsonRoot struct {
	Key    string `json:"key"`
	Domain string `json:"domain"`
}

type jsonVerified struct {
	Subject  string          `json:"subject"`
	Depth    uint8           `json:"depth"`
	Validity json.RawMessage `json:"validity"`
	Hops     int             `json:"hops"`
}

type jsonRefusal struct {
	Code string `json:"code"`
	Hop  int    `json:"hop"`
}

func loadTable(t *testing.T) table {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/Bitspark/nightseam\n") {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod of the root module above the test directory")
		}
		dir = parent
	}
	data, err := os.ReadFile(filepath.Join(dir, "conformance", "tables", "auth-grant.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tb table
	if err := json.Unmarshal(data, &tb); err != nil {
		t.Fatal(err)
	}
	return tb
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func (tb table) seed(t *testing.T, name string) []byte {
	t.Helper()
	k, ok := tb.Keys[name]
	if !ok {
		t.Fatalf("no key %q", name)
	}
	return unhex(t, k.Seed)
}

// pubkey resolves a key name, or 64 hex characters, to a public key.
func (tb table) pubkey(t *testing.T, s string) [32]byte {
	t.Helper()
	var out [32]byte
	if k, ok := tb.Keys[s]; ok {
		copy(out[:], unhex(t, k.Pubkey))
		return out
	}
	b := unhex(t, s)
	if len(b) != 32 {
		t.Fatalf("key %q is %d bytes", s, len(b))
	}
	copy(out[:], b)
	return out
}

func (tb table) envelope(t *testing.T, name string) []byte {
	t.Helper()
	e, ok := tb.Envelopes[name]
	if !ok {
		t.Fatalf("no envelope %q", name)
	}
	return unhex(t, e)
}

func (tb table) chain(t *testing.T, names []string) [][]byte {
	t.Helper()
	chain := make([][]byte, 0, len(names))
	for _, name := range names {
		chain = append(chain, tb.envelope(t, name))
	}
	return chain
}

// digest resolves an envelope name, or 64 hex characters, to a parent
// digest.
func (tb table) digest(t *testing.T, s string) *[32]byte {
	t.Helper()
	var out [32]byte
	if e, ok := tb.Envelopes[s]; ok {
		out = grant.Digest(unhex(t, e))
		return &out
	}
	b := unhex(t, s)
	if len(b) != 32 {
		t.Fatalf("digest %q is %d bytes", s, len(b))
	}
	copy(out[:], b)
	return &out
}

func (tb table) root(t *testing.T, r jsonRoot) grant.Root {
	t.Helper()
	return grant.Root{Key: tb.pubkey(t, r.Key), Domain: r.Domain}
}

// validity reads "unbounded", {"expires_at": n} or "inherit"; inherit is
// reported apart, since no Validity value carries it.
func validity(t *testing.T, raw json.RawMessage) (v grant.Validity, inherit bool) {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "unbounded":
			return grant.Validity{}, false
		case "inherit":
			return grant.Validity{}, true
		}
		t.Fatalf("validity %q", s)
	}
	var finite struct {
		ExpiresAt uint64 `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &finite); err != nil {
		t.Fatalf("validity %s: %v", raw, err)
	}
	return grant.Validity{Finite: true, ExpiresAt: finite.ExpiresAt}, false
}

func (tb table) grant(t *testing.T, jg jsonGrant) (g grant.Grant, inherit bool) {
	t.Helper()
	g = grant.Grant{
		Domain:    jg.Domain,
		Subject:   tb.pubkey(t, jg.Subject),
		Scope:     jg.Scope,
		Actions:   jg.Actions,
		Delegable: jg.Delegable,
		Depth:     jg.Depth,
	}
	if jg.Parent != nil {
		g.Parent = tb.digest(t, *jg.Parent)
	}
	g.Validity, inherit = validity(t, jg.Validity)
	return g, inherit
}

func now(raw json.RawMessage) grant.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return grant.Time{}
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err != nil {
		panic(err)
	}
	return grant.Time{Present: true, Now: n}
}

func describe(v grant.Validity) string {
	if !v.Finite {
		return "unbounded"
	}
	return fmt.Sprintf("expires_at %d", v.ExpiresAt)
}

func (tb table) expectVerified(t *testing.T, want jsonVerified, got grant.Verified, refusal *grant.Refusal) {
	t.Helper()
	if refusal != nil {
		t.Fatalf("refused %s at hop %d, want verified", refusal.Code, refusal.Hop)
	}
	wantValidity, _ := validity(t, want.Validity)
	if got.Subject != tb.pubkey(t, want.Subject) || got.Depth != want.Depth || got.Validity != wantValidity || got.Hops != want.Hops {
		t.Fatalf("verified subject %x depth %d %s hops %d, want subject %s depth %d %s hops %d",
			got.Subject, got.Depth, describe(got.Validity), got.Hops,
			want.Subject, want.Depth, describe(wantValidity), want.Hops)
	}
}

func expectRefusal(t *testing.T, want jsonRefusal, refusal *grant.Refusal) {
	t.Helper()
	if refusal == nil {
		t.Fatalf("verified, want refused %s at hop %d", want.Code, want.Hop)
	}
	if string(refusal.Code) != want.Code || refusal.Hop != want.Hop {
		t.Fatalf("refused %s at hop %d, want %s at hop %d", refusal.Code, refusal.Hop, want.Code, want.Hop)
	}
}

func expectGrant(t *testing.T, want, got grant.Grant) {
	t.Helper()
	wantParent, gotParent := "none", "none"
	if want.Parent != nil {
		wantParent = hex.EncodeToString(want.Parent[:])
	}
	if got.Parent != nil {
		gotParent = hex.EncodeToString(got.Parent[:])
	}
	if got.Domain != want.Domain || got.Subject != want.Subject || gotParent != wantParent ||
		!sameList(got.Scope, want.Scope) || !sameList(got.Actions, want.Actions) || !sameList(got.Delegable, want.Delegable) ||
		got.Depth != want.Depth || got.Validity != want.Validity {
		t.Fatalf("decoded %+v (parent %s), want %+v (parent %s)", got, gotParent, want, wantParent)
	}
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTable reproduces every case of auth-grant.json: the count of cases
// run is the count of cases in the file, and every family's output is
// held byte for byte, code for code.
func TestTable(t *testing.T) {
	tb := loadTable(t)
	if tb.Domain != grant.Domain {
		t.Fatalf("table domain %q, package domain %q", tb.Domain, grant.Domain)
	}
	const want = 72
	if len(tb.Cases) != want {
		t.Fatalf("%d cases in the table, the packet states %d", len(tb.Cases), want)
	}
	ran := 0
	for _, raw := range tb.Cases {
		var head struct {
			Family string `json:"family"`
			ID     string `json:"id"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatal(err)
		}
		ok := t.Run(head.ID, func(t *testing.T) {
			switch head.Family {
			case "grant_encode":
				tb.runEncode(t, raw)
			case "grant_decode":
				tb.runDecode(t, raw)
			case "grant_seal":
				tb.runSeal(t, raw)
			case "grant_open":
				tb.runOpen(t, raw)
			case "grant_verify":
				tb.runVerify(t, raw)
			case "grant_issue":
				tb.runIssue(t, raw)
			default:
				t.Fatalf("unknown family %q", head.Family)
			}
		})
		if ok {
			ran++
		}
	}
	if ran != want {
		t.Fatalf("%d of %d cases reproduced", ran, want)
	}
}

func (tb table) runEncode(t *testing.T, raw json.RawMessage) {
	var c struct {
		Grant   jsonGrant `json:"grant"`
		Body    string    `json:"body"`
		Refused bool      `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	g, inherit := tb.grant(t, c.Grant)
	if inherit {
		// No Validity value spells inherit: it is an issuance input, and the
		// encoder has no input for it — which is what the case records.
		if !c.Refused {
			t.Fatal("inherit offered to Encode is not recorded as refused")
		}
		return
	}
	body, err := grant.Encode(g)
	if c.Refused {
		if err == nil {
			t.Fatalf("encoded %x, want refused", body)
		}
		return
	}
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !bytes.Equal(body, unhex(t, c.Body)) {
		t.Fatalf("body %x, want %s", body, c.Body)
	}
	decoded, code := grant.Decode(body)
	if code != "" {
		t.Fatalf("the canonical body decodes as %s", code)
	}
	expectGrant(t, g, decoded)
}

func (tb table) runDecode(t *testing.T, raw json.RawMessage) {
	var c struct {
		Body    string    `json:"body"`
		Grant   jsonGrant `json:"grant"`
		Refused string    `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got, code := grant.Decode(unhex(t, c.Body))
	if c.Refused != "" {
		if string(code) != c.Refused {
			t.Fatalf("code %q, want %q", code, c.Refused)
		}
		return
	}
	if code != "" {
		t.Fatalf("refused %s", code)
	}
	want, _ := tb.grant(t, c.Grant)
	expectGrant(t, want, got)
}

func (tb table) runSeal(t *testing.T, raw json.RawMessage) {
	var c struct {
		Issuer   string    `json:"issuer"`
		Grant    jsonGrant `json:"grant"`
		Envelope string    `json:"envelope"`
		Digest   string    `json:"digest"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	g, _ := tb.grant(t, c.Grant)
	env, err := grant.Seal(tb.seed(t, c.Issuer), g)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(env, unhex(t, c.Envelope)) {
		t.Fatalf("envelope %x, want %s", env, c.Envelope)
	}
	if digest := grant.Digest(env); hex.EncodeToString(digest[:]) != c.Digest {
		t.Fatalf("digest %x, want %s", digest, c.Digest)
	}
}

func (tb table) runOpen(t *testing.T, raw json.RawMessage) {
	var c struct {
		Envelope string    `json:"envelope"`
		Issuer   string    `json:"issuer"`
		Grant    jsonGrant `json:"grant"`
		Refused  string    `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	issuer, got, code := grant.Open(unhex(t, c.Envelope))
	if c.Refused != "" {
		if string(code) != c.Refused {
			t.Fatalf("code %q, want %q", code, c.Refused)
		}
		if issuer != [32]byte{} {
			t.Fatalf("a refused envelope names issuer %x", issuer)
		}
		return
	}
	if code != "" {
		t.Fatalf("refused %s", code)
	}
	if issuer != tb.pubkey(t, c.Issuer) {
		t.Fatalf("issuer %x, want %s", issuer, c.Issuer)
	}
	want, _ := tb.grant(t, c.Grant)
	expectGrant(t, want, got)
}

func (tb table) runVerify(t *testing.T, raw json.RawMessage) {
	var c struct {
		Root     jsonRoot        `json:"root"`
		Chain    []string        `json:"chain"`
		Request  grant.Request   `json:"request"`
		Now      json.RawMessage `json:"now"`
		Verified *jsonVerified   `json:"verified"`
		Refused  *jsonRefusal    `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	root := tb.root(t, c.Root)
	chain := tb.chain(t, c.Chain)
	got, refusal := grant.Verify(root, chain, c.Request, now(c.Now))
	if c.Refused != nil {
		expectRefusal(t, *c.Refused, refusal)
		// Every refusal of steps 1, 2 and 4 holds Inspect as it holds Verify;
		// only coverage is Verify's alone.
		_, inspected := grant.Inspect(root, chain, now(c.Now))
		if c.Refused.Code == "not_covered" || (c.Refused.Code == "domain_mismatch" && c.Request.Domain != root.Domain) {
			if inspected != nil && (inspected.Code != refusal.Code || inspected.Hop != refusal.Hop) {
				t.Fatalf("Inspect refused %s at hop %d for a chain that only fails coverage", inspected.Code, inspected.Hop)
			}
		} else if inspected == nil || inspected.Code != refusal.Code || inspected.Hop != refusal.Hop {
			t.Fatalf("Inspect answers %v, Verify refused %s at hop %d", inspected, refusal.Code, refusal.Hop)
		}
		return
	}
	tb.expectVerified(t, *c.Verified, got, refusal)
	inspected, refusal := grant.Inspect(root, chain, now(c.Now))
	if refusal != nil || inspected != got {
		t.Fatalf("Inspect answers %+v / %v, Verify %+v", inspected, refusal, got)
	}
}

func (tb table) runIssue(t *testing.T, raw json.RawMessage) {
	var c struct {
		Root     jsonRoot        `json:"root"`
		Parents  []string        `json:"parents"`
		Issuer   string          `json:"issuer"`
		Child    jsonGrant       `json:"child"`
		Now      json.RawMessage `json:"now"`
		Envelope string          `json:"envelope"`
		SameAs   string          `json:"same_as"`
		Refused  *jsonRefusal    `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	root := tb.root(t, c.Root)
	child, inherit := tb.grant(t, c.Child)
	got, refusal := grant.Issue(root, tb.chain(t, c.Parents), tb.seed(t, c.Issuer), child, inherit, now(c.Now))
	if c.Refused != nil {
		expectRefusal(t, *c.Refused, refusal)
		return
	}
	if refusal != nil {
		t.Fatalf("refused %s at hop %d", refusal.Code, refusal.Hop)
	}
	if !bytes.Equal(got, unhex(t, c.Envelope)) {
		t.Fatalf("envelope %x, want %s", got, c.Envelope)
	}
	if c.SameAs != "" && !bytes.Equal(got, tb.envelope(t, c.SameAs)) {
		t.Fatalf("envelope differs from %s", c.SameAs)
	}
	// What was issued verifies under the chain it was issued under, and a
	// second issuance seals the same bytes.
	chain := append(tb.chain(t, c.Parents), got)
	if _, refusal := grant.Inspect(root, chain, now(c.Now)); refusal != nil {
		t.Fatalf("the issued chain is refused %s at hop %d", refusal.Code, refusal.Hop)
	}
	again, refusal := grant.Issue(root, tb.chain(t, c.Parents), tb.seed(t, c.Issuer), child, inherit, now(c.Now))
	if refusal != nil || !bytes.Equal(again, got) {
		t.Fatal("a retry sealed other bytes")
	}
}
