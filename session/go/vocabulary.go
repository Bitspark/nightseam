package session

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Bitspark/nightseam/duplex/go"
)

// The session's own vocabulary on the wire. A layer that speaks on the wire
// does it as the tunnel does — ordinary frames of the profile under a prefix
// the layer reserves, which the peer forwards and reads nothing into
// (docs/wire/vocabulary.md); `channel.open` and `channel.credit` are the tunnel's.
// These two are the relay's alone: the relay produces them, a consumer reads
// them, a machine that sends one has its connection ended, and neither is
// logged, a session's own frames being state rather than messages of it.
const (
	// Prefix is what every name of the session's vocabulary begins with, and
	// what no family declares a method or an event under.
	Prefix = "session."
	// ControlEvent says who holds control of the session:
	// {"holder": "<origin>"}, or {"holder": null} where nobody does. Every
	// attachment is sent it when control changes, and a consumer once on
	// attach, before its replay, so that it knows the state it joined.
	ControlEvent = "session.control"
	// CursorEvent says where in the session's one order the frame delivered
	// just before it stood: {"sequence": N}, the log's own sequence, which is
	// what a consumer resumes from. A frame the log cut is delivered as
	// nothing and carries none, the next cursor naming the next sequence.
	CursorEvent = "session.cursor"
)

// vocabulary is one frame of the session's own: an ordinary event of the
// profile whose name is the session tier's, written by the relay and by
// nothing else.
type vocabulary struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Event   string `json:"event"`
	Data    any    `json:"data"`
}

// holderData is what a control event carries: the origin of whoever holds
// control, and null where nobody does — which an origin alone could not say
// of a consumer attached under no name.
type holderData struct {
	Holder *string `json:"holder"`
}

// cursorData is what a cursor event carries: the log's sequence of the frame
// delivered just before it.
type cursorData struct {
	Sequence int64 `json:"sequence"`
}

func controlFrame(holder string, held bool) []byte {
	data := holderData{}
	if held {
		data.Holder = &holder
	}
	return sessionFrame(ControlEvent, data)
}

func cursorFrame(sequence int64) []byte {
	return sessionFrame(CursorEvent, cursorData{Sequence: sequence})
}

func sessionFrame(event string, data any) []byte {
	encoded, err := json.Marshal(vocabulary{Version: 1, Kind: "event", Event: event, Data: data})
	if err != nil {
		return nil
	}
	return encoded
}

// reserved reports whether a frame names an operation of the session's own
// vocabulary, which is what the relay refuses from the machine.
func reserved(name string) bool { return strings.HasPrefix(name, Prefix) }

// controlWatch is one OnControl registration, kept by identity so that
// stopping one leaves every other where it was.
type controlWatch struct {
	fn func(origin string, held bool)
}

// Holder is who holds control of the session, as the relay last told this
// consumer: the origin that consumer was attached under, and whether anybody
// holds it at all. It is what the last session.control this attachment was
// sent said, so a consumer reading it here and one reading the wire agree.
func (a *Attachment) Holder() (origin string, held bool) {
	a.state.Lock()
	defer a.state.Unlock()
	return a.holder, a.held
}

// Sequence is the log's sequence of the last frame delivered to this
// consumer — what the last session.cursor said — and zero where it has been
// delivered none. It is what the consumer reattaches after, and the relay's
// own count rather than one kept by counting frames, which a message the log
// cut would put out by one.
func (a *Attachment) Sequence() int64 {
	a.state.Lock()
	defer a.state.Unlock()
	return a.sequence
}

// OnControl asks to be told each time this consumer is sent a
// session.control, and returns the way to stop asking. It is not called with
// the state the consumer joined at — that frame is sent before Attach
// returns, which is before there is anywhere to call — and Holder reads it
// instead.
//
// fn is called on whichever goroutine control moved on, outside the
// consumer's turn on its connection, so that one which sends holds nothing
// up. stop ends this registration and no other, and may be called more than
// once.
func (a *Attachment) OnControl(fn func(origin string, held bool)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	registered := &controlWatch{fn: fn}
	a.state.Lock()
	a.controls = append(a.controls, registered)
	a.state.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.state.Lock()
			defer a.state.Unlock()
			for i, watching := range a.controls {
				if watching == registered {
					a.controls = append(a.controls[:i], a.controls[i+1:]...)
					return
				}
			}
		})
	}
}

// control tells this consumer who holds control and sets the state that
// says, the registrations called once its turn on the connection is over.
func (a *Attachment) control(holder string, held bool) {
	a.send.Lock()
	err := a.controlHeld(holder, held)
	a.send.Unlock()
	a.told(holder, held)
	if err != nil {
		a.end(duplex.CodePolicyViolation, "the consumer did not take the session's frames")
	}
}

// controlHeld tells it with the consumer's turn already taken, which is how
// it reaches a consumer on attach: before the replay and before any frame of
// the family.
func (a *Attachment) controlHeld(holder string, held bool) error {
	frame := controlFrame(holder, held)
	a.state.Lock()
	a.holder, a.held = holder, held
	a.state.Unlock()
	return a.sendHeld(frame)
}

// cursorHeld says where the frame just delivered stood, with the turn taken,
// so that nothing comes between a frame and the cursor that names it.
func (a *Attachment) cursorHeld(sequence int64) error {
	frame := cursorFrame(sequence)
	a.state.Lock()
	a.sequence = sequence
	a.state.Unlock()
	return a.sendHeld(frame)
}

// told calls every OnControl registration, in the order they registered.
func (a *Attachment) told(holder string, held bool) {
	a.state.Lock()
	if len(a.controls) == 0 {
		a.state.Unlock()
		return
	}
	registered := make([]*controlWatch, len(a.controls))
	copy(registered, a.controls)
	a.state.Unlock()
	for _, watching := range registered {
		watching.fn(holder, held)
	}
}
