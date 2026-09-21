package over_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
	"github.com/Bitspark/nightseam/auth/go/over"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The profile over a peer, on a pipe of two peers with the keys and
// envelopes the tables share: the exchange establishes what auth-boot.json
// says it establishes, a handler decides on the connection's context, a
// context by construction is trust, and an exported reference and an
// emission are decided under the invoking and receiving connections.

type overTable struct {
	Root      struct{ Key, Domain string }
	Keys      map[string]struct{ Seed, Pubkey string }
	Envelopes map[string]string
}

func loadOverTable(t *testing.T) overTable {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "conformance", "tables", "auth-exposure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table overTable
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	return table
}

func (o overTable) key(name string) [32]byte {
	var key [32]byte
	copy(key[:], mustHex(o.Keys[name].Pubkey))
	return key
}

func (o overTable) seed(name string) []byte { return mustHex(o.Keys[name].Seed) }

func (o overTable) envelope(name string) []byte { return mustHex(o.Envelopes[name]) }

func mustHex(text string) []byte {
	b, err := hex.DecodeString(text)
	if err != nil {
		panic(err)
	}
	return b
}

const overDeclaration = `{"definitions":{
 "worker":{"kind":"family","server":{"ref":"worker/$server"},"client":{"ref":"worker/$client"},"types":{"Status":{"ref":"worker/Status"}}},
 "worker/$server":{"kind":"side","methods":{"list":{"request":{"ref":"worker/ListRequest"}}},"events":{"progress":{"ref":"worker/Progress"}}},
 "worker/$client":{"kind":"side","methods":{},"events":{}},
 "worker/ListRequest":{"kind":"record","fields":[{"name":"projectId"}]},
 "worker/Progress":{"kind":"record","fields":[{"name":"projectId"},{"name":"percent"}]},
 "worker/Status":{"kind":"callable","request":{"primitive":"string"}}}}`

func overPolicy() auth.Policy {
	return auth.Policy{Family: "worker", Digest: "d1", Treatments: map[string]auth.Treatment{
		"method:list":     {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{projectId}"},
		"event:progress":  {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{projectId}"},
		"callable:Status": {Kind: auth.KindGuarded, Action: "read", Scope: "{export}"},
	}}
}

type overPair struct {
	client, server *runtime.Peer
	layer          *over.Layer
}

// pair builds a client and a server over a pipe; the server's context is
// prepared for the profile, or trusted, as the test says.
func pair(t *testing.T, serverCtx context.Context, options over.Options, prepare func(*runtime.Peer, *over.Layer) error) overPair {
	t.Helper()
	a, b := duplex.Pipe(1 << 20)
	var p overPair
	server, err := runtime.NewPeer(serverCtx, b, runtime.ServerRole, runtime.Options{Prepare: func(peer *runtime.Peer) error {
		layer, err := over.Over(peer, options)
		if err != nil {
			return err
		}
		p.layer = layer
		if prepare != nil {
			return prepare(peer, layer)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := runtime.NewPeer(context.Background(), a, runtime.ClientRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	p.client, p.server = client, server
	return p
}

func codeOf(err error) string {
	var public *runtime.PublicError
	if errors.As(err, &public) {
		return public.Code
	}
	if err == nil {
		return ""
	}
	return "error:" + err.Error()
}

func TestSurfaceOfReadsTheDeclaration(t *testing.T) {
	surface, err := over.SurfaceOf("worker", "server", overDeclaration, "d1")
	if err != nil {
		t.Fatal(err)
	}
	want := auth.Surface{Family: "worker", Digest: "d1", Members: []auth.Member{
		{Key: "callable:Status"}, {Key: "event:progress", Fields: []string{"percent", "projectId"}}, {Key: "method:list", Fields: []string{"projectId"}},
	}}
	if got, _ := json.Marshal(surface); string(got) != string(mustJSON(want)) {
		t.Fatalf("surface %s, want %s", got, mustJSON(want))
	}
	if _, err := over.SurfaceOf("nobody", "server", overDeclaration, "d1"); err == nil {
		t.Fatal("a family the declaration lacks must be refused")
	}
	if _, err := over.SurfaceOf("worker", "sideways", overDeclaration, "d1"); err == nil {
		t.Fatal("a side is server or client")
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestTheExchangeEstablishesTheContextAndAHandlerDecidesOnIt(t *testing.T) {
	table := loadOverTable(t)
	root := grant.Root{Key: table.key("root"), Domain: table.Root.Domain}
	const audience = "https://api.example.test/nightseam"
	clock := uint64(1799990000)
	now := func() grant.Time { return grant.Time{Present: true, Now: clock} }
	nonce := func() [32]byte {
		var n [32]byte
		copy(n[:], mustHex("a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"))
		return n
	}
	surface, _ := over.SurfaceOf("worker", "server", overDeclaration, "d1")
	binding, refused := auth.Bind(surface, overPolicy())
	if refused != nil {
		t.Fatal(refused)
	}
	guard, err := over.NewGuard(binding, root, now)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	p := pair(t, over.Prepare(context.Background()), over.Options{Root: root, Audience: audience, Now: now, Nonce: nonce}, func(peer *runtime.Peer, _ *over.Layer) error {
		return peer.Handle("list", func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			var params struct {
				ProjectId string `json:"projectId"`
			}
			_ = json.Unmarshal(raw, &params)
			decision, err := guard.Decide(ctx, "method:list", over.Payload(params))
			if err != nil {
				return nil, err
			}
			ran++
			if err := guard.Effect(ctx, decision, nil); err != nil {
				return nil, err
			}
			return map[string]any{"count": 1, "subject": hex.EncodeToString(decision.Subject[:])}, nil
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if p.layer.Context() != nil {
		t.Fatal("a context before the exchange")
	}
	var out map[string]any
	// Before the exchange: refused, and no body ran.
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "7"}, &out); codeOf(err) != string(auth.Unauthenticated) || ran != 0 {
		t.Fatalf("before the exchange: %v, ran %d", err, ran)
	}
	// The exchange, as AUTH-BOOT-016.
	var challenged struct{ Nonce string }
	if err := p.client.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); err != nil || challenged.Nonce != "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1" {
		t.Fatalf("challenge: %q %v", challenged.Nonce, err)
	}
	proof, err := auth.Prove(table.seed("bob"), audience, mustHex(challenged.Nonce))
	if err != nil {
		t.Fatal(err)
	}
	bob := table.key("bob")
	var proved map[string]any
	prove := map[string]any{"subject": "ed25519:" + hex.EncodeToString(bob[:]), "possession": hex.EncodeToString(proof), "chain": []string{table.Envelopes["root_alice"], table.Envelopes["alice_bob"]}}
	if err := p.client.Call(ctx, over.ProveMethod, prove, &proved); err != nil || proved["expires_at"] != float64(1800000000) {
		t.Fatalf("prove: %v %v", proved, err)
	}
	if got := p.layer.Context(); got == nil || got.Subject != bob {
		t.Fatalf("context after the exchange: %+v", got)
	}
	// A context is immutable.
	if err := p.client.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); codeOf(err) != string(auth.Established) {
		t.Fatalf("second challenge: %v", err)
	}
	if err := p.client.Call(ctx, over.ProveMethod, prove, &proved); codeOf(err) != string(auth.Established) {
		t.Fatalf("second prove: %v", err)
	}
	// Decided on the connection's context.
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "7"}, &out); err != nil || out["subject"] != hex.EncodeToString(bob[:]) || ran != 1 {
		t.Fatalf("own project: %v %v", out, err)
	}
	err = p.client.Call(ctx, "list", map[string]string{"projectId": "8"}, &out)
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != string(auth.Denied) || string(public.Data) != `{"code":"not_covered","hop":1}` {
		t.Fatalf("sibling: %v %s", err, public.Data)
	}
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "7/ledger"}, &out); codeOf(err) != string(auth.SelectorInvalid) || ran != 1 {
		t.Fatalf("selector: %v", err)
	}
	// Expiry at use, with the connection open; then the clock returns.
	clock = 1800000000
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "7"}, &out); codeOf(err) != string(auth.Denied) {
		t.Fatalf("expired at use: %v", err)
	}
	clock = 1799990000
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "7"}, &out); err != nil {
		t.Fatalf("after the clock returns: %v", err)
	}
	// A malformed prove is refused as such; a failed prove consumes the challenge.
	fresh := pair(t, over.Prepare(context.Background()), over.Options{Root: root, Audience: audience, Now: now, Nonce: nonce}, nil)
	if err := fresh.client.Call(ctx, over.ProveMethod, map[string]any{"subject": "not a key"}, &proved); codeOf(err) != string(auth.Malformed) {
		t.Fatalf("malformed prove: %v", err)
	}
	if err := fresh.client.Call(ctx, over.ProveMethod, prove, &proved); codeOf(err) != string(auth.NoChallenge) {
		t.Fatalf("prove before a challenge: %v", err)
	}
	if err := fresh.client.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); err != nil {
		t.Fatal(err)
	}
	wrong, _ := auth.Prove(table.seed("mallory"), audience, mustHex(challenged.Nonce))
	forged := map[string]any{"subject": prove["subject"], "possession": hex.EncodeToString(wrong), "chain": prove["chain"]}
	if err := fresh.client.Call(ctx, over.ProveMethod, forged, &proved); codeOf(err) != string(auth.PossessionInvalid) {
		t.Fatalf("another key's proof: %v", err)
	}
	if err := fresh.client.Call(ctx, over.ProveMethod, prove, &proved); codeOf(err) != string(auth.NoChallenge) {
		t.Fatalf("one attempt per challenge: %v", err)
	}
	// No audience: the exchange is refused.
	piped := pair(t, over.Prepare(context.Background()), over.Options{Root: root, Now: now, Nonce: nonce}, nil)
	if err := piped.client.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); codeOf(err) != string(auth.Unsupported) {
		t.Fatalf("a pipe: %v", err)
	}
	// A peer whose context was not prepared cannot carry the profile.
	a, b := duplex.Pipe(1 << 10)
	unprepared, err := runtime.NewPeer(context.Background(), b, runtime.ServerRole, runtime.Options{Prepare: func(peer *runtime.Peer) error {
		_, err := over.Over(peer, over.Options{Root: root, Now: now, Nonce: nonce})
		return err
	}})
	if err == nil {
		unprepared.Close()
		t.Fatal("an unprepared peer took the profile")
	}
	_ = a.Close(context.Background(), duplex.CodeNormal, "")
}

func TestAContextByConstructionIsTrust(t *testing.T) {
	table := loadOverTable(t)
	root := grant.Root{Key: table.key("root"), Domain: table.Root.Domain}
	now := func() grant.Time { return grant.Time{Present: true, Now: 1799990000} }
	nonce := func() [32]byte { return [32]byte{1} }
	surface, _ := over.SurfaceOf("worker", "server", overDeclaration, "d1")
	binding, _ := auth.Bind(surface, overPolicy())
	guard, _ := over.NewGuard(binding, root, now)
	trusted := &auth.Context{Subject: table.key("alice"), Chain: [][]byte{table.envelope("root_alice")}}
	p := pair(t, over.Trust(context.Background(), trusted), over.Options{Root: root, Now: now, Nonce: nonce}, func(peer *runtime.Peer, _ *over.Layer) error {
		return peer.Handle("list", func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			if over.ContextOf(ctx) != trusted {
				return nil, errors.New("the handler did not see the constructed context")
			}
			decision, err := guard.Decide(ctx, "method:list", map[string]any{"projectId": "9"})
			if err != nil {
				return nil, err
			}
			return hex.EncodeToString(decision.Subject[:]), nil
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out any
	if err := p.client.Call(ctx, over.ChallengeMethod, struct{}{}, &out); codeOf(err) != string(auth.Established) {
		t.Fatalf("the exchange on a trusted peer: %v", err)
	}
	alice := table.key("alice")
	if err := p.client.Call(ctx, "list", map[string]string{"projectId": "9"}, &out); err != nil || out != hex.EncodeToString(alice[:]) {
		t.Fatalf("decided on the constructed context: %v %v", out, err)
	}
	if over.ContextOf(context.Background()) != nil || over.ContextOf(nil) != nil {
		t.Fatal("a context from nowhere")
	}
}

func TestAnExportedReferenceIsDecidedUnderTheInvokingConnectionAndAnEmissionPerRecipient(t *testing.T) {
	table := loadOverTable(t)
	root := grant.Root{Key: table.key("root"), Domain: table.Root.Domain}
	const audience = "https://api.example.test/nightseam"
	now := func() grant.Time { return grant.Time{Present: true, Now: 1799990000} }
	nonce := func() [32]byte {
		var n [32]byte
		copy(n[:], mustHex("a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"))
		return n
	}
	surface, _ := over.SurfaceOf("worker", "server", overDeclaration, "d1")
	binding, _ := auth.Bind(surface, overPolicy())
	guard, _ := over.NewGuard(binding, root, now)
	if err := guard.Export("status:7", "callable:Status", "projects/7"); err != nil {
		t.Fatal(err)
	}
	if err := guard.Export("x", "method:list", "projects/7"); err == nil {
		t.Fatal("a method is not a callable member")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connections := map[string]overPair{}
	for _, who := range []struct {
		name  string
		chain []string
	}{{"bob", []string{"root_alice", "alice_bob"}}, {"carol", []string{"root_alice", "alice_bob", "bob_carol"}}} {
		p := pair(t, over.Prepare(context.Background()), over.Options{Root: root, Audience: audience, Now: now, Nonce: nonce}, nil)
		var challenged struct{ Nonce string }
		if err := p.client.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); err != nil {
			t.Fatal(err)
		}
		proof, _ := auth.Prove(table.seed(who.name), audience, mustHex(challenged.Nonce))
		key := table.key(who.name)
		chain := make([]string, 0, len(who.chain))
		for _, e := range who.chain {
			chain = append(chain, table.Envelopes[e])
		}
		var proved any
		if err := p.client.Call(ctx, over.ProveMethod, map[string]any{"subject": "ed25519:" + hex.EncodeToString(key[:]), "possession": hex.EncodeToString(proof), "chain": chain}, &proved); err != nil {
			t.Fatalf("%s: %v", who.name, err)
		}
		connections[who.name] = p
	}
	// The same reference, invoked under each connection's context.
	if d, err := guard.Invoke(connections["bob"].server.Context(), "status:7", nil); err != nil || d.Scope != "projects/7" {
		t.Fatalf("bob invokes his job: %v %v", d, err)
	}
	if _, err := guard.Invoke(connections["carol"].server.Context(), "status:7", nil); codeOf(err) != string(auth.Denied) {
		t.Fatalf("carol invokes bob's job: %v", err)
	}
	if _, err := guard.Invoke(context.Background(), "status:7", nil); codeOf(err) != string(auth.Unauthenticated) {
		t.Fatalf("no connection invokes: %v", err)
	}
	if _, err := guard.Invoke(connections["bob"].server.Context(), "nobody", nil); codeOf(err) != string(auth.ReferenceUnknown) {
		t.Fatalf("a reference not exported: %v", err)
	}
	// One emission, decided per recipient.
	deliveries := guard.Emit("event:progress", map[string]any{"projectId": "7", "percent": 50}, []auth.Recipient{
		over.RecipientOf("bob", connections["bob"].server.Context()),
		over.RecipientOf("carol", connections["carol"].server.Context()),
		over.RecipientOf("nobody", context.Background()),
	})
	want := map[string]string{"bob": "", "carol": string(auth.Denied), "nobody": string(auth.Unauthenticated)}
	for _, d := range deliveries {
		got := ""
		if d.Refused != nil {
			got = string(d.Refused.Code)
		}
		if got != want[d.Recipient] {
			t.Errorf("%s: %q, want %q", d.Recipient, got, want[d.Recipient])
		}
	}
}
