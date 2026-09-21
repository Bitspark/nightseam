package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// TestNoClockNoNetwork holds the packets' promise that nothing here does
// I/O, reads a clock or touches the network: no package of this module
// imports time, net, os, the process or the entropy of the platform. Every
// nonce, every id and every decision time is an argument.
func TestNoClockNoNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "list", "-json", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"time", "net", "os", "io", "syscall", "crypto/rand", "math/rand", "math/rand/v2", "os/exec", "os/signal", "io/fs", "context"}
	decoder := json.NewDecoder(bytes.NewReader(out))
	packages := 0
	for {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		packages++
		for _, imported := range p.Imports {
			for _, f := range forbidden {
				if imported == f || strings.HasPrefix(imported, f+"/") {
					t.Errorf("%s imports %s; the profile reads its arguments and answers", p.ImportPath, imported)
				}
			}
		}
	}
	if packages < 2 {
		t.Fatalf("listed %d packages; auth and grant are two", packages)
	}
}

// TestTerms holds the terms grammar beyond the table: a round trip, the
// depth rendered when asked or not zero, and every refusal at parse.
func TestTerms(t *testing.T) {
	terms, err := auth.ParseTerms([]string{"action:read", "action:write", "delegable:read", "scope:projects/7", "scope:teams", "depth:2"})
	if err != nil {
		t.Fatal(err)
	}
	want := auth.Terms{Actions: []string{"read", "write"}, Delegable: []string{"read"}, Scope: []string{"projects/7", "teams"}, Depth: 2}
	if !sameStrings(terms.Actions, want.Actions) || !sameStrings(terms.Delegable, want.Delegable) || !sameStrings(terms.Scope, want.Scope) || terms.Depth != 2 {
		t.Fatalf("parsed %+v", terms)
	}
	if rendered := auth.RenderTerms(terms, false); !sameStrings(rendered, []string{"action:read", "action:write", "delegable:read", "scope:projects/7", "scope:teams", "depth:2"}) {
		t.Fatalf("rendered %v", rendered)
	}
	zero, err := auth.ParseTerms([]string{"action:read"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered := auth.RenderTerms(zero, false); !sameStrings(rendered, []string{"action:read"}) {
		t.Fatalf("rendered %v without depth", rendered)
	}
	if rendered := auth.RenderTerms(zero, true); !sameStrings(rendered, []string{"action:read", "depth:0"}) {
		t.Fatalf("rendered %v with depth", rendered)
	}
	if empty, err := auth.ParseTerms(nil); err != nil || empty.Depth != 0 || len(auth.RenderTerms(empty, false)) != 0 {
		t.Fatalf("empty terms: %+v, %v", empty, err)
	}
	for name, entries := range map[string][]string{
		"scope before action":       {"scope:projects/7", "action:read"},
		"depth before scope":        {"depth:0", "scope:projects/7"},
		"actions out of order":      {"action:write", "action:read"},
		"a duplicate action":        {"action:read", "action:read"},
		"delegable outside action":  {"action:read", "delegable:write"},
		"depth twice":               {"action:read", "depth:0", "depth:1"},
		"depth above the bound":     {"action:read", "depth:16"},
		"depth with a leading zero": {"action:read", "depth:01"},
		"depth that is no number":   {"action:read", "depth:x"},
		"an unknown tag":            {"actions:read"},
		"no tag":                    {"read"},
		"an empty entry":            {"action:"},
		"a control character":       {"action:re\tad"},
		"an entry over the bound":   {"action:" + strings.Repeat("a", 1025)},
	} {
		if terms, err := auth.ParseTerms(entries); err == nil {
			t.Errorf("%s parsed as %+v", name, terms)
		}
	}
}

// TestConnectionEdges holds the exchange where the table does not: a proof
// of the wrong size is Malformed before the exchange is consulted and
// consumes no challenge; a second
// challenge replaces the first; an audience outside the grammar makes no
// connection; the context keeps its own copy of the chain.
func TestConnectionEdges(t *testing.T) {
	var tb table
	loadJSON(t, "auth-boot.json", &tb)
	root := tb.root(t)
	audience := "https://api.example.test/nightseam"
	if _, err := auth.NewConnection("https://API.example.test/nightseam"); err == nil {
		t.Fatal("an audience outside the grammar made a connection")
	}
	conn, err := auth.NewConnection(audience)
	if err != nil {
		t.Fatal(err)
	}
	nonce := unhex(t, "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1")
	if _, refusal := conn.Prove(root, tb.pubkey(t, "bob"), make([]byte, 63), nil, grant.Time{}); refusal == nil || refusal.Code != auth.Malformed {
		t.Fatalf("a short proof before a challenge: %s", describeRefusal(refusal))
	}
	if _, refusal := conn.Prove(root, tb.pubkey(t, "bob"), make([]byte, 64), nil, grant.Time{}); refusal == nil || refusal.Code != auth.NoChallenge {
		t.Fatalf("a proof before a challenge: %s", describeRefusal(refusal))
	}
	if _, refusal := conn.Challenge(bytes.Repeat([]byte{0xa2}, 32)); refusal != nil {
		t.Fatal(describeRefusal(refusal))
	}
	if _, refusal := conn.Challenge(nonce); refusal != nil {
		t.Fatal(describeRefusal(refusal))
	}
	if _, refusal := conn.Prove(root, tb.pubkey(t, "bob"), make([]byte, 63), nil, grant.Time{}); refusal == nil || refusal.Code != auth.Malformed {
		t.Fatalf("a short proof: %s", describeRefusal(refusal))
	}
	proof, err := auth.Prove(tb.seed(t, "bob"), audience, nonce)
	if err != nil {
		t.Fatal(err)
	}
	chain := tb.chain(t, []string{"root_alice", "alice_bob"})
	ctx, refusal := conn.Prove(root, tb.pubkey(t, "bob"), proof, chain, grant.Time{Present: true, Now: 1799990000})
	if refusal != nil {
		t.Fatalf("the challenge the second nonce replaced the first with: %s", describeRefusal(refusal))
	}
	chain[0][0] ^= 0xff
	if bytes.Equal(ctx.Chain[0], chain[0]) {
		t.Fatal("the context shares the caller's envelope bytes")
	}
	if _, refusal := auth.Call(root, ctx, grant.Request{Domain: "example.test", Action: "read", Scope: "projects/7"}, grant.Time{Present: true, Now: 1799990000}); refusal != nil {
		t.Fatalf("the kept chain: %s", describeRefusal(refusal))
	}
	pipe, err := auth.NewConnection("")
	if err != nil {
		t.Fatal(err)
	}
	if _, refusal := pipe.Prove(root, tb.pubkey(t, "bob"), proof, chain, grant.Time{}); refusal == nil || refusal.Code != auth.Unsupported {
		t.Fatalf("a prove on a pipe: %s", describeRefusal(refusal))
	}
	if pipe.Context() != nil {
		t.Fatal("a pipe has a context")
	}
}

// TestPublic holds the wire form of a refusal: the code, a sentence that
// names nothing of any grant, and the grant code and hop as data where the
// refusal carries them.
func TestPublic(t *testing.T) {
	public := (&auth.Refusal{Code: auth.Denied, Grant: &grant.Refusal{Code: grant.Expired, Hop: 1}}).Public()
	if public.Code != "auth.denied" || public.Message == "" || string(public.Data) != `{"code":"expired","hop":1}` {
		t.Fatalf("%+v", public)
	}
	public = (&auth.Refusal{Code: auth.Unauthenticated}).Public()
	if public.Code != "auth.unauthenticated" || public.Message == "" || public.Data != nil {
		t.Fatalf("%+v", public)
	}
	public = (&auth.Refusal{Code: auth.Code(auth.OwnerPrefix + "archived")}).Public()
	if public.Code != "owner:archived" || public.Message == "" {
		t.Fatalf("%+v", public)
	}
}

// countingStore wraps a store and fails one Put, the way a replica that
// lost the compare-and-set sees it.
type countingStore struct {
	*memoryStore
	failNext bool
}

func (s *countingStore) Put(r auth.Record, expectVersion int) bool {
	ok := s.memoryStore.Put(r, expectVersion)
	if s.failNext {
		// The other replica made this very transition first: the store
		// holds it, and this replica's compare-and-set loses.
		s.failNext = false
		return false
	}
	return ok
}

// TestServiceEdges holds the bootstrap where the table does not: the
// parameters Begin refuses, the bound on live records, a lost
// compare-and-set at collect answering the same bytes, and a gone record
// whose id may be begun again.
func TestServiceEdges(t *testing.T) {
	var tb table
	loadJSON(t, "auth-boot.json", &tb)
	audience := "https://api.example.test/nightseam"
	store := newMemoryStore()
	service := &auth.Service{Root: tb.root(t), Audience: audience, Store: store}
	id := unhex(t, "11111111111111111111111111111111")
	nonce := unhex(t, "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1")
	browser := tb.pubkeyBytes(t, "browser")
	scope := []string{"action:read", "scope:projects/7", "depth:0"}
	for name, refused := range map[string]func() auth.LoginCode{
		"a short id":      func() auth.LoginCode { _, c := service.Begin(0, id[:15], nonce, browser, scope, 3600); return c },
		"a short nonce":   func() auth.LoginCode { _, c := service.Begin(0, id, nonce[:31], browser, scope, 3600); return c },
		"a short browser": func() auth.LoginCode { _, c := service.Begin(0, id, nonce, browser[:31], scope, 3600); return c },
		"a long validity": func() auth.LoginCode { _, c := service.Begin(0, id, nonce, browser, scope, 2592001); return c },
		"terms out of order": func() auth.LoginCode {
			_, c := service.Begin(0, id, nonce, browser, []string{"depth:0", "action:read"}, 3600)
			return c
		},
	} {
		if code := refused(); code != auth.InvalidRequest {
			t.Errorf("%s: %q", name, code)
		}
	}
	if store.Count() != 0 {
		t.Fatal("a refused begin stored a record")
	}
	if _, code := service.Begin(1000, id, nonce, browser, scope, 3600); code != "" {
		t.Fatal(code)
	}
	if _, code := service.Begin(1300, id, nonce, browser, scope, 3600); code != "" {
		t.Fatalf("a gone record's id begun again: %q", code)
	}
	if record, ok := store.Get(id); !ok || record.Version != 2 || record.ExpiresAt != 1600 {
		t.Fatalf("the record begun again: %+v", record)
	}
	for i := 1; i < 4096; i++ {
		other := append([]byte(nil), id...)
		other[0], other[1] = byte(i>>8), byte(i)
		if _, code := service.Begin(1300, other, nonce, browser, scope, 3600); code != "" {
			t.Fatalf("record %d: %q", i, code)
		}
	}
	if _, code := service.Begin(1300, bytes.Repeat([]byte{0x22}, 16), nonce, browser, scope, 3600); code != auth.SlowDown {
		t.Fatalf("the 4097th live record: %q", code)
	}

	// Two collectors: the one that loses the compare-and-set answers what
	// the other stored.
	racing := &countingStore{memoryStore: newMemoryStore()}
	service = &auth.Service{Root: tb.root(t), Audience: audience, Store: racing}
	if _, code := service.Begin(1799990000, id, nonce, browser, scope, 3600); code != "" {
		t.Fatal(code)
	}
	proof := unhex(t, "455f2f9982879ae995dfa7fcd86fc8a37f03003b07e4e96cef23097c185d0197140e446f135b24f267c47f997216027525e4168aa506ecdc611d8e6ea497dd0f")
	if code, refusal := service.Answer(1799990002, id, tb.pubkey(t, "alice"), proof, tb.chain(t, []string{"root_alice", "alice_browser"}), 1); code != "" {
		t.Fatalf("%q %v", code, refusal)
	}
	collect := unhex(t, "5c7088293a14d57cb6f84d5a205dab5c75cd1e88b34ab4796357ba744ca8989e51b27c636ed2a4e4b8994a5dcb2d78031a765e7e4844fc1c7a260afa6db71b0c")
	racing.failNext = true
	answer, code := service.Collect(1799990003, id, collect)
	if code != "" || answer.Principal != tb.pubkey(t, "alice") || !bytes.Equal(answer.Possession, proof) {
		t.Fatalf("the losing collector: %q %+v", code, answer)
	}
	if record, _ := racing.Get(id); record.State != auth.Collected || record.Version != 3 {
		t.Fatalf("after the race: %+v", record)
	}
	again, code := service.Collect(1799990008, id, collect)
	if code != "" || !bytes.Equal(again.Possession, answer.Possession) || !sameChains(again.Authority, answer.Authority) {
		t.Fatalf("the recovery: %q", code)
	}
}

// TestExposureEdges holds the exposure where the table does not: an export
// of a method or of a scope outside the grant's rules is an error, a
// re-export under another scope is an error and under the same one is
// not, an emission of a member that is not an event reaches nobody, and
// Effect on no decision refuses.
func TestExposureEdges(t *testing.T) {
	var tb exposureTable
	loadJSON(t, "auth-exposure.json", &tb)
	binding, refused := tb.bind(t, "P")
	if refused != nil {
		t.Fatal(refused)
	}
	if err := binding.Export("x", "method:start", "projects/7"); err == nil {
		t.Error("a method exported")
	}
	if err := binding.Export("x", "callable:JobStatus", "projects/\x007"); err == nil {
		t.Error("a scope with a control character exported")
	}
	if err := binding.Export("", "callable:JobStatus", "projects/7"); err == nil {
		t.Error("an empty reference exported")
	}
	if err := binding.Export("job7.status", "callable:JobStatus", "projects/7"); err != nil {
		t.Fatal(err)
	}
	if err := binding.Export("job7.status", "callable:JobStatus", "projects/7"); err != nil {
		t.Errorf("the same export again: %v", err)
	}
	if err := binding.Export("job7.status", "callable:JobStatus", "projects/8"); err == nil {
		t.Error("a re-export under another scope")
	}
	root := tb.root(t)
	at := grant.Time{Present: true, Now: 1799990000}
	deliveries := binding.Emit(root, "method:start", map[string]any{"projectId": "7"}, []auth.Recipient{{Name: "bob", Ctx: tb.context(t, "bob")}}, at)
	if len(deliveries) != 1 || deliveries[0].Refused == nil || deliveries[0].Refused.Code != auth.UnknownMember {
		t.Fatalf("a method emitted: %+v", deliveries)
	}
	if refusal := binding.Effect(root, nil, tb.context(t, "bob"), at, nil); refusal == nil || refusal.Code != auth.UnknownMember {
		t.Fatalf("an effect of no decision: %s", describeRefusal(refusal))
	}
	decision, refusal := binding.Decide(root, "method:start", map[string]any{"document": "d", "projectId": "7"}, "", tb.context(t, "bob"), at)
	if refusal != nil {
		t.Fatal(describeRefusal(refusal))
	}
	if refusal := binding.Effect(root, decision, tb.context(t, "carol"), at, nil); refusal == nil || refusal.Code != auth.SubjectMismatch {
		t.Fatalf("an effect under another connection: %s", describeRefusal(refusal))
	}
	if refusal := binding.Effect(root, decision, nil, at, nil); refusal == nil || refusal.Code != auth.Unauthenticated {
		t.Fatalf("an effect with no context: %s", describeRefusal(refusal))
	}
	if scope, err := auth.Render("projects	/{projectId}", map[string]any{"projectId": "7"}, ""); err == nil {
		t.Fatalf("a template with a control character rendered %q", scope)
	}
	routes := binding.Routes()
	routes[0] = "changed"
	if binding.Routes()[0] == "changed" {
		t.Fatal("Routes hands out its own slice")
	}
	if _, refused := auth.Bind(auth.Surface{Family: "f", Digest: "d", Members: []auth.Member{{Key: "method:a"}, {Key: "method:a"}}}, auth.Policy{Family: "f", Digest: "d", Treatments: map[string]auth.Treatment{"method:a": {Kind: auth.KindPublic}}}); refused == nil || refused.Code != auth.ContractMismatch {
		t.Fatalf("a surface declaring a member twice: %v", refused)
	}
	if _, refused := auth.Bind(auth.Surface{Family: "f", Digest: "d", Members: []auth.Member{{Key: "method:a"}}}, auth.Policy{Family: "f", Digest: "d", Treatments: map[string]auth.Treatment{"method:a": {Kind: "open"}}}); refused == nil || refused.Code != auth.TemplateInvalid {
		t.Fatalf("an unknown kind: %v", refused)
	}
	if _, refused := auth.Bind(auth.Surface{Family: "f", Digest: "d", Members: []auth.Member{{Key: "method:a"}}}, auth.Policy{Family: "f", Digest: "d", Treatments: map[string]auth.Treatment{"method:a": {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{"}}}); refused == nil || refused.Code != auth.TemplateInvalid {
		t.Fatalf("an unclosed hole: %v", refused)
	}
	if _, refused := auth.Bind(auth.Surface{Family: "f", Digest: "d", Members: []auth.Member{{Key: "method:a"}}}, auth.Policy{Family: "f", Digest: "d", Treatments: map[string]auth.Treatment{"method:a": {Kind: auth.KindGuarded, Action: "", Scope: "projects"}}}); refused == nil || refused.Code != auth.TemplateInvalid {
		t.Fatalf("a guarded treatment with no action: %v", refused)
	}
}
