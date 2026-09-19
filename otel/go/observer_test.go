package otel_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Bitspark/nightseam/otel/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The adapter is a mapping from an event to a span or to a span event, so what
// holds it is the spans it leaves behind, rendered and compared byte for byte
// under testdata. A change to a kind, to an attribute's name, to what encloses
// what or to what is left out shows up as a diff of a golden, which is what a
// reviewer reads. When the change is meant, rewrite them:
//
//	go test ./otel/go -update
var update = flag.Bool("update", false, "rewrite the golden spans under testdata from the current output")

// sentinel stands for every payload a peer carries, as it does in the
// runtime's own observer suite and in the slog adapter's: it travels in the
// params of a call, in what answers it, in the data of a public error and in
// the data of an event, and it must appear in no span this adapter opens.
const sentinel = "payload-sentinel-4bf92f35"

// exported is one tracer over an exporter that holds what ends, so that a test
// reads the spans in the order they were ended.
func exported(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	return provider.Tracer("nightseam-otel-test"), exporter
}

// channelOpened stands for an event of a layer running over the peer — a
// tunnel's, a later profile's — which reaches this adapter
// through the peer's observer before this package has a case for it. Its
// fields are of every kind the general path renders, and one of a kind it
// renders not at all.
type channelOpened struct {
	At      time.Time
	Family  string
	ID      int64
	Opener  bool
	Role    runtime.Role
	Trace   runtime.Trace
	Waiting time.Duration
	Payload json.RawMessage
}

func (channelOpened) ObserverEvent() {}

// Every event of the surface, one after another: the request spans with their
// kinds, their outcomes and the frames inside them, the profile's events as
// spans of no duration, and the connection enclosing what has no request of
// its own — a layer's event among them, reflected into attributes without this
// package knowing what it is.
func TestEveryEventIsASpanOrASpanEvent(t *testing.T) {
	tracer, exporter := exported(t)
	observer := otel.Observer(tracer)
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	step := func(n int) time.Time { return at.Add(time.Duration(n) * time.Millisecond) }
	traced := runtime.Trace{Parent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", State: "nightseam=1"}
	// A response names no method, carries the request's traceparent without a
	// tracestate, and belongs to no family the caller labelled: what an event
	// does not carry is what the span does not say.
	bare := runtime.Trace{Parent: traced.Parent}
	for _, event := range []runtime.ObserverEvent{
		runtime.ConnectionOpened{At: step(0), Role: runtime.ClientRole},
		runtime.RequestStarted{At: step(1), ID: "c:1", Method: "turn.start", Trace: traced, Family: "turns"},
		runtime.FrameSent{At: step(2), Kind: "request", Name: "turn.start", Bytes: 148, ID: "c:1", Trace: traced, Family: "turns"},
		runtime.FrameReceived{At: step(3), Kind: "response", Bytes: 96, ID: "c:1", Trace: bare},
		runtime.RequestEnded{At: step(4), ID: "c:1", Method: "turn.start", Duration: 3 * time.Millisecond,
			Outcome: runtime.OutcomeOK, Trace: traced, Family: "turns"},
		runtime.RequestStarted{At: step(5), ID: "s:1", Method: "turn.stop", Incoming: true, Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(6), ID: "s:1", Method: "turn.stop", Incoming: true, Duration: time.Millisecond,
			Outcome: runtime.OutcomeErrored, ErrorCode: "denied", Trace: traced, Family: "turns"},
		runtime.RequestStarted{At: step(7), ID: "c:2", Method: "turn.wait", Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(8), ID: "c:2", Method: "turn.wait", Duration: time.Millisecond,
			Outcome: runtime.OutcomeCancelled, Trace: traced, Family: "turns"},
		runtime.RequestStarted{At: step(9), ID: "c:3", Method: "turn.wait", Trace: traced, Family: "turns"},
		runtime.RequestEnded{At: step(10), ID: "c:3", Method: "turn.wait", Duration: time.Millisecond,
			Outcome: runtime.OutcomeTimedOut, Trace: traced, Family: "turns"},
		runtime.EventEmitted{At: step(11), Name: "grants.deliver", Bytes: 42, Trace: traced, Family: "grants"},
		runtime.EventDelivered{At: step(12), Name: "grants.deliver", Bytes: 42, Trace: bare},
		runtime.Backpressure{At: step(13), Queued: 64, Deadline: 5 * time.Second},
		runtime.Backpressure{At: step(14), Queued: 64, Stalled: true, Deadline: 5 * time.Second},
		runtime.HandlerPanic{At: step(15), Method: "turn.start", Value: "the handler gave up", Trace: traced, Family: "turns"},
		channelOpened{At: step(16), Family: "probe", ID: 7, Opener: true, Role: runtime.ServerRole,
			Trace: traced, Waiting: 2 * time.Second, Payload: json.RawMessage(`{"secret":"` + sentinel + `"}`)},
		runtime.ConnectionClosed{At: step(17), Code: 1000, Local: true},
	} {
		observer.Observe(event)
	}
	holdGolden(t, "events.txt", spelled(exporter.GetSpans()))
}

// An event a request span encloses is on that span and not on the connection:
// a frame names the request it belongs to, and the request is still open.
// Where the request is over, or where the peer raised one and serves one under
// the same id, the connection encloses it instead.
func TestAFrameIsOnTheRequestItBelongsTo(t *testing.T) {
	tracer, exporter := exported(t)
	observer := otel.Observer(tracer)
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	observer.Observe(runtime.ConnectionOpened{At: at, Role: runtime.ClientRole})
	observer.Observe(runtime.RequestStarted{At: at, ID: "c:1", Method: "echo"})
	observer.Observe(runtime.RequestStarted{At: at, ID: "c:1", Method: "echo", Incoming: true})
	// Two requests bear this id, so neither encloses: the adapter would be
	// guessing, and the connection takes it.
	observer.Observe(runtime.FrameSent{At: at, Kind: "request", Name: "echo", ID: "c:1"})
	observer.Observe(runtime.RequestEnded{At: at, ID: "c:1", Method: "echo", Incoming: true})
	observer.Observe(runtime.FrameReceived{At: at, Kind: "response", ID: "c:1"})
	observer.Observe(runtime.RequestEnded{At: at, ID: "c:1", Method: "echo"})
	// A frame of a request that is over is the connection's too.
	observer.Observe(runtime.FrameReceived{At: at, Kind: "response", ID: "c:1"})
	observer.Observe(runtime.ConnectionClosed{At: at, Code: 1000, Local: true})
	events := map[string]int{}
	for _, span := range exporter.GetSpans() {
		for _, event := range span.Events {
			events[span.Name+" "+event.Name]++
		}
	}
	want := map[string]int{"echo frame received": 1, "connection frame sent": 1, "connection frame received": 1}
	for name, count := range want {
		if events[name] != count {
			t.Errorf("%q was recorded %d times and not %d; the spans carry %v", name, events[name], count, events)
		}
	}
	if len(events) != len(want) {
		t.Errorf("the spans carry %v", events)
	}
}

// An observer told of no connection has nowhere to put what has no span of its
// own, and records nothing rather than opening a span for it.
func TestWithoutAnEnclosingSpanNothingIsRecorded(t *testing.T) {
	tracer, exporter := exported(t)
	observer := otel.Observer(tracer)
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	observer.Observe(runtime.Backpressure{At: at, Queued: 1})
	observer.Observe(channelOpened{At: at, Family: "probe", ID: 1})
	observer.Observe(runtime.ConnectionClosed{At: at, Code: 1000})
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("an observer that saw no connection opened left %s", spelled(spans))
	}
}

// spelled renders what the exporter holds as the text a golden is: the spans in
// the order they ended, their ids as the labels of that order, and every time
// as an offset from the first — so that what is held is the mapping this
// package is and not the ids and the clock of the run that made them.
func spelled(spans tracetest.SpanStubs) string {
	label := map[trace.SpanID]string{}
	var first time.Time
	for i, span := range spans {
		label[span.SpanContext.SpanID()] = fmt.Sprintf("#%d", i+1)
		if first.IsZero() || span.StartTime.Before(first) {
			first = span.StartTime
		}
	}
	since := func(at time.Time) string { return fmt.Sprintf("+%dms", at.Sub(first)/time.Millisecond) }
	var out strings.Builder
	for i, span := range spans {
		parent := "none"
		if span.Parent.IsValid() {
			if known, ok := label[span.Parent.SpanID()]; ok {
				parent = known
			} else {
				parent = span.Parent.SpanID().String()
			}
		}
		status := span.Status.Code.String()
		if span.Status.Description != "" {
			status += " " + span.Status.Description
		}
		fmt.Fprintf(&out, "#%d %q kind=%s parent=%s %s..%s status=%s\n",
			i+1, span.Name, span.SpanKind, parent, since(span.StartTime), since(span.EndTime), status)
		for _, attribute := range span.Attributes {
			fmt.Fprintf(&out, "\t%s=%s\n", attribute.Key, attribute.Value.Emit())
		}
		for _, event := range span.Events {
			fmt.Fprintf(&out, "\tevent %q %s\n", event.Name, since(event.Time))
			for _, attribute := range event.Attributes {
				fmt.Fprintf(&out, "\t\t%s=%s\n", attribute.Key, attribute.Value.Emit())
			}
		}
	}
	return out.String()
}

// holdGolden compares what the adapter left behind against a golden, or
// rewrites it under -update.
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
		t.Fatalf("%s has no golden; run with -update. the adapter left:\n%s", name, got)
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs\n--- golden ---\n%s--- left ---\n%s", name, want, got)
	}
}
