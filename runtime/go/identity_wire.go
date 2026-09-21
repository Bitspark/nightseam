package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

// IdentityPreparation installs model receivers before a carrier starts reading,
// checks its declaration after attachment, and releases dispatch after binding.
// It owns its registrations, never the source carrier or another receive queue.
type IdentityPreparation struct {
	source                  duplex.Wire
	expected                DeclarationIdentity
	limit                   int
	mu                      sync.Mutex
	started, checked, ready bool
	err                     error
	released                chan struct{}
	releaseOnce             sync.Once
	ctx                     context.Context
	cancel                  context.CancelFunc
	timer                   *time.Timer
	identityDetach          func()
	registrations           map[*identityRegistration]struct{}
	pending                 map[returnKey]*identityDelivery
}

type identityRegistration struct {
	receiver duplex.Receiver
	detach   func()
	active   bool
	closed   sync.Once
}

type identityDelivery struct {
	mu                   sync.Mutex
	registration         *identityRegistration
	path                 []string
	message              duplex.Message
	cancelled, forwarded bool
	cancelledSignal      chan struct{}
}

type identityWire struct{ preparation *IdentityPreparation }
type identityWatchWire struct {
	duplex.Wire
	preparation *IdentityPreparation
}

func (w identityWatchWire) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	closed := receiver.Closed
	receiver.Closed = func(code duplex.Code, reason string) {
		defer w.preparation.fail(ErrClosed)
		if closed != nil {
			closed(code, reason)
		}
	}
	return w.Wire.Receive(path, receiver)
}

// PrepareIdentity is synchronous: callers install receivers through Wire before
// attaching their carrier, then call Check and bind the model before Ready.
// RequestTimeout bounds the whole preparation, including a factory never bound.
func PrepareIdentity(wire duplex.Wire, expected DeclarationIdentity, options Options) (*IdentityPreparation, error) {
	if wire == nil {
		return nil, errors.New("identity preparation requires a wire")
	}
	if err := validateIdentity(expected); err != nil {
		return nil, err
	}
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &IdentityPreparation{source: wire, expected: expected, limit: options.MaxConcurrentHandlers, released: make(chan struct{}), ctx: ctx, cancel: cancel, registrations: map[*identityRegistration]struct{}{}, pending: map[returnKey]*identityDelivery{}}
	handler, _ := IdentityHandler(expected)
	detach, err := HandleWire(identityWatchWire{wire, p}, []string{IdentityMethod}, func(ctx context.Context, raw json.RawMessage) (any, error) { return handler(ctx, nil, raw) })
	if err != nil {
		cancel()
		return nil, err
	}
	p.mu.Lock()
	p.identityDetach = detach
	if p.err != nil {
		err = p.err
		p.mu.Unlock()
		detach()
		return nil, err
	}
	p.timer = time.AfterFunc(options.RequestTimeout, func() { p.fail(context.DeadlineExceeded) })
	p.mu.Unlock()
	return p, nil
}

// Wire registers on the source directly and holds only model dispatch. Its
// outgoing sends require readiness; Check uses the ungated source separately.
func (p *IdentityPreparation) Wire() duplex.Wire { return &identityWire{preparation: p} }

// Check makes the interpretation's first request. It may be started only once.
// A failed or cancelled check releases waiting deliveries without dispatching.
func (p *IdentityPreparation) Check(ctx context.Context) error {
	if ctx == nil {
		return errors.New("identity check requires a context")
	}
	p.mu.Lock()
	if p.err != nil {
		err := p.err
		p.mu.Unlock()
		return err
	}
	if p.started {
		p.mu.Unlock()
		return errors.New("identity check already started")
	}
	p.started = true
	p.mu.Unlock()
	checking, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer func() { stop(); cancel() }()
	err := CheckIdentity(checking, func(ctx context.Context, method string, params, result any) error {
		return CallWire(ctx, p.source, []string{method}, params, result)
	}, p.expected)
	if err != nil {
		p.fail(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.checked = true
	return nil
}

// Ready releases dispatch after a successful check and model binding.
func (p *IdentityPreparation) Ready() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	if !p.checked {
		return errors.New("identity has not been checked")
	}
	if p.ready {
		return errors.New("identity preparation is already ready")
	}
	p.ready = true
	if p.timer != nil {
		p.timer.Stop()
	}
	p.releaseOnce.Do(func() { close(p.released) })
	return nil
}

// Close abandons only this interpretation, including its deferred deliveries.
// The source wire and unrelated registrations remain usable.
func (p *IdentityPreparation) Close() { p.fail(ErrClosed) }

func (p *IdentityPreparation) fail(err error) {
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return
	}
	p.err = err
	if p.timer != nil {
		p.timer.Stop()
	}
	identityDetach := p.identityDetach
	type ownedRegistration struct {
		registration *identityRegistration
		detach       func()
	}
	registrations := make([]ownedRegistration, 0, len(p.registrations))
	for registration := range p.registrations {
		registration.active = false
		registrations = append(registrations, ownedRegistration{registration, registration.detach})
	}
	p.registrations = map[*identityRegistration]struct{}{}
	p.mu.Unlock()
	p.cancel()
	if identityDetach != nil {
		identityDetach()
	}
	for _, owned := range registrations {
		if owned.detach != nil {
			owned.detach()
		}
		registration := owned.registration
		registration.closed.Do(func() {
			defer func() { _ = recover() }()
			if registration.receiver.Closed != nil {
				registration.receiver.Closed(duplex.CodeNormal, "interpretation ended")
			}
		})
	}
	p.releaseOnce.Do(func() { close(p.released) })
}

func (w *identityWire) Send(path []string, message duplex.Message) error {
	p := w.preparation
	p.mu.Lock()
	err, ready := p.err, p.ready
	p.mu.Unlock()
	if err != nil {
		return err
	}
	if !ready {
		return &PublicError{Code: "busy", Message: "declaration interpretation is not ready"}
	}
	return p.source.Send(path, message)
}

// Closing an actual wire preserves normal carrier ownership. Close on the
// preparation itself is the operation for abandoning only this interpretation.
func (w *identityWire) Close(code duplex.Code, reason string) error {
	w.preparation.Close()
	return w.preparation.source.Close(code, reason)
}

func (w *identityWire) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	if receiver.Message == nil {
		return nil, errors.New("a wire receiver requires a callback")
	}
	p := w.preparation
	registration := &identityRegistration{receiver: receiver, active: true}
	p.mu.Lock()
	if p.err != nil {
		err := p.err
		p.mu.Unlock()
		return nil, err
	}
	p.registrations[registration] = struct{}{}
	p.mu.Unlock()
	detach, err := p.source.Receive(path, duplex.Receiver{Namespace: receiver.Namespace,
		Message: func(path []string, message duplex.Message) { p.deliver(registration, path, message) },
		Closed:  func(duplex.Code, string) { p.fail(ErrClosed) },
	})
	p.mu.Lock()
	registration.detach = detach
	if err != nil {
		registration.active = false
		delete(p.registrations, registration)
	}
	failed := p.err
	p.mu.Unlock()
	if failed != nil && detach != nil {
		detach()
	}
	if err != nil {
		return nil, err
	}
	if failed != nil {
		return nil, failed
	}
	return func() {
		p.mu.Lock()
		registration.active = false
		delete(p.registrations, registration)
		p.mu.Unlock()
		detach()
	}, nil
}

func (p *IdentityPreparation) deliver(registration *identityRegistration, path []string, message duplex.Message) {
	key := returnKey{message.Return, message.Frame.ID}
	p.mu.Lock()
	if message.Frame.Kind == duplex.ProfileCancel {
		delivery := p.pending[key]
		ready := p.ready && p.err == nil && registration.active
		p.mu.Unlock()
		if delivery != nil {
			delivery.mu.Lock()
			if delivery.forwarded {
				delivery.registration.receiver.Message(path, message)
			} else if !delivery.cancelled {
				delivery.cancelled = true
				close(delivery.cancelledSignal)
				sendWireResponse(delivery.message, nil, context.Canceled)
			}
			delivery.mu.Unlock()
		} else if ready {
			registration.receiver.Message(path, message)
		}
		return
	}
	err, ready := p.err, p.ready
	if !registration.active && err == nil {
		err = ErrClosed
	}
	if err != nil || ready {
		p.mu.Unlock()
		if err != nil {
			if message.Frame.Kind == duplex.ProfileRequest {
				sendWireResponse(message, nil, err)
			}
			return
		}
		registration.receiver.Message(path, message)
		return
	}
	if message.Frame.Kind == duplex.ProfileEvent {
		p.mu.Unlock()
		<-p.released
		p.mu.Lock()
		ready = p.ready && p.err == nil && registration.active
		p.mu.Unlock()
		// A refused interpretation discards its held event. Returning an event
		// error would close a shared carrier through RegisterWire.
		if ready {
			registration.receiver.Message(path, message)
		}
		return
	}
	if message.Frame.Kind != duplex.ProfileRequest {
		p.mu.Unlock()
		return
	}
	if p.pending[key] != nil {
		p.mu.Unlock()
		sendWireResponse(message, nil, &PublicError{Code: "invalid_message", Message: "Duplicate active request identifier"})
		return
	}
	if len(p.pending) >= p.limit {
		p.mu.Unlock()
		sendWireResponse(message, nil, &PublicError{Code: "busy", Message: "Too many deferred model requests"})
		return
	}
	delivery := &identityDelivery{registration: registration, path: path, message: message, cancelledSignal: make(chan struct{})}
	p.pending[key] = delivery
	p.mu.Unlock()
	// A local root delivers request callbacks serially. Holding that callback
	// would block later identity requests; defer only within the existing
	// admitted-request budget, retaining cancellation ordering until release.
	go func() {
		defer func() {
			p.mu.Lock()
			if p.pending[key] == delivery {
				delete(p.pending, key)
			}
			p.mu.Unlock()
		}()
		select {
		case <-p.released:
		case <-delivery.cancelledSignal:
		}
		delivery.mu.Lock()
		defer delivery.mu.Unlock()
		if delivery.cancelled {
			return
		}
		p.mu.Lock()
		err := p.err
		if !registration.active && err == nil {
			err = ErrClosed
		}
		p.mu.Unlock()
		if err != nil {
			sendWireResponse(message, nil, err)
			return
		}
		defer func() {
			if recover() != nil {
				sendWireResponse(message, nil, errors.New("wire receiver panic"))
			}
		}()
		registration.receiver.Message(path, message)
		delivery.forwarded = true
	}()
}
