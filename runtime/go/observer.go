package runtime

import (
	"context"
	"errors"
	"fmt"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

// Observer is what a peer tells about the traffic it carries. It emits and
// never aggregates, and it chooses no backend: an observer sees names, ids,
// sizes, durations, outcomes and close codes — never a payload. A params
// object reaches no observer by any path, and diagnostic logging is one
// observer among others; the runtime writes to no logger of its own.
//
// Observe is called on whichever goroutine the event happened on — the
// reader, a handler, a caller — and so from several at once: an observer that
// keeps anything keeps it under a lock of its own, and one that blocks holds
// up the connection it is watching. One that panics panics alone: the peer
// recovers it, loses that event and carries on, a diagnostic being no reason
// for a connection to end on a goroutine the consumer does not own.
type Observer interface{ Observe(event ObserverEvent) }

// ObserverEvent is one thing a peer did, or one thing a layer running over a
// peer did: it is implemented by the events of this package, by those of the
// tunnel and of the live layer, which reach an observer through the peer they
// run over, and by nothing else. A type switch is the consumer's dispatch,
// and a later profile or a later layer may add a case to it. It is not named
// Event because an Event of this package is one event of the profile, which
// an observer only ever hears about.
type ObserverEvent interface{ ObserverEvent() }

// ConnectionOpened is the peer taking the connection over, before it has read
// or written anything on it.
type ConnectionOpened struct {
	At   time.Time
	Role Role
}

// ConnectionClosed is the connection ending, once and whatever ended it. Local
// says this side ended it, which is every case but the one a peer can lay at
// the other's door: a close the remote sent, with its code and its reason.
type ConnectionClosed struct {
	At     time.Time
	Code   int
	Reason string
	Local  bool
}

// FrameSent is one frame handed to the transport, which is as far as this peer
// carries it: told by the writer immediately before the bytes leave, so that a
// frame is observed sent before anything it draws can be received. Name is what
// the frame names — the method of a request, the name of an event — and a
// response and a cancel name nothing, a response's method being no member of
// the wire.
type FrameSent struct {
	At     time.Time
	Kind   string
	Name   string
	Bytes  int
	ID     string
	Trace  Trace
	Family string
}

// FrameReceived is one frame the decoder accepted, before anything routed it.
// A frame the decoder refused closes the connection and is no frame at all.
type FrameReceived struct {
	At     time.Time
	Kind   string
	Name   string
	Bytes  int
	ID     string
	Trace  Trace
	Family string
}

// RequestStarted is a request beginning, whichever side raised it: Incoming is
// one this peer serves, and a request it refuses for want of a method or of a
// slot begins and ends like any other.
type RequestStarted struct {
	At       time.Time
	ID       string
	Method   string
	Incoming bool
	Trace    Trace
	Family   string
}

// RequestEnded pairs with every RequestStarted. Duration is the span between
// the two, and ErrorCode names the local cause: request_timeout for a deadline,
// cancelled for a cancellation, or the public error's code for a refusal.
// These codes hold for incoming and outgoing requests; success names no code.
type RequestEnded struct {
	At        time.Time
	ID        string
	Method    string
	Incoming  bool
	Duration  time.Duration
	Outcome   Outcome
	ErrorCode string
	Trace     Trace
	Family    string
}

// EventEmitted is the application emitting an event, before the frame carrying
// it is queued. Bytes is the size of the event's data, as EventDelivered's is.
type EventEmitted struct {
	At     time.Time
	Name   string
	Bytes  int
	Trace  Trace
	Family string
}

// EventDelivered is an event reaching the application, before its handler and
// the listeners run.
type EventDelivered struct {
	At     time.Time
	Name   string
	Bytes  int
	Trace  Trace
	Family string
}

// Backpressure is a queue that could not take a frame: Queued is its depth,
// Deadline the write deadline the producer then waited out where it waited,
// and Stalled the peer giving up on the consumer and closing the connection.
type Backpressure struct {
	At       time.Time
	Queued   int
	Stalled  bool
	Deadline time.Duration
}

// HandlerPanic is a method handler that gave up. Value is the panic value as
// %v renders it and nothing else: what the handler was given is the handler's,
// and reaches no observer here.
type HandlerPanic struct {
	At     time.Time
	Method string
	Value  string
	Trace  Trace
	Family string
}

func (ConnectionOpened) ObserverEvent() {}
func (ConnectionClosed) ObserverEvent() {}
func (FrameSent) ObserverEvent()        {}
func (FrameReceived) ObserverEvent()    {}
func (RequestStarted) ObserverEvent()   {}
func (RequestEnded) ObserverEvent()     {}
func (EventEmitted) ObserverEvent()     {}
func (EventDelivered) ObserverEvent()   {}
func (Backpressure) ObserverEvent()     {}
func (HandlerPanic) ObserverEvent()     {}

// Outcome is how a request ended.
type Outcome int

const (
	// OutcomeOK is a request answered with a result.
	OutcomeOK Outcome = iota
	// OutcomeErrored is a request answered with an error.
	OutcomeErrored
	// OutcomeCancelled is a request the caller withdrew before it was answered.
	OutcomeCancelled
	// OutcomeTimedOut is a request that outlived its deadline.
	OutcomeTimedOut
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "ok"
	case OutcomeErrored:
		return "error"
	case OutcomeCancelled:
		return "cancelled"
	case OutcomeTimedOut:
		return "timeout"
	}
	return fmt.Sprintf("outcome(%d)", int(o))
}

// outcomeOf reads an outcome from what ended the request, and the public code
// where it ended in one. A remote peer's refusal arrives as a public error
// whatever it says, so a response of code cancelled is the caller's error and
// not the caller's cancellation.
func outcomeOf(err error) (Outcome, string) {
	var public *PublicError
	switch {
	case err == nil:
		return OutcomeOK, ""
	case errors.As(err, &public) && public != nil:
		return OutcomeErrored, public.Code
	case errors.Is(err, context.DeadlineExceeded):
		return OutcomeTimedOut, "request_timeout"
	case errors.Is(err, context.Canceled):
		return OutcomeCancelled, "cancelled"
	}
	return OutcomeErrored, ""
}

// Observer is what this peer was given, or nil. Whatever runs over a peer — a
// tunnel, a live scope — observes through this one rather than taking its own.
func (p *Peer) Observer() Observer { return p.options.Observer }

// Observe tells this peer's observer one event, the runtime's own or one of a
// layer running over the peer; a peer given no observer does nothing. It is
// how a tunnel and a live scope observe — through the peer they run over, which
// is the observer they were never given a second way to choose.
func (p *Peer) Observe(event ObserverEvent) {
	if p.options.Observer == nil {
		return
	}
	p.emit(event)
}

// emit is the one place an observer is called from, and the panic it may
// raise ends here rather than where the event happened: Observe runs on
// whichever goroutine the traffic did — the reader, a handler, a caller —
// none of which the consumer owns, and an observer that gave up there would
// take the connection with it, which is a diagnostic deciding whether a peer
// carries traffic. The event is lost and nothing else is: an observer emits
// and never aggregates, so a peer has no state of its own to repair on one's
// behalf and nothing to tell it about the loss that would not go the same
// way. A panicking observer is a bug in the observer, and nothing here reports
// it: a peer that logged one would be choosing the backend it says it chooses
// none of.
func (p *Peer) emit(event ObserverEvent) {
	defer func() { _ = recover() }()
	p.options.Observer.Observe(event)
}

// family is the family a method or event name belongs to, as the generated
// install labelled it. An unlabelled name has no family rather than a guessed
// one: the runtime parses no names.
func (p *Peer) family(name string) string { return p.options.Families[name] }

// named is what a frame names; trace is the W3C members it carries, verbatim.
func (f frame) named() string {
	if f.Event != "" {
		return f.Event
	}
	return f.Method
}

func (f frame) traced() Trace { return Trace{Parent: f.Traceparent, State: f.Tracestate} }

// Every hook point below asks for an observer before it builds anything: a
// peer given none reads no clock and allocates no event for nobody.

func (p *Peer) observeOpened(role Role) {
	if p.options.Observer == nil {
		return
	}
	p.emit(ConnectionOpened{At: time.Now(), Role: role})
}

// observeClosed says what ended the connection, under the code the wire
// carried and no other: the close this side sent, 1006 where it aborted and
// sent nothing at all, or the remote's own where the remote closed first. A
// code an operator reads here is one a gateway between the two read as well.
func (p *Peer) observeClosed(err error, code bitwire.Code, reason string) {
	if p.options.Observer == nil {
		return
	}
	closed := ConnectionClosed{At: time.Now(), Code: int(duplex.CodeAbnormalClosure), Local: true}
	if code != codeAborted {
		closed.Code, closed.Reason = int(code), reason
	}
	var remote *duplex.CloseError
	if errors.As(err, &remote) {
		closed.Code, closed.Reason, closed.Local = int(remote.Code), remote.Reason, false
	}
	p.emit(closed)
}

func (p *Peer) observeSent(f frame, bytes int) {
	if p.options.Observer == nil {
		return
	}
	name := f.named()
	p.emit(FrameSent{At: time.Now(), Kind: f.Kind, Name: name, Bytes: bytes,
		ID: f.ID, Trace: f.traced(), Family: p.family(name)})
}

func (p *Peer) observeReceived(f frame, bytes int) {
	if p.options.Observer == nil {
		return
	}
	name := f.named()
	p.emit(FrameReceived{At: time.Now(), Kind: f.Kind, Name: name, Bytes: bytes,
		ID: f.ID, Trace: f.traced(), Family: p.family(name)})
}

// requestStarted returns when the request began, which requestEnded measures
// its duration from; a peer with no observer measures nothing.
func (p *Peer) requestStarted(id, method string, incoming bool, trace Trace) time.Time {
	if p.options.Observer == nil {
		return time.Time{}
	}
	at := time.Now()
	p.emit(RequestStarted{At: at, ID: id, Method: method, Incoming: incoming,
		Trace: trace, Family: p.family(method)})
	return at
}

func (p *Peer) requestEnded(started time.Time, id, method string, incoming bool, trace Trace, err error) {
	if p.options.Observer == nil {
		return
	}
	at := time.Now()
	outcome, code := outcomeOf(err)
	p.emit(RequestEnded{At: at, ID: id, Method: method, Incoming: incoming,
		Duration: at.Sub(started), Outcome: outcome, ErrorCode: code, Trace: trace, Family: p.family(method)})
}

func (p *Peer) observeEmitted(f frame) {
	if p.options.Observer == nil {
		return
	}
	p.emit(EventEmitted{At: time.Now(), Name: f.Event, Bytes: len(f.Data),
		Trace: f.traced(), Family: p.family(f.Event)})
}

func (p *Peer) observeDelivered(queued queuedEvent) {
	if p.options.Observer == nil {
		return
	}
	p.emit(EventDelivered{At: time.Now(), Name: queued.event.Name, Bytes: len(queued.event.Data),
		Trace: queued.trace, Family: p.family(queued.event.Name)})
}

func (p *Peer) observeBackpressure(queued int, stalled bool, deadline time.Duration) {
	if p.options.Observer == nil {
		return
	}
	p.emit(Backpressure{At: time.Now(), Queued: queued, Stalled: stalled, Deadline: deadline})
}

func (p *Peer) observePanic(f frame, value any) {
	if p.options.Observer == nil {
		return
	}
	p.emit(HandlerPanic{At: time.Now(), Method: f.Method, Value: fmt.Sprint(value),
		Trace: f.traced(), Family: p.family(f.Method)})
}
