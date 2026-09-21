package duplex

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// WireRecord is an immutable, sequenced opaque message. Message retains its
// original local capabilities and reference scope; replay does not rebind them.
type WireRecord struct {
	Sequence uint64
	Path     []string
	Message  Message
}

// WireLog is consumer-owned storage with one exclusive RecordedWire writer.
// Append assigns the next contiguous sequence, beginning at one; Head is zero
// for empty storage. Read may run concurrently with Append and returns immutable
// records. Operations must honor cancellation. Storage owns persistence, not a
// new lifetime or serialization rule for capabilities inside a Message.
type WireLog interface {
	Head(context.Context) (uint64, error)
	Append(context.Context, []string, Message) (uint64, error)
	Read(context.Context, uint64) (WireRecord, error)
}

const maxRecordSequence uint64 = 1<<53 - 1

var (
	ErrRecordSequence = errors.New("record sequence is invalid")
	ErrRecordOverflow = errors.New("record queue is full")
)

// MemoryWireLog retains messages without interpreting or serializing them.
// Callers must not mutate an admitted message or a returned record's message.
type MemoryWireLog struct {
	mu      sync.RWMutex
	entries []WireRecord
}

func NewMemoryWireLog() *MemoryWireLog { return &MemoryWireLog{} }
func (l *MemoryWireLog) Head(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return uint64(len(l.entries)), nil
}
func (l *MemoryWireLog) Append(ctx context.Context, p []string, m Message) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seq := uint64(len(l.entries)) + 1
	if seq > maxRecordSequence {
		return 0, ErrRecordSequence
	}
	l.entries = append(l.entries, WireRecord{seq, append([]string{}, p...), m})
	return seq, nil
}
func (l *MemoryWireLog) Read(ctx context.Context, seq uint64) (WireRecord, error) {
	if err := ctx.Err(); err != nil {
		return WireRecord{}, err
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if seq == 0 || seq > uint64(len(l.entries)) {
		return WireRecord{}, ErrRecordSequence
	}
	e := l.entries[seq-1]
	e.Path = append([]string{}, e.Path...)
	return e, nil
}

// RecordOptions uses one bound for writer admission and each live handoff.
// OnClose runs asynchronously, outside append exclusion and the Send stack.
type RecordOptions struct {
	MaxQueuedMessages int
	OnClose           func(error)
}
type recordCommand struct {
	path    []string
	message Message
	control func(uint64)
}

// RecordedWire appends before forwarding, with storage work off Send's stack.
// A successful Send promises admission only; Head fences admitted appends.
type RecordedWire struct {
	target    Wire
	log       WireLog
	options   RecordOptions
	ctx       context.Context
	cancel    context.CancelFunc
	queue     chan recordCommand
	mu        sync.Mutex
	closed    bool
	head      uint64
	followers map[*Follower]struct{}
}

// Record takes exclusive append ownership of log. Setup reads its initial head;
// the target is this composition's carrier and closes when the recorder ends.
func Record(target Wire, log WireLog, options RecordOptions) (*RecordedWire, error) {
	if target == nil || log == nil {
		return nil, fmt.Errorf("record requires a target and storage")
	}
	if options.MaxQueuedMessages == 0 {
		options.MaxQueuedMessages = 64
	}
	if options.MaxQueuedMessages < 1 {
		return nil, fmt.Errorf("record queue bound must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	head, err := log.Head(ctx)
	if err != nil || head > maxRecordSequence {
		cancel()
		if err == nil {
			err = ErrRecordSequence
		}
		return nil, err
	}
	w := &RecordedWire{target: target, log: log, options: options, ctx: ctx, cancel: cancel, queue: make(chan recordCommand, options.MaxQueuedMessages), head: head, followers: map[*Follower]struct{}{}}
	go w.run()
	return w, nil
}
func (w *RecordedWire) admit(command recordCommand) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrClosed
	}
	select {
	case w.queue <- command:
		w.mu.Unlock()
		return nil
	default:
		w.mu.Unlock()
		w.end(1008, "record queue is full", ErrRecordOverflow)
		return ErrRecordOverflow
	}
}
func (w *RecordedWire) Send(path []string, message Message) error {
	if _, err := EncodePath(path); err != nil {
		return err
	}
	return w.admit(recordCommand{path: append([]string{}, path...), message: message})
}
func (w *RecordedWire) Receive(path []string, receiver Receiver) (func(), error) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	return w.target.Receive(path, receiver)
}
func (w *RecordedWire) Close(code Code, reason string) error { w.end(code, reason, nil); return nil }
func (w *RecordedWire) end(code Code, reason string, err error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.cancel()
	followers := make([]*Follower, 0, len(w.followers))
	for f := range w.followers {
		followers = append(followers, f)
	}
	w.mu.Unlock()
	// No consumer callback, including carrier close, runs inside Send or a lock.
	go func() {
		for _, f := range followers {
			f.end(code, reason, err)
		}
		_ = w.target.Close(code, reason)
		if w.options.OnClose != nil {
			w.options.OnClose(err)
		}
	}()
}
func (w *RecordedWire) run() {
	defer func() {
		for {
			select {
			case <-w.queue:
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-w.ctx.Done():
			return
		case command := <-w.queue:
			if w.ctx.Err() != nil {
				return
			}
			if command.control != nil {
				command.control(w.head)
				continue
			}
			seq, err := w.log.Append(w.ctx, command.path, command.message)
			if err != nil {
				w.end(1011, "record storage failed", err)
				return
			}
			if seq != w.head+1 || seq > maxRecordSequence {
				w.end(1011, "record storage sequence is invalid", ErrRecordSequence)
				return
			}
			entry := WireRecord{seq, command.path, command.message}
			w.head = seq
			w.mu.Lock()
			var overflow []*Follower
			for f := range w.followers {
				if !f.offer(entry) {
					overflow = append(overflow, f)
				}
			}
			w.mu.Unlock()
			for _, f := range overflow {
				f.end(1008, "record handoff queue is full", ErrRecordOverflow)
			}
			if w.ctx.Err() != nil {
				return
			}
			if err := w.target.Send(entry.Path, entry.Message); err != nil {
				w.end(1011, "record target refused", err)
				return
			}
		}
	}
}

// Head fences all earlier admissions. It does not imply target delivery.
func (w *RecordedWire) Head(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	result := make(chan uint64, 1)
	if err := w.admit(recordCommand{control: func(head uint64) { result <- head }}); err != nil {
		return 0, err
	}
	select {
	case head := <-result:
		return head, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-w.ctx.Done():
		return 0, ErrClosed
	}
}

// Follower owns a target carrier, its replay writer and bounded live handoff.
// Head is the atomic attach boundary; Done includes writer and carrier cleanup.
type Follower struct {
	Head          uint64
	Done          <-chan struct{}
	owner         *RecordedWire
	target        Wire
	live          chan WireRecord
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	stopped       bool
	err           error
	done          chan struct{}
	carrierClosed chan struct{}
}

func (f *Follower) Err() error { f.mu.Lock(); defer f.mu.Unlock(); return f.err }
func (f *Follower) Close()     { f.end(1000, "follow closed", nil) }
func (f *Follower) offer(entry WireRecord) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return true
	}
	select {
	case f.live <- entry:
		return true
	default:
		return false
	}
}
func (f *Follower) end(code Code, reason string, err error) {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.stopped = true
	f.err = err
	f.cancel()
	f.mu.Unlock()
	f.owner.mu.Lock()
	delete(f.owner.followers, f)
	f.owner.mu.Unlock()
	go func() { _ = f.target.Close(code, reason); close(f.carrierClosed) }()
}

// Follow atomically takes a head and registers its handoff in append order.
// It replays (after,Head] on its own worker, then follows later appends. after
// is a local cursor; carrying it remotely requires an ordinary consumer member.
// ctx owns this follower's lifetime, not the recorder or its other followers.
func (w *RecordedWire) Follow(ctx context.Context, after uint64, target Wire) (*Follower, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if target == nil {
		return nil, fmt.Errorf("follow requires a target")
	}
	type response struct {
		f   *Follower
		err error
	}
	result := make(chan response, 1)
	err := w.admit(recordCommand{control: func(head uint64) {
		if err := ctx.Err(); err != nil {
			result <- response{err: err}
			return
		}
		if after > head {
			result <- response{err: ErrRecordSequence}
			return
		}
		life, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		f := &Follower{Head: head, Done: done, done: done, owner: w, target: target, live: make(chan WireRecord, w.options.MaxQueuedMessages), ctx: life, cancel: cancel, carrierClosed: make(chan struct{})}
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			cancel()
			result <- response{err: ErrClosed}
			return
		}
		w.followers[f] = struct{}{}
		w.mu.Unlock()
		go f.run(after)
		result <- response{f: f}
	}})
	if err != nil {
		return nil, err
	}
	select {
	case result := <-result:
		return result.f, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.ctx.Done():
		return nil, ErrClosed
	}
}
func (f *Follower) run(after uint64) {
	defer func() {
		f.end(1000, "follow ended", f.ctx.Err())
		<-f.carrierClosed
		for {
			select {
			case <-f.live:
				continue
			default:
				close(f.done)
				return
			}
		}
	}()
	send := func(entry WireRecord) bool {
		if f.ctx.Err() != nil {
			return false
		}
		if err := f.target.Send(append([]string{}, entry.Path...), entry.Message); err != nil {
			f.end(1011, "follow target refused", err)
			return false
		}
		return true
	}
	for seq := after + 1; seq <= f.Head; seq++ {
		entry, err := f.owner.log.Read(f.ctx, seq)
		if err != nil {
			if f.ctx.Err() != nil {
				return
			}
			f.end(1011, "record replay failed", err)
			return
		}
		if entry.Sequence != seq {
			f.end(1011, "record replay sequence is invalid", ErrRecordSequence)
			return
		}
		if !send(entry) {
			return
		}
	}
	for {
		select {
		case <-f.ctx.Done():
			return
		case entry := <-f.live:
			if !send(entry) {
				return
			}
		}
	}
}
