package otel

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Bitspark/nightseam/runtime/go"
)

// Observer is a runtime.Observer that opens a span per request on tracer: a
// server span for a request this peer serves, a client span for one it
// raises, ended where the peer says the request ended, with the outcome as
// the span's status and the error code as an attribute. An event the
// application emitted or was delivered is a span of no duration under the
// trace its frame carried; the connection is a span of its own, from the peer
// taking the connection over to the close that ended it; and everything else
// a peer or a tunnel tells — a frame, backpressure, a handler that
// gave up, a channel — is a span event on the
// enclosing span, which is the request's where the event names one still open
// here and the connection's otherwise. Where there is no enclosing span the
// event is not recorded: an adapter never told a connection opened has
// nowhere to put what happened on it.
//
// A nil tracer is the global provider's, an observer being no reason for a
// peer to panic on a goroutine the consumer does not own. One Observer is one
// peer's: it keeps the spans it opened under the request ids of the
// connection it watches, and it is called from every goroutine that peer
// observes from, so it keeps them under a lock of its own.
func Observer(tracer trace.Tracer) runtime.Observer {
	if tracer == nil {
		tracer = otelglobal.GetTracerProvider().Tracer(scope)
	}
	return &observer{tracer: tracer, requests: map[requestKey]*opened{}}
}

type observer struct {
	tracer     trace.Tracer
	mu         sync.Mutex
	connection trace.Span
	requests   map[requestKey]*opened
}

// opened is one span this observer has open, held by identity: a trace.Span is
// not a comparable value in every implementation, so the registry withdraws the
// one it published and never one that merely looks like it.
type opened struct{ span trace.Span }

// requestKey names one request of the connection this observer watches: an id
// is minted per connection and per direction, and the runtime refuses a second
// live request under an id it already serves.
type requestKey struct {
	id       string
	incoming bool
}

// Observe is the type switch a consumer of the Observer dispatches by, written
// once. Every attribute below is named, and none of them is a payload.
func (o *observer) Observe(event runtime.ObserverEvent) {
	switch e := event.(type) {
	case runtime.ConnectionOpened:
		_, span := o.tracer.Start(context.Background(), "connection",
			trace.WithTimestamp(e.At), trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(attribute.String("nightseam.role", string(e.Role))))
		o.mu.Lock()
		o.connection = span
		o.mu.Unlock()
	case runtime.ConnectionClosed:
		o.mu.Lock()
		span := o.connection
		o.connection = nil
		o.mu.Unlock()
		if span == nil {
			return
		}
		span.SetAttributes(attribute.Int("nightseam.close.code", e.Code),
			attribute.String("nightseam.close.reason", e.Reason),
			attribute.Bool("nightseam.close.local", e.Local))
		span.End(trace.WithTimestamp(e.At))
	case runtime.RequestStarted:
		kind := trace.SpanKindClient
		if e.Incoming {
			kind = trace.SpanKindServer
		}
		_, span := o.tracer.Start(parented(e.Trace), e.Method,
			trace.WithTimestamp(e.At), trace.WithSpanKind(kind),
			trace.WithAttributes(labelled([]attribute.KeyValue{
				attribute.String("nightseam.request.id", e.ID),
				attribute.String("nightseam.method", e.Method),
			}, e.Family)...))
		held := &opened{span: span}
		o.mu.Lock()
		o.requests[requestKey{id: e.ID, incoming: e.Incoming}] = held
		o.mu.Unlock()
		// A server span is what the handler's own context is given, which is
		// the one place the propagator and the observer meet.
		if e.Incoming {
			publish(e.Trace.Parent, held)
		}
	case runtime.RequestEnded:
		key := requestKey{id: e.ID, incoming: e.Incoming}
		o.mu.Lock()
		held := o.requests[key]
		delete(o.requests, key)
		o.mu.Unlock()
		if e.Incoming {
			withdraw(e.Trace.Parent, held)
		}
		if held == nil {
			return
		}
		span := held.span
		if e.ErrorCode != "" {
			span.SetAttributes(attribute.String("nightseam.error.code", e.ErrorCode))
		}
		if e.Outcome == runtime.OutcomeOK {
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Error, e.Outcome.String())
		}
		span.End(trace.WithTimestamp(e.At))
	case runtime.EventEmitted:
		o.moment(e.Trace, e.Name, trace.SpanKindProducer, e.At, e.Bytes, e.Family)
	case runtime.EventDelivered:
		o.moment(e.Trace, e.Name, trace.SpanKindConsumer, e.At, e.Bytes, e.Family)
	case runtime.FrameSent:
		o.record(e.ID, e.At, "frame sent", labelled([]attribute.KeyValue{
			attribute.String("nightseam.frame.kind", e.Kind), attribute.String("nightseam.name", e.Name),
			attribute.Int("nightseam.bytes", e.Bytes), attribute.String("nightseam.request.id", e.ID),
		}, e.Family))
	case runtime.FrameReceived:
		o.record(e.ID, e.At, "frame received", labelled([]attribute.KeyValue{
			attribute.String("nightseam.frame.kind", e.Kind), attribute.String("nightseam.name", e.Name),
			attribute.Int("nightseam.bytes", e.Bytes), attribute.String("nightseam.request.id", e.ID),
		}, e.Family))
	case runtime.Backpressure:
		o.record("", e.At, "backpressure", []attribute.KeyValue{
			attribute.Int("nightseam.queued", e.Queued), attribute.Bool("nightseam.stalled", e.Stalled),
			attribute.String("nightseam.deadline", e.Deadline.String()),
		})
	case runtime.HandlerPanic:
		o.record("", e.At, "handler panic", labelled([]attribute.KeyValue{
			attribute.String("nightseam.method", e.Method), attribute.String("nightseam.panic", e.Value),
		}, e.Family))
	default:
		// An event this package has no case for is recorded as what it is
		// rather than dropped: its Go type name, and the fields of it a name, a
		// count or a flag could be — so that a tunnel's events and a later
		// profile's reach a span before this package knows anything about
		// them, and nothing larger reaches one at all.
		at, attributes := reflected(event)
		o.record("", at, fmt.Sprintf("%T", event), attributes)
	}
}

// moment is an event of the profile as a span of no duration: it happened at
// one instant and the peer measures nothing across it, so what it says is the
// trace it belongs to, its name, its size and its family.
func (o *observer) moment(t runtime.Trace, name string, kind trace.SpanKind, at time.Time, bytes int, family string) {
	_, span := o.tracer.Start(parented(t), name, trace.WithTimestamp(at), trace.WithSpanKind(kind),
		trace.WithAttributes(labelled([]attribute.KeyValue{
			attribute.String("nightseam.name", name), attribute.Int("nightseam.bytes", bytes),
		}, family)...))
	span.End(trace.WithTimestamp(at))
}

// record adds one span event to the enclosing span: the request's where id
// names one this observer still has open, and the connection's otherwise. An
// observer told of no connection records none, there being nowhere to put it.
func (o *observer) record(id string, at time.Time, name string, attributes []attribute.KeyValue) {
	o.mu.Lock()
	span := o.connection
	if id != "" {
		outgoing, raised := o.requests[requestKey{id: id}]
		incoming, served := o.requests[requestKey{id: id, incoming: true}]
		// One id, one span, or the connection's: a request this peer raised
		// and one it serves may bear the same id, and then neither encloses.
		if raised != served {
			if raised {
				span = outgoing.span
			} else {
				span = incoming.span
			}
		}
	}
	o.mu.Unlock()
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithTimestamp(at), trace.WithAttributes(attributes...))
}

// labelled adds the family a name belongs to, as the generated install
// labelled it, and leaves it out where the name is unlabelled: a span says
// nothing of a family rather than saying it has none.
func labelled(attributes []attribute.KeyValue, family string) []attribute.KeyValue {
	if family == "" {
		return attributes
	}
	return append(attributes, attribute.String("nightseam.family", family))
}

// The two halves of this adapter meet here and nowhere else. An observer is
// told a request started but is never given a context; a propagator is given a
// context but knows no span. So a server span this package opens is published
// under the traceparent the frame carried — which names one in-flight request
// — and Extract takes it up. Where two requests are open under one traceparent
// neither is taken up: the adapter would be guessing, and the remote span
// context it extracted stands instead.
var live = struct {
	mu sync.Mutex
	by map[string][]*opened
}{by: map[string][]*opened{}}

func publish(traceparent string, span *opened) {
	if traceparent == "" {
		return
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	live.by[traceparent] = append(live.by[traceparent], span)
}

func withdraw(traceparent string, span *opened) {
	if traceparent == "" || span == nil {
		return
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	open := live.by[traceparent]
	for i, held := range open {
		if held == span {
			open = append(open[:i], open[i+1:]...)
			break
		}
	}
	if len(open) == 0 {
		delete(live.by, traceparent)
		return
	}
	live.by[traceparent] = open
}

func enclosing(traceparent string) (trace.Span, bool) {
	live.mu.Lock()
	defer live.mu.Unlock()
	if open := live.by[traceparent]; len(open) == 1 {
		return open[0].span, true
	}
	return nil, false
}

// reflected reads an event this package has no case for: when it happened,
// from an At it may carry, and its other exported fields as attributes of the
// kinds a name, a count, a flag, a duration or a trace has. A field of any
// other kind is dropped rather than rendered, which is what keeps a payload
// out of this path as surely as out of the named ones.
func reflected(event runtime.ObserverEvent) (time.Time, []attribute.KeyValue) {
	at := time.Now()
	value := reflect.ValueOf(event)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return at, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return at, nil
	}
	fields := value.Type()
	attributes := make([]attribute.KeyValue, 0, fields.NumField())
	for i := range fields.NumField() {
		field := fields.Field(i)
		if !field.IsExported() {
			continue
		}
		if moment, ok := value.Field(i).Interface().(time.Time); ok {
			if field.Name == "At" {
				at = moment
			}
			continue
		}
		attributes = append(attributes, attributed(snake(field.Name), value.Field(i))...)
	}
	return at, attributes
}

// attributed is one field as the attributes it is worth: none, where its kind
// is not one this adapter renders.
func attributed(name string, value reflect.Value) []attribute.KeyValue {
	if t, ok := value.Interface().(runtime.Trace); ok {
		return traced(t)
	}
	if named, ok := value.Interface().(fmt.Stringer); ok {
		return []attribute.KeyValue{attribute.String("nightseam."+name, named.String())}
	}
	switch value.Kind() {
	case reflect.String:
		return []attribute.KeyValue{attribute.String("nightseam."+name, value.String())}
	case reflect.Bool:
		return []attribute.KeyValue{attribute.Bool("nightseam."+name, value.Bool())}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []attribute.KeyValue{attribute.Int64("nightseam."+name, value.Int())}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return []attribute.KeyValue{attribute.Int64("nightseam."+name, int64(value.Uint()))}
	case reflect.Float32, reflect.Float64:
		return []attribute.KeyValue{attribute.Float64("nightseam."+name, value.Float())}
	}
	return nil
}

// traced is the trace a frame carried, each member left out where it is
// empty: a span event says traceparent where there is one.
func traced(t runtime.Trace) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, 2)
	if t.Parent != "" {
		attributes = append(attributes, attribute.String("nightseam."+traceparentKey, t.Parent))
	}
	if t.State != "" {
		attributes = append(attributes, attribute.String("nightseam."+tracestateKey, t.State))
	}
	return attributes
}

// snake is a field's name in the lower snake case the attributes of the named
// cases are spelled in: ID is id, ErrorCode is error_code.
func snake(name string) string {
	runes := []rune(name)
	spelled := make([]byte, 0, len(runes)+4)
	for i, r := range runes {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			previous := runes[i-1]
			lowerFollows := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if (previous >= 'a' && previous <= 'z') || (previous >= '0' && previous <= '9') || lowerFollows {
				spelled = append(spelled, '_')
			}
		}
		if upper {
			r += 'a' - 'A'
		}
		spelled = append(spelled, byte(r))
	}
	return string(spelled)
}
