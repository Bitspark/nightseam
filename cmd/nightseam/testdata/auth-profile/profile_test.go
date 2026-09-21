// The authority profile over generated sockets: the real exchange with the
// tables' keys, a guarded generated model in each language serving the
// other, expiry at use through a clock the test moves, emissions per
// recipient, returned callables and the reverse direction — the routes the
// synthetic witness ran, now under the profile.
package profile_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	binding "example.test/generated/api/go/worker-binding"
	protocol "example.test/generated/api/go/worker-protocol"
	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
	"github.com/Bitspark/nightseam/auth/go/over"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// ---- the tables' keys ------------------------------------------------------------------

type table struct {
	Root      struct{ Key, Domain string }
	Keys      map[string]struct{ Seed, Pubkey string }
	Envelopes map[string]string
}

var fixtures = sync.OnceValue(func() table {
	data, err := os.ReadFile("auth-exposure.json")
	if err != nil {
		panic(err)
	}
	var t table
	if err := json.Unmarshal(data, &t); err != nil {
		panic(err)
	}
	return t
})

func mustHex(text string) []byte {
	b, err := hex.DecodeString(text)
	if err != nil {
		panic(err)
	}
	return b
}

func keyOf(name string) [32]byte {
	var key [32]byte
	copy(key[:], mustHex(fixtures().Keys[name].Pubkey))
	return key
}

func rootOf() grant.Root { return grant.Root{Key: keyOf("root"), Domain: fixtures().Root.Domain} }

func chainOf(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fixtures().Envelopes[n])
	}
	return out
}

func chainBytes(names ...string) [][]byte {
	out := make([][]byte, 0, len(names))
	for _, n := range names {
		out = append(out, mustHex(fixtures().Envelopes[n]))
	}
	return out
}

// ---- the clock and the entropy, both the test's --------------------------------------------

var clock atomic.Uint64

func now() grant.Time { return grant.Time{Present: true, Now: clock.Load()} }

var nonces atomic.Uint32

// nonce is deterministic entropy: the n-th challenge is n repeated.
func nonce() [32]byte {
	var n [32]byte
	fill := byte(0xa0 + nonces.Add(1))
	for i := range n {
		n[i] = fill
	}
	return n
}

// ---- the policy and the guarded worker --------------------------------------------------

func serverPolicy() auth.Policy {
	return auth.Policy{Family: "worker", Digest: protocol.WireDigest(), Treatments: map[string]auth.Treatment{
		"method:list":           {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{projectId}"},
		"method:whoami":         {Kind: auth.KindPublic},
		"method:start":          {Kind: auth.KindGuarded, Action: "write", Scope: "projects/{projectId}"},
		"method:subscribe":      {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{projectId}"},
		"event:progress":        {Kind: auth.KindGuarded, Action: "read", Scope: "projects/{projectId}"},
		"callable:JobStatus":    {Kind: auth.KindGuarded, Action: "read", Scope: "{export}"},
		"callable:JobCancel":    {Kind: auth.KindGuarded, Action: "write", Scope: "{export}"},
		"callable:ProgressSink": {Kind: auth.KindDenied},
	}}
}

func serverGuard(t *testing.T) *over.Guard {
	t.Helper()
	surface, err := over.SurfaceOf("worker", "server", protocol.WireDeclaration(), protocol.WireDigest())
	if err != nil {
		t.Fatal(err)
	}
	bound, refused := auth.Bind(surface, serverPolicy())
	if refused != nil {
		t.Fatal(refused)
	}
	guard, err := over.NewGuard(bound, rootOf(), now)
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

type counters struct{ dispatched, effects, emissions, callables atomic.Int64 }

// worker is the owner: every member decides at entry on the context the
// binding delivers, and the effect again inside the owner's lock.
type worker struct {
	guard  *over.Guard
	c      *counters
	client protocol.Client
	ctx    context.Context // the connection's, for emission
	mu     sync.Mutex
	jobs   atomic.Int64
}

func (w *worker) List(ctx context.Context, params protocol.ListRequest) (protocol.Listing, error) {
	if _, err := w.guard.Decide(ctx, "method:list", over.Payload(params)); err != nil {
		return protocol.Listing{}, err
	}
	w.c.dispatched.Add(1)
	return protocol.Listing{Count: 1}, nil
}

func (w *worker) Whoami(ctx context.Context) (string, error) {
	if _, err := w.guard.Decide(ctx, "method:whoami", nil); err != nil {
		return "", err
	}
	if c := over.ContextOf(ctx); c != nil {
		return hex.EncodeToString(c.Subject[:]), nil
	}
	return "", nil
}

func (w *worker) Start(ctx context.Context, params protocol.StartRequest) (protocol.Job, error) {
	decision, err := w.guard.Decide(ctx, "method:start", over.Payload(params))
	if err != nil {
		return protocol.Job{}, err
	}
	w.c.dispatched.Add(1)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.guard.Effect(ctx, decision, nil); err != nil {
		return protocol.Job{}, err
	}
	w.c.effects.Add(1)
	ref := "job:" + params.ProjectId + ":" + hex.EncodeToString([]byte{byte(w.jobs.Add(1))})
	if err := w.guard.Export(ref, "callable:JobStatus", "projects/"+params.ProjectId); err != nil {
		return protocol.Job{}, err
	}
	cancelRef := ref + ":cancel"
	if err := w.guard.Export(cancelRef, "callable:JobCancel", "projects/"+params.ProjectId); err != nil {
		return protocol.Job{}, err
	}
	return protocol.Job{
		ProjectId: params.ProjectId,
		Status: func(ctx context.Context, id string) (string, error) {
			if _, err := w.guard.Invoke(ctx, ref, nil); err != nil {
				return "", err
			}
			w.c.callables.Add(1)
			return "running:" + id, nil
		},
		Cancel: func(ctx context.Context, id string) (bool, error) {
			if _, err := w.guard.Invoke(ctx, cancelRef, nil); err != nil {
				return false, err
			}
			w.c.callables.Add(1)
			return true, nil
		},
	}, nil
}

func (w *worker) Subscribe(ctx context.Context, params protocol.Subscription) (bool, error) {
	if _, err := w.guard.Decide(ctx, "method:subscribe", over.Payload(params)); err != nil {
		return false, err
	}
	w.c.dispatched.Add(1)
	return params.Sink(ctx, protocol.Progress{ProjectId: params.ProjectId, Percent: 1})
}

func (w *worker) emit(data protocol.Progress) error {
	deliveries := w.guard.Emit("event:progress", over.Payload(data), []auth.Recipient{over.RecipientOf("this", w.ctx)})
	if deliveries[0].Refused != nil {
		return deliveries[0].Refused.Public()
	}
	w.c.emissions.Add(1)
	return w.client.Events.Progress(w.ctx, data)
}

// ---- a served side --------------------------------------------------------------------------

type served struct {
	server   *httptest.Server
	audience string
	guard    *over.Guard
	counters counters
	workers  sync.Map // *runtime.Peer → *worker
	prepared chan *runtime.Peer
}

func serve(t *testing.T, controls func(*served, *runtime.Peer) error) *served {
	t.Helper()
	s := &served{guard: serverGuard(t), prepared: make(chan *runtime.Peer, 8)}
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return over.Prepare(r.Context()), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			if _, err := over.Over(peer, over.Options{Root: rootOf(), Audience: s.audience, Now: now, Nonce: nonce}); err != nil {
				return err
			}
			scope, err := live.Over(peer, live.Options{MaxImports: 32, MaxExports: 64})
			if err != nil {
				return err
			}
			wire, err := binding.ToWire(func(client protocol.Client) (protocol.Server, error) {
				w := &worker{guard: s.guard, c: &s.counters, client: client, ctx: peer.Context()}
				s.workers.Store(peer, w)
				return protocol.Server{Methods: w, Events: struct{}{}}, nil
			}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)})
			if err != nil {
				return err
			}
			detach, err := runtime.ForwardWire(peer.Wire(), wire)
			if err != nil {
				return err
			}
			go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
			if controls != nil {
				if err := controls(s, peer); err != nil {
					return err
				}
			}
			s.prepared <- peer
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.server = httptest.NewServer(handler)
	t.Cleanup(s.server.Close)
	url := "ws" + strings.TrimPrefix(s.server.URL, "http")
	if s.audience, err = auth.Audience(url); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *served) url() string { return "ws" + strings.TrimPrefix(s.server.URL, "http") }

func (s *served) emitAll(data protocol.Progress) (delivered, dropped int) {
	s.workers.Range(func(_, value any) bool {
		if err := value.(*worker).emit(data); err != nil {
			dropped++
		} else {
			delivered++
		}
		return true
	})
	return
}

func controls(s *served, peer *runtime.Peer) error {
	if err := peer.Handle("test.emit", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
		var p protocol.Progress
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		delivered, dropped := s.emitAll(p)
		return map[string]int{"delivered": delivered, "dropped": dropped}, nil
	}); err != nil {
		return err
	}
	if err := peer.Handle("test.notify", func(ctx context.Context, peer *runtime.Peer, raw json.RawMessage) (any, error) {
		var p protocol.Progress
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		value, ok := s.workers.Load(peer)
		if !ok {
			return nil, errors.New("no worker for this connection")
		}
		ok, err := value.(*worker).client.Methods.Notify(ctx, p)
		if err != nil {
			return map[string]any{"refused": codeOf(err)}, nil
		}
		return map[string]any{"ok": ok}, nil
	}); err != nil {
		return err
	}
	return peer.Handle("test.clock", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
		var at uint64
		if err := json.Unmarshal(raw, &at); err != nil {
			return nil, err
		}
		clock.Store(at)
		return at, nil
	})
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

// ---- Go serves, TypeScript is the client ------------------------------------------------------

func TestProfileGoServerTypeScriptClient(t *testing.T) {
	clock.Store(1799990000)
	s := serve(t, controls)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "profile.ts", s.url(), "go-server")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("AUTH-PROFILE typescript client: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	if s.counters.effects.Load() != 1 || s.counters.emissions.Load() != 1 || s.counters.callables.Load() != 3 {
		t.Fatalf("effects=%d emissions=%d callables=%d", s.counters.effects.Load(), s.counters.emissions.Load(), s.counters.callables.Load())
	}
}

// ---- TypeScript serves, Go is the client ------------------------------------------------------

// The TypeScript side dials the socket and serves the model under its own
// layer; this side, the host of the socket, is the model's client: it runs
// the exchange against the TypeScript layer and is decided there.
func TestProfileTypeScriptServerGoClient(t *testing.T) {
	clock.Store(1799990000)
	type connection struct {
		peer     *runtime.Peer
		complete func(context.Context) (protocol.ServerModel, error)
		scope    *live.Scope
	}
	connections := make(chan connection, 4)
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			scope, err := live.Over(peer, live.Options{MaxImports: 32, MaxExports: 64})
			if err != nil {
				return err
			}
			complete, cleanup, err := binding.PrepareFromWire(peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)})
			if err != nil {
				return err
			}
			go func() { <-peer.Done(); cleanup() }()
			connections <- connection{peer: peer, complete: complete, scope: scope}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	audience, err := auth.Audience(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "profile.ts", url, "typescript-server")
	output := &strings.Builder{}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	var c connection
	select {
	case c = <-connections:
	case err := <-finished:
		t.Fatalf("the TypeScript server ended before connecting: %v\n%s", err, output)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	model, err := c.complete(ctx)
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	access, err := model(protocol.Client{Methods: quiet{}, Events: quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	// Before the exchange, a protected member is refused by the TypeScript guard.
	_, err = access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"})
	if codeOf(err) != string(auth.Unauthenticated) {
		t.Fatalf("before the exchange: %v\n%s", err, output)
	}
	// The exchange, against the TypeScript layer, with bob's key.
	var challenged struct{ Nonce string }
	if err := c.peer.Call(ctx, over.ChallengeMethod, struct{}{}, &challenged); err != nil {
		t.Fatal(err)
	}
	proof, err := auth.Prove(mustHex(fixtures().Keys["bob"].Seed), audience, mustHex(challenged.Nonce))
	if err != nil {
		t.Fatal(err)
	}
	bob := keyOf("bob")
	var proved map[string]any
	if err := c.peer.Call(ctx, over.ProveMethod, map[string]any{"subject": "ed25519:" + hex.EncodeToString(bob[:]), "possession": hex.EncodeToString(proof), "chain": chainOf("root_alice", "alice_bob")}, &proved); err != nil {
		t.Fatalf("prove: %v\n%s", err, output)
	}
	if proved["expires_at"] != float64(1800000000) {
		t.Fatalf("proved: %v", proved)
	}
	// Decided in TypeScript on the context the exchange made.
	if _, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); err != nil {
		t.Fatalf("own project: %v", err)
	}
	_, err = access.Methods.List(ctx, protocol.ListRequest{ProjectId: "8"})
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != string(auth.Denied) || !strings.Contains(string(public.Data), `"not_covered"`) {
		t.Fatalf("sibling: %v %s", err, public.Data)
	}
	if _, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7/ledger"}); codeOf(err) != string(auth.SelectorInvalid) {
		t.Fatalf("selector: %v", err)
	}
	// A returned callable, decided at each invocation in TypeScript; expiry at use.
	owner := c.scope.Owner().Child()
	defer owner.Release()
	octx := live.WithOwner(ctx, owner)
	job, err := access.Methods.Start(octx, protocol.StartRequest{ProjectId: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := job.Status(octx, "j1"); err != nil || status != "running:j1" {
		t.Fatalf("status: %q %v", status, err)
	}
	var at uint64
	if err := c.peer.Call(ctx, "test.clock", 1800000000, &at); err != nil {
		t.Fatal(err)
	}
	if _, err := job.Status(octx, "j1"); codeOf(err) != string(auth.Denied) {
		t.Fatalf("retained callable after expiry: %v", err)
	}
	if _, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); codeOf(err) != string(auth.Denied) {
		t.Fatalf("expired at use: %v", err)
	}
	if err := c.peer.Call(ctx, "test.clock", 1799990000, &at); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); err != nil {
		t.Fatalf("after the clock returns: %v", err)
	}
	var done any
	if err := c.peer.Call(ctx, "test.done", struct{}{}, &done); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatalf("the TypeScript server: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}

type quiet struct{}

func (quiet) Notify(context.Context, protocol.Progress) (bool, error) { return true, nil }
func (quiet) Progress(context.Context, protocol.Progress) error       { return nil }
