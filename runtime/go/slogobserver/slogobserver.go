// Package slogobserver writes what a peer tells an observer to a slog.Logger:
// one record per event, at a level per kind, with the event's fields as
// attributes. It is the one shipped Go adapter of runtime.Observer, and it
// lives in the core module because log/slog is the standard library — nothing
// else joins it here, and a backend of any other kind is a module of its own.
//
// A record carries what an observer is told and nothing more: names, ids,
// sizes, durations, outcomes and close codes, never a payload. Diagnostic
// logging is one observer among others, so a consumer that wants none installs
// none and the runtime writes to no logger of its own.
package slogobserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
)

// New is an Observer writing each event to logger as one record, timed at the
// event's own At rather than at the moment the record is written: the adapter
// reads no clock of its own except for an event that carries no time it knows.
// A nil logger is slog.Default, an observer being no reason for a peer to
// panic on a goroutine the consumer does not own.
func New(logger *slog.Logger) runtime.Observer {
	if logger == nil {
		logger = slog.Default()
	}
	return observer{logger: logger}
}

// observer keeps nothing, which is what an adapter of this kind must not do:
// it emits and never aggregates, and so it is safe on every goroutine a peer
// observes from without a lock of its own.
type observer struct{ logger *slog.Logger }

// Observe is the type switch a consumer of the Observer dispatches by, written
// once. The level is the kind's: a connection opening and closing is what an
// operator reads without asking for more, a request that ended in an error is
// a warning, a handler that gave up is an error, and the traffic itself —
// frames, events, backpressure — is the detail under it.
func (o observer) Observe(event runtime.ObserverEvent) {
	switch e := event.(type) {
	case runtime.ConnectionOpened:
		o.log(e.At, slog.LevelInfo, "connection opened", slog.String("role", string(e.Role)))
	case runtime.ConnectionClosed:
		o.log(e.At, slog.LevelInfo, "connection closed",
			slog.Int("code", e.Code), slog.String("reason", e.Reason), slog.Bool("local", e.Local))
	case runtime.FrameSent:
		o.log(e.At, slog.LevelDebug, "frame sent", attrs([]slog.Attr{
			slog.String("kind", e.Kind), slog.String("name", e.Name),
			slog.Int("bytes", e.Bytes), slog.String("id", e.ID),
		}, e.Trace, e.Family)...)
	case runtime.FrameReceived:
		o.log(e.At, slog.LevelDebug, "frame received", attrs([]slog.Attr{
			slog.String("kind", e.Kind), slog.String("name", e.Name),
			slog.Int("bytes", e.Bytes), slog.String("id", e.ID),
		}, e.Trace, e.Family)...)
	case runtime.RequestStarted:
		o.log(e.At, slog.LevelDebug, "request started", attrs([]slog.Attr{
			slog.String("id", e.ID), slog.String("method", e.Method), slog.Bool("incoming", e.Incoming),
		}, e.Trace, e.Family)...)
	case runtime.RequestEnded:
		// A request that ended in an error is the one an operator is looking
		// for; a cancellation and a deadline are the caller's own doing, which
		// the caller already knows, and read as the traffic they are.
		level := slog.LevelDebug
		if e.Outcome == runtime.OutcomeErrored {
			level = slog.LevelWarn
		}
		o.log(e.At, level, "request ended", attrs([]slog.Attr{
			slog.String("id", e.ID), slog.String("method", e.Method), slog.Bool("incoming", e.Incoming),
			slog.Duration("duration", e.Duration), slog.String("outcome", e.Outcome.String()),
			slog.String("error_code", e.ErrorCode),
		}, e.Trace, e.Family)...)
	case runtime.EventEmitted:
		o.log(e.At, slog.LevelDebug, "event emitted", attrs([]slog.Attr{
			slog.String("name", e.Name), slog.Int("bytes", e.Bytes),
		}, e.Trace, e.Family)...)
	case runtime.EventDelivered:
		o.log(e.At, slog.LevelDebug, "event delivered", attrs([]slog.Attr{
			slog.String("name", e.Name), slog.Int("bytes", e.Bytes),
		}, e.Trace, e.Family)...)
	case runtime.Backpressure:
		o.log(e.At, slog.LevelDebug, "backpressure",
			slog.Int("queued", e.Queued), slog.Bool("stalled", e.Stalled), slog.Duration("deadline", e.Deadline))
	case runtime.HandlerPanic:
		o.log(e.At, slog.LevelError, "handler panic", attrs([]slog.Attr{
			slog.String("method", e.Method), slog.String("value", e.Value),
		}, e.Trace, e.Family)...)
	default:
		// An event this package has no case for is logged as what it is rather
		// than dropped: its Go type name and whatever fields it has, so that a
		// tunnel's events, a live scope's, a later profile's appear here before
		// this package knows anything about them. Its time is this adapter's,
		// the At such an event carries being no field this switch can read.
		o.log(time.Now(), slog.LevelDebug, fmt.Sprintf("%T", event), slog.Any("event", event))
	}
}

// attrs is the head of an event's attributes followed by the trace the frame
// carried and the family its name belongs to, each left out where it is empty:
// a record says traceparent where there is one, and an unlabelled name says
// nothing about a family rather than saying it has none.
func attrs(head []slog.Attr, trace runtime.Trace, family string) []slog.Attr {
	if trace.Parent != "" {
		head = append(head, slog.String("traceparent", trace.Parent))
	}
	if trace.State != "" {
		head = append(head, slog.String("tracestate", trace.State))
	}
	if family != "" {
		head = append(head, slog.String("family", family))
	}
	return head
}

// log writes one record, asking the handler first so that an adapter under a
// level nobody reads builds nothing on the goroutine it was called from, and
// dating it at when the event happened rather than at when it is written.
func (o observer) log(at time.Time, level slog.Level, message string, attributes ...slog.Attr) {
	handler := o.logger.Handler()
	ctx := context.Background()
	if !handler.Enabled(ctx, level) {
		return
	}
	record := slog.NewRecord(at, level, message, 0)
	record.AddAttrs(attributes...)
	_ = handler.Handle(ctx, record)
}
