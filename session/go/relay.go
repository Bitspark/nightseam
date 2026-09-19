package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// relay is one bound session: the machine's channel, the consumers attached
// to it, who holds control, and what each side is waiting on. It is the
// routing table of the boundary rule, and nothing above it.
//
// Ids: every peer mints c:N per connection, so two consumers attached over
// a session's life both send c:1. The relay is the family's client towards
// the machine and mints the ids it sends up itself — c:N unique per session
// — keeping which consumer's request each stands for. The machine's own
// ids, s:N, are one peer's and travel down as they are.
type relay struct {
	registry   *Registry
	id         string
	up         *tunnel.Channel
	governance Governance
	log        Log
	options    Options

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	// upSend is the one goroutine at a time the seam allows on a channel,
	// and it is held across the log and the send, so that what the log says
	// the consumers sent is the order the machine saw.
	upSend sync.Mutex

	mu       sync.Mutex
	closed   bool
	attached []*Attachment
	holder   *Attachment
	minted   int64
	sequence int64
	inflight map[string]*pending // the relay's id to the consumer's
	routed   map[string]*routed  // the machine's id to where it went
}

// pending is one request a consumer has open towards the machine.
type pending struct {
	at *Attachment
	id string
}

// routed is one request the machine sent down, kept verbatim because
// control moving re-routes it to whoever holds it then, and with what it
// named and carried because whoever is told about that move is told what
// followed control.
type routed struct {
	message []byte
	id      string
	method  string
	trace   runtime.Trace
	asks    bool
	at      *Attachment
}

func newRelay(registry *Registry, id string, up *tunnel.Channel, g Governance, log Log) *relay {
	ctx, cancel := context.WithCancel(context.Background())
	return &relay{registry: registry, id: id, up: up, governance: g, log: log, options: registry.options,
		ctx: ctx, cancel: cancel, inflight: map[string]*pending{}, routed: map[string]*routed{}}
}

// seat places the relay's cursor at the log's head, so that a session bound
// over a log that already holds frames goes on from its end rather than from
// nothing: a consumer attaching before the machine has spoken is replayed
// what the log holds. It reads the log once, from after zero, and takes the
// last sequence Replay delivered, which is the head because Replay delivers
// in ascending sequence order.
//
// Run before the pump and under the relay's lock: a frame the machine sends
// while the cursor is being seated is recorded behind the read, above the
// head, rather than under a sequence the log has already given out. A Log
// that knows its head without a read may later say so as an optional
// interface the relay prefers where a log has one, which leaves every
// existing Log valid and this read what a log without it is bound by.
func (r *relay) seat() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	head := int64(0)
	if err := r.log.Replay(r.ctx, 0, func(frame Frame) error {
		head = frame.Sequence
		return nil
	}); err != nil {
		return err
	}
	r.sequence = head
	return nil
}

// pump reads the machine's channel until it ends.
func (r *relay) pump() {
	for {
		frame, err := r.up.Receive(r.ctx)
		if err != nil {
			r.end(err)
			return
		}
		if frame.Kind != duplex.Text {
			r.end(&duplex.CloseError{Code: duplex.CodeUnsupportedData, Reason: "a session speaks JSON text frames"})
			return
		}
		r.fromMachine(frame.Data)
	}
}

// fromMachine routes one frame of the machine's: an event to every attached
// channel, a response to the one consumer that asked for it, a request to
// the holder of control.
func (r *relay) fromMachine(data []byte) {
	m, err := decodeMessage(data)
	if err != nil {
		r.end(&duplex.CloseError{Code: duplex.CodeProtocolError, Reason: err.Error()})
		return
	}
	attached := r.record(Down, nil, m, data)
	switch m.kind() {
	case "event":
		for _, a := range attached {
			a.deliver(data)
		}
	case "response":
		if at, id, ok := r.resolve(m.id()); ok {
			at.deliver(m.withID(id))
		}
	case "request":
		asking := r.governance.Asks(m.method())
		at, ok := r.route(m, data, asking)
		if m.id() == "" {
			return
		}
		r.askRaised(m, asking)
		if ok {
			at.deliver(data)
			r.askRouted(at, m.id(), m.method(), m.trace())
		}
	case "cancel":
		if at, ok := r.withdraw(m.id()); ok {
			at.deliver(data)
		}
	}
}

// fromConsumer routes one frame of a consumer's. A frame that decides — a
// deciding method, a cancel, an answer to what the machine asked — is the
// holder's alone; anything else any consumer may send.
func (r *relay) fromConsumer(a *Attachment, data []byte) {
	m, err := decodeMessage(data)
	if err != nil {
		a.end(duplex.CodeProtocolError, err.Error())
		return
	}
	switch m.kind() {
	case "request":
		if m.id() == "" {
			return
		}
		if r.governance.Decides(m.method()) && !r.controls(a) {
			r.refuse(a, m, ErrorNotControlling, "The consumer does not hold control of the session.")
			return
		}
		minted, ok := r.mint(a, m.id())
		if !ok {
			r.refuse(a, m, ErrorBusy, "The session has too many requests open.")
			return
		}
		r.sendUp(a, m, m.withID(minted))
	case "cancel":
		// A cancel decides: it withdraws what the machine is working on.
		// Nothing answers a cancel, so a refused one is dropped.
		if !r.controls(a) {
			return
		}
		minted, ok := r.outgoing(a, m.id())
		if !ok {
			return
		}
		r.sendUp(a, m, m.withID(minted))
	case "response":
		// An answer to what the machine asked decides, so only the consumer
		// the request was routed to answers it.
		answered, ok := r.answered(a, m.id())
		if !ok {
			return
		}
		r.askAnswered(a, m.id(), answered.method, m.trace())
		r.sendUp(a, m, data)
	case "event":
		r.sendUp(a, m, data)
	}
}

// record appends a frame to the session's log and returns its sequence and
// the consumers there were when that sequence was assigned; one attaching
// between the two is not among them and takes the frame from the log's
// replay instead. from is the consumer whose frame it is, and nil for the
// machine's, which is what the frame's origin is read from.
func (r *relay) record(direction Direction, from *Attachment, m *message, data []byte) []*Attachment {
	origin := ""
	if from != nil {
		origin = from.Origin
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	sequence, err := r.log.Append(r.ctx, Frame{Direction: direction, Origin: origin, At: time.Now().UTC(), Message: data})
	if err != nil {
		r.mu.Unlock()
		// A session whose frames cannot be recorded is not a session.
		r.end(fmt.Errorf("the session's log refused a frame: %w", err))
		return nil
	}
	r.sequence = sequence
	attached := make([]*Attachment, len(r.attached))
	copy(attached, r.attached)
	r.mu.Unlock()
	r.frameAppended(sequence, direction, from, m, len(data))
	return attached
}

// sendUp records a consumer's frame and sends it to the machine, one
// goroutine at a time and in the order it recorded them.
func (r *relay) sendUp(a *Attachment, m *message, data []byte) {
	if data == nil {
		return
	}
	r.upSend.Lock()
	defer r.upSend.Unlock()
	r.record(Up, a, m, data)
	ctx, cancel := context.WithTimeout(r.ctx, r.options.SendTimeout)
	defer cancel()
	if err := r.up.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: data}); err != nil {
		r.end(err)
	}
}

// refusal is the one frame a relay writes of itself: a response to a
// consumer for a request the machine never saw, which is why it is not in
// the log.
type refusal struct {
	Version int                  `json:"version"`
	Kind    string               `json:"kind"`
	ID      string               `json:"id"`
	Error   *runtime.PublicError `json:"error"`
}

func (r *relay) refuse(a *Attachment, m *message, code, reason string) {
	data, err := json.Marshal(refusal{Version: 1, Kind: "response", ID: m.id(), Error: &runtime.PublicError{Code: code, Message: reason}})
	if err != nil {
		return
	}
	r.refused(a, code, m)
	a.deliver(data)
}

func (r *relay) controls(a *Attachment) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return a.Role == Participant && r.holder == a
}

// mint gives a consumer's request an id of the session's own.
func (r *relay) mint(a *Attachment, id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.inflight) >= r.options.MaxInflight {
		return "", false
	}
	r.minted++
	minted := "c:" + strconv.FormatInt(r.minted, 10)
	r.inflight[minted] = &pending{at: a, id: id}
	return minted, true
}

// outgoing is the session's id for a request a consumer sent under its own.
func (r *relay) outgoing(a *Attachment, id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, open := range r.inflight {
		if open.at == a && open.id == id {
			return name, true
		}
	}
	return "", false
}

// resolve gives a response of the machine's back to the consumer that
// asked, under the id it asked with.
func (r *relay) resolve(id string) (*Attachment, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	open, ok := r.inflight[id]
	if !ok {
		return nil, "", false
	}
	delete(r.inflight, id)
	return open.at, open.id, true
}

// route sends a request of the machine's to the holder of control and keeps
// it while nobody answers, so that control moving carries it along.
func (r *relay) route(m *message, data []byte, asks bool) (*Attachment, bool) {
	if m.id() == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, false
	}
	kept := make([]byte, len(data))
	copy(kept, data)
	r.routed[m.id()] = &routed{message: kept, id: m.id(), method: m.method(), trace: m.trace(), asks: asks, at: r.holder}
	return r.holder, r.holder != nil
}

// answered closes a request of the machine's that the consumer it was
// routed to has answered, and gives back what it was.
func (r *relay) answered(a *Attachment, id string) (*routed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	open, ok := r.routed[id]
	if !ok || open.at != a {
		return nil, false
	}
	delete(r.routed, id)
	return open, true
}

// withdraw closes a request of the machine's that the machine cancelled.
func (r *relay) withdraw(id string) (*Attachment, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	open, ok := r.routed[id]
	if !ok {
		return nil, false
	}
	delete(r.routed, id)
	return open.at, open.at != nil
}

// asking reports whether the machine asked something nobody has answered.
func (r *relay) asking() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, open := range r.routed {
		if open.asks {
			return true
		}
	}
	return false
}

// control moves control of the session and re-routes every request the
// machine is waiting on to whoever holds it now.
func (r *relay) control(holder *Attachment) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("the session ended")
	}
	if holder != nil {
		if holder.Role != Participant {
			r.mu.Unlock()
			return fmt.Errorf("an %s is never given control", holder.Role)
		}
		if holder.relay != r || !r.holds(holder) {
			r.mu.Unlock()
			return errors.New("the holder is not attached to this session")
		}
	}
	r.holder = holder
	open := make([]routed, 0, len(r.routed))
	for _, request := range r.routed {
		request.at = holder
		open = append(open, *request)
	}
	r.mu.Unlock()
	r.controlChanged(holder)
	if holder == nil {
		return nil
	}
	for _, request := range open {
		holder.deliver(request.message)
		r.askRouted(holder, request.id, request.method, request.trace)
	}
	return nil
}

// holds reports whether an attachment is one of the session's; the caller
// holds the lock.
func (r *relay) holds(a *Attachment) bool {
	for _, attached := range r.attached {
		if attached == a {
			return true
		}
	}
	return false
}

// attach adds a consumer and replays the log to it before any live frame
// reaches it: the frames after its sequence, up to the one the session had
// reached when it was added, in one order.
func (r *relay) attach(down *tunnel.Channel, role Role, origin string, after int64) (*Attachment, error) {
	if down == nil {
		return nil, errors.New("a consumer attaches over a channel")
	}
	if role != Participant && role != Observer {
		return nil, fmt.Errorf("unknown role %s", role)
	}
	if after < 0 {
		return nil, errors.New("a consumer resumes from a sequence")
	}
	ctx, cancel := context.WithCancel(r.ctx)
	a := &Attachment{Role: role, Origin: origin, Channel: down, relay: r, ctx: ctx, cancel: cancel}
	// Held from before the consumer is one of the session's, so that a frame
	// the session records meanwhile waits behind the replay rather than
	// overtaking it.
	a.send.Lock()
	r.mu.Lock()
	switch {
	case r.closed:
		r.mu.Unlock()
		a.send.Unlock()
		cancel()
		return nil, errors.New("the session ended")
	case len(r.attached) >= r.options.MaxAttachments:
		r.mu.Unlock()
		a.send.Unlock()
		cancel()
		return nil, fmt.Errorf("session %q has %d consumers attached", r.id, len(r.attached))
	}
	r.attached = append(r.attached, a)
	ceiling := r.sequence
	r.mu.Unlock()
	// Told before the replay runs, because the consumer is one of the
	// session's from here: a frame recorded meanwhile is already its own and
	// waits behind the replay rather than before the attachment.
	r.sessionAttached(a, after)
	err := r.replay(a, after, ceiling)
	a.send.Unlock()
	if err != nil {
		a.Detach()
		return nil, err
	}
	go a.pump()
	return a, nil
}

// errReplayed ends a replay at the sequence the session had reached when
// the consumer was added; what came after reaches it live.
var errReplayed = errors.New("the replay reached the session's sequence")

func (r *relay) replay(a *Attachment, after, ceiling int64) error {
	if ceiling <= after {
		return nil
	}
	err := r.log.Replay(r.ctx, after, func(frame Frame) error {
		if frame.Sequence > ceiling {
			return errReplayed
		}
		// A cut message is not a message: what the log truncated is there for
		// a consumer that reads the log, not for a channel that speaks the
		// family.
		if frame.Truncated {
			return nil
		}
		return a.sendHeld(frame.Message)
	})
	if errors.Is(err, errReplayed) {
		return nil
	}
	return err
}

// detach removes a consumer from the session, releasing control it held —
// an open ask then waits for the next holder — and dropping what it asked
// the machine, whose answers no longer have anywhere to go.
func (r *relay) detach(a *Attachment) (removed, released bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, attached := range r.attached {
		if attached == a {
			r.attached = append(r.attached[:i], r.attached[i+1:]...)
			removed = true
			break
		}
	}
	if r.holder == a {
		r.holder, released = nil, true
	}
	for id, open := range r.inflight {
		if open.at == a {
			delete(r.inflight, id)
		}
	}
	for _, open := range r.routed {
		if open.at == a {
			open.at = nil
		}
	}
	return removed, released
}

// end ends the session: every consumer's channel is closed with the close
// the machine's carried, and the registry forgets it.
func (r *relay) end(err error) {
	r.once.Do(func() {
		code, reason := duplex.CodeGoingAway, "the session's channel closed"
		var closed *duplex.CloseError
		switch {
		case errors.As(err, &closed):
			code, reason = closed.Code, closed.Reason
		case err != nil && !errors.Is(err, duplex.ErrClosed) && !errors.Is(err, context.Canceled):
			code, reason = duplex.CodeInternalError, err.Error()
		}
		r.registry.forget(r.id, r)
		r.mu.Lock()
		r.closed = true
		attached := r.attached
		r.attached, r.holder = nil, nil
		r.mu.Unlock()
		for _, a := range attached {
			a.end(code, reason)
		}
		// Last, and after every consumer it carried is gone: a session is
		// unbound once there is nothing left of it to be told about.
		r.sessionUnbound(code, reason)
		ctx, cancel := context.WithTimeout(context.Background(), r.options.SendTimeout)
		_ = r.up.Close(ctx, code, reason)
		cancel()
		r.cancel()
	})
}

// pump reads a consumer's channel until it ends; its ending detaches it and
// nothing else.
func (a *Attachment) pump() {
	for {
		frame, err := a.Channel.Receive(a.ctx)
		if err != nil {
			a.end(duplex.CodeNormal, "detached")
			return
		}
		if frame.Kind != duplex.Text {
			a.end(duplex.CodeUnsupportedData, "a session speaks JSON text frames")
			return
		}
		a.relay.fromConsumer(a, frame.Data)
	}
}

// deliver sends one frame to the consumer, after every frame sent to it
// before; a consumer that does not take them is detached rather than
// allowed to hold up the session.
func (a *Attachment) deliver(data []byte) {
	a.send.Lock()
	err := a.sendHeld(data)
	a.send.Unlock()
	if err != nil {
		a.end(duplex.CodePolicyViolation, "the consumer did not take the session's frames")
	}
}

// sendHeld sends one frame with the consumer's turn already taken.
func (a *Attachment) sendHeld(data []byte) error {
	if data == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, a.relay.options.SendTimeout)
	defer cancel()
	return a.Channel.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: data})
}

func (a *Attachment) end(code duplex.Code, reason string) {
	a.once.Do(func() {
		// A consumer the session had already let go of — one the session's
		// own ending took with it — is no detachment: the session says it
		// ended, once, rather than saying goodbye to each of them. And one
		// that left holding control leaves the session with nobody holding
		// it, which is the change releasing control by hand makes.
		removed, released := a.relay.detach(a)
		if released {
			a.relay.controlChanged(nil)
		}
		if removed {
			a.relay.sessionDetached(a)
		}
		a.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), a.relay.options.SendTimeout)
		_ = a.Channel.Close(ctx, code, reason)
		cancel()
	})
}
