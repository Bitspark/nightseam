package slogobserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/runtime/go/slogobserver"
)

// The adapter is a mapping from an event to a record, so what holds it is the
// text it writes, byte for byte, under testdata. A change to a level, to an
// attribute's name or to what is left out shows up as a diff of a golden log,
// which is what a reviewer reads. When the change is meant, rewrite them:
//
//	go test ./runtime/go/slogobserver -update
var update = flag.Bool("update", false, "rewrite the golden logs under testdata from the current output")

// sentinel stands for every payload a peer carries, as it does in the runtime's
// own observer suite: it travels in the params of a call, in what answers it,
// in the data of a public error and in the data of an event, and it must appear
// in no line this adapter writes.
const sentinel = "payload-sentinel-4bf92f35"

// Every event of the surface, one after another, with the level its kind is
// logged at, the fields of the event as attributes in lower snake case, and the
// trace and the family left out where the event carries neither. The times are
// the events' own: a record is dated where the peer saw the thing happen.
func TestEveryEventIsOneRecord(t *testing.T) {
	var out bytes.Buffer
	observer := slogobserver.New(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	step := func(n int) time.Time { return at.Add(time.Duration(n) * time.Millisecond) }
	traced := runtime.Trace{Parent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", State: "nightseam=1"}
	// A response names no method, carries the request's traceparent without a
	// tracestate, and belongs to no family the caller labelled: what an event
	// does not carry is what the record does not say.
	bare := runtime.Trace{Parent: traced.Parent}
	for _, event := range []runtime.ObserverEvent{
		runtime.ConnectionOpened{At: step(0), Role: runtime.ClientRole},
		runtime.RequestStarted{At: step(1), ID: "c:1", Method: "turn.start", Trace: traced, Family: "turns"},
		runtime.FrameSent{At: step(2), Kind: "request", Name: "turn.start", Bytes: 148, ID: "c:1", Trace: traced, Family: "turns"},
		runtime.FrameReceived{At: step(3), Kind: "response", Bytes: 96, ID: "c:1", Trace: bare},
		runtime.RequestEnded{At: step(4), ID: "c:1", Method: "turn.start", Duration: 2 * time.Millisecond,
			Outcome: runtime.OutcomeOK, Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(5), ID: "c:2", Method: "turn.stop", Incoming: true, Duration: 1500 * time.Microsecond,
			Outcome: runtime.OutcomeErrored, ErrorCode: "denied", Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(6), ID: "c:3", Method: "turn.wait", Duration: 3 * time.Millisecond,
			Outcome: runtime.OutcomeCancelled, Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(7), ID: "c:4", Method: "turn.wait", Duration: 200 * time.Millisecond,
			Outcome: runtime.OutcomeTimedOut, Trace: traced, Family: "turns"},
		runtime.EventEmitted{At: step(8), Name: "grants.deliver", Bytes: 42, Trace: traced, Family: "grants"},
		runtime.EventDelivered{At: step(9), Name: "grants.deliver", Bytes: 42, Trace: bare},
		runtime.Backpressure{At: step(10), Queued: 64, Deadline: 5 * time.Second},
		runtime.Backpressure{At: step(11), Queued: 64, Stalled: true, Deadline: 5 * time.Second},
		runtime.HandlerPanic{At: step(12), Method: "turn.start", Value: "the handler gave up", Trace: traced, Family: "turns"},
		runtime.ConnectionClosed{At: step(13), Code: 1000, Local: true},
	} {
		observer.Observe(event)
	}
	holdGolden(t, "events.log", out.String())
}

// One call, one reverse call, one event and one close, read off a connection
// two peers actually held: what the adapter writes for the traffic a peer sees
// is the golden, with the clock, the duration and the trace the propagator
// minted pinned, those being the three things no run repeats.
func TestAnExchangeIsGolden(t *testing.T) {
	out := new(buffer)
	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: pinned})
	ticked := make(chan struct{})
	labels := map[string]string{"echo": "probe", "tick": "probe"}
	peer, remote := newPair(t, runtime.Options{
		Families: labels,
		Handlers: map[string]runtime.Handler{
			"echo": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return "answered", nil },
		},
		Events: map[string]runtime.EventHandler{
			"tick": func(context.Context, *runtime.Peer, json.RawMessage) { close(ticked) },
		},
	}, runtime.Options{
		Observer: slogobserver.New(slog.New(handler)),
		Families: labels,
		Handlers: map[string]runtime.Handler{
			"back": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return "returned", nil },
		},
	})
	var answered, returned string
	if err := peer.Call(context.Background(), "echo", nil, &answered); err != nil || answered != "answered" {
		t.Fatalf("echo = %q, error=%v", answered, err)
	}
	if err := remote.Call(context.Background(), "back", nil, &returned); err != nil || returned != "returned" {
		t.Fatalf("back = %q, error=%v", returned, err)
	}
	if err := peer.Emit(context.Background(), "tick", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, ticked)
	_ = peer.Close()
	receive(t, peer.Done())
	// The close is written on the goroutine that ended the connection, so the
	// golden is read once every line of it is there.
	out.await(t, 12)
	holdGolden(t, "exchange.log", out.String())
}

// The rule of the whole surface, on the text this time: a sentinel travelling
// in a call's params, in what answered it, in a public error's data and in an
// event's data appears in no line either side wrote.
func TestNoPayloadReachesALine(t *testing.T) {
	payload := map[string]string{"secret": sentinel}
	delivered := make(chan struct{})
	client, server := new(buffer), new(buffer)
	logger := func(out *buffer) *slog.Logger {
		return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	peer, remote := newPair(t, runtime.Options{
		Observer: slogobserver.New(logger(server)),
		Handlers: map[string]runtime.Handler{
			"echo": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return payload, nil },
			"deny": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
				return nil, &runtime.PublicError{Code: "denied", Message: "Denied.",
					Data: json.RawMessage(`{"secret":"` + sentinel + `"}`)}
			},
			"boom": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { panic("the handler gave up") },
		},
		Events: map[string]runtime.EventHandler{
			"tick": func(context.Context, *runtime.Peer, json.RawMessage) { close(delivered) },
		},
	}, runtime.Options{Observer: slogobserver.New(logger(client))})
	var result map[string]string
	if err := peer.Call(context.Background(), "echo", payload, &result); err != nil || result["secret"] != sentinel {
		t.Fatalf("echo = %v, error=%v", result, err)
	}
	var denied *runtime.PublicError
	if err := peer.Call(context.Background(), "deny", payload, nil); !errors.As(err, &denied) ||
		!strings.Contains(string(denied.Data), sentinel) {
		t.Fatalf("deny = %v", err)
	}
	if err := peer.Call(context.Background(), "boom", payload, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := peer.Emit(context.Background(), "tick", payload); err != nil {
		t.Fatal(err)
	}
	receive(t, delivered)
	_ = peer.Close()
	receive(t, peer.Done())
	receive(t, remote.Done())
	// The sentinel travelled every path there is, so what follows is about what
	// the logs were spared and not about an idle connection.
	server.await(t, 12)
	if !strings.Contains(server.String(), `msg="handler panic"`) {
		t.Fatalf("the server logged no handler panic:\n%s", server)
	}
	if !strings.Contains(client.String(), "error_code=denied") {
		t.Fatalf("the client logged no refused request:\n%s", client)
	}
	for side, out := range map[string]*buffer{"client": client, "server": server} {
		for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
			if strings.Contains(line, sentinel) {
				t.Fatalf("%s wrote a payload: %s", side, line)
			}
		}
	}
}

// channelOpened stands for an event of a layer running over the peer — a
// tunnel's, a live scope's, a later profile's — which reaches this adapter
// through the peer's observer before this package has a case for it.
type channelOpened struct {
	At     time.Time
	ID     uint64
	Family string
}

func (channelOpened) ObserverEvent() {}

// An event this package does not know is logged as what it is rather than
// dropped: its Go type name, and its fields with it.
func TestAnUnknownEventIsLoggedAndNotDropped(t *testing.T) {
	var out bytes.Buffer
	observer := slogobserver.New(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	observer.Observe(channelOpened{At: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC), ID: 7, Family: "probe"})
	written := out.String()
	if lines := strings.Count(written, "\n"); lines != 1 {
		t.Fatalf("an unknown event wrote %d lines:\n%s", lines, written)
	}
	for _, want := range []string{"level=DEBUG", "msg=slogobserver_test.channelOpened", "ID:7", "Family:probe"} {
		if !strings.Contains(written, want) {
			t.Fatalf("an unknown event was logged as %q, which says nothing of %q", strings.TrimSuffix(written, "\n"), want)
		}
	}
}

// pinned replaces what no run of an exchange repeats — the clock, a duration
// measured across a connection, the trace the default propagator mints at
// random — with a constant, so that what the golden holds is the mapping this
// package is and not the machine it ran on.
func pinned(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return slog.Time(slog.TimeKey, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	case "duration":
		return slog.Duration("duration", time.Millisecond)
	case "traceparent":
		return slog.String("traceparent", "00-"+strings.Repeat("0", 32)+"-"+strings.Repeat("0", 16)+"-01")
	}
	return a
}

// holdGolden compares what the adapter wrote against a golden log, or rewrites
// it under -update.
func holdGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Fatalf("%s has no golden; run with -update. the adapter wrote:\n%s", name, got)
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs\n--- golden ---\n%s--- written ---\n%s", name, want, got)
	}
}

// buffer is where a handler writes while the peers that feed it are still
// running: Observe is called from whichever goroutine the event happened on,
// so what was written is read under a lock.
type buffer struct {
	changed chan struct{}
	mu      sync.Mutex
	written bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
	return b.written.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written.String()
}

// next captures a notification before reading the state, so no update is lost.
func (b *buffer) next() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
	return b.changed
}

// await waits until at least count lines were written, so that a golden is
// never read off a log a peer is still finishing.
func (b *buffer) await(t *testing.T, count int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		next := b.next()
		if written := b.String(); strings.Count(written, "\n") >= count {
			return
		}
		select {
		case <-next:
		case <-deadline.C:
			t.Fatalf("%d lines were written, want %d:\n%s", strings.Count(b.String(), "\n"), count, b)
		}
	}
}

// newPair is two peers over one WebSocket, as the runtime's own suite builds
// them: the adapter is held against traffic a peer really carried.
func newPair(t *testing.T, serverOptions, clientOptions runtime.Options) (*runtime.Peer, *runtime.Peer) {
	t.Helper()
	connected := make(chan *runtime.Peer, 1)
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Options:      serverOptions,
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		OnConnect:    func(peer *runtime.Peer) { connected <- peer },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, _, err := runtime.Dial(ctx, server.URL, runtime.DialOptions{Options: clientOptions})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	remote := receive(t, connected)
	t.Cleanup(func() { _ = remote.Close() })
	return client, remote
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for peer activity")
		var zero T
		return zero
	}
}
