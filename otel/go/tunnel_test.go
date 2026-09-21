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
	"github.com/Bitspark/nightseam/tunnel/go"
)

// payload is what the probe family's echo takes and answers with.
type payload struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// multiplexed is the transport a call is traced over here: two tunnels on one
// pipe, and peers over the channels they open. The two peers carrying the
// tunnels take observer, which is nil where a test wants the traffic of the
// inner peers and nothing under it.
type multiplexed struct {
	t         *testing.T
	ctx       context.Context
	opening   *tunnel.Tunnel
	accepting *tunnel.Tunnel
	carriers  []*runtime.Peer
	peers     []*runtime.Peer
}

func multiplex(t *testing.T, observer runtime.Observer) *multiplexed {
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
	m := &multiplexed{t: t, ctx: ctx, opening: opening, accepting: accepting,
		carriers: []*runtime.Peer{near, far}}
	t.Cleanup(func() { near.Close(); far.Close() })
	return m
}

// channels is one connected pair of channels of the probe family.
func (m *multiplexed) channels() (near, far *tunnel.Connection) {
	m.t.Helper()
	accepted := make(chan *tunnel.Connection, 1)
	go func() {
		channel, err := m.accepting.AcceptConnection(m.ctx)
		if err != nil {
			m.t.Error(err)
		}
		accepted <- channel
	}()
	opened, err := m.opening.OpenConnection(m.ctx, "probe")
	if err != nil {
		m.t.Fatal(err)
	}
	far = <-accepted
	if far == nil {
		m.t.Fatal("nobody accepted the channel")
	}
	return opened, far
}

// speaker is a peer over one end of a channel, with this adapter on both
// hooks.
func (m *multiplexed) speaker(over *tunnel.Connection, role runtime.Role, tracer trace.Tracer, options runtime.Options) *runtime.Peer {
	m.t.Helper()
	options.Observer, options.Propagator = otel.Observer(tracer), otel.Propagator(nil)
	peer, err := runtime.NewPeer(m.ctx, over, role, options)
	if err != nil {
		m.t.Fatal(err)
	}
	m.peers = append(m.peers, peer)
	return peer
}

// quiet closes every peer a test spoke over, and then the two carrying the
// tunnels under them, and waits for each: a connection span ends where its
// peer does, and what it encloses is exported with it.
func (m *multiplexed) quiet() {
	for _, peers := range [][]*runtime.Peer{m.peers, m.carriers} {
		for _, peer := range peers {
			peer.Close()
		}
		for _, peer := range peers {
			<-peer.Done()
		}
	}
}

// One call from a client, over a channel of a tunnel, to a server handler
// that calls back the other way, is one trace: the client's own work encloses
// the call it made, the server's handler runs under a server span of that same
// trace, and the reverse call — and the client's answer to it — are under the
// handler and not beside it. A client span and the server span of one request
// are siblings: the runtime injects the trace before it says the request
// started, so the frame carries the span the caller's context held and both
// are that span's children. What nests is the work.
func TestOneCallOverAChannelIsOneSpanTree(t *testing.T) {
	tracer, exporter := exported(t)
	m := multiplex(t, nil)
	serverNear, serverFar := m.channels()
	m.speaker(serverFar, runtime.ServerRole, tracer, runtime.Options{
		Families: map[string]string{"echo": "probe", "reverse": "probe"},
		Handlers: map[string]runtime.Handler{
			// The handler calls back from the context of the request that ran
			// it, which is what puts the reverse call under the handler.
			"echo": func(ctx context.Context, peer *runtime.Peer, params json.RawMessage) (any, error) {
				var reversed payload
				if err := peer.Call(ctx, "reverse", payload{Text: "gate", Count: 1}, &reversed); err != nil {
					return nil, err
				}
				return reversed, nil
			},
		},
	})
	client := m.speaker(serverNear, runtime.ClientRole, tracer, runtime.Options{
		Families: map[string]string{"echo": "probe", "reverse": "probe"},
		Handlers: map[string]runtime.Handler{
			"reverse": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
				return payload{Text: "etag", Count: 1}, nil
			},
		},
	})
	work, root := tracer.Start(context.Background(), "the client's own work")
	var answered payload
	if err := client.Call(work, "echo", payload{Text: "gate", Count: 1}, &answered); err != nil {
		t.Fatal(err)
	}
	root.End()
	if answered.Text != "etag" {
		t.Fatalf("the server answered %+v", answered)
	}
	// The connection spans of the two peers end when the peers do; the five
	// spans the tree is made of have ended already.
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
		traces[span.SpanContext.TraceID()] = true
	}
	want := map[string]string{
		"the client's own work (internal)": "nothing",
		"echo (client)":                    "the client's own work (internal)",
		"echo (server)":                    "the client's own work (internal)",
		"reverse (client)":                 "echo (server)",
		"reverse (server)":                 "echo (server)",
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
// channels run over, which is where the tunnel's events reach an observer.
func TestNoPayloadReachesASpan(t *testing.T) {
	tracer, exporter := exported(t)
	m := multiplex(t, otel.Observer(tracer))
	serverNear, serverFar := m.channels()
	secret := map[string]string{"secret": sentinel}
	delivered := make(chan struct{})
	server := m.speaker(serverFar, runtime.ServerRole, tracer, runtime.Options{
		Handlers: map[string]runtime.Handler{
			"echo": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return secret, nil },
			"deny": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
				return nil, &runtime.PublicError{Code: "denied", Message: "Denied.",
					Data: json.RawMessage(`{"secret":"` + sentinel + `"}`)}
			},
			"boom": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { panic("the handler gave up") },
		},
	})
	client := m.speaker(serverNear, runtime.ClientRole, tracer, runtime.Options{
		Events: map[string]runtime.EventHandler{
			"changed": func(context.Context, *runtime.Peer, json.RawMessage) { close(delivered) },
		},
	})
	var result map[string]string
	if err := client.Call(context.Background(), "echo", secret, &result); err != nil || result["secret"] != sentinel {
		t.Fatalf("echo = %v, error=%v", result, err)
	}
	var denied *runtime.PublicError
	if err := client.Call(context.Background(), "deny", secret, nil); !errors.As(err, &denied) ||
		!strings.Contains(string(denied.Data), sentinel) {
		t.Fatalf("deny = %v", err)
	}
	if err := client.Call(context.Background(), "boom", secret, nil); err == nil {
		t.Fatal("a panicking handler answered without an error")
	}
	if err := server.Emit(context.Background(), "changed", secret); err != nil {
		t.Fatal(err)
	}
	<-delivered
	m.quiet()
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
	for _, want := range []string{"echo", "deny", "boom", "changed", "handler panic", "tunnel.ChannelOpened"} {
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
