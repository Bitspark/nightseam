package otel_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Bitspark/nightseam/otel/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The same event sequences run against both adapters. Only the host spelling
// of a span event is normalized; span topology, timing and routing are shared.
func TestSharedObserverSequences(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/otel-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name    string
			Events  []observerSequenceEvent
			OnStart []observerSequenceEvent `json:"on_start"`
			Spans   []struct {
				Name         string
				Kind         string
				StartMS      int64  `json:"start_ms"`
				EndMS        int64  `json:"end_ms"`
				TraceID      string `json:"trace_id"`
				ParentSpanID string `json:"parent_span_id"`
				Attributes   map[string]any
				Events       []struct {
					Name string
					AtMS int64 `json:"at_ms"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the shared observer fixture has no cases")
	}
	origin := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			var observer runtime.Observer
			first := true
			processor := &sequenceStartProcessor{
				SpanProcessor: sdktrace.NewSimpleSpanProcessor(exporter),
				start: func() {
					if !first {
						return
					}
					first = false
					for _, event := range row.OnStart {
						observer.Observe(event.observed(t, origin))
					}
				},
			}
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
			t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
			observer = otel.Observer(provider.Tracer("shared-observer-sequences"))
			for _, event := range row.Events {
				observer.Observe(event.observed(t, origin))
			}
			spans := exporter.GetSpans()
			if len(spans) != len(row.Spans) {
				t.Fatalf("got %d spans, want %d:\n%s", len(spans), len(row.Spans), spelled(spans))
			}
			for i, want := range row.Spans {
				span := spans[i]
				if span.Name != want.Name || span.SpanKind.String() != want.Kind ||
					span.StartTime.Sub(origin).Milliseconds() != want.StartMS || span.EndTime.Sub(origin).Milliseconds() != want.EndMS {
					t.Fatalf("span %d has the wrong name, kind or lifetime:\n%s", i, spelled(spans))
				}
				parent := ""
				if span.Parent.IsValid() {
					parent = span.Parent.SpanID().String()
				}
				if parent != want.ParentSpanID || (want.TraceID != "" && span.SpanContext.TraceID().String() != want.TraceID) {
					t.Fatalf("span %d has the wrong trace parent: %v", i, span.Parent)
				}
				attributes := map[string]any{}
				for _, attribute := range span.Attributes {
					attributes[string(attribute.Key)] = attribute.Value.AsInterface()
				}
				for key, value := range want.Attributes {
					got, _ := json.Marshal(attributes[key])
					expected, _ := json.Marshal(value)
					if string(got) != string(expected) {
						t.Errorf("span %d attribute %s: got %s, want %s", i, key, got, expected)
					}
				}
				if len(span.Events) != len(want.Events) {
					t.Fatalf("span %d has %d events, want %d:\n%s", i, len(span.Events), len(want.Events), spelled(spans))
				}
				for j, expected := range want.Events {
					event := span.Events[j]
					name := event.Name
					switch name {
					case "frame sent":
						name = "frame.sent"
					case "frame received":
						name = "frame.received"
					case "otel_test.sequenceLayerEvent":
						name = "channel.opened"
					}
					if name != expected.Name || event.Time.Sub(origin).Milliseconds() != expected.AtMS {
						t.Errorf("span %d event %d: got %s at %v, want %s at %dms", i, j, name, event.Time, expected.Name, expected.AtMS)
					}
				}
			}
		})
	}
}

// The SDK calls OnStart before the tracer returns the span to the adapter.
type sequenceStartProcessor struct {
	sdktrace.SpanProcessor
	start func()
}

func (p *sequenceStartProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) { p.start() }

func TestConnectionClosesFromAnotherGoroutineDuringSpanStart(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	var observer runtime.Observer
	at := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	processor := &sequenceStartProcessor{
		SpanProcessor: sdktrace.NewSimpleSpanProcessor(exporter),
		start: func() {
			done := make(chan struct{})
			go func() {
				observer.Observe(runtime.ConnectionClosed{At: at.Add(time.Millisecond), Code: 1000})
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the observer held its lock while calling the consumer's tracer")
			}
		},
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	observer = otel.Observer(provider.Tracer("concurrent-connection-close"))
	observer.Observe(runtime.ConnectionOpened{At: at, Role: runtime.ClientRole})
	spans := exporter.GetSpans()
	if len(spans) != 1 || !spans[0].EndTime.Equal(at.Add(time.Millisecond)) {
		t.Fatalf("the close racing span creation was not retained: %s", spelled(spans))
	}
}

type observerSequenceEvent struct {
	Type       string
	AtMS       int64 `json:"at_ms"`
	Role       runtime.Role
	Name       string
	Bytes      int
	Family     string
	ID         json.RawMessage
	Method     string
	Incoming   bool
	Kind       string
	DurationMS int64 `json:"durationMs"`
	Outcome    string
	Queued     int
	Stalled    bool
	DeadlineMS int64 `json:"deadlineMs"`
	Code       int
	Reason     string
	Local      bool
	Trace      struct {
		Traceparent string
		Tracestate  string
	}
}

type sequenceLayerEvent struct {
	At     time.Time
	ID     int64
	Family string
}

func (sequenceLayerEvent) ObserverEvent() {}

func (e observerSequenceEvent) observed(t *testing.T, origin time.Time) runtime.ObserverEvent {
	t.Helper()
	at := origin.Add(time.Duration(e.AtMS) * time.Millisecond)
	trace := runtime.Trace{Parent: e.Trace.Traceparent, State: e.Trace.Tracestate}
	id := ""
	if len(e.ID) > 0 && e.Type != "channel.opened" {
		if err := json.Unmarshal(e.ID, &id); err != nil {
			t.Fatal(err)
		}
	}
	switch e.Type {
	case "connection.opened":
		return runtime.ConnectionOpened{At: at, Role: e.Role}
	case "connection.closed":
		return runtime.ConnectionClosed{At: at, Code: e.Code, Reason: e.Reason, Local: e.Local}
	case "event.emitted":
		return runtime.EventEmitted{At: at, Name: e.Name, Bytes: e.Bytes, Trace: trace, Family: e.Family}
	case "event.delivered":
		return runtime.EventDelivered{At: at, Name: e.Name, Bytes: e.Bytes, Trace: trace, Family: e.Family}
	case "request.started":
		return runtime.RequestStarted{At: at, ID: id, Method: e.Method, Incoming: e.Incoming, Trace: trace, Family: e.Family}
	case "request.ended":
		if e.Outcome != "ok" {
			t.Fatalf("unsupported fixture outcome %q", e.Outcome)
		}
		return runtime.RequestEnded{At: at, ID: id, Method: e.Method, Incoming: e.Incoming,
			Duration: time.Duration(e.DurationMS) * time.Millisecond, Outcome: runtime.OutcomeOK, Trace: trace, Family: e.Family}
	case "frame.sent":
		return runtime.FrameSent{At: at, Kind: e.Kind, Name: e.Name, Bytes: e.Bytes, ID: id, Trace: trace, Family: e.Family}
	case "frame.received":
		return runtime.FrameReceived{At: at, Kind: e.Kind, Name: e.Name, Bytes: e.Bytes, ID: id, Trace: trace, Family: e.Family}
	case "backpressure":
		return runtime.Backpressure{At: at, Queued: e.Queued, Stalled: e.Stalled, Deadline: time.Duration(e.DeadlineMS) * time.Millisecond}
	case "channel.opened":
		var channelID int64
		if err := json.Unmarshal(e.ID, &channelID); err != nil {
			t.Fatal(err)
		}
		return sequenceLayerEvent{At: at, ID: channelID, Family: e.Family}
	default:
		t.Fatalf("unsupported fixture event %q", e.Type)
		return nil
	}
}
