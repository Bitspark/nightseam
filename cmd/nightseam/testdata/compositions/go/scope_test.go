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
//   - a reference is resolved once: repeated import shares the one
//     attachment, because a second attachment is a second peer reading one
//     channel's frames and the two would take each other's replies;
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
	"sync"

	jobbinding "example.test/generated/api/go/job-binding"
	jobclient "example.test/generated/api/go/job-client"
	sinkbinding "example.test/generated/api/go/sink-binding"
	sinkclient "example.test/generated/api/go/sink-client"
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
	peer    *runtime.Peer
}

// proxy is the one attachment this side has over an imported binding, and the
// number of aliases of it that have not been released.
type proxy struct {
	family  string
	peer    *runtime.Peer
	aliases int
}

func newScope(carrier *tunnel.Tunnel) *scope {
	return &scope{carrier: carrier, served: map[int64]*binding{}, held: map[int64]*proxy{}}
}

// context outlives any one call: a reference survives the call that
// introduced it, so what a holder does with it later runs under the
// connection rather than under a request that has ended.
func (s *scope) context() context.Context { return s.carrier.Peer().Context() }

// export opens a channel for the family, serves the implementation over it
// and answers the reference. serve is the generated Attach or Serve of that
// family; which of the two it is depends on the side the operations are
// declared on, not on who is exporting.
func (s *scope) export(ctx context.Context, family string, serve func(context.Context, *tunnel.Channel) (*runtime.Peer, error)) (int64, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, errScopeClosed
	}
	channel, err := s.carrier.Open(ctx, family, "")
	if err != nil {
		return 0, err
	}
	// The connection's context and not the call's: a peer made under a
	// request's context dies when that request ends, and a binding is meant
	// to outlive the call that published it. This is the first thing the
	// composition gets wrong if it is written the obvious way.
	peer, err := serve(s.context(), channel)
	if err != nil {
		// A failed publication leaves no binding behind: the channel it was
		// to be served on is closed here, before any reference to it exists.
		_ = channel.Close(ctx, duplex.CodeInternalError, "the export failed")
		return 0, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = peer.Close()
		return 0, errScopeClosed
	}
	s.served[channel.ID] = &binding{channel: channel, peer: peer}
	s.mu.Unlock()
	return channel.ID, nil
}

// imported resolves a reference the connection carried into the one
// attachment this side keeps over it. attach is the generated Attach or Serve
// of the other side of that family.
func (s *scope) imported(ctx context.Context, id int64, family string, attach func(context.Context, *tunnel.Channel) (*runtime.Peer, error)) (*runtime.Peer, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errScopeClosed
	}
	if _, mine := s.served[id]; mine {
		s.mu.Unlock()
		return nil, errOwnReference
	}
	if held, ok := s.held[id]; ok {
		if held.family != family {
			s.mu.Unlock()
			return nil, errWrongContract
		}
		held.aliases++
		s.mu.Unlock()
		return held.peer, nil
	}
	s.mu.Unlock()

	// The whole of the scope check: a reference is an id on this connection.
	// An id from another connection resolves to nothing here, or — if that
	// number happens to be open here — to a channel of another family, which
	// the next line refuses.
	channel, ok := s.carrier.Channel(id)
	if !ok {
		return nil, errUnknownReference
	}
	if channel.Family != family {
		return nil, fmt.Errorf("%w: %s, not %s", errWrongContract, channel.Family, family)
	}
	// Under the connection's context, for the reason export gives.
	peer, err := attach(s.context(), channel)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = peer.Close()
		return nil, errScopeClosed
	}
	if held, ok := s.held[id]; ok {
		// Another goroutine resolved the same reference first; its attachment
		// is the one, and this one never reads a frame.
		held.aliases++
		s.mu.Unlock()
		_ = peer.Close()
		return held.peer, nil
	}
	s.held[id] = &proxy{family: family, peer: peer, aliases: 1}
	s.mu.Unlock()
	return peer, nil
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
	_ = held.peer.Close()
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
	_ = served.peer.Close()
	_ = served.channel.Close(ctx, duplex.CodeNormal, "the binding was released")
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
		_ = b.peer.Close()
		_ = b.channel.Close(ctx, duplex.CodeGoingAway, "the scope closed")
	}
	for _, p := range held {
		_ = p.peer.Close()
	}
}

// The typed halves of the two references this proof exchanges.
//
// A family's operations are declared on one of its two sides, and that
// decides which generated package implements it: the sink's are the client
// side's, so its implementor attaches the generated client and its holder
// serves the generated binding and calls through Remote; the job's are the
// server side's, and the two are the other way round. The declaration picks
// the direction, and TypeScript — which the generator gives a client and no
// binding — can therefore implement a client side only.

func (s *scope) exportSink(ctx context.Context, impl sinkclient.Handler) (int64, error) {
	return s.export(ctx, "sink", func(ctx context.Context, channel *tunnel.Channel) (*runtime.Peer, error) {
		client, err := sinkclient.Attach(ctx, channel, runtime.Options{}, impl, sinkclient.Events{})
		if err != nil {
			return nil, err
		}
		return client.Peer, nil
	})
}

func (s *scope) importSink(ctx context.Context, id int64) (*sinkbinding.Remote, error) {
	peer, err := s.imported(ctx, id, "sink", func(ctx context.Context, channel *tunnel.Channel) (*runtime.Peer, error) {
		return sinkbinding.Serve(ctx, channel, runtime.Options{}, noServerSide{})
	})
	if err != nil {
		return nil, err
	}
	return &sinkbinding.Remote{Peer: peer}, nil
}

func (s *scope) exportJob(ctx context.Context, impl jobbinding.Handler) (int64, error) {
	return s.export(ctx, "job", func(ctx context.Context, channel *tunnel.Channel) (*runtime.Peer, error) {
		return jobbinding.Serve(ctx, channel, runtime.Options{}, impl)
	})
}

func (s *scope) importJob(ctx context.Context, id int64) (*jobclient.Client, error) {
	peer, err := s.imported(ctx, id, "job", func(ctx context.Context, channel *tunnel.Channel) (*runtime.Peer, error) {
		client, err := jobclient.Attach(ctx, channel, runtime.Options{}, nil, jobclient.Events{})
		if err != nil {
			return nil, err
		}
		return client.Peer, nil
	})
	if err != nil {
		return nil, err
	}
	return &jobclient.Client{Peer: peer}, nil
}

// noServerSide implements the empty server side of a family whose operations
// are all the client side's.
type noServerSide struct{}
