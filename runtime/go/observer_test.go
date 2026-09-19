package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/Bitspark/nightseam/runtime/go"
	"github.com/coder/websocket"
)

// sentinel stands for every payload a peer carries. It travels in the params
// of a call, in what answers it, in the data of a public error and in the data
// of an event, and it must appear in nothing an observer is ever told.
const sentinel = "payload-sentinel-4bf92f35"

// recorder keeps what it was told, which is what a test needs and what no
// adapter should do. Observe is called from whichever goroutine the event
// happened on, so it holds a lock of its own.
type recorder struct {
	mu       sync.Mutex
	observed []ws.ObserverEvent
}

func (r *recorder) Observe(event ws.ObserverEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observed = append(r.observed, event)
}

func (r *recorder) all() []ws.ObserverEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ws.ObserverEvent(nil), r.observed...)
}

// lines renders what was observed so that a sequence reads as one, leaving out
// what no test can pin: the clock, a duration, a frame's size.
func (r *recorder) lines() []string {
	observed := r.all()
	lines := make([]string, 0, len(observed))
	for _, event := range observed {
		lines = append(lines, line(event))
	}
	return lines
}

// await waits until at least count events were observed, so that a test never
// reads a sequence the peer is still writing.
func (r *recorder) await(t *testing.T, count int) []ws.ObserverEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if observed := r.all(); len(observed) >= count {
			return observed
		}
		if time.Now().After(deadline) {
			t.Fatalf("observed %d events, want %d:\n%s", len(r.all()), count, strings.Join(r.lines(), "\n"))
		}
		time.Sleep(time.Millisecond)
	}
}

// line is the type switch a consumer dispatches by, which is also how this
// suite reads a sequence: every event of the surface has a case here.
func line(event ws.ObserverEvent) string {
	switch e := event.(type) {
	case ws.ConnectionOpened:
		return "opened " + string(e.Role)
	case ws.ConnectionClosed:
		return fmt.Sprintf("closed %d local=%t", e.Code, e.Local)
	case ws.FrameSent:
		return fmt.Sprintf("sent %s %q id=%s family=%q", e.Kind, e.Name, e.ID, e.Family)
	case ws.FrameReceived:
		return fmt.Sprintf("received %s %q id=%s family=%q", e.Kind, e.Name, e.ID, e.Family)
	case ws.RequestStarted:
		return fmt.Sprintf("started %s %s incoming=%t family=%q", e.ID, e.Method, e.Incoming, e.Family)
	case ws.RequestEnded:
		return fmt.Sprintf("ended %s %s incoming=%t outcome=%s code=%q family=%q", e.ID, e.Method, e.Incoming, e.Outcome, e.ErrorCode, e.Family)
	case ws.EventEmitted:
		return fmt.Sprintf("emitted %q family=%q", e.Name, e.Family)
	case ws.EventDelivered:
		return fmt.Sprintf("delivered %q family=%q", e.Name, e.Family)
	case ws.Backpressure:
		return fmt.Sprintf("backpressure queued=%d stalled=%t", e.Queued, e.Stalled)
	case ws.HandlerPanic:
		return fmt.Sprintf("panic %s %q family=%q", e.Method, e.Value, e.Family)
	}
	return fmt.Sprintf("unknown %T", event)
}

// holdsNoPayload is the rule of the whole surface: every event an observer saw,
// marshalled and rendered, names nothing the connection carried.
func holdsNoPayload(t *testing.T, side string, observed []ws.ObserverEvent) {
	t.Helper()
	for _, event := range observed {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("%s: marshal %T: %v", side, event, err)
		}
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("%s: %T carried a payload: %s", side, event, encoded)
		}
		if rendered := fmt.Sprintf("%+v", event); strings.Contains(rendered, sentinel) {
			t.Fatalf("%s: %T carried a payload beyond its marshalled members: %s", side, event, rendered)
		}
	}
}

// found is the first event of one type an observer saw, and whether it saw one.
func found[T ws.ObserverEvent](observed []ws.ObserverEvent) (T, bool) {
	for _, event := range observed {
		if match, ok := event.(T); ok {
			return match, true
		}
	}
	var zero T
	return zero, false
}

// A params object must not reach an observer by any path: not through a call,
// not through what answers it, not through an error's data, not through an
// event's, not through the value a handler panicked with, and not through the
// meta a frame carries, which is a consumer's carriage and may hold a
// credential.
func TestAnObserverIsToldNoPayload(t *testing.T) {
	payload := map[string]string{"secret": sentinel}
	delivered := make(chan struct{})
	client, server := new(recorder), new(recorder)
	peer, remote := newPair(t, ws.Options{
		Observer: server,
		Handlers: map[string]ws.Handler{
			"echo": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return payload, nil },
			"deny": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
				return nil, &ws.PublicError{Code: "denied", Message: "Denied.", Data: json.RawMessage(`{"secret":"` + sentinel + `"}`)}
			},
			"boom": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { panic("the handler gave up") },
		},
		Events: map[string]ws.EventHandler{
			"tick": func(context.Context, *ws.Peer, json.RawMessage) { close(delivered) },
		},
	}, ws.Options{Observer: client})
	carrying := ws.WithMeta(context.Background(), ws.Meta{"secret": sentinel})
	var result map[string]string
	if err := peer.Call(carrying, "echo", payload, &result); err != nil || result["secret"] != sentinel {
		t.Fatalf("echo = %v, error=%v", result, err)
	}
	var denied *ws.PublicError
	if err := peer.Call(carrying, "deny", payload, nil); !errors.As(err, &denied) || !strings.Contains(string(denied.Data), sentinel) {
		t.Fatalf("deny = %v", err)
	}
	if err := peer.Call(carrying, "boom", payload, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := peer.Emit(carrying, "tick", payload); err != nil {
		t.Fatal(err)
	}
	receive(t, delivered)
	_ = peer.Close()
	receive(t, peer.Done())
	receive(t, remote.Done())
	// The sentinel travelled every path there is, so what follows is about what
	// the observers were spared and not about an idle connection.
	if _, ok := found[ws.HandlerPanic](server.all()); !ok {
		t.Fatalf("the server observed no handler panic:\n%s", strings.Join(server.lines(), "\n"))
	}
	if _, ok := found[ws.EventDelivered](server.all()); !ok {
		t.Fatalf("the server observed no delivered event:\n%s", strings.Join(server.lines(), "\n"))
	}
	refused := false
	for _, event := range client.all() {
		if ended, ok := event.(ws.RequestEnded); ok && ended.ErrorCode == "denied" {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("the client observed no refused request:\n%s", strings.Join(client.lines(), "\n"))
	}
	holdsNoPayload(t, "client", client.all())
	holdsNoPayload(t, "server", server.all())
}

// One call each way, one event each way and one close: each side sees its own
// events in order, with the family the caller labelled the name with and none
// where it labelled nothing.
func TestAnObserverSeesACallAnEventAndACloseInOrder(t *testing.T) {
	ticked, tocked := make(chan struct{}), make(chan struct{})
	client, server := new(recorder), new(recorder)
	labels := map[string]string{"echo": "probe", "tick": "probe"}
	peer, remote := newPair(t, ws.Options{
		Observer: server,
		Families: labels,
		Handlers: map[string]ws.Handler{
			"echo": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return "answered", nil },
		},
		Events: map[string]ws.EventHandler{
			"tick": func(context.Context, *ws.Peer, json.RawMessage) { close(ticked) },
		},
	}, ws.Options{
		Observer: client,
		Families: labels,
		Handlers: map[string]ws.Handler{
			"back": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return "returned", nil },
		},
		Events: map[string]ws.EventHandler{
			"tock": func(context.Context, *ws.Peer, json.RawMessage) { close(tocked) },
		},
	})
	// Every request below begins after this, so no duration measured from where
	// one began may exceed what has elapsed since.
	before := time.Now()
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
	if err := remote.Emit(context.Background(), "tock", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, tocked)
	_ = peer.Close()
	receive(t, peer.Done())
	receive(t, remote.Done())
	want := []string{
		`opened client`,
		`started c:1 echo incoming=false family="probe"`,
		`sent request "echo" id=c:1 family="probe"`,
		`received response "" id=c:1 family=""`,
		`ended c:1 echo incoming=false outcome=ok code="" family="probe"`,
		`received request "back" id=s:1 family=""`,
		`started s:1 back incoming=true family=""`,
		`ended s:1 back incoming=true outcome=ok code="" family=""`,
		`sent response "" id=s:1 family=""`,
		`emitted "tick" family="probe"`,
		`sent event "tick" id= family="probe"`,
		`received event "tock" id= family=""`,
		`delivered "tock" family=""`,
		`closed 1000 local=true`,
	}
	client.await(t, len(want))
	if got := strings.Join(client.lines(), "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("the client observed\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	// The close the other side sees is the transport's to describe; every event
	// before it is the same exchange read from that end.
	want = []string{
		`opened server`,
		`received request "echo" id=c:1 family="probe"`,
		`started c:1 echo incoming=true family="probe"`,
		`ended c:1 echo incoming=true outcome=ok code="" family="probe"`,
		`sent response "" id=c:1 family=""`,
		`started s:1 back incoming=false family=""`,
		`sent request "back" id=s:1 family=""`,
		`received response "" id=s:1 family=""`,
		`ended s:1 back incoming=false outcome=ok code="" family=""`,
		`received event "tick" id= family="probe"`,
		`delivered "tick" family="probe"`,
		`emitted "tock" family=""`,
		`sent event "tock" id= family=""`,
	}
	server.await(t, len(want)+1)
	got := server.lines()
	if strings.Join(got[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Fatalf("the server observed\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if last := got[len(got)-1]; !strings.HasPrefix(last, "closed ") {
		t.Fatalf("the server's last event = %q", last)
	}
	// The fields no rendering above pins: a clock on every event, a size on
	// every frame, a duration on a request that crossed the connection, and the
	// trace the frame carried on the events that concern one.
	for _, event := range client.all() {
		switch e := event.(type) {
		case ws.ConnectionOpened:
			if e.At.IsZero() || e.Role != ws.ClientRole {
				t.Fatalf("connection opened = %+v", e)
			}
		case ws.FrameSent:
			if e.At.IsZero() || e.Bytes <= 0 {
				t.Fatalf("frame sent = %+v", e)
			}
			if e.Kind == "request" && e.Trace.Parent == "" {
				t.Fatalf("a sent request carried a trace the observer was not told: %+v", e)
			}
		case ws.FrameReceived:
			if e.At.IsZero() || e.Bytes <= 0 || e.Trace.Parent == "" {
				t.Fatalf("frame received = %+v", e)
			}
		case ws.RequestEnded:
			if e.At.IsZero() || e.Outcome != ws.OutcomeOK || e.Trace.Parent == "" {
				t.Fatalf("request ended = %+v", e)
			}
			// A clock coarser than a loopback exchange reads no time at all for
			// one, so what a duration is held to is that it was measured from
			// where the request began and not from some other zero.
			if e.Duration < 0 || e.Duration > time.Since(before) {
				t.Fatalf("a request's duration was not measured from its start: %+v", e)
			}
		}
	}
}

// The four outcomes a request ends with, each read from the side that knows
// it: the caller's own clock and context, the peer's refusal of a method it
// does not have.
func TestAnObserverSeesEveryOutcomeOfARequest(t *testing.T) {
	waiting := make(chan struct{}, 2)
	client, server := new(recorder), new(recorder)
	peer, remote := newPair(t, ws.Options{
		Observer: server,
		Handlers: map[string]ws.Handler{
			"ok": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return true, nil },
			"deny": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
				return nil, &ws.PublicError{Code: "denied", Message: "Denied."}
			},
			"wait": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
				waiting <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}, ws.Options{Observer: client, RequestTimeout: 200 * time.Millisecond})
	if err := peer.Call(context.Background(), "ok", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := peer.Call(context.Background(), "deny", nil, nil); err == nil {
		t.Fatal("deny answered without an error")
	}
	if err := peer.Call(context.Background(), "missing", nil, nil); err == nil {
		t.Fatal("an unknown method answered without an error")
	}
	cancellable, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() { returned <- peer.Call(cancellable, "wait", nil, nil) }()
	receive(t, waiting)
	cancel()
	if err := receive(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call = %v", err)
	}
	// The caller's own RequestTimeout ends the next one with no cancellation of
	// anyone's making: a deadline is an outcome of its own.
	if err := peer.Call(context.Background(), "wait", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out call = %v", err)
	}
	ended := map[string]string{}
	for _, event := range client.await(t, 20) {
		if e, ok := event.(ws.RequestEnded); ok {
			ended[e.ID] = fmt.Sprintf("%s outcome=%s code=%q", e.Method, e.Outcome, e.ErrorCode)
		}
	}
	for id, want := range map[string]string{
		"c:1": `ok outcome=ok code=""`,
		"c:2": `deny outcome=error code="denied"`,
		"c:3": `missing outcome=error code="method_not_found"`,
		"c:4": `wait outcome=cancelled code=""`,
		"c:5": `wait outcome=timeout code=""`,
	} {
		if ended[id] != want {
			t.Fatalf("the caller saw %s end as %q, want %q", id, ended[id], want)
		}
	}
	// The peer that served them refuses the unknown method itself, and the
	// request it never started ends as the one it did.
	incoming := map[string]string{}
	for _, event := range server.all() {
		if e, ok := event.(ws.RequestEnded); ok && e.Incoming {
			incoming[e.ID] = fmt.Sprintf("%s outcome=%s code=%q", e.Method, e.Outcome, e.ErrorCode)
		}
	}
	for id, want := range map[string]string{
		"c:1": `ok outcome=ok code=""`,
		"c:3": `missing outcome=error code="method_not_found"`,
	} {
		if incoming[id] != want {
			t.Fatalf("the peer saw %s end as %q, want %q", id, incoming[id], want)
		}
	}
	_ = peer.Close()
	receive(t, remote.Done())
}

// The value the handler gave up with, as %v renders it, and never what it was
// given: a panic reaches an observer as a fact about the handler alone.
func TestAnObserverSeesAHandlerPanicWithTheValueAndNotTheParams(t *testing.T) {
	observed := new(recorder)
	peer, _ := newPair(t, ws.Options{
		Observer: observed,
		Families: map[string]string{"boom": "probe"},
		Handlers: map[string]ws.Handler{
			"boom": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
				panic(errors.New("the handler gave up"))
			},
		},
	}, ws.Options{})
	if err := peer.Call(context.Background(), "boom", map[string]string{"secret": sentinel}, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	panicked, ok := found[ws.HandlerPanic](observed.await(t, 6))
	if !ok {
		t.Fatalf("no handler panic was observed:\n%s", strings.Join(observed.lines(), "\n"))
	}
	if panicked.Method != "boom" || panicked.Value != "the handler gave up" || panicked.Family != "probe" || panicked.At.IsZero() {
		t.Fatalf("handler panic = %+v", panicked)
	}
	holdsNoPayload(t, "server", observed.all())
}

// The stall the peer already disconnects on is the one an observer is told
// about, with the depth of the queue that could not take the frame.
func TestAnObserverSeesBackpressureWhenTheEventConsumerStalls(t *testing.T) {
	started := make(chan struct{})
	observed := new(recorder)
	peer, remote := newPair(t, ws.Options{}, ws.Options{
		Observer:      observed,
		QueueCapacity: 1,
		Events: map[string]ws.EventHandler{
			"progress": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) {
				close(started)
				<-ctx.Done()
			},
		},
	})
	if err := remote.Emit(context.Background(), "progress", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	for _, value := range []int{2, 3} {
		if err := remote.Emit(context.Background(), "progress", value); err != nil {
			t.Fatal(err)
		}
	}
	receive(t, peer.Done())
	if !errors.Is(peer.Err(), ws.ErrBackpressure) {
		t.Fatalf("stalled event consumer error = %v", peer.Err())
	}
	stalled := false
	for _, event := range observed.all() {
		if e, ok := event.(ws.Backpressure); ok && e.Stalled {
			if e.Queued != 1 || e.At.IsZero() {
				t.Fatalf("backpressure = %+v", e)
			}
			stalled = true
		}
	}
	if !stalled {
		t.Fatalf("the stall was not observed:\n%s", strings.Join(observed.lines(), "\n"))
	}
	receive(t, remote.Done())
}

// Every member a frame carries reaches the events that concern it verbatim,
// which is what an adapter opens a span from.
func TestAnObserverIsToldTheTraceAFrameCarries(t *testing.T) {
	const parent, state = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "congo=t61rcWkgMzE"
	delivered := make(chan struct{})
	observed := new(recorder)
	peer, conn, ctx := rawPeer(t, ws.Options{
		Observer: observed,
		Families: map[string]string{"progress": "probe"},
		Events: map[string]ws.EventHandler{
			"progress": func(context.Context, *ws.Peer, json.RawMessage) { close(delivered) },
		},
	})
	traced := `{"version":1,"kind":"event","event":"progress","data":1,"traceparent":"` + parent + `","tracestate":"` + state + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(traced)); err != nil {
		t.Fatal(err)
	}
	receive(t, delivered)
	received, ok := found[ws.FrameReceived](observed.await(t, 3))
	if !ok || received.Trace.Parent != parent || received.Trace.State != state || received.Family != "probe" || received.Bytes != len(traced) {
		t.Fatalf("frame received = %+v (found=%t)", received, ok)
	}
	event, ok := found[ws.EventDelivered](observed.all())
	if !ok || event.Trace.Parent != parent || event.Trace.State != state || event.Name != "progress" || event.Family != "probe" {
		t.Fatalf("event delivered = %+v (found=%t)", event, ok)
	}
	if peer.Err() != nil {
		t.Fatalf("an observed connection failed: %v", peer.Err())
	}
}

// A peer given no observer runs every hook point and builds no event at any of
// them: the guard is at each call site, and nothing here may reach a nil one.
func TestAPeerWithNoObserverRunsEveryHookPoint(t *testing.T) {
	delivered := make(chan struct{})
	peer, remote := newPair(t, ws.Options{
		Handlers: map[string]ws.Handler{
			"ok":   func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return true, nil },
			"boom": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { panic("no observer here either") },
		},
		Events: map[string]ws.EventHandler{
			"tick": func(context.Context, *ws.Peer, json.RawMessage) { close(delivered) },
		},
	}, ws.Options{})
	if peer.Observer() != nil || remote.Observer() != nil {
		t.Fatal("a peer given no observer has one")
	}
	var answered bool
	if err := peer.Call(context.Background(), "ok", nil, &answered); err != nil || !answered {
		t.Fatalf("ok = %t, error=%v", answered, err)
	}
	if err := peer.Call(context.Background(), "boom", nil, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := peer.Call(context.Background(), "missing", nil, nil); err == nil {
		t.Fatal("an unknown method answered without an error")
	}
	if err := peer.Emit(context.Background(), "tick", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, delivered)
	_ = peer.Close()
	receive(t, peer.Done())
	receive(t, remote.Done())
}

// panicking is the observer a consumer wrote badly: it gives up on every event
// it is told, on whichever goroutine told it.
type panicking struct{}

func (panicking) Observe(ws.ObserverEvent) { panic("the observer gave up") }

// A panicking observer panics alone. Observe runs on the goroutine the traffic
// did — the reader, a handler, a caller, none of them the consumer's — so the
// peer recovers it where it happened, loses that event and carries on: a call
// answers, a handler that gave up answers with an error rather than twice, an
// event reaches its listener, a layer reaches the same guard through the
// peer's own Observe, and the connection ends when this side says so and not
// when a diagnostic did. It is the twin of the TypeScript suite's "an observer
// that throws interrupts no routing".
func TestAPanickingObserverInterruptsNoRouting(t *testing.T) {
	delivered := make(chan struct{})
	client, remote := newPair(t, ws.Options{
		Observer: panicking{},
		Handlers: map[string]ws.Handler{
			"ping": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return "pong", nil },
			"boom": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { panic("the handler gave up") },
		},
		Events: map[string]ws.EventHandler{
			"tick": func(context.Context, *ws.Peer, json.RawMessage) { close(delivered) },
		},
	}, ws.Options{Observer: panicking{}})

	var answer string
	if err := client.Call(context.Background(), "ping", nil, &answer); err != nil || answer != "pong" {
		t.Fatalf("ping = %q, error=%v", answer, err)
	}
	if err := client.Call(context.Background(), "boom", nil, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := client.Emit(context.Background(), "tick", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, delivered)

	// What a layer emits takes the same path a tunnel and a session take.
	client.Observe(ws.Backpressure{At: time.Now(), Queued: 1})
	remote.Observe(ws.Backpressure{At: time.Now(), Queued: 1})

	if err := client.Call(context.Background(), "ping", nil, &answer); err != nil || answer != "pong" {
		t.Fatalf("a call after a panicking observer = %q, error=%v", answer, err)
	}
	if client.Err() != nil || remote.Err() != nil {
		t.Fatalf("a panicking observer ended a connection: client=%v remote=%v", client.Err(), remote.Err())
	}
	_ = client.Close()
	receive(t, client.Done())
	receive(t, remote.Done())
}
