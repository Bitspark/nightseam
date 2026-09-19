package otel_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/otel/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/session/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// payload is what the probe family's echo takes and answers with.
type payload struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// governed is the probe family as a session governs by it: echo decides,
// reverse asks. Those are the two rules session/go/sessiontest reads from the
// corpus for the same scenario; they are spelled here because this module
// keeps its dependencies to the runtime and to OpenTelemetry, and the tool's
// loader is neither.
func governed() session.Governance {
	return session.Governance{
		Decides: func(method string) bool { return method == "echo" },
		Asks:    func(method string) bool { return method == "reverse" },
	}
}

// relayed is the transport a session stands on: two tunnels over one pipe,
// with a registry on the near side. The near end of a channel is what the
// registry binds or attaches; the far end is what a machine or a consumer
// speaks over, and the peers over those far ends are what a test observes.
// The two peers carrying the tunnels take observer, which is nil where a test
// wants the traffic of the session's own peers and nothing under it.
type relayed struct {
	t         *testing.T
	ctx       context.Context
	registry  *session.Registry
	opening   *tunnel.Tunnel
	accepting *tunnel.Tunnel
	carriers  []*runtime.Peer
	peers     []*runtime.Peer
}

func relay(t *testing.T, observer runtime.Observer) *relayed {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, b := duplex.Pipe(1 << 20)
	near, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	far, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	opening, err := tunnel.New(near, tunnel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	accepting, err := tunnel.New(far, tunnel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := session.New(session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := &relayed{t: t, ctx: ctx, registry: registry,
		opening: opening, accepting: accepting, carriers: []*runtime.Peer{near, far}}
	t.Cleanup(func() { near.Close(); far.Close() })
	return r
}

// channels is one connected pair of channels of the probe family.
func (r *relayed) channels() (near, far *tunnel.Channel) {
	r.t.Helper()
	accepted := make(chan *tunnel.Channel, 1)
	go func() {
		channel, err := r.accepting.Accept(r.ctx)
		if err != nil {
			r.t.Error(err)
		}
		accepted <- channel
	}()
	opened, err := r.opening.Open(r.ctx, "probe")
	if err != nil {
		r.t.Fatal(err)
	}
	far = <-accepted
	if far == nil {
		r.t.Fatal("nobody accepted the channel")
	}
	return opened, far
}

// speaker is a peer over one end of a channel — the machine's or a
// consumer's — with this adapter on both hooks.
func (r *relayed) speaker(over *tunnel.Channel, role runtime.Role, tracer trace.Tracer, options runtime.Options) *runtime.Peer {
	r.t.Helper()
	options.Observer, options.Propagator = otel.Observer(tracer), otel.Propagator(nil)
	peer, err := runtime.NewPeer(r.ctx, over, role, options)
	if err != nil {
		r.t.Fatal(err)
	}
	r.peers = append(r.peers, peer)
	return peer
}

// quiet closes every peer a test spoke over, and then the two carrying the
// tunnels under them, and waits for each: a connection span ends where its
// peer does, and what it encloses is exported with it.
func (r *relayed) quiet() {
	for _, peers := range [][]*runtime.Peer{r.peers, r.carriers} {
		for _, peer := range peers {
			peer.Close()
		}
		for _, peer := range peers {
			<-peer.Done()
		}
	}
}

// One call from a consumer, through a session relay, to a machine handler
// whose ask the holder of control answers, is one trace: the consumer's own
// work encloses the call it made, the machine's handler runs under a server
// span of that same trace, and what the handler asks — and the answer of the
// consumer holding control — are under the handler and not beside it. A
// client span and the server span of one request are siblings: the runtime
// injects the trace before it says the request started, so the frame carries
// the span the caller's context held and both are that span's children. What
// nests is the work.
func TestOneCallThroughARelayIsOneSpanTree(t *testing.T) {
	tracer, exporter := exported(t)
	r := relay(t, nil)
	machineNear, machineFar := r.channels()
	if err := r.registry.Bind("s", machineNear, governed(), session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	r.speaker(machineFar, runtime.ServerRole, tracer, runtime.Options{
		Families: map[string]string{"echo": "probe", "reverse": "probe"},
		Handlers: map[string]runtime.Handler{
			// The agent raises its ask from the context of the request that
			// ran it, which is what puts the ask under the handler.
			"echo": func(ctx context.Context, peer *runtime.Peer, params json.RawMessage) (any, error) {
				var reversed payload
				if err := peer.Call(ctx, "reverse", payload{Text: "gate", Count: 1}, &reversed); err != nil {
					return nil, err
				}
				return reversed, nil
			},
		},
	})
	consumerNear, consumerFar := r.channels()
	attachment, err := r.registry.Attach("s", consumerNear, session.Participant, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.registry.Control("s", attachment); err != nil {
		t.Fatal(err)
	}
	// The session's vocabulary is delivered to the consumer on its event
	// loop, which is not the goroutine the call's answer returns on: the
	// control is sent on attach and the cursor after the answer it stands
	// for, and neither delivery — which is where its span is written — is
	// ordered before Call returns. A test that read the exporter then read a
	// snapshot, and failed on a loaded runner where the loop had not run
	// yet. So the consumer handles both, and the test waits for its own
	// handlers, which run after the delivery is observed.
	delivered := make(chan string, 8)
	consumer := r.speaker(consumerFar, runtime.ClientRole, tracer, runtime.Options{
		Families: map[string]string{"echo": "probe", "reverse": "probe"},
		Handlers: map[string]runtime.Handler{
			"reverse": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
				return payload{Text: "etag", Count: 1}, nil
			},
		},
		Events: map[string]runtime.EventHandler{
			session.ControlEvent: func(context.Context, *runtime.Peer, json.RawMessage) { delivered <- session.ControlEvent },
			session.CursorEvent:  func(context.Context, *runtime.Peer, json.RawMessage) { delivered <- session.CursorEvent },
		},
	})
	work, root := tracer.Start(context.Background(), "the consumer's own work")
	var answered payload
	if err := consumer.Call(work, "echo", payload{Text: "gate", Count: 1}, &answered); err != nil {
		t.Fatal(err)
	}
	root.End()
	if answered.Text != "etag" {
		t.Fatalf("the machine answered %+v", answered)
	}
	for seen := map[string]bool{}; !seen[session.ControlEvent] || !seen[session.CursorEvent]; {
		select {
		case name := <-delivered:
			seen[name] = true
		case <-t.Context().Done():
			t.Fatalf("the session's vocabulary was not delivered to the consumer: %v", seen)
		}
	}
	// The connection spans of the two peers end when the peers do; the five
	// spans the tree is made of, and the two of the vocabulary, have ended
	// already.
	tree := map[string]string{}
	traces := map[trace.TraceID]bool{}
	spans := exporter.GetSpans()
	byID := map[trace.SpanID]tracetest.SpanStub{}
	for _, span := range spans {
		byID[span.SpanContext.SpanID()] = span
	}
	for _, span := range spans {
		if span.Name == "connection" {
			continue
		}
		parent := "nothing"
		if held, ok := byID[span.Parent.SpanID()]; ok {
			parent = described(held)
		}
		tree[described(span)] = parent
		if strings.HasPrefix(span.Name, session.Prefix) {
			// The session's own vocabulary is the relay's, written of itself
			// and under no call: it carries no trace context, so each of
			// these is a span under nothing and in a trace of its own, which
			// is no part of the call's.
			continue
		}
		traces[span.SpanContext.TraceID()] = true
	}
	want := map[string]string{
		"the consumer's own work (internal)": "nothing",
		"echo (client)":                      "the consumer's own work (internal)",
		"echo (server)":                      "the consumer's own work (internal)",
		"reverse (client)":                   "echo (server)",
		"reverse (server)":                   "echo (server)",
		"session.control (consumer)":         "nothing",
		"session.cursor (consumer)":          "nothing",
	}
	for span, parent := range want {
		if tree[span] != parent {
			t.Errorf("%s is under %q and not under %q", span, tree[span], parent)
		}
	}
	if len(tree) != len(want) {
		t.Errorf("the call left %v", tree)
	}
	if len(traces) != 1 {
		t.Errorf("one call is %d traces", len(traces))
	}
}

func described(span tracetest.SpanStub) string {
	return span.Name + " (" + span.SpanKind.String() + ")"
}

// The rule of the whole surface, on the spans this time: a sentinel
// travelling in a call's params, in what answered it, in a public error's data
// and in an event's data appears in no span name, no attribute, no span event
// and no status of either peer — nor of the peers carrying the tunnel the
// session runs over, which is where the tunnel's events and the session's ten
// reach an observer.
func TestNoPayloadReachesASpan(t *testing.T) {
	tracer, exporter := exported(t)
	r := relay(t, otel.Observer(tracer))
	machineNear, machineFar := r.channels()
	if err := r.registry.Bind("s", machineNear, governed(), session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	secret := map[string]string{"secret": sentinel}
	delivered := make(chan struct{})
	machine := r.speaker(machineFar, runtime.ServerRole, tracer, runtime.Options{
		Handlers: map[string]runtime.Handler{
			"echo": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return secret, nil },
			"deny": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
				return nil, &runtime.PublicError{Code: "denied", Message: "Denied.",
					Data: json.RawMessage(`{"secret":"` + sentinel + `"}`)}
			},
			"boom": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { panic("the handler gave up") },
		},
	})
	consumerNear, consumerFar := r.channels()
	attachment, err := r.registry.Attach("s", consumerNear, session.Participant, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.registry.Control("s", attachment); err != nil {
		t.Fatal(err)
	}
	consumer := r.speaker(consumerFar, runtime.ClientRole, tracer, runtime.Options{
		Events: map[string]runtime.EventHandler{
			"changed": func(context.Context, *runtime.Peer, json.RawMessage) { close(delivered) },
		},
	})
	var result map[string]string
	if err := consumer.Call(context.Background(), "echo", secret, &result); err != nil || result["secret"] != sentinel {
		t.Fatalf("echo = %v, error=%v", result, err)
	}
	var denied *runtime.PublicError
	if err := consumer.Call(context.Background(), "deny", secret, nil); !errors.As(err, &denied) ||
		!strings.Contains(string(denied.Data), sentinel) {
		t.Fatalf("deny = %v", err)
	}
	if err := consumer.Call(context.Background(), "boom", secret, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := machine.Emit(context.Background(), "changed", secret); err != nil {
		t.Fatal(err)
	}
	<-delivered
	r.quiet()
	// The sentinel travelled every path there is, so what follows is about
	// what the spans were spared and not about an idle connection.
	spans := exporter.GetSpans()
	said := map[string]bool{}
	for _, span := range spans {
		said[span.Name] = true
		for _, event := range span.Events {
			said[event.Name] = true
		}
	}
	for _, want := range []string{"echo", "deny", "boom", "changed", "handler panic", "session.FrameAppended", "tunnel.ChannelOpened"} {
		if !said[want] {
			t.Fatalf("no span or span event of %q; the adapter left %v", want, said)
		}
	}
	for _, span := range spans {
		for _, said := range spelledOf(span) {
			if strings.Contains(said, sentinel) {
				t.Fatalf("a span said a payload: %s", said)
			}
		}
	}
}

// spelledOf is everything one span says in words: its name, its status, every
// attribute of it, and every span event on it with its own attributes.
func spelledOf(span tracetest.SpanStub) []string {
	said := []string{span.Name, span.Status.Description}
	for _, attribute := range span.Attributes {
		said = append(said, string(attribute.Key), attribute.Value.Emit())
	}
	for _, event := range span.Events {
		said = append(said, event.Name)
		for _, attribute := range event.Attributes {
			said = append(said, string(attribute.Key), attribute.Value.Emit())
		}
	}
	return said
}
