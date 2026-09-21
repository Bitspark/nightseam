package runtime

import (
	"errors"
	"sync"

	"github.com/Bitspark/nightseam/duplex/go"
)

// RoutingFacility resolves only invocations admitted by its profile integration.
// A dispatcher receives this capability separately from execution authority.
type RoutingFacility interface {
	Resolve(duplex.Message) (InvocationRouting, error)
}

// ExecutionFacility grants explicit body participation to an execution owner.
// It does not derive admission or verified context from caller metadata.
type ExecutionFacility interface {
	Resolve(duplex.Message) (InvocationExecution, error)
}

type InvocationRouting interface {
	Capture(control func(duplex.Message)) (InvocationCapture, error)
}
type InvocationCapture interface{ Delivered() }
type InvocationExecution interface {
	Begin() (InvocationBody, error)
}
type InvocationBody interface{ Done() }
type InvocationControl interface{ Deliver() }

// InvocationOwner is retained by the admitting profile, never by a dispatcher.
// Settlement, request delivery, body completion and control drain are independent.
type InvocationOwner interface {
	DispatchDone()
	Settle()
	QueueControl(duplex.Message) (InvocationControl, error)
}

// InvocationLimits bounds total captures (including depth and fan-out) and the
// number of simultaneously unfinished bodies within one admitted invocation.
type InvocationLimits struct{ Captures, Bodies int }

var ErrInvocationEnded = errors.New("invocation no longer admits participation")
var ErrInvocationLimit = errors.New("invocation participation limit reached")

type invocationState struct {
	mu                             sync.Mutex
	limits                         InvocationLimits
	settled, retired, dispatchDone bool
	deliveries, bodies, controls   int
	captures                       []*invocationCapture
	control                        *duplex.Message
	controlDelivered               bool
	onRetired                      func()
}
type invocationOwner struct{ state *invocationState }
type invocationRouting struct{ state *invocationState }
type invocationExecution struct{ state *invocationState }
type invocationCapture struct {
	state           *invocationState
	control         func(duplex.Message)
	ready, notified bool
}
type invocationBody struct {
	state *invocationState
	done  bool
}
type invocationControl struct {
	state     *invocationState
	delivered bool
}

// NewInvocation creates separate owner, routing and execution facades. Creating
// these objects does not grant admission into another endpoint's facility.
// The admitting profile must bind them to its own validated invocation identity.
// onRetired runs once, outside all lifecycle locks, when accounting can be freed.
func NewInvocation(limits InvocationLimits, onRetired func()) (InvocationOwner, InvocationRouting, InvocationExecution, error) {
	if limits.Captures < 1 || limits.Bodies < 1 {
		return nil, nil, nil, errors.New("invocation limits must be positive")
	}
	s := &invocationState{limits: limits, deliveries: 1, onRetired: onRetired}
	return invocationOwner{s}, invocationRouting{s}, invocationExecution{s}, nil
}

// finishLocked reclaims captures only after every authoritative participant has
// finished. Its returned notification must run after unlocking.
func (s *invocationState) finishLocked() func() {
	if s.retired || !s.settled || s.deliveries != 0 || s.bodies != 0 || s.controls != 0 {
		return nil
	}
	s.retired = true
	for _, capture := range s.captures {
		capture.control = nil
	}
	s.captures, s.control = nil, nil
	callback := s.onRetired
	s.onRetired = nil
	return callback
}
func invocationNotify(callback func()) {
	if callback != nil {
		callback()
	}
}

func (o invocationOwner) DispatchDone() {
	s := o.state
	s.mu.Lock()
	if !s.dispatchDone {
		s.dispatchDone = true
		s.deliveries--
	}
	callback := s.finishLocked()
	s.mu.Unlock()
	invocationNotify(callback)
}
func (o invocationOwner) Settle() {
	s := o.state
	s.mu.Lock()
	s.settled = true
	callback := s.finishLocked()
	s.mu.Unlock()
	invocationNotify(callback)
}
func (r invocationRouting) Capture(control func(duplex.Message)) (InvocationCapture, error) {
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.retired {
		return nil, ErrInvocationEnded
	}
	if len(s.captures) >= s.limits.Captures {
		return nil, ErrInvocationLimit
	}
	capture := &invocationCapture{state: s, control: control}
	s.captures = append(s.captures, capture)
	s.deliveries++
	return capture, nil
}
func (e invocationExecution) Begin() (InvocationBody, error) {
	s := e.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.retired {
		return nil, ErrInvocationEnded
	}
	if s.bodies >= s.limits.Bodies {
		return nil, ErrInvocationLimit
	}
	s.bodies++
	return &invocationBody{state: s}, nil
}
func (body *invocationBody) Done() {
	s := body.state
	s.mu.Lock()
	if body.done {
		s.mu.Unlock()
		return
	}
	body.done = true
	s.bodies--
	callback := s.finishLocked()
	s.mu.Unlock()
	invocationNotify(callback)
}
func (o invocationOwner) QueueControl(message duplex.Message) (InvocationControl, error) {
	s := o.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return nil, ErrInvocationEnded
	}
	if s.control != nil {
		return nil, nil
	}
	if message.Frame.Kind != duplex.ProfileCancel {
		return nil, errors.New("invocation control must be cancellation")
	}
	s.control = &message
	s.controls++
	return &invocationControl{state: s}, nil
}

// invokeControls contains arbitrary receiver code outside the state lock and
// retains a control reservation until all selected continuations have returned.
func (s *invocationState) invokeControls(callbacks []func(duplex.Message), message duplex.Message) {
	defer func() {
		s.mu.Lock()
		s.controls--
		notify := s.finishLocked()
		s.mu.Unlock()
		invocationNotify(notify)
	}()
	for _, callback := range callbacks {
		func() { defer func() { _ = recover() }(); callback(message) }()
	}
}
func (ticket *invocationControl) Deliver() {
	s := ticket.state
	s.mu.Lock()
	if ticket.delivered {
		s.mu.Unlock()
		return
	}
	ticket.delivered = true
	s.controlDelivered = true
	message := *s.control
	var callbacks []func(duplex.Message)
	for _, capture := range s.captures {
		if capture.ready && !capture.notified {
			capture.notified = true
			if capture.control != nil {
				callbacks = append(callbacks, capture.control)
			}
		}
	}
	s.mu.Unlock()
	s.invokeControls(callbacks, message)
}
func (capture *invocationCapture) Delivered() {
	s := capture.state
	s.mu.Lock()
	if capture.ready {
		s.mu.Unlock()
		return
	}
	capture.ready = true
	s.deliveries--
	if s.controlDelivered && !capture.notified {
		capture.notified = true
		callback, message := capture.control, *s.control
		s.controls++
		s.mu.Unlock()
		var callbacks []func(duplex.Message)
		if callback != nil {
			callbacks = append(callbacks, callback)
		}
		s.invokeControls(callbacks, message)
		return
	}
	callback := s.finishLocked()
	s.mu.Unlock()
	invocationNotify(callback)
}
