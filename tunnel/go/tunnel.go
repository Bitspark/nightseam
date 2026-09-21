// Package tunnel multiplexes channels over one peer of the nightseam.duplex/1
// profile: a third transport beneath the seam, after the WebSocket and the
// pipe. Either side opens a channel; each channel is a duplex.Conn, held to
// duplextest.Run as every transport is, and a peer of any family runs over
// it unchanged. The outer peer sees four operations of the profile's own —
// channel.open, a request; channel.frame, channel.credit and channel.close,
// events — and never what a channel carries: an inner frame is opaque to it,
// a text or a base64 string inside an event.
//
// Flow control is per channel, by credit. Each side may have at most a
// window of frames in flight to the other on a channel, the window the
// other side declared when the channel was opened, and a receiver returns
// credit as its consumer takes frames. A channel whose consumer does not
// receive therefore stalls its own sender and nothing else: the outer peer's
// queues are never the buffer, because the outer peer ends a connection
// whose events it cannot deliver, and that would end every channel.
//
// A channel's id is chosen by the side that opens it, odd for the client of
// the outer connection and even for its server, so both sides may open
// without a collision, and the id is what a handle names: {"channel": 12}
// in a family's message refers to channel 12 of the connection that carries
// the message, whichever side opened it.
package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The profile's operations of the tunnel.
const (
	OpenMethod  = "channel.open"
	FrameEvent  = "channel.frame"
	CreditEvent = "channel.credit"
	CloseEvent  = "channel.close"
)

// The public errors an open is refused with.
const (
	ErrorRefused          = "channel_refused"
	ErrorExists           = "channel_exists"
	ErrorInvalid          = "channel_invalid"
	ErrorContractMismatch = "contract_mismatch"
)

// Options limit a tunnel. Zero values select the defaults.
type Options struct {
	// Contracts names the locally known family digests used to check an
	// incoming channel before admitting it. An absent digest on either side
	// makes no revision claim. Values come from generated WireDigest functions.
	Contracts map[string]string
	// MaxFrameBytes bounds a frame received over a channel: a larger one is
	// refused before delivery and the channel with it, as the seam requires.
	// The outer peer's own limit bounds the event that carries a frame, and
	// so what a sender may send. Zero: one mebibyte.
	MaxFrameBytes int64
	// Window is how many frames the other side may have in flight to this
	// one on a channel before its Send waits for credit; it is declared to
	// the other side when a channel opens. Zero: 32.
	Window int
	// AcceptCapacity is how many channels the other side may have opened
	// that nobody here has accepted or resolved yet; an open beyond it is
	// refused with channel_refused. Zero: 64.
	AcceptCapacity int
}

func (o Options) normalized() (Options, error) {
	o.Contracts = maps.Clone(o.Contracts)
	for family, digest := range o.Contracts {
		if family == "" || (digest != "" && !validDigest(digest)) {
			return o, errors.New("a tunnel contract needs a family and an absent or lowercase SHA-256 digest")
		}
	}
	if o.MaxFrameBytes < 0 || o.Window < 0 || o.AcceptCapacity < 0 {
		return o, errors.New("tunnel limits must not be negative")
	}
	if o.MaxFrameBytes == 0 {
		o.MaxFrameBytes = 1 << 20
	}
	if o.Window == 0 {
		o.Window = 32
	}
	if o.AcceptCapacity == 0 {
		o.AcceptCapacity = 64
	}
	return o, nil
}

// Tunnel is the channels of one outer peer.
type Tunnel struct {
	peer    *runtime.Peer
	options Options
	mu      sync.Mutex
	next    int64
	parity  int64
	table   map[int64]*Channel
	pending []*Channel
	wake    chan struct{}
}

// New multiplexes channels over a peer, registering the tunnel's operations
// on it; a peer carries one tunnel.
func New(peer *runtime.Peer, options Options) (*Tunnel, error) {
	if peer == nil {
		return nil, errors.New("a tunnel needs a peer")
	}
	o, err := options.normalized()
	if err != nil {
		return nil, err
	}
	t := &Tunnel{peer: peer, options: o, next: 1, parity: 1, table: map[int64]*Channel{}, wake: make(chan struct{}, 1)}
	if peer.Role() == runtime.ServerRole {
		t.next, t.parity = 2, 0
	}
	if err := peer.Handle(OpenMethod, t.onOpen); err != nil {
		return nil, err
	}
	for name, handler := range map[string]runtime.EventHandler{FrameEvent: t.onFrame, CreditEvent: t.onCredit, CloseEvent: t.onClose} {
		if err := peer.HandleEvent(name, handler); err != nil {
			return nil, err
		}
	}
	go t.watch()
	return t, nil
}

// Peer is the outer peer the tunnel runs over.
func (t *Tunnel) Peer() *runtime.Peer { return t.peer }

// watch ends every channel with going away once the outer peer is done.
func (t *Tunnel) watch() {
	<-t.peer.Done()
	t.mu.Lock()
	channels := make([]*Channel, 0, len(t.table))
	for _, c := range t.table {
		channels = append(channels, c)
	}
	t.table = map[int64]*Channel{}
	t.pending = nil
	t.mu.Unlock()
	for _, c := range channels {
		c.endRemote(&duplex.CloseError{Code: duplex.CodeGoingAway, Reason: "the connection carrying the channel closed"})
	}
}

type openParams struct {
	Channel int64  `json:"channel"`
	Family  string `json:"family"`
	Digest  string `json:"digest,omitempty"`
	Window  int    `json:"window"`
}

type openResult struct {
	Window int `json:"window"`
}

type framePayload struct {
	Channel int64   `json:"channel"`
	Text    *string `json:"text,omitempty"`
	Binary  *string `json:"binary,omitempty"`
}

type creditPayload struct {
	Channel int64 `json:"channel"`
	Frames  int   `json:"frames"`
}

type closePayload struct {
	Channel int64  `json:"channel"`
	Code    int    `json:"code"`
	Reason  string `json:"reason"`
}

// Open opens a channel to the other side, saying what family it speaks, and
// returns it once the other side accepted it.
func (t *Tunnel) Open(ctx context.Context, family, digest string) (*Channel, error) {
	if family == "" {
		return nil, t.refused(family, errors.New("a channel is opened for a family"))
	}
	if digest != "" && !validDigest(digest) {
		return nil, t.refuse(family, ErrorInvalid, "a channel digest is lowercase SHA-256 hex")
	}
	t.mu.Lock()
	id := t.next
	t.next += 2
	c := t.newChannel(id, family, digest, 0)
	t.table[id] = c
	t.mu.Unlock()
	var result openResult
	if err := t.peer.Call(ctx, OpenMethod, openParams{Channel: id, Family: family, Digest: digest, Window: t.options.Window}, &result); err != nil {
		t.remove(id)
		c.endLocal(duplex.CodeNormal, "")
		return nil, t.refused(family, err)
	}
	if result.Window <= 0 {
		t.remove(id)
		c.endLocal(duplex.CodeNormal, "")
		return nil, t.refused(family, errors.New("the other side declared no window"))
	}
	c.grant(result.Window)
	t.observeOpened(c, true)
	return c, nil
}

// Accept returns the next channel the other side opened that nobody here
// has taken yet, by Accept or by Channel.
func (t *Tunnel) Accept(ctx context.Context) (*Channel, error) {
	for {
		t.mu.Lock()
		if len(t.pending) > 0 {
			c := t.pending[0]
			t.pending = t.pending[1:]
			t.mu.Unlock()
			t.observeOpened(c, false)
			t.observeAccepted(c)
			return c, nil
		}
		t.mu.Unlock()
		select {
		case <-t.wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.peer.Done():
			return nil, duplex.ErrClosed
		}
	}
}

// Channel resolves an id to the channel it names, whichever side opened it;
// a channel the other side opened counts as taken.
func (t *Tunnel) Channel(id int64) (*Channel, bool) {
	t.mu.Lock()
	c, ok := t.table[id]
	taken := false
	for i, p := range t.pending {
		if ok && p == c {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			taken = true
			break
		}
	}
	t.mu.Unlock()
	if !ok {
		return nil, false
	}
	if taken {
		t.observeOpened(c, false)
		t.observeAccepted(c)
	}
	return c, true
}

func (t *Tunnel) remove(id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.table[id]
	if !ok {
		return
	}
	delete(t.table, id)
	for i, p := range t.pending {
		if p == c {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			break
		}
	}
}

func (t *Tunnel) lookup(id int64) *Channel {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.table[id]
}

// onOpen accepts a channel the other side opens: its id must be the other
// side's to give, unused, and there must be room among the channels nobody
// has taken.
func (t *Tunnel) onOpen(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
	var params openParams
	if err := json.Unmarshal(raw, &params); err != nil || params.Channel <= 0 || params.Family == "" || params.Window <= 0 {
		return nil, t.refuse(params.Family, ErrorInvalid, "channel.open needs a positive channel id of the opener's parity, a family and a window")
	}
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	if _, present := members["digest"]; present && !validDigest(params.Digest) {
		return nil, t.refuse(params.Family, ErrorInvalid, "a channel digest is lowercase SHA-256 hex")
	}
	if expected := t.options.Contracts[params.Family]; expected != "" && params.Digest != "" && params.Digest != expected {
		return nil, t.refuse(params.Family, ErrorContractMismatch, fmt.Sprintf("the declaration digest for %s differs", params.Family))
	}
	c, code, message := t.admit(params)
	if c == nil {
		return nil, t.refuse(params.Family, code, message)
	}
	t.observeOpened(c, false)
	return openResult{Window: t.options.Window}, nil
}

// admit takes the channel an open names, or says with what code and message
// the open is refused. The table is the tunnel's own and nobody is told while
// it is held: an observer runs where the event happened, and this one would
// otherwise run under the lock every channel of the tunnel waits on.
func (t *Tunnel) admit(params openParams) (*Channel, string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if params.Channel%2 == t.parity {
		return nil, ErrorInvalid, "the channel id is of this side's parity"
	}
	if _, exists := t.table[params.Channel]; exists {
		return nil, ErrorExists, fmt.Sprintf("channel %d is open", params.Channel)
	}
	if len(t.pending) >= t.options.AcceptCapacity {
		return nil, ErrorRefused, "no room for a channel nobody has accepted"
	}
	c := t.newChannel(params.Channel, params.Family, params.Digest, params.Window)
	t.table[params.Channel] = c
	t.pending = append(t.pending, c)
	select {
	case t.wake <- struct{}{}:
	default:
	}
	return c, "", ""
}

// refuse tells of an open this side refuses and returns the refusal the other
// side sees, which is the reason told here.
func (t *Tunnel) refuse(family, code, message string) error {
	t.observeRefused(family, message)
	return &runtime.PublicError{Code: code, Message: message}
}

func (t *Tunnel) onFrame(_ context.Context, _ *runtime.Peer, raw json.RawMessage) {
	var payload framePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	c := t.lookup(payload.Channel)
	if c == nil {
		return
	}
	var frame duplex.Frame
	switch {
	case payload.Text != nil && payload.Binary == nil:
		frame = duplex.Frame{Kind: duplex.Text, Data: []byte(*payload.Text)}
	case payload.Binary != nil && payload.Text == nil:
		data, err := base64.StdEncoding.DecodeString(*payload.Binary)
		if err != nil {
			c.fail(duplex.CodeProtocolError, "a binary frame that is not base64")
			return
		}
		frame = duplex.Frame{Kind: duplex.Binary, Data: data}
	default:
		c.fail(duplex.CodeProtocolError, "a frame that is neither text nor binary")
		return
	}
	if int64(len(frame.Data)) > t.options.MaxFrameBytes {
		c.fail(duplex.CodeTooLarge, fmt.Sprintf("a frame of %d bytes exceeds the limit of %d", len(frame.Data), t.options.MaxFrameBytes))
		return
	}
	// The inbox is one window deep, so a sender that ignores the credit this
	// side returned finds no room rather than a queue that grows to its
	// choosing; the TypeScript channel bounds what it holds the same way.
	select {
	case c.inbox <- frame:
	default:
		c.fail(duplex.CodeProtocolError, fmt.Sprintf("a frame beyond the window of %d", t.options.Window))
	}
}

func (t *Tunnel) onCredit(_ context.Context, _ *runtime.Peer, raw json.RawMessage) {
	var payload creditPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Frames <= 0 {
		return
	}
	if c := t.lookup(payload.Channel); c != nil {
		c.grant(payload.Frames)
	}
}

func (t *Tunnel) onClose(_ context.Context, _ *runtime.Peer, raw json.RawMessage) {
	var payload closePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	c := t.lookup(payload.Channel)
	if c == nil {
		return
	}
	t.remove(payload.Channel)
	c.endRemote(&duplex.CloseError{Code: duplex.Code(payload.Code), Reason: payload.Reason})
}

// Channel is one channel of a tunnel: a duplex.Conn, and what the opener
// said of it — its id on the outer connection and the family it speaks.
type Channel struct {
	ID     int64
	Family string
	Digest string

	t        *Tunnel
	inbox    chan duplex.Frame
	mu       sync.Mutex
	credit   int
	waiting  int
	wake     chan struct{}
	taken    int
	closed   bool
	remote   *duplex.CloseError
	dead     error
	done     chan struct{}
	once     sync.Once
	announce sync.Once
	opened   bool
}

func (t *Tunnel) newChannel(id int64, family, digest string, credit int) *Channel {
	return &Channel{ID: id, Family: family, Digest: digest, t: t, inbox: make(chan duplex.Frame, t.options.Window), credit: credit, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func validDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, c := range digest {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

var _ duplex.Conn = (*Channel)(nil)

func (c *Channel) grant(frames int) {
	c.mu.Lock()
	c.credit += frames
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// state is what Send and Receive return once the channel ended, nil while
// it is open.
func (c *Channel) state() error {
	switch {
	case c.closed:
		return duplex.ErrClosed
	case c.dead != nil:
		return c.dead
	case c.remote != nil:
		return &duplex.CloseError{Code: c.remote.Code, Reason: c.remote.Reason}
	}
	return nil
}

// end ends the channel once, with the code and the reason the other side sees
// for it, and tells of the close. A channel nobody was told of closes to
// nobody: taking the announcement here leaves an open that never got that far
// with nothing to say, and opened is what the announcement set.
func (c *Channel) end(code duplex.Code, reason string, set func()) {
	c.once.Do(func() {
		c.mu.Lock()
		set()
		c.mu.Unlock()
		close(c.done)
		c.announce.Do(func() {})
		if c.opened {
			c.t.observeClosed(c, code, reason)
		}
	})
}

func (c *Channel) endLocal(code duplex.Code, reason string) {
	c.end(code, reason, func() { c.closed = true })
}

func (c *Channel) endRemote(closed *duplex.CloseError) {
	c.end(closed.Code, closed.Reason, func() { c.remote = closed })
}

// fail ends the channel on a frame it refuses, telling the other side why.
func (c *Channel) fail(code duplex.Code, reason string) {
	c.end(code, reason, func() { c.dead = fmt.Errorf("channel %d refused a frame: %s", c.ID, reason) })
	c.t.remove(c.ID)
	c.tell(code, reason)
}

// tell sends the close to the other side, best effort and briefly.
func (c *Channel) tell(code duplex.Code, reason string) {
	ctx, cancel := context.WithTimeout(c.t.peer.Context(), time.Second)
	defer cancel()
	_ = c.t.peer.Emit(ctx, CloseEvent, closePayload{Channel: c.ID, Code: int(code), Reason: reason})
}

// Send writes one frame after every frame sent before it, waiting for credit
// while the other side's window is full, until ctx ends.
func (c *Channel) Send(ctx context.Context, frame duplex.Frame) error {
	if frame.Kind != duplex.Text && frame.Kind != duplex.Binary {
		return duplex.ErrNoKind
	}
	if err := c.take(ctx); err != nil {
		return err
	}
	payload := framePayload{Channel: c.ID}
	if frame.Kind == duplex.Text {
		text := string(frame.Data)
		payload.Text = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(frame.Data)
		payload.Binary = &encoded
	}
	return c.t.peer.Emit(ctx, FrameEvent, payload)
}

// take spends one frame's credit, waiting while the other side's window is
// full until ctx ends or the channel does. A send that waits at all stalls,
// and says so once however often it wakes; what it says is how many senders
// are then waiting on the channel, this one among them.
func (c *Channel) take(ctx context.Context) error {
	stalled := false
	defer func() {
		if !stalled {
			return
		}
		c.mu.Lock()
		c.waiting--
		c.mu.Unlock()
	}()
	for {
		c.mu.Lock()
		if err := c.state(); err != nil {
			c.mu.Unlock()
			return err
		}
		if c.credit > 0 {
			c.credit--
			c.mu.Unlock()
			return nil
		}
		waiting := 0
		if !stalled {
			c.waiting++
			waiting, stalled = c.waiting, true
		}
		c.mu.Unlock()
		if stalled && waiting > 0 {
			c.t.observeStall(c, waiting)
		}
		select {
		case <-c.wake:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
		}
	}
}

// Receive returns the next frame in the order it was sent; what arrived
// before the other side closed is delivered before its close is.
func (c *Channel) Receive(ctx context.Context) (duplex.Frame, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return duplex.Frame{}, duplex.ErrClosed
	}
	select {
	case frame := <-c.inbox:
		return c.took(frame)
	default:
	}
	select {
	case frame := <-c.inbox:
		return c.took(frame)
	case <-ctx.Done():
		return duplex.Frame{}, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.state()
		dead := c.dead != nil
		c.mu.Unlock()
		if !dead {
			select {
			case frame := <-c.inbox:
				return c.took(frame)
			default:
			}
		}
		return duplex.Frame{}, err
	}
}

// took returns credit for what the consumer took, in halves of the window.
func (c *Channel) took(frame duplex.Frame) (duplex.Frame, error) {
	c.mu.Lock()
	c.taken++
	frames := 0
	if c.taken >= max(c.t.options.Window/2, 1) {
		frames, c.taken = c.taken, 0
	}
	c.mu.Unlock()
	if frames > 0 {
		ctx, cancel := context.WithTimeout(c.t.peer.Context(), time.Second)
		_ = c.t.peer.Emit(ctx, CreditEvent, creditPayload{Channel: c.ID, Frames: frames})
		cancel()
	}
	return frame, nil
}

// Close ends the channel with a code and a reason the other side will see.
func (c *Channel) Close(ctx context.Context, code duplex.Code, reason string) error {
	c.mu.Lock()
	ended := c.state() != nil
	c.mu.Unlock()
	c.endLocal(code, reason)
	c.t.remove(c.ID)
	if !ended {
		_ = c.t.peer.Emit(ctx, CloseEvent, closePayload{Channel: c.ID, Code: int(code), Reason: reason})
	}
	return nil
}

// Abort ends the channel at once; the other side sees an abnormal closure,
// as it would a dropped transport.
func (c *Channel) Abort() error {
	c.mu.Lock()
	ended := c.state() != nil
	c.mu.Unlock()
	c.endLocal(duplex.CodeAbnormalClosure, "")
	c.t.remove(c.ID)
	if !ended {
		c.tell(duplex.CodeAbnormalClosure, "")
	}
	return nil
}
