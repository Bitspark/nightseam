package probe

import (
	context "context"
	protocol "example.com/probe/api/go/probe-protocol"
)

// Handler is the behavior of the probe family's server side: what its
// protocol's ServerMethods declares, one method per operation the server
// implements. Fill the methods in; nightseam wrote this file once and will
// not touch it again.
type Handler struct{ Remote protocol.Client }

var _ protocol.ServerMethods = Handler{}

// Model binds one implementation to its opposite-side access for this session.
func Model(remote protocol.Client) (protocol.Server, error) {
	return protocol.Server{Methods: Handler{Remote: remote}, Events: struct{}{}}, nil
}

// Echo: Returns the payload, its text reversed by the caller.
//
// Duplex means both ends call: the server tells everyone the payload
// changed and then calls the client back, inside the request, to have it
// reversed — typed both ways, with nothing of the envelope written here.
func (h Handler) Echo(ctx context.Context, params protocol.Payload) (protocol.Payload, error) {
	if err := h.Remote.Events.Changed(ctx, params); err != nil {
		return protocol.Payload{}, err
	}
	return h.Remote.Methods.Reverse(ctx, params)
}

// Watch: Takes a callback and answers a record of callables: a live reference
// travels in each direction within one call.
//
// The third level, and the one neither data nor an ordinary call reaches. The
// client's `notice` arrives as a function value — the generated binding
// imported it into the connection's live scope before this method saw it — and
// the `stop` returned here is exported the same way, so the client calls it as
// a function of its own.
//
// What makes it a live reference rather than a callback is the line that
// closes over `notice` and calls it from inside `stop`: by then the call that
// carried it has long returned. The reference outlives the call, and releasing
// it, cancelling that call and asking the job to stop are three different
// things.
func (Handler) Watch(ctx context.Context, params protocol.Watch) (protocol.Subscription, error) {
	notice := params.Watcher.Notice
	if err := notice(ctx, protocol.Payload{Text: params.Label, Count: 1}); err != nil {
		return protocol.Subscription{}, err
	}
	return protocol.Subscription{
		Stop: func(ctx context.Context) error {
			return notice(ctx, protocol.Payload{Text: params.Label + " done", Count: 0})
		},
	}, nil
}
