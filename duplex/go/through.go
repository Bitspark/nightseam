package duplex

import (
	"sync"

	bitwire "github.com/Bitspark/bitwire/wire/go"
)

type throughWire struct {
	origin   bitwire.Endpoint
	access   bitwire.Wire
	mu       sync.Mutex
	closed   bool
	detach   func()
	receiver *bitwire.Receiver
}

// Through is an endpoint that receives on a borrowed origin and sends through
// separately composed access, such as declared bound access over that origin.
// Its single receive attachment borrows one attachment from origin. Closing it
// detaches that attachment and leaves both origin and access usable; an ending
// origin ends it too.
func Through(origin bitwire.Endpoint, access bitwire.Wire) bitwire.Endpoint {
	return &throughWire{origin: origin, access: access}
}

func (w *throughWire) Send(path []string, message bitwire.Message) error {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return ErrClosed
	}
	return w.access.Send(path, message)
}

func (w *throughWire) Receive(receiver bitwire.Receiver) (func(), error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, ErrClosed
	}
	if w.receiver != nil {
		w.mu.Unlock()
		return nil, ErrReceiverExists
	}
	attached := &receiver
	w.receiver = attached
	w.mu.Unlock()
	detach, err := w.origin.Receive(bitwire.Receiver{
		Message: receiver.Message,
		Closed:  func(code bitwire.Code, reason string) { w.end(attached, code, reason) },
	})
	w.mu.Lock()
	current := w.receiver == attached
	if err == nil && current {
		w.detach = detach
	}
	if err != nil && current {
		w.receiver = nil
	}
	w.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !current {
		// Closure raced the origin's Receive; its late disposer is still ours.
		detach()
		return nil, ErrClosed
	}
	return func() { w.release(attached) }, nil
}

func (w *throughWire) release(attached *bitwire.Receiver) {
	w.mu.Lock()
	if w.receiver != attached {
		w.mu.Unlock()
		return
	}
	detach := w.detach
	w.receiver, w.detach = nil, nil
	w.mu.Unlock()
	if detach != nil {
		detach()
	}
}

// end delivers the ending once and refuses later use; code and reason are the
// origin's when it ended first.
func (w *throughWire) end(attached *bitwire.Receiver, code bitwire.Code, reason string) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	current := w.receiver
	detach := w.detach
	w.receiver, w.detach = nil, nil
	w.mu.Unlock()
	if detach != nil {
		detach()
	}
	if current != nil && (attached == nil || current == attached) && current.Closed != nil {
		current.Closed(code, reason)
	}
}

func (w *throughWire) Close(code bitwire.Code, reason string) error {
	w.end(nil, code, reason)
	return nil
}
