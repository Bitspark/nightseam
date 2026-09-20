package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/live/go/livetest"
	"github.com/Bitspark/nightseam/runtime/go"
)

// over is two scopes over a pipe: the pair every shared case runs on.
func over(t *testing.T, options live.Options) livetest.Pair {
	t.Helper()
	return overObserved(t, options, nil)
}

func overObserved(t *testing.T, options live.Options, observer runtime.Observer) livetest.Pair {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a, b := duplex.Pipe(8 << 20)
	var sa, sb *live.Scope
	pa, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{
		Observer: observer,
		Prepare:  func(p *runtime.Peer) (err error) { sa, err = live.Over(p, options); return err },
	})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{
		Prepare: func(p *runtime.Peer) (err error) { sb, err = live.Over(p, options); return err },
	})
	if err != nil {
		t.Fatal(err)
	}
	return livetest.Pair{A: sa, B: sb, Close: func() { pa.Close(); pb.Close(); cancel() }}
}

// TestSuite is the shared suite, which is what holds this runtime and its
// TypeScript twin to one behavior.
func TestSuite(t *testing.T) {
	for _, c := range livetest.Cases() {
		t.Run(c.Name, func(t *testing.T) {
			p := over(t, live.Options{})
			defer p.Close()
			c.Run(t, p)
		})
	}
}

// TestOverASocket runs the suite over a real WebSocket rather than a pipe: the
// layer is a layer of the profile and runs over any connection of the seam.
func TestOverASocket(t *testing.T) {
	var served *live.Scope
	ready := make(chan struct{})
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			served, err = live.Over(p, live.Options{})
			return err
		}},
		OnConnect:    func(*runtime.Peer) { close(ready) },
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var dialled *live.Scope
	peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			dialled, err = live.Over(p, live.Options{})
			return err
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	<-ready

	reported := make(chan string, 1)
	exported, err := dialled.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		reported <- string(r)
		return json.RawMessage("null"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	arrived, err := served.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := served.Owner().Import(arrived, "probe/Report")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(ctx, json.RawMessage("42")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reported:
		if got != "42" {
			t.Errorf("the callback was asked %s over a socket", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the callback was never reached over a socket")
	}
}

func TestSerializedReferenceNewConnection(t *testing.T) {
	serializedReferenceNewConnection(t)
}

// Old serialized bytes can be decoded and imported on a new connection, but
// invocation cannot resolve them to a fresh binding in the new export table.
func serializedReferenceNewConnection(t *testing.T) {
	t.Helper()
	first := over(t, live.Options{})
	defer first.Close()
	exported, err := first.A.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	carried, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := over(t, live.Options{})
	defer second.Close()
	fresh, err := second.A.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both connections have allocated their first binding. The fresh scope's
	// namespace keeps the old id from naming that new export.
	arrived, err := second.B.Decode(carried)
	if err != nil {
		t.Fatalf("old serialized bytes were refused at decode: %v", err)
	}
	if got := second.B.Counts(); got != (live.Counts{}) {
		t.Fatalf("decoding old bytes created an attachment: %+v", got)
	}
	invoke, err := second.B.Owner().Import(arrived, "probe/Report")
	if err != nil {
		t.Fatalf("old serialized bytes were refused at import: %v", err)
	}
	if got := second.B.Counts(); got != (live.Counts{Imports: 1}) {
		t.Fatalf("importing old bytes did not retain an attachment: %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = invoke(ctx, json.RawMessage("1"))
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != live.ErrorReferenceUnknown {
		t.Fatalf("a reference of an ended connection resolved in a new one: %v", err)
	}
	if got := second.B.Counts(); got != (live.Counts{Imports: 1}) {
		t.Fatalf("the unknown-binding refusal changed its attachment: %+v", got)
	}
	freshRaw, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	freshArrived, err := second.B.Decode(freshRaw)
	if err != nil {
		t.Fatal(err)
	}
	freshInvoke, err := second.B.Owner().Import(freshArrived, "probe/Report")
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := freshInvoke(ctx, json.RawMessage(`"fresh"`)); err != nil || string(answer) != `"fresh"` {
		t.Fatalf("the fresh binding did not remain usable: %s, %v", answer, err)
	}
	if err := second.A.Peer().Handle("ordinary", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) { return raw, nil }); err != nil {
		t.Fatal(err)
	}
	var answer string
	if err := second.B.Peer().Call(ctx, "ordinary", "alive", &answer); err != nil || answer != "alive" {
		t.Fatalf("ordinary RPC after stale invocation: %q, %v", answer, err)
	}
	if got := second.A.Counts(); got != (live.Counts{Exports: 1}) {
		t.Errorf("the fresh export count changed: %+v", got)
	}
	if got := second.B.Counts(); got != (live.Counts{Imports: 2}) {
		t.Errorf("the old and fresh references should retain two attachments: %+v", got)
	}
}

func TestScopeOfThePeer(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	found, ok := live.ScopeOf(p.A.Peer())
	if !ok || found != p.A {
		t.Fatalf("the scope over a peer was not found on it")
	}
	if _, ok := live.ScopeOf(nil); ok {
		t.Errorf("a nil peer carries a scope")
	}
}

func TestTheBoundsRefuseAndLeaveNothing(t *testing.T) {
	p := over(t, live.Options{MaxExports: 1, MaxImports: 1})
	defer p.Close()
	first, err := p.A.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.A.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		return r, nil
	}); err == nil {
		t.Fatal("a second export past the bound was admitted")
	} else {
		var public *runtime.PublicError
		if !errors.As(err, &public) || public.Code != live.ErrorTooManyExports {
			t.Errorf("expected too_many_exports, got %v", err)
		}
	}
	if got := p.A.Counts(); got.Exports != 1 {
		t.Errorf("a refused export left %d behind", got.Exports)
	}

	raw, _ := json.Marshal(first)
	arrived, _ := p.B.Decode(raw)
	if _, err := p.B.Owner().Import(arrived, "probe/Report"); err != nil {
		t.Fatal(err)
	}
	second, _ := p.B.Decode(json.RawMessage(`{"binding":"deadbeefdeadbeef.9","contract":"probe/Report"}`))
	if _, err := p.B.Owner().Import(second, "probe/Report"); err == nil {
		t.Fatal("a second import past the bound was admitted")
	} else {
		var public *runtime.PublicError
		if !errors.As(err, &public) || public.Code != live.ErrorTooManyImports {
			t.Errorf("expected too_many_imports, got %v", err)
		}
	}
	if got := p.B.Counts(); got.Imports != 1 {
		t.Errorf("a refused import left %d behind", got.Imports)
	}
}

func TestNegativeBoundsAreRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := duplex.Pipe(1 << 20)
	peer, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if _, err := live.Over(peer, live.Options{MaxExports: -1}); err == nil {
		t.Errorf("a negative bound was admitted")
	}
	if _, err := live.Over(nil, live.Options{}); err == nil {
		t.Errorf("a scope was made over no peer")
	}
}

// TestAReferenceIsNotAConstructibleValue holds the operator's verdict: the
// only ways into a reference are an export and a decode of this scope's.
func TestAReferenceIsNotAConstructibleValue(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	var forged live.Reference
	if _, err := json.Marshal(forged); err == nil {
		t.Errorf("a reference nobody minted marshalled")
	}
	if _, err := p.B.Owner().Import(forged, "probe/Report"); err == nil {
		t.Errorf("a reference nobody minted imported")
	}
	if _, err := p.B.Decode(json.RawMessage(`{"binding":"","contract":"probe/Report"}`)); err == nil {
		t.Errorf("a reference naming no binding decoded")
	}
	if _, err := p.B.Decode(json.RawMessage(`{"binding":"a.1"}`)); err == nil {
		t.Errorf("a reference carrying no contract decoded")
	}
	if _, err := p.B.Decode(json.RawMessage(`7`)); err == nil {
		t.Errorf("a number decoded as a reference")
	}
}

func TestTheObserverIsToldAndSeesNoPayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := duplex.Pipe(1 << 20)
	seen := &recorder{}
	var sa, sb *live.Scope
	pa, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{
		Observer: seen,
		Prepare:  func(p *runtime.Peer) (err error) { sa, err = live.Over(p, live.Options{}); return err },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pa.Close()
	pb, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{
		Prepare: func(p *runtime.Peer) (err error) { sb, err = live.Over(p, live.Options{}); return err },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()

	exported, err := sa.Owner().Export("probe/Report", func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sa.Owner().Export("", nil); err == nil {
		t.Fatal("an export of no contract was admitted")
	}
	raw, _ := json.Marshal(exported)
	arrived, _ := sb.Decode(raw)
	if _, err := sb.Owner().Import(arrived, "probe/Report"); err != nil {
		t.Fatal(err)
	}
	if err := sa.Release(exported); err != nil {
		t.Fatal(err)
	}

	kinds := seen.kinds()
	for _, want := range []string{"live.LiveExported", "live.LiveRefused", "live.LiveReleased"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("the observer was never told %s; it saw %s", want, kinds)
		}
	}
	for _, event := range seen.events() {
		if exported, ok := event.(live.LiveExported); ok && exported.Contract != "probe/Report" {
			t.Errorf("the exported event named %q", exported.Contract)
		}
	}
}

// recorder is told from whichever goroutine the event happened on, as any
// observer is, and so holds what it was told under a lock of its own.
type recorder struct {
	mu   sync.Mutex
	seen []runtime.ObserverEvent
}

func (r *recorder) Observe(event runtime.ObserverEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, event)
}

func (r *recorder) events() []runtime.ObserverEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runtime.ObserverEvent(nil), r.seen...)
}

func (r *recorder) kinds() string {
	names := make([]string, 0)
	for _, event := range r.events() {
		names = append(names, fmt.Sprintf("%T", event))
	}
	return strings.Join(names, " ")
}
