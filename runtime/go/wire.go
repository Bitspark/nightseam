package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

type routedFrame struct {
	path    []string
	message duplex.Message
}

type returnKey struct {
	address *duplex.ReturnAddress
	id      string
}

// peerWire is the existing peer's relative dispatch surface. Its return
// associations stay beside the peer's carrier pending/incoming tables; views
// in duplex only choose a path and never correlate an id.
type peerWire struct {
	peer      *Peer
	queue     chan routedFrame
	mu        sync.Mutex
	incoming  map[returnKey]context.CancelFunc
	receivers map[string]*wireRegistration
}

type wireRegistration struct {
	receiver duplex.Receiver
}

type wireFrameKey struct{}
type wireDispatchContext struct {
	ctx   context.Context
	peer  *Peer
	frame frame
}

// Wire selects this peer's root origin. Repeated selection shares the peer,
// its queues and its carrier lifetime.
func (p *Peer) Wire() duplex.Wire {
	p.wireOnce.Do(func() {
		p.wire = &peerWire{peer: p, queue: make(chan routedFrame, p.options.QueueCapacity), incoming: map[returnKey]context.CancelFunc{}, receivers: map[string]*wireRegistration{}}
		go p.wire.run()
	})
	return p.wire
}

func (w *peerWire) Send(path []string, message duplex.Message) error {
	name, err := duplex.EncodePath(path)
	if err != nil {
		return err
	}
	if name == "" && (message.Frame.Kind == duplex.ProfileRequest || message.Frame.Kind == duplex.ProfileEvent) {
		return errors.New("a root wire operation needs a nonempty path")
	}
	if err := w.peer.Err(); err != nil {
		return err
	}
	if (message.Frame.Kind == duplex.ProfileRequest || message.Frame.Kind == duplex.ProfileCancel) && (message.Return == nil || message.Return.Wire == nil) {
		return errors.New("a wire request or cancellation requires a return address")
	}
	if message.Frame.Kind != duplex.ProfileRequest && message.Frame.Kind != duplex.ProfileEvent && message.Frame.Kind != duplex.ProfileCancel {
		return errors.New("a response is sent to its request's return address")
	}
	if err := validateWireFrame(name, message.Frame, w.peer.options.MaxFrameBytes); err != nil {
		return err
	}
	message.Frame.Params = append(json.RawMessage(nil), message.Frame.Params...)
	message.Frame.Data = append(json.RawMessage(nil), message.Frame.Data...)
	message.Frame.Meta = maps.Clone(message.Frame.Meta)
	select {
	case w.queue <- routedFrame{path: append([]string(nil), path...), message: message}:
		return nil
	default:
		w.peer.observeBackpressure(len(w.queue), true, w.peer.options.WriteTimeout)
		w.peer.fail(ErrBackpressure)
		return ErrBackpressure
	}
}

func (w *peerWire) Close(code duplex.Code, reason string) error {
	w.peer.end(ErrClosed, code, reason)
	return nil
}

func (w *peerWire) run() {
	defer func() {
		w.mu.Lock()
		var receivers []duplex.Receiver
		for _, registration := range w.receivers {
			receivers = append(receivers, registration.receiver)
		}
		w.receivers = map[string]*wireRegistration{}
		for _, cancel := range w.incoming {
			cancel()
		}
		w.mu.Unlock()
		for _, receiver := range receivers {
			if receiver.Closed != nil {
				receiver.Closed(duplex.CodeGoingAway, "peer ended")
			}
		}
	}()
	for {
		select {
		case <-w.peer.Done():
			return
		case delivered := <-w.queue:
			f := delivered.message.Frame
			key := returnKey{delivered.message.Return, f.ID}
			switch f.Kind {
			case duplex.ProfileCancel:
				w.mu.Lock()
				cancel := w.incoming[key]
				w.mu.Unlock()
				if cancel != nil {
					cancel()
				}
			case duplex.ProfileRequest:
				ctx := w.peer.options.Propagator.Extract(w.peer.Context(), Trace{Parent: f.Traceparent, State: f.Tracestate})
				ctx = WithMeta(ctx, f.Meta)
				ctx, cancel := context.WithCancel(ctx)
				w.mu.Lock()
				_, duplicate := w.incoming[key]
				full := len(w.incoming) >= w.peer.options.MaxPendingRequests
				if !duplicate && !full {
					w.incoming[key] = cancel
				}
				w.mu.Unlock()
				if duplicate || full {
					cancel()
					code, message := "busy", "Outstanding call limit reached"
					if duplicate {
						code, message = "invalid_message", "Duplicate active request identifier"
					}
					sendWireResponse(delivered.message, nil, &PublicError{Code: code, Message: message})
					continue
				}
				name, err := duplex.EncodePath(delivered.path)
				var call *admittedCall
				if err == nil {
					call, err = w.peer.beginCall(ctx, name, f.Params)
				}
				if err != nil {
					cancel()
					w.mu.Lock()
					delete(w.incoming, key)
					w.mu.Unlock()
					sendWireResponse(delivered.message, nil, WithoutUnpublishedProof(err))
					continue
				}
				w.mu.Lock()
				w.incoming[key] = func() { call.withdraw(); cancel() }
				w.mu.Unlock()
				go func() {
					defer func() { cancel(); w.mu.Lock(); delete(w.incoming, key); w.mu.Unlock() }()
					var result json.RawMessage
					err := call.await(&result)
					sendWireResponse(delivered.message, result, WithoutUnpublishedProof(err))
				}()
			case duplex.ProfileEvent:
				name, err := duplex.EncodePath(delivered.path)
				ctx := w.peer.options.Propagator.Extract(w.peer.Context(), Trace{Parent: f.Traceparent, State: f.Tracestate})
				if err == nil {
					err = w.peer.Emit(WithMeta(ctx, f.Meta), name, f.Data)
				}
				if err != nil {
					w.peer.fail(err)
				}
			}
		}
	}
}

func (w *peerWire) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	name, err := duplex.EncodePath(path)
	if err != nil {
		return nil, err
	}
	if name == "" || receiver.Message == nil {
		return nil, errors.New("a wire receiver requires a nonempty operation path and callback")
	}
	registration := &wireRegistration{receiver: receiver}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.receivers[name]; exists {
		return nil, duplex.ErrReceiverExists
	}
	w.peer.mu.Lock()
	defer w.peer.mu.Unlock()
	if w.peer.err != nil {
		return nil, w.peer.err
	}
	if w.peer.handlers[name] != nil || w.peer.eventHandlers[name] != nil {
		return nil, duplex.ErrReceiverExists
	}
	selected := append([]string(nil), path...)
	w.peer.handlers[name] = func(ctx context.Context, _ *Peer, params json.RawMessage) (any, error) {
		var result json.RawMessage
		incoming, _ := ctx.Value(wireFrameKey{}).(frame)
		dispatch := &wireDispatchContext{ctx: ctx, peer: w.peer, frame: incoming}
		err := callWire(WithMeta(ctx, MetaFrom(ctx)), &receiverWire{receiver: receiver}, selected, params, &result, dispatch)
		return result, err
	}
	w.peer.eventHandlers[name] = func(ctx context.Context, _ *Peer, data json.RawMessage) {
		trace, _ := TraceOf(ctx)
		receiver.Message(selected, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileEvent, Data: data, Traceparent: trace.Parent, Tracestate: trace.State, Meta: MetaFrom(ctx)}})
	}
	w.receivers[name] = registration
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.receivers[name] != registration {
				return
			}
			delete(w.receivers, name)
			w.peer.mu.Lock()
			defer w.peer.mu.Unlock()
			delete(w.peer.handlers, name)
			delete(w.peer.eventHandlers, name)
		})
	}, nil
}

// receiverWire is used only inside the peer's already asynchronous request
// dispatch. It reuses the same completion primitive when handing a decoded
// request to a generated wire receiver.
type receiverWire struct{ receiver duplex.Receiver }

func (w *receiverWire) Send(path []string, message duplex.Message) error {
	w.receiver.Message(path, message)
	return nil
}
func (w *receiverWire) Receive([]string, duplex.Receiver) (func(), error) {
	return nil, duplex.ErrReceiverExists
}
func (w *receiverWire) Close(duplex.Code, string) error { return nil }

type replyWire struct {
	id       string
	reply    chan pendingResult
	done     chan struct{}
	once     sync.Once
	dispatch *wireDispatchContext
}

func (w *replyWire) Send(path []string, message duplex.Message) error {
	if len(path) != 0 || message.Frame.Kind != duplex.ProfileResponse || message.Frame.ID != w.id {
		return errors.New("invalid wire response")
	}
	var limit int64
	if w.dispatch != nil {
		limit = w.dispatch.peer.options.MaxFrameBytes
	}
	if err := validateWireFrame("", message.Frame, limit); err != nil {
		return err
	}
	r := pendingResult{result: message.Frame.Result}
	if f := message.Frame.Error; f != nil {
		r.err = &PublicError{Code: f.Code, Message: f.Message, Data: f.Data}
	}
	select {
	case <-w.done:
		return ErrClosed
	default:
	}
	select {
	case w.reply <- r:
		return nil
	default:
		return errors.New("duplicate wire response")
	}
}
func (w *replyWire) Receive([]string, duplex.Receiver) (func(), error) {
	return nil, duplex.ErrReceiverExists
}
func (w *replyWire) Close(duplex.Code, string) error { w.once.Do(func() { close(w.done) }); return nil }

// CallWire calls a relative operation through the peer's request primitive.
// Its local return address is independent of every other call's identifier.
func CallWire(ctx context.Context, wire duplex.Wire, path []string, params, result any) error {
	return callWire(ctx, wire, path, params, result, nil)
}
func callWire(ctx context.Context, wire duplex.Wire, path []string, params, result any, dispatch *wireDispatchContext) error {
	if ctx == nil || wire == nil {
		return Unpublished(errors.New("a wire call requires a context and wire"))
	}
	if err := ctx.Err(); err != nil {
		return Unpublished(err)
	}
	if _, err := duplex.EncodePath(path); err != nil {
		return Unpublished(errors.New("a wire call requires a valid operation path"))
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		return Unpublished(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	returning := &replyWire{id: "c:1", reply: make(chan pendingResult, 1), done: make(chan struct{}), dispatch: dispatch}
	defer returning.Close(duplex.CodeNormal, "")
	address := &duplex.ReturnAddress{Wire: returning}
	trace := DefaultPropagator.Inject(ctx)
	if dispatch != nil {
		trace = Trace{Parent: dispatch.frame.Traceparent, State: dispatch.frame.Tracestate}
	}
	request := duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileRequest, ID: returning.id, Params: encoded, Traceparent: trace.Parent, Tracestate: trace.State, Meta: outgoingMeta(ctx)}, Return: address}
	if err := wire.Send(path, request); err != nil {
		return Unpublished(err)
	}
	cancelRemote, err := awaitReply(ctx, returning.reply, returning.done, func() error { return ErrClosed }, result)
	if cancelRemote {
		_ = wire.Send(path, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileCancel, ID: returning.id, Traceparent: trace.Parent, Tracestate: trace.State}, Return: address})
	}
	return err
}

// WireHandler is a typed adapter's decoded request body, independent of the
// concrete carrier. The runtime supplies cancellation and response routing.
type WireHandler func(context.Context, json.RawMessage) (any, error)

// EmitWire admits one event at a relative path. The return says only that the
// destination accepted it; processing and transport remain asynchronous.
func EmitWire(ctx context.Context, wire duplex.Wire, path []string, data any) error {
	if ctx == nil || wire == nil {
		return Unpublished(errors.New("a wire event requires a context and wire"))
	}
	if err := ctx.Err(); err != nil {
		return Unpublished(err)
	}
	if _, err := duplex.EncodePath(path); err != nil {
		return Unpublished(errors.New("a wire event requires a valid operation path"))
	}
	encoded, err := MarshalJSON(data)
	if err != nil {
		return Unpublished(err)
	}
	trace := DefaultPropagator.Inject(ctx)
	return Unpublished(wire.Send(path, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileEvent, Data: encoded, Traceparent: trace.Parent, Tracestate: trace.State, Meta: outgoingMeta(ctx)}}))
}

// HandleWire registers one relative operation. The receiver returns before
// running application code, and request cancellation uses its return address.
func HandleWire(wire duplex.Wire, path []string, handler WireHandler) (func(), error) {
	if wire == nil || handler == nil {
		return nil, errors.New("a wire handler requires a wire and body")
	}
	var mu sync.Mutex
	incoming := map[returnKey]context.CancelFunc{}
	return wire.Receive(path, duplex.Receiver{
		Closed: func(duplex.Code, string) {
			mu.Lock()
			defer mu.Unlock()
			for _, cancel := range incoming {
				cancel()
			}
		},
		Message: func(_ []string, message duplex.Message) {
			key := returnKey{message.Return, message.Frame.ID}
			if message.Frame.Kind == duplex.ProfileCancel {
				mu.Lock()
				cancel := incoming[key]
				mu.Unlock()
				if cancel != nil {
					cancel()
				}
				return
			}
			if message.Frame.Kind != duplex.ProfileRequest {
				return
			}
			var dispatch *wireDispatchContext
			base := context.Background()
			if message.Return != nil {
				if returning, ok := message.Return.Wire.(*replyWire); ok {
					dispatch = returning.dispatch
				}
			}
			if dispatch != nil {
				base = dispatch.ctx
			}
			ctx := DefaultPropagator.Extract(base, Trace{Parent: message.Frame.Traceparent, State: message.Frame.Tracestate})
			ctx, cancel := context.WithCancel(withIncomingMeta(ctx, message.Frame.Meta))
			mu.Lock()
			if incoming[key] != nil {
				mu.Unlock()
				cancel()
				sendWireResponse(message, nil, &PublicError{Code: "invalid_message", Message: "Duplicate active request identifier"})
				return
			}
			incoming[key] = cancel
			mu.Unlock()
			go func() {
				defer func() { cancel(); mu.Lock(); delete(incoming, key); mu.Unlock() }()
				result, err := invokeWireHandler(ctx, handler, message.Frame.Params, dispatch)
				if err == nil {
					err = ctx.Err()
				}
				data, marshalErr := MarshalJSON(result)
				if err == nil {
					err = marshalErr
				}
				sendWireResponse(message, data, WithoutUnpublishedProof(err))
			}()
		},
	})
}

func invokeWireHandler(ctx context.Context, handler WireHandler, params json.RawMessage, dispatch *wireDispatchContext) (result any, err error) {
	defer func() {
		if value := recover(); value != nil {
			if dispatch != nil {
				dispatch.peer.observePanic(dispatch.frame, value)
			}
			err = errors.New("wire handler panic")
		}
	}()
	return handler(ctx, params)
}

func sendWireResponse(request duplex.Message, result json.RawMessage, err error) {
	if request.Return == nil || request.Return.Wire == nil {
		return
	}
	f := duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileResponse, ID: request.Frame.ID, Result: result, Traceparent: request.Frame.Traceparent, Tracestate: request.Frame.Tracestate}
	if err != nil {
		var public *PublicError
		switch {
		case errors.As(err, &public) && public != nil && public.Code != "" && public.Message != "":
			f.Error = &duplex.ProfileError{Code: public.Code, Message: public.Message, Data: public.Data}
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			f.Error = &duplex.ProfileError{Code: "cancelled", Message: "Request cancelled"}
		case errors.Is(err, ErrClosed):
			f.Error = &duplex.ProfileError{Code: "disconnected", Message: "Connection ended; outcome may be unknown"}
		default:
			f.Error = &duplex.ProfileError{Code: "internal", Message: "Internal error"}
		}
		f.Result = nil
	}
	if err := request.Return.Wire.Send(nil, duplex.Message{Frame: f}); err != nil {
		// A malformed or oversized public result must settle as a bounded
		// refusal, just as the carrier peer's respond does.
		f.Result = nil
		f.Error = &duplex.ProfileError{Code: "internal", Message: "Response could not be encoded"}
		_ = request.Return.Wire.Send(nil, duplex.Message{Frame: f})
	}
}

// The structured boundary uses the profile's existing validator. A logical
// return address identifies an origin independently of the carrier role, so
// either profile identifier prefix is valid before the peer remaps it.
func validateWireFrame(name string, value duplex.ProfileFrame, limit int64) error {
	f := frame{Version: value.Version, Kind: string(value.Kind), ID: value.ID,
		Params: value.Params, Result: value.Result, Data: value.Data,
		Traceparent: value.Traceparent, Tracestate: value.Tracestate, Meta: value.Meta}
	if value.Error != nil {
		f.Error = &PublicError{Code: value.Error.Code, Message: value.Error.Message, Data: value.Error.Data}
	}
	switch value.Kind {
	case duplex.ProfileRequest:
		f.Method = name
	case duplex.ProfileEvent:
		f.Event = name
	}
	data, err := MarshalJSON(f)
	if err != nil {
		return err
	}
	if limit > 0 && int64(len(data)) > limit {
		return errors.New("wire frame exceeds the carrier limit")
	}
	if _, err := decodeFrame(data); err != nil {
		return err
	}
	if f.ID != "" && !validID(f.ID, "c:") && !validID(f.ID, "s:") {
		return errors.New("invalid wire request identifier")
	}
	return nil
}
