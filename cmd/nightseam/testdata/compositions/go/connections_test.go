package composition_test

// Each connection assembles its model before the peer reads. Its tunnel and
// consumer scope belong to that connection; generated adapters receive a Wire.
import (
	"context"
	cellbinding "example.test/generated/api/go/cell-binding"
	cellprotocol "example.test/generated/api/go/cell-protocol"
	topicbinding "example.test/generated/api/go/topic-binding"
	topicprotocol "example.test/generated/api/go/topic-protocol"
	workerbinding "example.test/generated/api/go/worker-binding"
	workerprotocol "example.test/generated/api/go/worker-protocol"
	duplex "github.com/Bitspark/nightseam/duplex/go"
	runtime "github.com/Bitspark/nightseam/runtime/go"
	tunnel "github.com/Bitspark/nightseam/tunnel/go"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const settle = 3 * time.Second

func serveModel(t *testing.T, build func(*runtime.Peer, *scope) (duplex.Wire, error)) *httptest.Server {
	t.Helper()
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			carrier, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				return err
			}
			model, err := build(peer, newScope(carrier))
			if err != nil {
				return err
			}
			if _, err = runtime.ForwardWire(peer.Wire(), model); err != nil {
				_ = model.Close(duplex.CodeInternalError, "setup failed")
				return err
			}
			go func() { <-peer.Done(); _ = model.Close(duplex.CodeNormal, "connection ended") }()
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
	return server
}

// The consumer retains transport lifetime separately from generated methods.
type workerAccess struct {
	workerprotocol.ServerMethods
	peer *runtime.Peer
}

func (c *workerAccess) Close() error { return c.peer.Close() }

type cellAccess struct {
	cellprotocol.ServerMethods
	peer *runtime.Peer
}

func (c *cellAccess) Close() error { return c.peer.Close() }

type topicAccess struct {
	topicprotocol.ServerMethods
	peer *runtime.Peer
}

func (c *topicAccess) Close() error { return c.peer.Close() }

type workers struct {
	worker *theWorker
	server *httptest.Server
}

func serveWorkers(t *testing.T) *workers {
	worker := newWorker()
	server := serveModel(t, func(peer *runtime.Peer, s *scope) (duplex.Wire, error) {
		worker.attach(peer, s)
		return workerbinding.ToWire(func(workerprotocol.Client) (workerprotocol.Server, error) {
			return workerprotocol.Server{Methods: &workerSession{worker, s}, Events: struct{}{}}, nil
		}, runtime.AdapterContext{})
	})
	return &workers{worker: worker, server: server}
}
func (w *workers) url() string { return "ws" + strings.TrimPrefix(w.server.URL, "http") }

type caller struct {
	client  *workerAccess
	carrier *tunnel.Tunnel
	scope   *scope
}

func dialWorkers(t *testing.T, ctx context.Context, w *workers) *caller {
	t.Helper()
	var carrier *tunnel.Tunnel
	var access workerprotocol.Server
	peer, _, err := runtime.Dial(ctx, w.url(), runtime.DialOptions{Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
		carrier, err = tunnel.New(peer, tunnel.Options{})
		if err != nil {
			return err
		}
		factory, err := workerbinding.FromWire(ctx, peer.Wire(), runtime.AdapterContext{})
		if err != nil {
			return err
		}
		access, err = factory(workerprotocol.Client{Methods: struct{}{}, Events: struct{}{}})
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	called := &caller{client: &workerAccess{access.Methods, peer}, carrier: carrier, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = peer.Close() })
	return called
}
func link(t *testing.T, ctx context.Context, from, to *workers) {
	called := dialWorkers(t, ctx, to)
	from.worker.origin, from.worker.originScope = called.client.ServerMethods, called.scope
}

type cells struct {
	cell   *theCell
	server *httptest.Server
}

func serveCells(t *testing.T) *cells {
	cell := newCell()
	server := serveModel(t, func(peer *runtime.Peer, s *scope) (duplex.Wire, error) {
		cell.attach(peer, s)
		return cellbinding.ToWire(func(cellprotocol.Client) (cellprotocol.Server, error) {
			return cellprotocol.Server{Methods: &cellSession{cell, s}, Events: struct{}{}}, nil
		}, runtime.AdapterContext{})
	})
	return &cells{cell: cell, server: server}
}

type cellCaller struct {
	client *cellAccess
	scope  *scope
}

func dialCells(t *testing.T, ctx context.Context, c *cells) *cellCaller {
	t.Helper()
	var carrier *tunnel.Tunnel
	var access cellprotocol.Server
	peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(c.server.URL, "http"), runtime.DialOptions{Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
		carrier, err = tunnel.New(peer, tunnel.Options{})
		if err != nil {
			return err
		}
		factory, err := cellbinding.FromWire(ctx, peer.Wire(), runtime.AdapterContext{})
		if err != nil {
			return err
		}
		access, err = factory(cellprotocol.Client{Methods: struct{}{}, Events: struct{}{}})
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	called := &cellCaller{client: &cellAccess{access.Methods, peer}, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = peer.Close() })
	return called
}

type topics struct {
	topic  *theTopic
	server *httptest.Server
}

func serveTopics(t *testing.T) *topics {
	topic := newTopic()
	server := serveModel(t, func(peer *runtime.Peer, s *scope) (duplex.Wire, error) {
		topic.attach(peer, s)
		return topicbinding.ToWire(func(topicprotocol.Client) (topicprotocol.Server, error) {
			return topicprotocol.Server{Methods: &topicSession{topic, s}, Events: struct{}{}}, nil
		}, runtime.AdapterContext{})
	})
	return &topics{topic: topic, server: server}
}

type topicCaller struct {
	client *topicAccess
	scope  *scope
}

func dialTopics(t *testing.T, ctx context.Context, tp *topics) *topicCaller {
	t.Helper()
	var carrier *tunnel.Tunnel
	var access topicprotocol.Server
	peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(tp.server.URL, "http"), runtime.DialOptions{Options: runtime.Options{Prepare: func(peer *runtime.Peer) (err error) {
		carrier, err = tunnel.New(peer, tunnel.Options{})
		if err != nil {
			return err
		}
		factory, err := topicbinding.FromWire(ctx, peer.Wire(), runtime.AdapterContext{})
		if err != nil {
			return err
		}
		access, err = factory(topicprotocol.Client{Methods: struct{}{}, Events: struct{}{}})
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	called := &topicCaller{client: &topicAccess{access.Methods, peer}, scope: newScope(carrier)}
	t.Cleanup(func() { called.scope.close(context.Background()); _ = peer.Close() })
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
