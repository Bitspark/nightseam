package session

import (
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
)

// What a session tells the observer of the peer it runs over. They are events
// of runtime.ObserverEvent, so one observer hears them beside the runtime's
// own and the tunnel's, and a consumer's type switch covers all three layers;
// a session takes no observer of its own and observes through its peer or not
// at all. Every one of them says which session it is of and when it happened;
// every one that concerns a frame carries that frame's trace verbatim; none of
// them carries a payload, which is the log's and never a hook's.
//
// The observer is the peer the machine's channel runs over — a session's own
// side — whichever consumer a given event is about.

// SessionBound is a session bound to the channel its machine speaks on, which
// it stands on until that channel closes.
type SessionBound struct {
	At      time.Time
	Session string
}

// SessionUnbound is that channel closed: the session is gone from the registry
// and every consumer of it was ended with this close.
type SessionUnbound struct {
	At      time.Time
	Session string
	Code    int
	Reason  string
}

// SessionAttached is a consumer joining a session in a role, under the origin
// its frames are logged with, resuming from the sequence it already holds.
type SessionAttached struct {
	At      time.Time
	Session string
	Role    Role
	Origin  string
	After   int64
}

// SessionDetached is a consumer leaving one, by detaching or by its channel
// closing; the session stands. A consumer the session's own ending took with
// it detaches from nothing, and the session says unbound instead.
type SessionDetached struct {
	At      time.Time
	Session string
	Role    Role
	Origin  string
}

// AskRaised is the machine sending a request the holder of control must
// answer. Asking is whether the family's session tier counts it, which is what
// Attention names; one it does not count waits for a holder all the same.
type AskRaised struct {
	At      time.Time
	Session string
	ID      string
	Method  string
	Asking  bool
	Trace   runtime.Trace
}

// AskRouted is that request handed to a holder — where it arrived, and again
// each time control moved while it stood open.
type AskRouted struct {
	At      time.Time
	Session string
	ID      string
	Method  string
	Origin  string
	Trace   runtime.Trace
}

// AskAnswered is the holder answering one, under the method it was opened with
// and with the trace the answer itself carried.
type AskAnswered struct {
	At      time.Time
	Session string
	ID      string
	Method  string
	Origin  string
	Trace   runtime.Trace
}

// ControlChanged is control of a session moving. Held says there is a holder;
// where there is none, control was released and stands with nobody, which an
// origin alone could not say of a consumer attached under no name.
type ControlChanged struct {
	At      time.Time
	Session string
	Origin  string
	Held    bool
}

// FrameAppended is one frame taking its place in the session's log: its
// sequence, which way it went, the origin of the consumer whose frame it was —
// none for the machine's own — what it named and how large it was, and never
// the message.
type FrameAppended struct {
	At        time.Time
	Session   string
	Sequence  int64
	Direction Direction
	Origin    string
	Bytes     int
	Method    string
	Trace     runtime.Trace
}

// Refused is a consumer's frame the relay answered in the machine's place
// rather than forwarding: not_controlling where control is not held, busy
// where the session has too many requests open. Neither reaches the log,
// because the machine never saw it.
type Refused struct {
	At      time.Time
	Session string
	Code    string
	Method  string
	Role    Role
	Origin  string
	Trace   runtime.Trace
}

func (SessionBound) ObserverEvent()    {}
func (SessionUnbound) ObserverEvent()  {}
func (SessionAttached) ObserverEvent() {}
func (SessionDetached) ObserverEvent() {}
func (AskRaised) ObserverEvent()       {}
func (AskRouted) ObserverEvent()       {}
func (AskAnswered) ObserverEvent()     {}
func (ControlChanged) ObserverEvent()  {}
func (FrameAppended) ObserverEvent()   {}
func (Refused) ObserverEvent()         {}

// The events of this layer are events of the runtime's observer, which is the
// only observer there is.
var (
	_ runtime.ObserverEvent = SessionBound{}
	_ runtime.ObserverEvent = SessionUnbound{}
	_ runtime.ObserverEvent = SessionAttached{}
	_ runtime.ObserverEvent = SessionDetached{}
	_ runtime.ObserverEvent = AskRaised{}
	_ runtime.ObserverEvent = AskRouted{}
	_ runtime.ObserverEvent = AskAnswered{}
	_ runtime.ObserverEvent = ControlChanged{}
	_ runtime.ObserverEvent = FrameAppended{}
	_ runtime.ObserverEvent = Refused{}
)

// observe tells the observer of the peer the machine's channel runs over one
// event of this session; a session over a peer given no observer does nothing.
func (r *relay) observe(event runtime.ObserverEvent) { r.up.Peer().Observe(event) }

// observed reports whether that peer was given an observer at all, which
// every emitter asks before it builds anything.
func (r *relay) observed() bool { return r.up.Peer().Observer() != nil }

// origin is what a consumer was attached under, and "" where the change is
// the machine's or the session's own.
func origin(a *Attachment) string {
	if a == nil {
		return ""
	}
	return a.Origin
}
