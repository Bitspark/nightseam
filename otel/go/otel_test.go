package otel_test

import (
	"context"
	"regexp"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/Bitspark/nightseam/otel/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// accepted is what a peer holds every traceparent it reads to. A frame
// carrying anything else closes the connection, and the runtime validates
// nothing of what a propagator returns — so this is the whole of the
// propagator's contract, and the test below is the whole of holding this
// adapter to it.
var accepted = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// Every Inject of this adapter is a traceparent the remote peer accepts,
// whatever the context holds and whichever propagator it was given: an
// OpenTelemetry propagator writes nothing for a context with no span, nothing
// for one whose span context is invalid, and a propagator of another making
// may write the all-zero one for a span that never was — and the frame that
// carried any of those would be refused.
func TestEveryInjectIsATraceparentAPeerAccepts(t *testing.T) {
	recording, span := sdktrace.NewTracerProvider().Tracer("holds").Start(context.Background(), "recording")
	defer span.End()
	quiet, _ := noop.NewTracerProvider().Tracer("holds").Start(context.Background(), "not recording")
	sampledByNothing := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:  trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
	}))
	contexts := map[string]context.Context{
		"a context with no span":                  context.Background(),
		"a context whose span is not recording":   quiet,
		"a context whose span context is invalid": trace.ContextWithSpanContext(context.Background(), trace.SpanContext{}),
		"a context whose span context is the zero one": trace.ContextWithSpanContext(context.Background(),
			trace.NewSpanContext(trace.SpanContextConfig{})),
		"a context whose span is sampled by nothing": sampledByNothing,
		"a context whose span is recording":          recording,
	}
	propagators := map[string]runtime.Propagator{
		"the default":                otel.Propagator(nil),
		"W3C Trace Context":          otel.Propagator(propagation.TraceContext{}),
		"a propagator carrying none": otel.Propagator(propagation.Baggage{}),
		"a composite":                otel.Propagator(propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})),
	}
	for held, propagator := range propagators {
		for described, ctx := range contexts {
			injected := propagator.Inject(ctx)
			if !accepted.MatchString(injected.Parent) {
				t.Errorf("%s injected %q from %s, which no peer accepts", held, injected.Parent, described)
				continue
			}
			// The zero ids are the form's, and no trace: a backend reading one
			// has a frame that correlates with nothing.
			if injected.Parent[3:35] == "00000000000000000000000000000000" || injected.Parent[36:52] == "0000000000000000" {
				t.Errorf("%s injected the zero trace %q from %s", held, injected.Parent, described)
			}
		}
	}
}

// What the propagator does carry, it carries as it is: a context whose span
// context is valid reaches the frame as that span and no other, which is what
// makes the remote peer's server span a child of it.
func TestAValidSpanContextReachesTheFrameAsItIs(t *testing.T) {
	_, span := sdktrace.NewTracerProvider().Tracer("holds").Start(context.Background(), "carried")
	defer span.End()
	ctx := trace.ContextWithSpan(context.Background(), span)
	sc := span.SpanContext()
	want := "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-01"
	if injected := otel.Propagator(nil).Inject(ctx); injected.Parent != want {
		t.Fatalf("a recording span reached the frame as %q and not %q", injected.Parent, want)
	}
}

// An incoming frame's trace is the handler's context: as OpenTelemetry's
// remote span context, which is what makes it the parent of whatever the
// handler's own peer opens under it. A frame carrying no trace leaves the
// context as it was, so that what is sent from there begins a trace rather
// than continuing one that is not there.
func TestExtractIsTheIncomingTraceAsARemoteSpanContext(t *testing.T) {
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	propagator := otel.Propagator(nil)
	ctx := propagator.Extract(context.Background(), runtime.Trace{Parent: traceparent, State: "nightseam=1"})
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsRemote() {
		t.Fatalf("the incoming trace reached the context as %+v", sc)
	}
	if sc.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || sc.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("the incoming trace reached the context as %s", sc.TraceID())
	}
	if sc.TraceState().String() != "nightseam=1" {
		t.Fatalf("the tracestate reached the context as %q", sc.TraceState().String())
	}
	// It goes back out as it came in, which is what a response and a cancel
	// of it carry and what a relay forwards verbatim.
	if injected := propagator.Inject(ctx); injected.Parent != traceparent {
		t.Fatalf("an extracted trace was injected as %q", injected.Parent)
	}
	if bare := propagator.Extract(context.Background(), runtime.Trace{}); trace.SpanContextFromContext(bare).IsValid() {
		t.Fatal("a frame carrying no trace put one in the context")
	}
}
