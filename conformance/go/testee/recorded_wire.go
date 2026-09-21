package main

// This is an acceptance witness, not the generated record/follow API. Its
// in-memory append store and subscriber writers are consumer compositions of
// Wire. Keeping them in the testee makes that boundary explicit.
import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Bitspark/nightseam/duplex/go"
)

type recordedEntry struct {
	path     []string
	message  duplex.Message
	sequence int
}

type recordedWire struct {
	mu        sync.Mutex
	entries   []recordedEntry
	followers map[*recordedFollower]bool
}

type recordedFollower struct {
	target duplex.Wire
	live   chan recordedEntry
	stop   chan struct{}
	done   chan struct{}
	sent   chan int
	paused chan struct{}
	resume chan struct{}
	once   sync.Once
}

func newRecordedWire() *recordedWire { return &recordedWire{followers: map[*recordedFollower]bool{}} }
func (w *recordedWire) head() int    { w.mu.Lock(); defer w.mu.Unlock(); return len(w.entries) }
func (w *recordedWire) Send(path []string, message duplex.Message) error {
	w.mu.Lock()
	entry := recordedEntry{append([]string{}, path...), message, len(w.entries) + 1}
	w.entries = append(w.entries, entry)
	for follower := range w.followers {
		select {
		case follower.live <- entry:
		default:
			delete(w.followers, follower)
			// Signal only. The subscriber's own writer closes its carrier, off
			// the producer's stack and outside append exclusion.
			follower.once.Do(func() { close(follower.stop) })
		}
	}
	w.mu.Unlock()
	return nil
}
func (*recordedWire) Receive([]string, duplex.Receiver) (func(), error) {
	return nil, duplex.ErrNoRoute
}
func (w *recordedWire) Close(duplex.Code, string) error {
	w.mu.Lock()
	for follower := range w.followers {
		follower.once.Do(func() { close(follower.stop) })
	}
	w.followers = map[*recordedFollower]bool{}
	w.mu.Unlock()
	return nil
}

func (w *recordedWire) attach(after int, target duplex.Wire, bound int, pause bool) (int, *recordedFollower) {
	f := &recordedFollower{target: target, live: make(chan recordedEntry, bound), stop: make(chan struct{}), done: make(chan struct{}), sent: make(chan int, 32), paused: make(chan struct{}), resume: make(chan struct{})}
	w.mu.Lock()
	head := len(w.entries)
	history := append([]recordedEntry{}, w.entries[after:head]...)
	w.followers[f] = true // The head and handoff registration share append's lock.
	w.mu.Unlock()
	go func() {
		defer close(f.done)
		defer target.Close(1008, "recorded handoff ended")
		send := func(entry recordedEntry) bool {
			select {
			case <-f.stop:
				return false
			default:
			}
			if err := target.Send(entry.path, entry.message); err != nil {
				return false
			}
			f.sent <- entry.sequence
			return true
		}
		for i, entry := range history {
			if !send(entry) {
				return
			}
			if i == 0 && pause {
				close(f.paused)
				select {
				case <-f.resume:
				case <-f.stop:
					return
				}
			}
		}
		for {
			select {
			case <-f.stop:
				return
			case entry := <-f.live:
				if !send(entry) {
					return
				}
			}
		}
	}()
	return head, f
}

// An asynchronous fixture root: selection and mounting use the production
// implementation, while this root supplies deterministic queued delivery. A
// forward hop receives and sends the same opaque Message to a second root.
type recordedRoot struct {
	mu        sync.Mutex
	receivers map[string]duplex.Receiver
	queue     chan recordedEntry
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
}

func newRecordedRoot() *recordedRoot {
	r := &recordedRoot{receivers: map[string]duplex.Receiver{}, queue: make(chan recordedEntry, 16), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for {
			select {
			case <-r.stop:
				return
			case entry := <-r.queue:
				key, _ := duplex.EncodePath(entry.path)
				r.mu.Lock()
				receiver := r.receivers[key]
				r.mu.Unlock()
				if receiver.Message != nil {
					receiver.Message(append([]string{}, entry.path...), entry.message)
				}
			}
		}
	}()
	return r
}
func (r *recordedRoot) Send(path []string, message duplex.Message) error {
	select {
	case <-r.stop:
		return duplex.ErrClosed
	default:
	}
	select {
	case r.queue <- recordedEntry{path: append([]string{}, path...), message: message}:
		return nil
	default:
		return fmt.Errorf("witness output queue full")
	}
}
func (r *recordedRoot) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	key, err := duplex.EncodePath(path)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if _, exists := r.receivers[key]; exists {
		r.mu.Unlock()
		return nil, duplex.ErrReceiverExists
	}
	r.receivers[key] = receiver
	r.mu.Unlock()
	return func() { r.mu.Lock(); delete(r.receivers, key); r.mu.Unlock() }, nil
}
func (r *recordedRoot) Close(duplex.Code, string) error {
	r.once.Do(func() { close(r.stop) })
	<-r.done
	return nil
}

type recordedPresentation struct {
	wire   duplex.Wire
	root   *recordedRoot
	end    *recordedRoot
	values chan int
	closed chan int
	errors chan error
}

func presentRecorded(w *recordedWire) (*recordedPresentation, error) {
	p := &recordedPresentation{root: newRecordedRoot(), end: newRecordedRoot(), values: make(chan int, 32), closed: make(chan int, 4), errors: make(chan error, 4)}
	destination := duplex.At(duplex.Mount(map[string]duplex.Wire{"out": duplex.At(p.end, []string{"destination"})}), []string{"out"})
	p.wire = duplex.At(duplex.Mount(map[string]duplex.Wire{"outer": duplex.Mount(map[string]duplex.Wire{"in": duplex.At(p.root, []string{"source"})})}), []string{"outer", "in"})
	_, err := destination.Receive([]string{"tick"}, duplex.Receiver{Message: func(path []string, m duplex.Message) {
		if len(path) != 1 || path[0] != "tick" {
			p.errors <- fmt.Errorf("recorded destination received path %q", path)
			return
		}
		// A real application callback reenters the store. Calling this under
		// append exclusion deadlocks and fails the witness's deadline.
		_ = w.head()
		var value int
		if err := json.Unmarshal(m.Frame.Data, &value); err != nil {
			p.errors <- err
			return
		}
		p.values <- value
	}})
	if err == nil {
		_, err = p.wire.Receive([]string{"tick"}, duplex.Receiver{Message: func(path []string, message duplex.Message) {
			if err := destination.Send(path, message); err != nil {
				p.errors <- err
			}
		}, Closed: func(code duplex.Code, _ string) { _ = w.head(); p.closed <- int(code) }})
	}
	return p, err
}
func (p *recordedPresentation) close() {
	_ = p.wire.Close(1000, "done")
	_ = p.root.Close(1000, "done")
	_ = p.end.Close(1000, "done")
}
func recordedMessage(value int) duplex.Message {
	return duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileEvent, Data: json.RawMessage(fmt.Sprint(value))}}
}
func recordedWait[T any](ctx context.Context, channel <-chan T) (T, error) {
	select {
	case value := <-channel:
		return value, nil
	case <-ctx.Done():
		var zero T
		return zero, fail("timeout", "recorded wire witness: %v", ctx.Err())
	}
}
func recordedSent(ctx context.Context, f *recordedFollower, last int) error {
	for {
		value, err := recordedWait(ctx, f.sent)
		if err != nil {
			return err
		}
		if value == last {
			return nil
		}
	}
}
func (p *recordedPresentation) collect(ctx context.Context) ([]int, error) {
	// A fence through the same mount and forward hop proves every earlier
	// delivery has arrived; an extra replay cannot hide behind a length check.
	if err := p.wire.Send([]string{"tick"}, recordedMessage(0)); err != nil {
		return nil, err
	}
	values := []int{}
	for {
		select {
		case err := <-p.errors:
			return nil, err
		case value := <-p.values:
			if value == 0 {
				return values, nil
			}
			values = append(values, value)
		case <-ctx.Done():
			return nil, fail("timeout", "recorded delivery fence did not arrive")
		}
	}
}

func recordedHeadCase(ctx context.Context, before bool) (any, error) {
	w := newRecordedWire()
	defer w.Close(1000, "done")
	p, err := presentRecorded(w)
	if err != nil {
		return nil, err
	}
	defer p.close()
	source := duplex.At(duplex.Mount(map[string]duplex.Wire{"record": w}), []string{"record"})
	appendValue := func(value int) error { return source.Send([]string{"tick"}, recordedMessage(value)) }
	for value := 1; value <= 3; value++ {
		if err := appendValue(value); err != nil {
			return nil, err
		}
	}
	cut, next := "head_before_append", 4
	if before {
		cut, next = "append_before_head", 5
		if err := appendValue(4); err != nil {
			return nil, err
		}
	}
	head, f := w.attach(0, p.wire, 2, true)
	defer func() { f.once.Do(func() { close(f.stop) }); <-f.done }()
	if _, err := recordedWait(ctx, f.paused); err != nil {
		return nil, err
	}
	// Producer completion is observed while the replay writer is still held.
	produced := make(chan error, 1)
	go func() {
		for value := next; value <= 5; value++ {
			if err := appendValue(value); err != nil {
				produced <- err
				return
			}
		}
		produced <- nil
	}()
	if err, waitErr := recordedWait(ctx, produced); waitErr != nil {
		return nil, waitErr
	} else if err != nil {
		return nil, err
	}
	close(f.resume)
	if err := recordedSent(ctx, f, 5); err != nil {
		return nil, err
	}
	if err := appendValue(6); err != nil {
		return nil, err
	}
	if err := recordedSent(ctx, f, 6); err != nil {
		return nil, err
	}
	first, err := p.collect(ctx)
	if err != nil {
		return nil, err
	}
	second, err := presentRecorded(w)
	if err != nil {
		return nil, err
	}
	defer second.close()
	_, late := w.attach(3, second.wire, 2, false)
	defer func() { late.once.Do(func() { close(late.stop) }); <-late.done }()
	if err := recordedSent(ctx, late, 6); err != nil {
		return nil, err
	}
	after, err := second.collect(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cut": cut, "head": head, "first": first, "after_three": after, "producer_progress": true, "callbacks_outside_append": true}, nil
}

func recordedStallCase(ctx context.Context) (any, error) {
	w := newRecordedWire()
	defer w.Close(1000, "done")
	for value := 1; value <= 3; value++ {
		_ = w.Send([]string{"tick"}, recordedMessage(value))
	}
	stalled, err := presentRecorded(w)
	if err != nil {
		return nil, err
	}
	defer stalled.close()
	healthy, err := presentRecorded(w)
	if err != nil {
		return nil, err
	}
	defer healthy.close()
	_, slow := w.attach(0, stalled.wire, 2, true)
	defer func() { slow.once.Do(func() { close(slow.stop) }); <-slow.done }()
	if _, err := recordedWait(ctx, slow.paused); err != nil {
		return nil, err
	}
	_, fast := w.attach(3, healthy.wire, 2, false)
	defer func() { fast.once.Do(func() { close(fast.stop) }); <-fast.done }()
	for value := 4; value <= 5; value++ {
		_ = w.Send([]string{"tick"}, recordedMessage(value))
		if err := recordedSent(ctx, fast, value); err != nil {
			return nil, err
		}
	}
	queued := len(slow.live)
	_ = w.Send([]string{"tick"}, recordedMessage(6))
	if err := recordedSent(ctx, fast, 6); err != nil {
		return nil, err
	}
	if _, err := recordedWait(ctx, slow.done); err != nil {
		return nil, err
	}
	code, err := recordedWait(ctx, stalled.closed)
	if err != nil {
		return nil, err
	}
	if err := stalled.wire.Send([]string{"tick"}, recordedMessage(99)); err == nil {
		return nil, fmt.Errorf("stalled carrier accepted after close")
	}
	underneath := make(chan int, 1)
	_, err = stalled.root.Receive([]string{"probe"}, duplex.Receiver{Message: func(_ []string, message duplex.Message) {
		var value int
		_ = json.Unmarshal(message.Frame.Data, &value)
		underneath <- value
	}})
	if err != nil {
		return nil, err
	}
	if err := stalled.root.Send([]string{"probe"}, recordedMessage(99)); err != nil {
		return nil, err
	}
	probe, err := recordedWait(ctx, underneath)
	if err != nil {
		return nil, err
	}
	_ = w.Send([]string{"tick"}, recordedMessage(7))
	if err := recordedSent(ctx, fast, 7); err != nil {
		return nil, err
	}
	values, err := healthy.collect(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"bound": 2, "queued_at_bound": queued, "closed": 1 + len(stalled.closed), "close_code": code, "healthy": values, "underneath": []int{probe}, "head": w.head(), "producer_progress": true}, nil
}

func (t *testee) recordedWireOps() map[string]func(request) (any, error) {
	return map[string]func(request) (any, error){"peer.recorded_wire_witness": func(r request) (any, error) {
		within, err := r.within()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), within)
		defer cancel()
		cases := []any{}
		for _, before := range []bool{false, true} {
			observation, err := recordedHeadCase(ctx, before)
			if err != nil {
				return nil, err
			}
			cases = append(cases, observation)
		}
		stalled, err := recordedStallCase(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"cases": cases, "stalled": stalled}, nil
	}}
}
