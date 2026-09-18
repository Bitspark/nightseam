package wsruntime

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"

	"github.com/Bitspark/nighthall/api/go/duplex/ws"
)

// ServerOptions requires an explicit authentication and origin policy. The
// authenticated context is the base context for every incoming invocation.
type ServerOptions struct {
	Options      Options
	Authenticate func(*http.Request) (context.Context, error)
	CheckOrigin  func(*http.Request) bool
	OnConnect    func(*Peer)
}

func (o ServerOptions) validate() error {
	if o.Authenticate == nil || o.CheckOrigin == nil {
		return errors.New("duplex server requires explicit authentication and origin policies")
	}
	_, err := o.Options.normalized()
	return err
}

// Accept upgrades an authenticated request to a WebSocket and speaks the
// profile over it. The caller must keep its HTTP handler alive until
// Peer.Done; NewHandler implements that lifetime contract.
func Accept(w http.ResponseWriter, r *http.Request, options ServerOptions) (*Peer, error) {
	if err := options.validate(); err != nil {
		http.Error(w, "Invalid server configuration", http.StatusInternalServerError)
		return nil, err
	}
	if !options.CheckOrigin(r) {
		http.Error(w, "Origin denied", http.StatusForbidden)
		return nil, errors.New("duplex origin denied")
	}
	ctx, err := options.Authenticate(r)
	if err != nil || ctx == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, errors.New("duplex authentication failed")
	}
	o, _ := options.Options.normalized()
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	conn := ws.New(socket, o.MaxFrameBytes)
	peer, err := NewPeer(ctx, conn, ServerRole, options.Options)
	if err != nil {
		_ = conn.Abort()
		return nil, err
	}
	return peer, nil
}

func NewHandler(options ServerOptions) (http.Handler, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := Accept(w, r, options)
		if err != nil {
			return
		}
		defer peer.Close()
		if options.OnConnect != nil {
			options.OnConnect(peer)
		}
		select {
		case <-peer.Done():
		case <-r.Context().Done():
		}
	}), nil
}

type DialOptions struct {
	Options    Options
	HTTPHeader http.Header
	HTTPClient *http.Client
}

// Dial opens a WebSocket and speaks the profile over it. It uses ctx for the
// handshake and the connection lifetime. Use a separate context for each
// Call; cancelling the dialing context disconnects the peer.
func Dial(ctx context.Context, url string, options DialOptions) (*Peer, *http.Response, error) {
	if ctx == nil {
		return nil, nil, errors.New("duplex dial requires a context")
	}
	o, err := options.Options.normalized()
	if err != nil {
		return nil, nil, err
	}
	socket, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: options.HTTPHeader, HTTPClient: options.HTTPClient})
	if err != nil {
		return nil, response, err
	}
	conn := ws.New(socket, o.MaxFrameBytes)
	peer, err := NewPeer(ctx, conn, ClientRole, options.Options)
	if err != nil {
		_ = conn.Abort()
		return nil, response, err
	}
	return peer, response, nil
}
