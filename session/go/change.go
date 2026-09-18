package session

import (
	"strconv"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
)

// ChangeKind is one thing that happened to a session. It is the session
// layer's own vocabulary and nothing of the transport beneath it: a frame
// the relay carried is one change, and what the relay decided about it —
// who holds control, what the machine asked, who answered — is another.
type ChangeKind int

const (
	// ChangeBound is a session becoming live, the first change of every one.
	ChangeBound ChangeKind = iota
	// ChangeUnbound is the machine's channel ending, the last.
	ChangeUnbound
	// ChangeAttached is a consumer added, in its role, from its sequence.
	ChangeAttached
	// ChangeDetached is a consumer removed, however it went.
	ChangeDetached
	// ChangeAskRaised is the machine sending a request the family asks:
	// something the holder of control must answer.
	ChangeAskRaised
	// ChangeAskRouted is an ask reaching a holder — where it was raised, and
	// again wherever control moves while it is open.
	ChangeAskRouted
	// ChangeAskAnswered is the holder answering one.
	ChangeAskAnswered
	// ChangeControlChanged is control moving, to a holder or to nobody.
	ChangeControlChanged
	// ChangeFrameAppended is one frame taking its place in the session's log.
	ChangeFrameAppended
	// ChangeRefused is a consumer's frame the relay answered itself rather
	// than carrying: not_controlling, or busy.
	ChangeRefused
)

func (k ChangeKind) String() string {
	switch k {
	case ChangeBound:
		return "bound"
	case ChangeUnbound:
		return "unbound"
	case ChangeAttached:
		return "attached"
	case ChangeDetached:
		return "detached"
	case ChangeAskRaised:
		return "ask_raised"
	case ChangeAskRouted:
		return "ask_routed"
	case ChangeAskAnswered:
		return "ask_answered"
	case ChangeControlChanged:
		return "control_changed"
	case ChangeFrameAppended:
		return "frame_appended"
	case ChangeRefused:
		return "refused"
	}
	return "change(" + strconv.Itoa(int(k)) + ")"
}

// Change is one domain change of a session, as OnChange tells it. It says
// what happened and to whom, and never what was in the frame: a consumer
// that needs the message reads the log, which is what a log is for.
type Change struct {
	// At is when it happened.
	At time.Time
	// Session is the id the session is bound under.
	Session string
	// Kind is what happened.
	Kind ChangeKind
	// Attachment is the consumer the change is about — the one that
	// attached, detached, was routed an ask, answered one, was given
	// control or was refused — and nil where the change is the machine's or
	// the session's own.
	Attachment *Attachment
	// Sequence is the sequence the log gave the frame, for
	// ChangeFrameAppended; the sequence the consumer resumed from, for
	// ChangeAttached; and zero for every other kind, which concerns no
	// place in the log.
	Sequence int64
	// Method is what the frame the change concerns names — a request's
	// method, an event's name — and "" where it names nothing or where no
	// frame is concerned.
	Method string
	// Trace is the trace context that frame carried, verbatim.
	Trace runtime.Trace
}

// OnChange asks to be told every domain change of every session of this
// registry, and returns the way to stop asking. It is the one place a
// consumer's session events are computed: what a consumer calls a session
// having changed, and which sessions want its attention, is this sequence
// named in the consumer's own words.
//
// fn is called on whichever goroutine the change happened on — a relay's
// pump, a consumer's, or the caller of Bind, Attach or Control — and so
// from several at once: one that keeps anything keeps it under a lock of
// its own, and one that blocks holds up the session it is watching. stop
// ends this registration and no other, and may be called more than once.
func (r *Registry) OnChange(fn func(Change)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	registered := &watcher{fn: fn}
	r.watch.Lock()
	r.watchers = append(r.watchers, registered)
	r.watch.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.watch.Lock()
			defer r.watch.Unlock()
			for i, watching := range r.watchers {
				if watching == registered {
					r.watchers = append(r.watchers[:i], r.watchers[i+1:]...)
					return
				}
			}
		})
	}
}

// watcher is one OnChange registration, kept by identity so that stopping
// one leaves every other where it was.
type watcher struct{ fn func(Change) }

// watching reports whether anything asked to be told. A registry nobody
// watches computes no change: the emitters read no clock and take no lock
// for nobody.
func (r *Registry) watching() bool {
	r.watch.RLock()
	defer r.watch.RUnlock()
	return len(r.watchers) != 0
}

// notify tells every registration one change, in the order they registered
// and on the goroutine the change happened on.
func (r *Registry) notify(change Change) {
	r.watch.RLock()
	if len(r.watchers) == 0 {
		r.watch.RUnlock()
		return
	}
	watchers := make([]*watcher, len(r.watchers))
	copy(watchers, r.watchers)
	r.watch.RUnlock()
	for _, watching := range watchers {
		watching.fn(change)
	}
}

// Every domain change of a session goes two ways from one place: as a
// Change to the registry's OnChange registrations, and as an event to the
// observer of the peer the machine's channel runs over. The emitters below
// are that one place — one per change, each reading the clock once for
// both, and each called with no lock of the relay's held, so that a
// registration calling back into the registry finds it as it left it.

func (r *relay) changed(change Change) {
	change.Session = r.id
	r.registry.notify(change)
}

// watched reports whether anything is listening at all.
func (r *relay) watched() bool { return r.registry.watching() }

func (r *relay) sessionBound() {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeBound})
}

// sessionUnbound is the whole of what a session ending says: the consumers
// it carried go with it rather than detaching one by one.
func (r *relay) sessionUnbound() {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeUnbound})
}

// sessionAttached says where the consumer resumed from, which is the one
// fact about it the attachment itself does not carry.
func (r *relay) sessionAttached(a *Attachment, after int64) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeAttached, Attachment: a, Sequence: after})
}

func (r *relay) sessionDetached(a *Attachment) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeDetached, Attachment: a})
}

// frameAppended is one frame of the session's conversation taking its place
// in the log, under the consumer that sent it where a consumer did — which
// is the frame's direction and its origin both — and never what was in it.
func (r *relay) frameAppended(sequence int64, from *Attachment, m *message) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeFrameAppended, Attachment: from,
		Sequence: sequence, Method: m.named(), Trace: m.trace()})
}

// askRaised is every request the machine opens, whether or not the family's
// Asks counts it: what Asks selects is what Attention names, and a request
// the holder is left standing with is raised either way.
func (r *relay) askRaised(m *message) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeAskRaised, Method: m.method(), Trace: m.trace()})
}

func (r *relay) askRouted(a *Attachment, method string, trace runtime.Trace) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeAskRouted, Attachment: a, Method: method, Trace: trace})
}

// askAnswered names the method the request it closes was opened under, and
// carries the trace of the answer that closed it.
func (r *relay) askAnswered(a *Attachment, method string, trace runtime.Trace) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeAskAnswered, Attachment: a, Method: method, Trace: trace})
}

func (r *relay) controlChanged(holder *Attachment) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeControlChanged, Attachment: holder})
}

func (r *relay) refused(a *Attachment, code string, m *message) {
	if !r.watched() {
		return
	}
	r.changed(Change{At: time.Now().UTC(), Kind: ChangeRefused, Attachment: a,
		Method: m.method(), Trace: m.trace()})
}
