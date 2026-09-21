package runtime

import (
	"errors"
	"slices"
	"sync"

	"github.com/Bitspark/nightseam/duplex/go"
)

// HandlerRegistry is the explicit registration capability used by generated
// bindings. Closing it releases its registrations, not its borrowed carrier.
type HandlerRegistry interface {
	duplex.Wire
	Register([]string, duplex.Receiver) (func(), error)
	Close(duplex.Code, string) error
}

type dispatchRegistration struct {
	path     []string
	receiver duplex.Receiver
}
type dispatchRoute struct {
	name   string
	prefix bool
}
type dispatchCaptureKey struct {
	owner *Dispatcher
	path  string
}
type dispatchCapture struct {
	registration *dispatchRegistration
	path         []string
}

// Each admitted runtime call owns this state. It is not a dispatcher-wide
// correlation table. A new carrier admission gets a fresh state even when it
// inherits verified context from an upstream call.
type wireRouteContext struct {
	mu       sync.Mutex
	retired  bool
	captures map[dispatchCaptureKey]dispatchCapture
}

func newWireRouteContext() *wireRouteContext {
	return &wireRouteContext{captures: map[dispatchCaptureKey]dispatchCapture{}}
}
func (c *wireRouteContext) retire() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.retired = true
	c.captures = nil
	c.mu.Unlock()
}
func routeContext(message duplex.Message) *wireRouteContext {
	if message.Return != nil {
		if owner, ok := message.Return.Wire.(interface{ wireDispatch() *wireDispatchContext }); ok {
			if context := owner.wireDispatch(); context != nil {
				return context.routes
			}
		}
	}
	return nil
}

// Dispatcher owns one endpoint attachment and an explicit exact/longest-prefix
// routing policy. Invocation routing uses the Nightseam profile's admitted-call
// lifetime association; an unmanaged request is refused, never silently given
// weaker detach/cancellation guarantees. Opaque endpoint wrappers are supported
// because the association accompanies the unchanged return capability.
type Dispatcher struct {
	root   duplex.Endpoint
	mu     sync.Mutex
	closed bool
	detach func()
	routes map[dispatchRoute]*dispatchRegistration
}

func NewDispatcher(root duplex.Endpoint) (*Dispatcher, error) {
	if root == nil {
		return nil, errors.New("dispatcher requires an endpoint")
	}
	d := &Dispatcher{root: root, routes: map[dispatchRoute]*dispatchRegistration{}}
	detach, err := root.Receive(duplex.Receiver{Message: d.deliver, Closed: func(code duplex.Code, reason string) { _ = d.Close(code, reason) }})
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	closed := d.closed
	if !closed {
		d.detach = detach
	}
	d.mu.Unlock()
	if closed {
		detach()
		return nil, ErrClosed
	}
	return d, nil
}

func (d *Dispatcher) Send(path []string, message duplex.Message) error {
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return ErrClosed
	}
	return d.root.Send(path, message)
}
func (d *Dispatcher) Register(path []string, receiver duplex.Receiver) (func(), error) {
	return d.register(path, receiver, false)
}
func (d *Dispatcher) RegisterPrefix(path []string, receiver duplex.Receiver) (func(), error) {
	return d.register(path, receiver, true)
}
func (d *Dispatcher) register(path []string, receiver duplex.Receiver, prefix bool) (func(), error) {
	name, err := duplex.EncodePath(path)
	if err != nil {
		return nil, err
	}
	key := dispatchRoute{name, prefix}
	registration := &dispatchRegistration{path: slices.Clone(path), receiver: receiver}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrClosed
	}
	if d.routes[key] != nil {
		return nil, duplex.ErrReceiverExists
	}
	d.routes[key] = registration
	return func() {
		d.mu.Lock()
		if d.routes[key] == registration {
			delete(d.routes, key)
		}
		d.mu.Unlock()
	}, nil
}
func (d *Dispatcher) match(path []string, name string) *dispatchRegistration {
	if exact := d.routes[dispatchRoute{name, false}]; exact != nil {
		return exact
	}
	var selected *dispatchRegistration
	for key, candidate := range d.routes {
		if key.prefix && len(candidate.path) <= len(path) && (selected == nil || len(candidate.path) > len(selected.path)) && slices.Equal(candidate.path, path[:len(candidate.path)]) {
			selected = candidate
		}
	}
	return selected
}
func (d *Dispatcher) deliver(path []string, message duplex.Message) {
	name, err := duplex.EncodePath(path)
	if err != nil {
		return
	}
	key := dispatchCaptureKey{d, name}
	context := routeContext(message)
	if message.Frame.Kind == duplex.ProfileCancel {
		if context == nil {
			return
		}
		context.mu.Lock()
		capture, exists := context.captures[key]
		context.mu.Unlock()
		if exists && capture.registration.receiver.Message != nil {
			capture.registration.receiver.Message(slices.Clone(capture.path), message)
		}
		return
	}
	if message.Frame.Kind == duplex.ProfileRequest && context == nil {
		_ = sendWireResponse(message, nil, &PublicError{Code: "invalid_message", Message: "Invocation requires a profile-owned lifetime association"})
		return
	}
	d.mu.Lock()
	var registration *dispatchRegistration
	if !d.closed {
		registration = d.match(path, name)
	}
	if registration != nil && message.Frame.Kind == duplex.ProfileRequest {
		context.mu.Lock()
		if context.retired {
			registration = nil
		} else if previous, exists := context.captures[key]; exists {
			registration = previous.registration
		} else {
			context.captures[key] = dispatchCapture{registration, slices.Clone(path)}
		}
		context.mu.Unlock()
	}
	d.mu.Unlock()
	if registration == nil || registration.receiver.Message == nil {
		if message.Frame.Kind == duplex.ProfileRequest {
			_ = sendWireResponse(message, nil, &PublicError{Code: "method_not_found", Message: "Unknown method"})
		}
		return
	}
	registration.receiver.Message(slices.Clone(path), message)
}

func (d *Dispatcher) Close(code duplex.Code, reason string) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	detach, routes := d.detach, d.routes
	d.detach, d.routes = nil, nil
	d.mu.Unlock()
	if detach != nil {
		detach()
	}
	for _, registration := range routes {
		if registration.receiver.Closed != nil {
			func() { defer func() { _ = recover() }(); registration.receiver.Closed(code, reason) }()
		}
	}
	return nil
}

// Select returns a receiving view of this shared dispatcher. The view owns its
// prefix route and never acquires closure authority over the root endpoint.
func (d *Dispatcher) Select(path []string) *SelectedEndpoint {
	return &SelectedEndpoint{owner: d, prefix: slices.Clone(path)}
}

type SelectedEndpoint struct {
	owner      *Dispatcher
	prefix     []string
	mu         sync.Mutex
	closed     bool
	attachment *selectedAttachment
}
type selectedAttachment struct {
	receiver duplex.Receiver
	detach   func()
}

func (s *SelectedEndpoint) Select(path []string) *SelectedEndpoint {
	return s.owner.Select(append(slices.Clone(s.prefix), path...))
}
func (s *SelectedEndpoint) Send(path []string, message duplex.Message) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrClosed
	}
	return s.owner.Send(append(slices.Clone(s.prefix), path...), message)
}
func (s *SelectedEndpoint) Receive(receiver duplex.Receiver) (func(), error) {
	attachment := &selectedAttachment{receiver: receiver}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if s.attachment != nil {
		s.mu.Unlock()
		return nil, duplex.ErrReceiverExists
	}
	s.attachment = attachment
	s.mu.Unlock()
	detach, err := s.owner.RegisterPrefix(s.prefix, duplex.Receiver{
		Message: func(path []string, message duplex.Message) {
			if receiver.Message != nil {
				receiver.Message(slices.Clone(path[len(s.prefix):]), message)
			} else if message.Frame.Kind == duplex.ProfileRequest {
				_ = sendWireResponse(message, nil, &PublicError{Code: "method_not_found", Message: "Unknown method"})
			}
		},
		Closed: func(code duplex.Code, reason string) { s.remove(attachment, true, code, reason) },
	})
	s.mu.Lock()
	active := s.attachment == attachment
	if err != nil && active {
		s.attachment = nil
	}
	if err == nil && active {
		attachment.detach = detach
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !active {
		detach()
		return nil, ErrClosed
	}
	return func() { s.remove(attachment, false, 0, "") }, nil
}
func (s *SelectedEndpoint) remove(attachment *selectedAttachment, tell bool, code duplex.Code, reason string) {
	s.mu.Lock()
	if s.attachment != attachment {
		s.mu.Unlock()
		return
	}
	s.attachment = nil
	detach := attachment.detach
	s.mu.Unlock()
	if detach != nil {
		detach()
	}
	if tell && attachment.receiver.Closed != nil {
		attachment.receiver.Closed(code, reason)
	}
}
func (s *SelectedEndpoint) Close(code duplex.Code, reason string) error {
	s.mu.Lock()
	s.closed = true
	attachment := s.attachment
	s.mu.Unlock()
	if attachment != nil {
		s.remove(attachment, true, code, reason)
	}
	return nil
}
