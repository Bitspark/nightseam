package live

import (
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
)

// The events below are what a scope tells about the bindings over it. A scope
// observes through the peer it runs over and takes no observer of its own: what
// the connection does and what the bindings over it do reach one observer, in
// the order they happened.
//
// Never a payload: what a callable is asked and what it answers is the business
// of the two sides of it, and no byte of a request or a result reaches an event
// here. A binding is named by its id, which is an address as a channel's id is.

// LiveExported is a binding coming into being, on the side that exported it.
type LiveExported struct {
	At       time.Time
	Contract string
	Binding  string
}

// LiveImported is an attachment to a binding being made here, once per binding:
// the same binding imported again reaches the attachment that already exists
// and says nothing a second time.
type LiveImported struct {
	At       time.Time
	Contract string
	Binding  string
}

// LiveReleased is a binding ending, once and whatever ended it — a release
// here, or the other side's word that it released. A scope that ends releases
// nothing one at a time: the connection ended, and that is the connection's
// event to report.
type LiveReleased struct {
	At       time.Time
	Contract string
	Binding  string
}

// LiveRefused is an export, an import or an invocation that did not happen, on
// the side that refused it. Code is the public code the refusal carries and
// Reason is what it said; it names no binding, since the point of most of them
// is that no binding answers to the name.
type LiveRefused struct {
	At       time.Time
	Contract string
	Code     string
	Reason   string
}

func (LiveExported) ObserverEvent() {}
func (LiveImported) ObserverEvent() {}
func (LiveReleased) ObserverEvent() {}
func (LiveRefused) ObserverEvent()  {}

// The events of this layer are events of the runtime's observer, which is the
// only observer there is.
var (
	_ runtime.ObserverEvent = LiveExported{}
	_ runtime.ObserverEvent = LiveImported{}
	_ runtime.ObserverEvent = LiveReleased{}
	_ runtime.ObserverEvent = LiveRefused{}
)

// Every hook point below asks the peer for an observer before it builds
// anything: a scope over a peer given none reads no clock and allocates no
// event for nobody, as the runtime's own hook points do.

func (s *Scope) observeExported(contract, id string) {
	if s.peer.Observer() == nil {
		return
	}
	s.peer.Observe(LiveExported{At: time.Now(), Contract: contract, Binding: id})
}

func (s *Scope) observeImported(contract, id string) {
	if s.peer.Observer() == nil {
		return
	}
	s.peer.Observe(LiveImported{At: time.Now(), Contract: contract, Binding: id})
}

func (s *Scope) observeReleased(contract, id string) {
	if s.peer.Observer() == nil {
		return
	}
	s.peer.Observe(LiveReleased{At: time.Now(), Contract: contract, Binding: id})
}

func (s *Scope) observeRefused(contract, code, reason string) {
	if s.peer.Observer() == nil {
		return
	}
	s.peer.Observe(LiveRefused{At: time.Now(), Contract: contract, Code: code, Reason: reason})
}
