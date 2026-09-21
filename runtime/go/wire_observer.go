package runtime

import (
	"errors"
	"strconv"
	"sync/atomic"
	"time"
)

var wireObservationSequence atomic.Uint64

// WireCallOptions configures one model call, independently of its carrier.
// A zero RequestTimeout uses the runtime default; Propagator nil uses DefaultPropagator.
type WireCallOptions struct {
	Propagator     Propagator
	RequestTimeout time.Duration
	Observer       Observer
	Family         string
}

// WireEmitOptions configures one model event emission, independently of its carrier.
type WireEmitOptions struct {
	Propagator Propagator
	Observer   Observer
	Family     string
}

func observeWire(observer Observer, event ObserverEvent) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(event)
}

// The existing helper completion owns this observation; no routing state is
// added, and an unobserved helper reads no clock.
func observeWireRequest(observer Observer, family, method string, incoming bool, trace Trace) func(error) {
	if observer == nil {
		return func(error) {}
	}
	// Logical frame identifiers belong to a return capability and can repeat
	// across calls. An observer sees no capability, so its lifetime identifier
	// is unique and distinct from the physical peer's c:/s: namespace.
	id := "wire:" + strconv.FormatUint(wireObservationSequence.Add(1), 10)
	started := time.Now()
	finished := false
	observeWire(observer, RequestStarted{At: started, ID: id, Method: method, Incoming: incoming, Trace: trace, Family: family})
	return func(err error) {
		if finished {
			return
		}
		finished = true
		at := time.Now()
		outcome, code := outcomeOf(err)
		if err != nil && code == "" {
			code = "internal"
			if errors.Is(err, ErrClosed) {
				code = "disconnected"
			}
		}
		observeWire(observer, RequestEnded{At: at, ID: id, Method: method, Incoming: incoming,
			Duration: at.Sub(started), Outcome: outcome, ErrorCode: code, Trace: trace, Family: family})
	}
}
