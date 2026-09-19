// Package session is a session as a thing with an identity that outlives
// connections: one up connection, the machine's; any number of down
// connections, the consumers', each in a role; one holder of control; an ask
// routed to the holder; and a log of every frame in one order, replayed from
// a sequence. Each side is a connection of the seam — duplex.Conn — which a
// channel of a tunnel is, and so are the seam's pipe and a bare socket: the
// relay sends on it, receives from it and closes it, and asks nothing about
// what multiplexed it. It is a runtime component beside the tunnel, generic
// over the family: what a family governs reaches it as the two functions the
// generator renders, Decides and Asks.
//
// The boundary rule it is drawn by: Nightseam owns what can be stated in
// terms of the profile and the sess layer and is the same for every
// consumer — mechanism. A consumer owns what names a concept of its own or
// decides a policy. So this package routes a frame by its kind and its
// method, mints the downstream ids two consumers would collide on and maps
// the responses back, holds who has control and re-routes the open ask when
// it moves, and states what a log is and ships one in memory. It has no
// authentication, no rule about who may take control or for how long, no
// durable store, and no lifecycle of its own: a consumer decides those and
// calls in.
//
// A relay reads a frame as a JSON object and rewrites its id alone. Every
// other member reaches the other side verbatim, in the place it arrived in,
// so a member the relay does not know — a trace context, a member of a
// later profile — passes through by construction.
package session

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// Governance is a family's sess table: the Decides and Asks the generator
// renders for it.
type Governance struct {
	// Decides reports whether a method needs control to send.
	Decides func(method string) bool
	// Asks reports whether a method the machine sends raises a request the
	// holder of control must answer.
	Asks func(method string) bool
}

// Role is what a consumer attached to a session may do. Who is given which
// role is the consumer's to decide; what each may do is the profile's, and
// is here.
type Role int

const (
	// Participant may decide while it holds control, and may be given it.
	Participant Role = iota
	// Observer never decides: a deciding frame of its is refused with
	// not_controlling, and it is never given control.
	Observer
)

func (r Role) String() string {
	switch r {
	case Participant:
		return "participant"
	case Observer:
		return "observer"
	}
	return "role(" + strconv.Itoa(int(r)) + ")"
}

// Options limit a registry's sessions. Zero selects the default; a negative
// limit is refused with ErrorInvalidOptions.
type Options struct {
	// MaxAttachments is how many consumers may be attached to one session at
	// once; an attach beyond it is refused. Default: 64.
	MaxAttachments int
	// MaxInflight is how many requests a session may have open towards the
	// machine at once; one beyond it is refused with busy. Default: 256.
	MaxInflight int
	// SendTimeout is how long a frame may wait for a channel to take it. A
	// consumer that does not take its frames is detached rather than allowed
	// to hold up the session, and a machine that does not ends it. It also
	// bounds how long one stalled consumer delays the others, since frames
	// reach them in one order. Default: ten seconds.
	SendTimeout time.Duration
	// Observer is where a session bound over a connection that runs over no
	// peer — the seam's pipe, a bare socket, an in-process machine — tells
	// what it does. A connection that runs over one, a tunnel channel being
	// the one there is, tells that peer's observer instead and never this,
	// whether or not the peer was given an observer: the peer is the
	// observer the consumer already chose. Default: none, which observes
	// nothing and costs nothing, as a peer given no observer does.
	Observer runtime.Observer
}

func (o Options) normalized() (Options, error) {
	if o.MaxAttachments < 0 {
		return o, coded(ErrorInvalidOptions, "MaxAttachments must not be negative")
	}
	if o.MaxInflight < 0 {
		return o, coded(ErrorInvalidOptions, "MaxInflight must not be negative")
	}
	if o.SendTimeout < 0 {
		return o, coded(ErrorInvalidOptions, "SendTimeout must not be negative")
	}
	if o.MaxAttachments == 0 {
		o.MaxAttachments = 64
	}
	if o.MaxInflight == 0 {
		o.MaxInflight = 256
	}
	if o.SendTimeout == 0 {
		o.SendTimeout = 10 * time.Second
	}
	return o, nil
}

// Registry is every live session by its id: the one place a frame of a
// session is routed, and what a consumer's operations are written over.
type Registry struct {
	options  Options
	mu       sync.Mutex
	sessions map[string]*relay

	// watch guards the OnChange registrations alone, so that telling one
	// about a change takes no lock a registration calling back in would
	// wait for.
	watch    sync.RWMutex
	watchers []*watcher
}

// New makes a registry with the limits its sessions run under.
// Zero limits select their defaults; a negative limit returns a nil registry
// and an *Error with code ErrorInvalidOptions.
func New(options Options) (*Registry, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	return &Registry{options: options, sessions: map[string]*relay{}}, nil
}

// Bind gives a session its own connection — the machine's — the governance
// of the family it speaks and the log its frames are kept in. The session is
// live from here until that connection closes, which ends every consumer
// attached to it with the same close. The relay sends on it, receives from
// it and closes it, and asks nothing about what multiplexed it: a tunnel
// channel is one such connection, and so are the seam's pipe and a bare
// socket, which is what an in-process machine binds over.
//
// Where the session tells what it does follows from that connection: one
// that runs over a peer — a tunnel channel, which reports Peer() — tells
// that peer's observer, as it did when a channel was all this took; one that
// does not tells Options.Observer; and where there is neither, nothing,
// which is the no-op an observer already means.
//
// The log's head is learned here through Header, or through one Replay from
// its beginning, and the session goes on from it: a durable log replays its frames
// to a consumer that attaches after nothing, rather than waiting for the
// machine to speak for the session to learn where it is. A log that cannot
// report its head is not bound.
func (r *Registry) Bind(id string, up duplex.Conn, g Governance, log Log) error {
	switch {
	case id == "":
		return coded(ErrorSessionInvalid, "a session is bound under an id")
	case up == nil:
		return coded(ErrorSessionInvalid, "a session is bound over a connection")
	case g.Decides == nil || g.Asks == nil:
		return coded(ErrorSessionInvalid, "a session is bound with the family's Decides and Asks")
	case log == nil:
		return coded(ErrorSessionInvalid, "a session is bound with a log")
	}
	relay := newRelay(r, id, up, g, log)
	// Before the session is anyone's and before the pump reads a frame, so
	// that what the machine sends meanwhile is recorded above the head.
	if err := relay.seat(); err != nil {
		relay.cancel()
		return coded(ErrorSessionInvalid, "the session's log could not be read at bind: %v", err)
	}
	r.mu.Lock()
	if _, bound := r.sessions[id]; bound {
		r.mu.Unlock()
		// The relay built above is nobody's, so its context goes with it.
		relay.cancel()
		return coded(ErrorSessionExists, "session %q is bound", id)
	}
	r.sessions[id] = relay
	r.mu.Unlock()
	relay.sessionBound()
	go relay.pump()
	return nil
}

// Attach adds a consumer to a session over a connection of its own, in a
// role, saying what the consumer is — stamped on every frame it sends — and
// the last sequence it holds, from which the log is replayed to it before
// any live frame reaches it. The connection is the seam's, as the machine's
// is: a tunnel channel where one connection carries many consumers, a bare
// socket where it carries one. Who may attach is checked before calling
// here.
func (r *Registry) Attach(id string, down duplex.Conn, role Role, origin string, after int64) (*Attachment, error) {
	relay := r.session(id)
	if relay == nil {
		return nil, coded(ErrorNoSession, "no session %q is bound", id)
	}
	return relay.attach(down, role, origin, after)
}

// Control gives one of a session's participants control of it, or releases
// control where the holder is nil; a request the machine asked and nobody
// has answered follows to the new holder. Who may take control, and for how
// long, is the caller's to decide.
func (r *Registry) Control(id string, holder *Attachment) error {
	relay := r.session(id)
	if relay == nil {
		return coded(ErrorNoSession, "no session %q is bound", id)
	}
	return relay.control(holder)
}

// Attention is every session with a request the machine asked and nobody
// has answered, by id, in order.
func (r *Registry) Attention() []string {
	r.mu.Lock()
	relays := make([]*relay, 0, len(r.sessions))
	for _, relay := range r.sessions {
		relays = append(relays, relay)
	}
	r.mu.Unlock()
	ids := make([]string, 0, len(relays))
	for _, relay := range relays {
		if relay.asking() {
			ids = append(ids, relay.id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) session(id string) *relay {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

func (r *Registry) forget(id string, relay *relay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[id] == relay {
		delete(r.sessions, id)
	}
}

// Attachment is one consumer on a session: what it may do, what the caller
// said it is, and the connection it speaks over.
type Attachment struct {
	// Role is what the consumer may do.
	Role Role
	// Origin is what the caller said the consumer is; every frame it sends
	// is logged with it.
	Origin string
	// Channel is the connection it speaks the family over.
	Channel duplex.Conn

	relay  *relay
	ctx    context.Context
	cancel context.CancelFunc
	send   sync.Mutex
	once   sync.Once

	// state is what the relay last told this consumer of the session's own
	// vocabulary — who holds control, where in the log it stands — and the
	// registrations waiting on the first of them. It is a lock of its own so
	// that reading the state takes no turn on the connection.
	state    sync.Mutex
	holder   string
	held     bool
	sequence int64
	controls []*controlWatch
}

// Detach removes the consumer from its session and ends its connection; the
// session and every other consumer go on. Control it held is released, and
// an open ask waits for the next holder. It may be called more than once.
func (a *Attachment) Detach() { a.end(duplex.CodeNormal, "detached") }
