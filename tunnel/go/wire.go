package tunnel

import (
	"context"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// Channel is a prepared structured wire on one tunnel connection. Acquisition
// builds its peer once; selecting and mounting this value never builds a peer.
type Channel struct {
	ID     int64
	Family string
	wire   duplex.Wire
}

var _ duplex.Wire = (*Channel)(nil)

func (c *Channel) Send(path []string, message duplex.Message) error {
	return c.wire.Send(path, message)
}
func (c *Channel) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	return c.wire.Receive(path, receiver)
}
func (c *Channel) Close(code duplex.Code, reason string) error { return c.wire.Close(code, reason) }

// Open opens a prepared wire. The wait context bounds acquisition; the inner
// peer lives with the outer peer. Prepare installs handlers before reading.
func (t *Tunnel) Open(ctx context.Context, family string, options runtime.Options) (*Channel, error) {
	c, err := t.openConnection(ctx, family)
	if err != nil {
		return nil, err
	}
	return c.asWire(options)
}

// Accept takes the next pending connection as a prepared wire.
func (t *Tunnel) Accept(ctx context.Context, options runtime.Options) (*Channel, error) {
	c, err := t.acceptConnection(ctx)
	if err != nil {
		return nil, err
	}
	return c.asWire(options)
}

// Channel resolves a wire by id. Its first acquisition owns its options;
// subsequent lookups return that same prepared channel. A raw claim conflicts.
func (t *Tunnel) Channel(id int64, options runtime.Options) (*Channel, bool, error) {
	c, ok := t.connection(id)
	if !ok {
		return nil, false, nil
	}
	channel, err := c.asWire(options)
	return channel, true, err
}

// OpenConnection opens raw frame transport without starting a profile reader.
func (t *Tunnel) OpenConnection(ctx context.Context, family string) (*Connection, error) {
	c, err := t.openConnection(ctx, family)
	if err != nil {
		return nil, err
	}
	if !c.asRaw() {
		return nil, presentationError()
	}
	return c, nil
}

// AcceptConnection takes raw transport, retaining its consumer-controlled credit.
func (t *Tunnel) AcceptConnection(ctx context.Context) (*Connection, error) {
	c, err := t.acceptConnection(ctx)
	if err != nil {
		return nil, err
	}
	if !c.asRaw() {
		return nil, presentationError()
	}
	return c, nil
}

// Connection resolves only raw transport; a wire already owns its reader.
func (t *Tunnel) Connection(id int64) (*Connection, bool) {
	c, ok := t.connection(id)
	if !ok || !c.asRaw() {
		return nil, false
	}
	return c, true
}

func presentationError() error {
	return &runtime.PublicError{Code: ErrorInvalid, Message: "A connection already has a reader presentation."}
}

func (c *Connection) asRaw() bool {
	c.presentationMu.Lock()
	defer c.presentationMu.Unlock()
	if c.channel != nil {
		return false
	}
	c.raw = true
	return true
}

func (c *Connection) asWire(options runtime.Options) (*Channel, error) {
	c.presentationMu.Lock()
	defer c.presentationMu.Unlock()
	if c.raw {
		return nil, presentationError()
	}
	if c.channel != nil {
		return c.channel, nil
	}
	role := runtime.ClientRole
	if c.ID%2 != c.t.parity {
		role = runtime.ServerRole
	}
	peer, err := runtime.NewPeer(c.t.peer.Context(), c, role, options)
	if err != nil {
		_ = c.Abort()
		return nil, err
	}
	c.channel = &Channel{ID: c.ID, Family: c.Family, wire: peer.Wire()}
	return c.channel, nil
}
