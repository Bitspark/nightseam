package composition_test

// The live layer, composed. Nothing below is a primitive of Nightseam: it is
// ordinary application state — a map, a mutex, a counter — over the public
// peers, tunnels and channels, and over the packages the generator renders
// from api/contracts. What it gives is the contract #202 proposes, and every
// rule of that contract is a line of this file rather than a guarantee
// something else makes:
//
//   - a binding is a channel this side opened and serves an implementation
//     over; a reference is that channel's id, which is what duplex.Handle
//     carries and the only reference form the language has;
//   - a reference means nothing off the connection that carried it, so a
//     scope resolves against its own tunnel and nothing else;
//   - a reference is resolved once: repeated import shares one generated
//     model and its consumer-owned alias count; the tunnel itself owns the
//     one prepared peer for each channel;
//   - the contract is checked at the import, against the family the channel
//     was opened for;
//   - release drops an alias, the last alias closes the attachment, and
//     neither calls anything of the application.
//
// docs/runtime/compositions.md is the page these rules are written on, and
// names the three places the basis does not supply them.

import (
	"context"
	"errors"
	"fmt"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"sync"

	jobbinding "example.test/generated/api/go/job-binding"
	jobprotocol "example.test/generated/api/go/job-protocol"
	sinkclient "example.test/generated/api/go/sink-client"
	sinkprotocol "example.test/generated/api/go/sink-protocol"
	duplex "github.com/Bitspark/nightseam/duplex/go"
	runtime "github.com/Bitspark/nightseam/runtime/go"
	tunnel "github.com/Bitspark/nightseam/tunnel/go"
)

// The refusals of the composed layer. They are the application's own errors;
// a family that wants one of them on the wire declares it, as worker, cell
// and topic declare unknown_reference.
var (
	errUnknownReference = errors.New("the reference names no channel of this connection")
	errWrongContract    = errors.New("the channel speaks another family")
	errOwnReference     = errors.New("the reference was minted here and is not imported here")
	errScopeClosed      = errors.New("the scope is closed")
)

// scope is one connection's live context: what this side has exported into it
// and what this side has imported out of it. It ends with the connection.
type scope struct {
	carrier *tunnel.Tunnel

	mu     sync.Mutex
	served map[int64]*binding
	held   map[int64]*proxy
	closed bool
}

// binding is an implementation this side serves on a channel of its own. The
// channel knows its own family, so the binding does not repeat it.
type binding struct {
	channel *tunnel.Channel
	wire    bitwire.Endpoint
}

// proxy is the one attachment this side has over an imported binding, and the
// number of aliases of it that have not been released.
type proxy struct {
	family  string
	channel *tunnel.Channel
	model   any
	aliases int
}

func newScope(carrier *tunnel.Tunnel) *scope {
	return &scope{carrier: carrier, served: map[int64]*binding{}, held: map[int64]*proxy{}}
}

// context outlives any one call: a reference survives the call that
// introduced it, so what a holder does with it later runs under the
// connection rather than under a request that has ended.
func (s *scope) context() context.Context { return s.carrier.Peer().Context() }

// export publishes a model Wire over one prepared channel. The model is ready
// before the channel's peer begins reading, and its lifetime follows the channel.
func (s *scope) export(ctx context.Context, family, digest string, model bitwire.Endpoint) (int64, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		_ = model.Close(duplex.CodeGoingAway, "scope closed")
		return 0, errScopeClosed
	}
	channel, err := s.carrier.Open(ctx, family, digest, runtime.Options{Prepare: func(peer *runtime.Peer) error {
		if _, err := runtime.ForwardWire(peer.Wire(), model); err != nil {
			return err
		}
		go func() { <-peer.Done(); _ = model.Close(duplex.CodeNormal, "channel ended") }()
		return nil
	}})
	if err != nil {
		_ = model.Close(duplex.CodeInternalError, "export failed")
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = channel.Close(duplex.CodeGoingAway, "scope closed")
		_ = model.Close(duplex.CodeGoingAway, "scope closed")
		return 0, errScopeClosed
	}
	s.served[channel.ID] = &binding{channel: channel, wire: model}
	return channel.ID, nil
}

// imported keeps the consumer's model and aliases per channel. Interpretation
// creates no second peer and runs while holding the consumer table lock, so two
// simultaneous imports cannot install competing reverse handlers.
func (s *scope) imported(ctx context.Context, id int64, family string, interpret func(bitwire.Endpoint) (any, error)) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errScopeClosed
	}
	if _, mine := s.served[id]; mine {
		return nil, errOwnReference
	}
	if held, ok := s.held[id]; ok {
		if held.family != family {
			return nil, errWrongContract
		}
		held.aliases++
		return held.model, nil
	}
	channel, ok, err := s.carrier.Channel(id, runtime.Options{})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errUnknownReference
	}
	if channel.Family != family {
		return nil, fmt.Errorf("%w: %s, not %s", errWrongContract, channel.Family, family)
	}
	model, err := interpret(channel)
	if err != nil {
		return nil, err
	}
	s.held[id] = &proxy{family: family, channel: channel, model: model, aliases: 1}
	return model, nil
}

// release drops one alias of an imported reference and reports whether
// anything was released. The last alias closes the one attachment; the
// implementation at the other end is told nothing beyond the channel closing,
// and no method of it is called.
func (s *scope) release(id int64) bool {
	s.mu.Lock()
	held, ok := s.held[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	held.aliases--
	if held.aliases > 0 {
		s.mu.Unlock()
		return true
	}
	delete(s.held, id)
	s.mu.Unlock()
	_ = held.channel.Close(duplex.CodeNormal, "last alias released")
	return true
}

// aliases is how many unreleased aliases this scope holds of a reference.
func (s *scope) aliases(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if held, ok := s.held[id]; ok {
		return held.aliases
	}
	return 0
}

// revoke ends a binding this side exported. The holder's next call over it
// fails; the holder is not told which of its references died beyond that.
func (s *scope) revoke(ctx context.Context, id int64) bool {
	s.mu.Lock()
	served, ok := s.served[id]
	if ok {
		delete(s.served, id)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	_ = served.wire.Close(duplex.CodeNormal, "binding released")
	_ = served.channel.Close(duplex.CodeNormal, "the binding was released")
	return true
}

// close ends the scope: every binding and every attachment goes with the
// connection, and nothing resolves in it again.
func (s *scope) close(ctx context.Context) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	served, held := s.served, s.held
	s.served, s.held = map[int64]*binding{}, map[int64]*proxy{}
	s.mu.Unlock()
	for _, b := range served {
		_ = b.wire.Close(duplex.CodeGoingAway, "scope closed")
		_ = b.channel.Close(duplex.CodeGoingAway, "the scope closed")
	}
	for _, p := range held {
		_ = p.channel.Close(duplex.CodeGoingAway, "scope closed")
	}
}

// The same generated model/Wire conversion serves either family side. The
// declaration chooses which side implements operations; carrier direction does
// not enter the adapter.
func (s *scope) exportSink(ctx context.Context, impl sinkprotocol.ClientMethods) (int64, error) {
	wire, err := sinkclient.ToWire(func(sinkprotocol.Server) (sinkprotocol.Client, error) {
		return sinkprotocol.Client{Methods: impl, Events: struct{}{}}, nil
	}, runtime.AdapterContext{})
	if err != nil {
		return 0, err
	}
	return s.export(ctx, "sink", sinkprotocol.WireDigest(), wire)
}
func (s *scope) importSink(ctx context.Context, id int64) (sinkprotocol.ClientMethods, error) {
	value, err := s.imported(ctx, id, "sink", func(wire bitwire.Endpoint) (any, error) {
		factory, err := sinkclient.FromWire(ctx, wire, runtime.AdapterContext{})
		if err != nil {
			return nil, err
		}
		model, err := factory(sinkprotocol.Server{Methods: struct{}{}, Events: struct{}{}})
		if err != nil {
			return nil, err
		}
		return model.Methods, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(sinkprotocol.ClientMethods), nil
}
func (s *scope) exportJob(ctx context.Context, impl jobprotocol.ServerMethods) (int64, error) {
	wire, err := jobbinding.ToWire(func(jobprotocol.Client) (jobprotocol.Server, error) {
		return jobprotocol.Server{Methods: impl, Events: struct{}{}}, nil
	}, runtime.AdapterContext{})
	if err != nil {
		return 0, err
	}
	return s.export(ctx, "job", jobprotocol.WireDigest(), wire)
}
func (s *scope) importJob(ctx context.Context, id int64) (jobprotocol.ServerMethods, error) {
	value, err := s.imported(ctx, id, "job", func(wire bitwire.Endpoint) (any, error) {
		factory, err := jobbinding.FromWire(ctx, wire, runtime.AdapterContext{})
		if err != nil {
			return nil, err
		}
		model, err := factory(jobprotocol.Client{Methods: struct{}{}, Events: struct{}{}})
		if err != nil {
			return nil, err
		}
		return model.Methods, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(jobprotocol.ServerMethods), nil
}
