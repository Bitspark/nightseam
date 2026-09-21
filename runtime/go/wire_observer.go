package runtime

import (
	"errors"
	"strconv"
	"sync/atomic"
	"time"
)

var wireObservationSequence atomic.Uint64

// WireCallOptions labels observations made by one model call. It does not
// reconfigure the selected wire or its carrier's observer.
type WireCallOptions struct {
	Observer Observer
	Family   string
}

// WireEmitOptions labels observations made by one model event emission.
type WireEmitOptions struct {
	Observer Observer
	Family   string
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
