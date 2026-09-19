package probe

import (
	context "context"
	binding "example.com/probe/api/go/probe-binding"
	protocol "example.com/probe/api/go/probe-protocol"
)

// Handler is the behavior of the probe family's server side: what its
// binding's binding.Handler declares, one method per operation the server
// implements. Fill the methods in; nightseam wrote this file once and will
// not touch it again.
type Handler struct{}

var _ binding.Handler = Handler{}

// Echo: Returns the payload, its text reversed by the caller.
//
// Duplex means both ends call: the server tells everyone the payload
// changed and then calls the client back, inside the request, to have it
// reversed — typed both ways, with nothing of the envelope written here.
func (Handler) Echo(ctx context.Context, remote *binding.Remote, params protocol.Payload) (protocol.Payload, error) {
	if err := remote.EmitChanged(ctx, params); err != nil {
		return protocol.Payload{}, err
	}
	return remote.Reverse(ctx, params)
}
