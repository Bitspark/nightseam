// Package runtime implements the nightseam.duplex/1 profile over a frames
// duplex connection (duplex): JSON text frames carrying requests,
// responses, events and cancellations. It never touches a WebSocket; Dial
// and Accept open one and hand it over as a connection, and a Peer over any
// other transport speaks the same profile byte for byte. It has no
// application authorization, replay, retries, or persistence policy.
package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

const Profile = "nightseam.duplex/1"

type Role string

const (
	ClientRole Role = "client"
	ServerRole Role = "server"
)

var (
	ErrClosed       = errors.New("duplex connection closed")
	ErrBackpressure = errors.New("duplex consumer is stalled")
)

// PublicError is safe to send to the remote caller. Other handler errors are
// replaced by a generic internal error; their messages are not disclosed.
type PublicError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *PublicError) Error() string { return e.Code + ": " + e.Message }

type Handler func(context.Context, *Peer, json.RawMessage) (any, error)
type EventHandler func(context.Context, *Peer, json.RawMessage)
type Event struct {
	Name string          `json:"event"`
	Data json.RawMessage `json:"data"`
}

// Options limits are per connection. Zero values select the documented defaults.
// Handlers may run concurrently; event callbacks run serially in receive order.
type Options struct {
	Handlers              map[string]Handler
	Events                map[string]EventHandler
	MaxConcurrentHandlers int
	QueueCapacity         int
	MaxFrameBytes         int64
	RequestTimeout        time.Duration
	WriteTimeout          time.Duration
}

func (o Options) normalized() (Options, error) {
	if o.MaxConcurrentHandlers < 0 || o.QueueCapacity < 0 || o.MaxFrameBytes < 0 || o.RequestTimeout < 0 || o.WriteTimeout < 0 {
		return o, errors.New("duplex limits must be positive")
	}
	if o.MaxConcurrentHandlers == 0 {
		o.MaxConcurrentHandlers = 64
	}
	if o.QueueCapacity == 0 {
		o.QueueCapacity = 128
	}
	if o.MaxFrameBytes == 0 {
		o.MaxFrameBytes = 1 << 20
	}
	if o.RequestTimeout == 0 {
		o.RequestTimeout = 30 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 10 * time.Second
	}
	for name, h := range o.Handlers {
		if name == "" || h == nil {
			return o, errors.New("invalid duplex method handler")
		}
	}
	for name, h := range o.Events {
		if name == "" || h == nil {
			return o, errors.New("invalid duplex event handler")
		}
	}
	return o, nil
}

type frame struct {
	Version int             `json:"version"`
	Kind    string          `json:"kind"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *PublicError    `json:"error,omitempty"`
	Event   string          `json:"event,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type pendingResult struct {
	result json.RawMessage
	err    error
}

// Peer owns a frames duplex connection until Close or transport failure.
// Never send on or receive from the connection after handing it to NewPeer.
// The connection's receive limit is its maker's to set to MaxFrameBytes;
// the peer refuses a larger frame it is nonetheless handed.
type Peer struct {
	conn          duplex.Conn
	ctx           context.Context
	cancel        context.CancelFunc
	options       Options
	prefix        string
	remotePrefix  string
	next          atomic.Uint64
	done          chan struct{}
	once          sync.Once
	mu            sync.Mutex
	err           error
	pending       map[string]chan pendingResult
	incoming      map[string]context.CancelFunc
	handlers      map[string]Handler
	eventHandlers map[string]EventHandler
	listeners     map[uint64]func(context.Context, Event)
	listenerID    uint64
	outputs       chan []byte
	events        chan Event
	slots         chan struct{}
}

func NewPeer(ctx context.Context, conn duplex.Conn, role Role, options Options) (*Peer, error) {
	if ctx == nil || conn == nil {
		return nil, errors.New("duplex requires a context and connection")
	}
	if role != ClientRole && role != ServerRole {
		return nil, errors.New("invalid duplex role")
	}
	o, err := options.normalized()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Peer{conn: conn, ctx: ctx, cancel: cancel, options: o, prefix: "c:", remotePrefix: "s:", done: make(chan struct{}),
		pending: make(map[string]chan pendingResult), incoming: make(map[string]context.CancelFunc), handlers: make(map[string]Handler),
		eventHandlers: make(map[string]EventHandler), listeners: make(map[uint64]func(context.Context, Event)),
		outputs: make(chan []byte, o.QueueCapacity), events: make(chan Event, o.QueueCapacity), slots: make(chan struct{}, o.MaxConcurrentHandlers)}
	if role == ServerRole {
		p.prefix, p.remotePrefix = "s:", "c:"
	}
	for k, v := range o.Handlers {
		p.handlers[k] = v
	}
	for k, v := range o.Events {
		p.eventHandlers[k] = v
	}
	go p.readLoop()
	go p.writeLoop()
	go p.eventLoop()
	go func() { <-ctx.Done(); p.fail(ctx.Err()) }()
	return p, nil
}

func (p *Peer) Done() <-chan struct{}    { return p.done }
func (p *Peer) Context() context.Context { return p.ctx }

// Role is the side of the connection this peer is: it prefixes the request
// ids it mints, and a tunnel over it chooses channel ids by it.
func (p *Peer) Role() Role {
	if p.prefix == "s:" {
		return ServerRole
	}
	return ClientRole
}

// MaxFrameBytes is the largest frame this peer sends or receives.
func (p *Peer) MaxFrameBytes() int64 { return p.options.MaxFrameBytes }
func (p *Peer) Err() error           { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) Close() error         { p.fail(ErrClosed); return nil }

func (p *Peer) fail(err error) {
	p.once.Do(func() {
		if err == nil {
			err = ErrClosed
		}
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		p.cancel()
		close(p.done)
		// An abort avoids a close-handshake wait after a stalled consumer or peer.
		_ = p.conn.Abort()
	})
}

// Handle registers a method. Duplicate registrations are rejected.
func (p *Peer) Handle(method string, handler Handler) error {
	if method == "" || handler == nil {
		return errors.New("invalid duplex method handler")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	if _, exists := p.handlers[method]; exists {
		return fmt.Errorf("method %q already registered", method)
	}
	p.handlers[method] = handler
	return nil
}

func (p *Peer) HandleEvent(name string, handler EventHandler) error {
	if name == "" || handler == nil {
		return errors.New("invalid duplex event handler")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	if _, exists := p.eventHandlers[name]; exists {
		return fmt.Errorf("event %q already registered", name)
	}
	p.eventHandlers[name] = handler
	return nil
}

// OnEvent observes every event and returns an idempotent unsubscribe function.
// Slow callbacks consume the bounded event queue and can disconnect the peer.
func (p *Peer) OnEvent(listener func(context.Context, Event)) func() {
	if listener == nil {
		return func() {}
	}
	p.mu.Lock()
	p.listenerID++
	id := p.listenerID
	p.listeners[id] = listener
	p.mu.Unlock()
	return func() { p.mu.Lock(); delete(p.listeners, id); p.mu.Unlock() }
}

// Call sends one request and waits for its response. It never retries. A caller
// cancellation also sends best-effort cancellation to the remote handler.
func (p *Peer) Call(ctx context.Context, method string, params, result any) error {
	if ctx == nil || method == "" {
		return errors.New("duplex call requires context and method")
	}
	ctx, cancel := context.WithTimeout(ctx, p.options.RequestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := p.prefix + strconv.FormatUint(p.next.Add(1), 10)
	reply := make(chan pendingResult, 1)
	p.mu.Lock()
	if p.err != nil {
		err = p.err
		p.mu.Unlock()
		return err
	}
	p.pending[id] = reply
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	if err := p.enqueue(ctx, frame{Version: 1, Kind: "request", ID: id, Method: method, Params: data}); err != nil {
		return err
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return r.err
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("decode duplex result: %w", err)
		}
		return nil
	case <-ctx.Done():
		p.cancelRequest(id)
		return ctx.Err()
	case <-p.done:
		return p.Err()
	}
}

// Cancellation is best effort; a congested transport must not extend the
// caller's already-expired deadline while waiting to send its cancellation.
func (p *Peer) cancelRequest(id string) {
	data, err := json.Marshal(frame{Version: 1, Kind: "cancel", ID: id})
	if err != nil || int64(len(data)) > p.options.MaxFrameBytes {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	select {
	case p.outputs <- data:
	default:
	}
}

// Emit queues an event. Success means queued for this connection, not persisted
// or processed by the remote application.
func (p *Peer) Emit(ctx context.Context, event string, data any) error {
	if ctx == nil || event == "" {
		return errors.New("duplex event requires context and name")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return p.enqueue(ctx, frame{Version: 1, Kind: "event", Event: event, Data: encoded})
}

func (p *Peer) enqueue(ctx context.Context, f frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if int64(len(data)) > p.options.MaxFrameBytes {
		return errors.New("duplex frame exceeds size limit")
	}
	select {
	case <-p.done:
		return p.Err()
	default:
	}
	// A full queue can be a healthy transient burst (for example durable event
	// replay). Pace the producer for one write deadline before declaring the
	// consumer stalled. Cancellation belongs to this send and does not close an
	// otherwise healthy connection.
	timer := time.NewTimer(p.options.WriteTimeout)
	defer timer.Stop()
	select {
	case p.outputs <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return p.Err()
	case <-timer.C:
		p.fail(ErrBackpressure)
		return ErrBackpressure
	}
}

func (p *Peer) writeLoop() {
	for {
		select {
		case <-p.done:
			return
		case data := <-p.outputs:
			ctx, cancel := context.WithTimeout(p.ctx, p.options.WriteTimeout)
			err := p.conn.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: data})
			cancel()
			if err != nil {
				p.fail(err)
				return
			}
		}
	}
}

func (p *Peer) readLoop() {
	for {
		received, err := p.conn.Receive(p.ctx)
		if err != nil {
			p.fail(err)
			return
		}
		if received.Kind != duplex.Text {
			p.fail(errors.New("duplex requires JSON text frames"))
			return
		}
		if int64(len(received.Data)) > p.options.MaxFrameBytes {
			p.fail(errors.New("duplex frame exceeds size limit"))
			return
		}
		f, err := decodeFrame(received.Data)
		if err != nil {
			p.fail(err)
			return
		}
		if f.ID != "" {
			prefix := p.remotePrefix
			if f.Kind == "response" {
				prefix = p.prefix
			}
			if !validID(f.ID, prefix) {
				p.fail(errors.New("invalid duplex request identifier"))
				return
			}
		}
		switch f.Kind {
		case "response":
			p.mu.Lock()
			reply := p.pending[f.ID]
			delete(p.pending, f.ID)
			p.mu.Unlock()
			if reply != nil {
				r := pendingResult{result: f.Result}
				if f.Error != nil {
					r.err = f.Error
				}
				reply <- r
			}
		case "cancel":
			p.mu.Lock()
			cancel := p.incoming[f.ID]
			p.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case "request":
			p.startRequest(f)
		case "event":
			if !p.enqueueEvent(Event{Name: f.Event, Data: f.Data}) {
				return
			}
		}
	}
}

func (p *Peer) enqueueEvent(event Event) bool {
	select {
	case p.events <- event:
		return true
	default:
	}
	// A tight decoder loop can fill a small queue before its ready consumer gets
	// a scheduling turn. Yield once, without waiting on application callbacks;
	// responses and cancellation must still use this same independent reader.
	runtime.Gosched()
	select {
	case p.events <- event:
		return true
	default:
		p.fail(ErrBackpressure)
		return false
	}
}

func validID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	n := strings.TrimPrefix(id, prefix)
	if n == "" || n[0] == '0' {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(n) <= 20
}

func (p *Peer) startRequest(f frame) {
	p.mu.Lock()
	if _, exists := p.incoming[f.ID]; exists {
		p.mu.Unlock()
		p.fail(errors.New("duplicate active duplex request identifier"))
		return
	}
	handler := p.handlers[f.Method]
	p.mu.Unlock()
	if handler == nil {
		p.rejectRequest(f.ID, &PublicError{Code: "method_not_found", Message: "Unknown method"})
		return
	}
	select {
	case p.slots <- struct{}{}:
	default:
		p.rejectRequest(f.ID, &PublicError{Code: "busy", Message: "Too many concurrent requests"})
		return
	}
	ctx, cancel := context.WithTimeout(p.ctx, p.options.RequestTimeout)
	p.mu.Lock()
	p.incoming[f.ID] = cancel
	p.mu.Unlock()
	go func() {
		defer func() { cancel(); p.mu.Lock(); delete(p.incoming, f.ID); p.mu.Unlock(); <-p.slots }()
		result, err := invokeHandler(ctx, p, handler, f.Params)
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		p.respond(f.ID, result, err)
	}()
}

// Rejections run on the reader because they do not consume handler slots. They
// must never wait for outbound capacity: that could hold up a response needed by
// an already active reverse call. A flood exhausting the rejection capacity
// closes the overloaded connection after giving the writer a scheduling turn.
func (p *Peer) rejectRequest(id string, public *PublicError) {
	data, err := json.Marshal(frame{Version: 1, Kind: "response", ID: id, Error: public})
	if err != nil || int64(len(data)) > p.options.MaxFrameBytes {
		p.fail(errors.New("duplex rejection exceeds frame limit"))
		return
	}
	select {
	case p.outputs <- data:
		return
	case <-p.done:
		return
	default:
	}
	runtime.Gosched()
	select {
	case p.outputs <- data:
	case <-p.done:
	default:
		p.fail(ErrBackpressure)
	}
}

func invokeHandler(ctx context.Context, p *Peer, h Handler, data json.RawMessage) (result any, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("duplex handler panic")
		}
	}()
	return h(ctx, p, data)
}

func (p *Peer) respond(id string, result any, err error) {
	f := frame{Version: 1, Kind: "response", ID: id}
	if err != nil {
		var public *PublicError
		switch {
		case errors.As(err, &public) && public != nil && public.Code != "" && public.Message != "":
			f.Error = public
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			f.Error = &PublicError{Code: "cancelled", Message: "Request cancelled"}
		default:
			f.Error = &PublicError{Code: "internal", Message: "Internal error"}
		}
	} else {
		f.Result, err = json.Marshal(result)
		if err != nil {
			f.Error = &PublicError{Code: "internal", Message: "Internal error"}
			f.Result = nil
		}
	}
	if err := p.enqueue(p.ctx, f); err != nil && p.ctx.Err() == nil {
		// An oversized/unencodable result cannot leave the remote call hanging.
		fallback := frame{Version: 1, Kind: "response", ID: id, Error: &PublicError{Code: "internal", Message: "Response could not be encoded"}}
		if retryErr := p.enqueue(p.ctx, fallback); retryErr != nil {
			p.fail(retryErr)
		}
	}
}

func (p *Peer) eventLoop() {
	for {
		select {
		case <-p.done:
			return
		case event := <-p.events:
			p.mu.Lock()
			handler := p.eventHandlers[event.Name]
			listeners := make([]func(context.Context, Event), 0, len(p.listeners))
			for _, l := range p.listeners {
				listeners = append(listeners, l)
			}
			p.mu.Unlock()
			func() {
				defer func() {
					if recover() != nil {
						p.fail(errors.New("duplex event handler panic"))
					}
				}()
				if handler != nil {
					handler(p.ctx, p, event.Data)
				}
				for _, listener := range listeners {
					listener(p.ctx, event)
				}
			}()
		}
	}
}

func decodeFrame(data []byte) (frame, error) {
	var f frame
	// Validate members separately so duplicate fields and explicit members from
	// another frame kind cannot disappear into Go zero values while decoding.
	fields, err := frameMembers(data)
	if err != nil {
		return f, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, fmt.Errorf("invalid duplex frame: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return f, errors.New("invalid trailing duplex frame content")
	}
	if f.Version != 1 {
		return f, errors.New("unsupported duplex frame version")
	}
	valid := false
	allowed := map[string]bool{"version": true, "kind": true}
	switch f.Kind {
	case "request":
		allowed["id"], allowed["method"], allowed["params"] = true, true, true
		valid = f.ID != "" && f.Method != "" && len(f.Params) > 0 && len(f.Result) == 0 && f.Error == nil && f.Event == "" && len(f.Data) == 0
	case "response":
		allowed["id"], allowed["result"], allowed["error"] = true, true, true
		valid = f.ID != "" && f.Method == "" && len(f.Params) == 0 && (len(f.Result) > 0) != (f.Error != nil) && f.Event == "" && len(f.Data) == 0
		_, hasResult := fields["result"]
		_, hasError := fields["error"]
		valid = valid && hasResult != hasError
	case "event":
		allowed["event"], allowed["data"] = true, true
		valid = f.ID == "" && f.Method == "" && len(f.Params) == 0 && len(f.Result) == 0 && f.Error == nil && f.Event != "" && len(f.Data) > 0
	case "cancel":
		allowed["id"] = true
		valid = f.ID != "" && f.Method == "" && len(f.Params) == 0 && len(f.Result) == 0 && f.Error == nil && f.Event == "" && len(f.Data) == 0
	}
	for name := range fields {
		if !allowed[name] {
			valid = false
		}
	}
	if f.Error != nil && (f.Error.Code == "" || f.Error.Message == "") {
		valid = false
	}
	if !valid {
		return f, errors.New("invalid duplex frame shape")
	}
	return f, nil
}

func frameMembers(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("duplex frame must be an object")
	}
	members := make(map[string]json.RawMessage)
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid duplex field name")
		}
		if _, exists := members[name]; exists {
			return nil, fmt.Errorf("duplicate duplex field %q", name)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		members[name] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid trailing duplex frame content")
	}
	return members, nil
}
