package composition_test

// How the proof is wired, in the shape a consumer wires it: one outer
// WebSocket speaking the worker family, a tunnel over that peer made in
// Prepare — before the peer reads its first frame — and a scope over the
// tunnel. Every reference exchanged below is a channel of that tunnel, and
// every socket here is a real one.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cellbinding "example.test/generated/api/go/cell-binding"
	cellclient "example.test/generated/api/go/cell-client"
	topicbinding "example.test/generated/api/go/topic-binding"
	topicclient "example.test/generated/api/go/topic-client"
	workerbinding "example.test/generated/api/go/worker-binding"
	workerclient "example.test/generated/api/go/worker-client"
	runtime "github.com/Bitspark/nightseam/runtime/go"
	tunnel "github.com/Bitspark/nightseam/tunnel/go"
)

const settle = 3 * time.Second

// workers is the server: one worker implementation behind a real HTTP server,
// with a scope per connection. The scope is per connection and not per
// process because a reference means nothing off the connection that carried
// it; two clients therefore cannot see, or name, each other's bindings.
type workers struct {
	worker *theWorker
	server *httptest.Server
}

func serveWorkers(t *testing.T) *workers {
	t.Helper()
	worker := newWorker()
	handler, err := workerbinding.NewHandler(worker, runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			carrier, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				return err
			}
			worker.attach(peer, newScope(carrier))
			return nil
		}},
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &workers{worker: worker, server: server}
}

func (w *workers) url() string { return "ws" + strings.TrimPrefix(w.server.URL, "http") }

// caller is a client of the worker family, with its own tunnel and its own
// scope over one real socket.
type caller struct {
	client  *workerclient.Client
	carrier *tunnel.Tunnel
	scope   *scope
}

func dialWorkers(t *testing.T, ctx context.Context, w *workers) *caller {
	t.Helper()
	var carrier *tunnel.Tunnel
	client, err := workerclient.Dial(ctx, w.url(), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
			carrier, err = tunnel.New(peer, tunnel.Options{})
			return err
		}},
	}, nil, workerclient.Events{})
	if err != nil {
		t.Fatal(err)
	}
	called := &caller{client: client, carrier: carrier, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = client.Close() })
	return called
}

// link gives one worker a connection to another, which is what it forwards
// into. The forwarding worker is an ordinary client of the other, with a
// tunnel and a scope of its own over that second socket.
func link(t *testing.T, ctx context.Context, from *workers, to *workers) {
	t.Helper()
	var carrier *tunnel.Tunnel
	client, err := workerclient.Dial(ctx, to.url(), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
			carrier, err = tunnel.New(peer, tunnel.Options{})
			return err
		}},
	}, nil, workerclient.Events{})
	if err != nil {
		t.Fatal(err)
	}
	from.worker.origin, from.worker.originScope = client, newScope(carrier)
	t.Cleanup(func() { _ = client.Close() })
}

// cells and topics are the two further compositions, each behind a socket of
// its own and each with a scope per connection for the same reason the worker
// has one.
type cells struct {
	cell   *theCell
	server *httptest.Server
}

func serveCells(t *testing.T) *cells {
	t.Helper()
	cell := newCell()
	handler, err := cellbinding.NewHandler(cell, runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			carrier, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				return err
			}
			cell.attach(peer, newScope(carrier))
			return nil
		}},
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &cells{cell: cell, server: server}
}

type cellCaller struct {
	client *cellclient.Client
	scope  *scope
}

func dialCells(t *testing.T, ctx context.Context, c *cells) *cellCaller {
	t.Helper()
	var carrier *tunnel.Tunnel
	client, err := cellclient.Dial(ctx, "ws"+strings.TrimPrefix(c.server.URL, "http"), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
			carrier, err = tunnel.New(peer, tunnel.Options{})
			return err
		}},
	}, nil, cellclient.Events{})
	if err != nil {
		t.Fatal(err)
	}
	called := &cellCaller{client: client, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = client.Close() })
	return called
}

type topics struct {
	topic  *theTopic
	server *httptest.Server
}

func serveTopics(t *testing.T) *topics {
	t.Helper()
	topic := newTopic()
	handler, err := topicbinding.NewHandler(topic, runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			carrier, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				return err
			}
			topic.attach(peer, newScope(carrier))
			return nil
		}},
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &topics{topic: topic, server: server}
}

type topicCaller struct {
	client *topicclient.Client
	scope  *scope
}

func dialTopics(t *testing.T, ctx context.Context, tp *topics) *topicCaller {
	t.Helper()
	var carrier *tunnel.Tunnel
	client, err := topicclient.Dial(ctx, "ws"+strings.TrimPrefix(tp.server.URL, "http"), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
			carrier, err = tunnel.New(peer, tunnel.Options{})
			return err
		}},
	}, nil, topicclient.Events{})
	if err != nil {
		t.Fatal(err)
	}
	called := &topicCaller{client: client, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = client.Close() })
	return called
}

// testContext bounds every case: a fixture that waits forever is a gate
// nobody passes.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// recorder is a sink an application supplies: it keeps what it was told, in
// the order it was told, and answers each report with the count it has taken
// — which is the pacing the worker is held to.
type recorder struct {
	mu       sync.Mutex
	taken    int64
	items    []int64
	texts    []string
	ended    string
	endings  int
	blockFor chan struct{}
	gate     chan struct{}
	delay    time.Duration
}

// slowBy makes the sink take its time over every report, which is how a case
// that needs the job to still be running when something reaches it says so
// without racing the wire.
func (r *recorder) slowBy(d time.Duration) {
	r.mu.Lock()
	r.delay = d
	r.mu.Unlock()
}

func newRecorder() *recorder { return &recorder{} }

// hold makes the sink stop answering until released, so that a test can watch
// the worker stall on it.
func (r *recorder) hold() {
	r.mu.Lock()
	r.blockFor = make(chan struct{})
	r.gate = make(chan struct{}, 1)
	r.mu.Unlock()
}

func (r *recorder) waitForReport(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	gate := r.gate
	r.mu.Unlock()
	select {
	case <-gate:
	case <-time.After(settle):
		t.Fatal("no report reached the held sink")
	}
}

func (r *recorder) resume() {
	r.mu.Lock()
	blocked := r.blockFor
	r.blockFor = nil
	r.mu.Unlock()
	if blocked != nil {
		close(blocked)
	}
}

func (r *recorder) snapshot() (taken int64, items []int64, ended string, endings int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.taken, append([]int64(nil), r.items...), r.ended, r.endings
}

// eventually polls a condition until it holds or the case's patience runs
// out; the wire is asynchronous and a test that reads once is a test that
// fails when it is slow.
func eventually(t *testing.T, why string, holds func() bool) {
	t.Helper()
	deadline := time.Now().Add(settle)
	for time.Now().Before(deadline) {
		if holds() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never became true: %s", why)
}
