package session

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// Direction is which way a frame went.
type Direction int

const (
	// Up is towards the machine: what a consumer sent.
	Up Direction = iota
	// Down is from the machine: what it sent back or of its own.
	Down
)

func (d Direction) String() string {
	switch d {
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return "direction(" + strconv.Itoa(int(d)) + ")"
}

// Frame is one message a session exchanged, verbatim, in one order, with
// where it came from. The ids it carries are the session's own — the
// relay's, minted so that two consumers do not collide — so the log reads
// as one conversation whoever was attached when.
type Frame struct {
	// Sequence is the frame's place in the session's one order, from one.
	Sequence int64
	// Direction is which way it went.
	Direction Direction
	// Origin is what the caller said the consumer that sent it is; a frame
	// from the machine carries none.
	Origin string
	// At is when the session exchanged it.
	At time.Time
	// Message is the frame as it went over the channel.
	Message json.RawMessage
	// Truncated says the log kept less than the whole message.
	Truncated bool
}

// Log is a session's frames in one order. The record's shape is the
// profile's and belongs here; the store is the consumer's, which is why
// this is an interface and the package ships only an in-memory one. A log
// handed to Bind that already holds frames is bound at its head: Bind asks
// Header where available, otherwise reads Replay from after zero and takes
// the last sequence delivered.
type Log interface {
	// Append records a frame and gives it its sequence, which is one more
	// than the last it gave.
	Append(ctx context.Context, frame Frame) (sequence int64, err error)
	// Replay delivers every frame after a sequence, in ascending sequence
	// order, until deliver returns an error, which Replay returns. The order
	// is the contract rather than a convenience of the memory log's: it is
	// what makes the last sequence Bind's read is given the log's head, so a
	// log that delivers out of order binds its session below its own end.
	Replay(ctx context.Context, after int64, deliver func(Frame) error) error
}

// Header is the optional capability of a Log that can report its head
// without replaying its frames. Head returns the last assigned sequence,
// or zero for an empty log. Bind prefers it to Replay; an error fails the
// binding rather than falling back to a read.
type Header interface {
	Head(ctx context.Context) (int64, error)
}

// NewMemoryLog keeps a session's frames in memory, each message bounded by
// maxFrameBytes: a message over the bound is stored cut, and the frame that
// carries it is Truncated. Zero or less is unbounded, which only a process
// that ends soon may ask for.
func NewMemoryLog(maxFrameBytes int64) Log {
	return &memoryLog{bound: maxFrameBytes}
}

type memoryLog struct {
	bound  int64
	mu     sync.Mutex
	next   int64
	frames []Frame
}

func (l *memoryLog) Head(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next, nil
}

func (l *memoryLog) Append(ctx context.Context, frame Frame) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	message := frame.Message
	truncated := frame.Truncated
	if l.bound > 0 && int64(len(message)) > l.bound {
		message, truncated = message[:l.bound], true
	}
	kept := make([]byte, len(message))
	copy(kept, message)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	frame.Sequence, frame.Message, frame.Truncated = l.next, kept, truncated
	l.frames = append(l.frames, frame)
	return frame.Sequence, nil
}

func (l *memoryLog) Replay(ctx context.Context, after int64, deliver func(Frame) error) error {
	if deliver == nil {
		return coded(ErrorInvalidOptions, "a replay needs somewhere to deliver")
	}
	l.mu.Lock()
	frames := make([]Frame, 0, len(l.frames))
	for _, frame := range l.frames {
		if frame.Sequence > after {
			frames = append(frames, frame)
		}
	}
	l.mu.Unlock()
	for _, frame := range frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deliver(frame); err != nil {
			return err
		}
	}
	return nil
}
