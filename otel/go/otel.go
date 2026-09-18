// Package otel binds Nightseam's two observability hooks to OpenTelemetry:
// Propagator moves a frame's W3C trace context between a context and the two
// members of the envelope, and Observer opens a span per request and records
// what a peer, a tunnel and a session tell it. It is a module of its own,
// because the core module depends on nothing and that is a released promise;
// it imports the runtime and nothing else of Nightseam, so that an event of a
// layer it has never heard of reaches a span all the same.
//
// # What Inject must return
//
// The runtime does not validate what Propagator.Inject returns: what it
// returns is what the frame carries, and a peer holds every traceparent it
// reads to ^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$ and closes the
// connection on one that is not of that form. An OpenTelemetry
// TextMapPropagator writes nothing at all for a context with no span and
// nothing for one whose span context is invalid, and a propagator of another
// making may write the all-zero traceparent for a span that never was — so
// this adapter mints a traceparent of its own wherever the propagator has
// nothing to say, and every Inject of it is one a peer accepts.
// TestEveryInjectIsATraceparentAPeerAccepts holds it over a context with no
// span, one whose span is not recording, one whose span context is invalid
// and one that is recording, under four propagators.
//
// # The shape of a trace
//
// A client span and the server span of the same request are siblings and not
// one inside the other: the runtime injects the trace before it says a
// request started, so what the frame carries is the span the caller's context
// held, and both spans are children of that. What nests across hops is the
// work — an incoming request's server span is the handler's own context, so
// what the handler sends is a child of the request that ran it, and a call
// through a relay to a machine handler whose ask is answered is one trace
// whose nesting is that chain. TestOneCallThroughARelayIsOneSpanTree spells
// the tree out.
//
// # Never a payload
//
// An observer sees names, ids, sizes, durations, outcomes and close codes, and
// no attribute of any span here is a payload: what the explicit cases record
// is named field by field, and an event this package has no case for is
// reflected into attributes of the kinds a name, a count or a flag has —
// anything else is dropped rather than rendered, so that an event carrying
// more than a name could not reach a span through the general path either.
package otel

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Bitspark/nightseam/runtime/go"
)

// scope names this adapter as the instrumentation a span came from.
const scope = "github.com/Bitspark/nightseam/otel/go"

const traceparentKey, tracestateKey = "traceparent", "tracestate"

// Propagator is a runtime.Propagator over an OpenTelemetry
// TextMapPropagator. A nil one is propagation.TraceContext{}, the W3C
// encoding both members of the envelope already are.
func Propagator(text propagation.TextMapPropagator) runtime.Propagator {
	if text == nil {
		text = propagation.TraceContext{}
	}
	return propagator{text: text}
}

type propagator struct{ text propagation.TextMapPropagator }

// Extract places an incoming frame's trace in the context its handler runs
// under, as OpenTelemetry's remote span context — and, where this package's
// observer has a server span open for that very frame, that span, so that
// what the handler sends is a child of the request that ran it and the span
// tree is the nesting. The runtime says a request started before it extracts
// for it and on the same goroutine, which is what makes the span there to be
// found; where it is not there, the remote span context stands alone, which
// is what an adapter without the two halves would do everywhere.
func (p propagator) Extract(ctx context.Context, incoming runtime.Trace) context.Context {
	if incoming.Parent == "" {
		return ctx
	}
	ctx = p.text.Extract(ctx, &carrier{parent: incoming.Parent, state: incoming.State})
	if span, ok := enclosing(incoming.Parent); ok {
		ctx = trace.ContextWithSpan(ctx, span)
	}
	return ctx
}

// Inject says what an outgoing frame carries: the trace context of the span
// the context holds, as the propagator writes it, and a minted one wherever
// what it wrote is not a traceparent a peer accepts — see the package
// comment, which is the contract this is the whole of.
func (p propagator) Inject(ctx context.Context) runtime.Trace {
	var written carrier
	p.text.Inject(ctx, &written)
	if accepted(written.parent) {
		return runtime.Trace{Parent: written.parent, State: written.state}
	}
	// A minted traceparent carries no tracestate: the state belongs to the
	// trace the propagator declined to write, and this is not that trace.
	return runtime.Trace{Parent: mint(trace.SpanContextFromContext(ctx))}
}

// carrier is the two members of an envelope as a TextMapCarrier. An envelope
// carries traceparent and tracestate and nothing else, so a propagator that
// writes a third member writes it nowhere: what a frame has no place for is
// not carried, rather than carried somewhere it does not belong.
type carrier struct{ parent, state string }

func (c *carrier) Get(key string) string {
	switch key {
	case traceparentKey:
		return c.parent
	case tracestateKey:
		return c.state
	}
	return ""
}

func (c *carrier) Set(key, value string) {
	switch key {
	case traceparentKey:
		c.parent = value
	case tracestateKey:
		c.state = value
	}
}

func (c *carrier) Keys() []string {
	keys := make([]string, 0, 2)
	if c.parent != "" {
		keys = append(keys, traceparentKey)
	}
	if c.state != "" {
		keys = append(keys, tracestateKey)
	}
	return keys
}

// parented is a frame's trace as a parent to start a span under: the span its
// traceparent names, remote because it is another peer's. A frame carrying
// none begins a trace here rather than continuing one that is not there. It
// reads W3C Trace Context and not whichever propagator a consumer chose,
// because the two members of the envelope are that encoding and no other.
func parented(t runtime.Trace) context.Context {
	if t.Parent == "" {
		return context.Background()
	}
	return propagation.TraceContext{}.Extract(context.Background(), &carrier{parent: t.Parent, state: t.State})
}

// accepted reports whether a traceparent is one a peer takes: the form the
// remote decoder holds every frame's to, and neither of its ids the zero one,
// which is what a propagator writes for a span that never was.
func accepted(traceparent string) bool {
	if len(traceparent) != 55 || traceparent[2] != '-' || traceparent[35] != '-' || traceparent[52] != '-' {
		return false
	}
	for i := range len(traceparent) {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if c := traceparent[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return !zero(traceparent[3:35]) && !zero(traceparent[36:52])
}

func zero(id string) bool {
	for i := range len(id) {
		if id[i] != '0' {
			return false
		}
	}
	return true
}

// mint is a traceparent of this adapter's own, for the one case the
// propagator had nothing to say. It keeps the context's trace id and its
// flags where they are there, so that a frame sent from a context whose span
// OpenTelemetry would not carry still belongs to the trace it is part of, and
// mints a trace of its own where there is none — sampled, as the runtime's own
// default propagator mints one, because a trace nothing decided against is one
// a peer downstream is free to record. A frame correlates across hops whether
// or not the consumer installed a tracer, which is the whole of why this is
// here.
func mint(sc trace.SpanContext) string {
	id, flags := sc.TraceID(), sc.TraceFlags()
	if !id.IsValid() {
		rand.Read(id[:])
		flags = trace.FlagsSampled
	}
	var span trace.SpanID
	rand.Read(span[:])
	return "00-" + hex.EncodeToString(id[:]) + "-" + hex.EncodeToString(span[:]) + "-" + hex.EncodeToString([]byte{byte(flags)})
}
