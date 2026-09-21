package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// A tunnel under control. Its channels are connections, handed out lazily
// by default: a channel carries a peer more often than raw
// frames, and a peer reads its own connection.
type tunnelOn struct {
	*tunnel.Tunnel
}

func (t *tunnelOn) shutdown() {}

func (t *testee) tunnelOf(r request, name string) (*tunnelOn, error) {
	handle, err := r.mustString(name)
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(handle)
	if !ok {
		return nil, fail("unknown_handle", "%s", handle)
	}
	tn, ok := object.(*tunnelOn)
	if !ok {
		return nil, invalid("%s is not a tunnel", handle)
	}
	return tn, nil
}

// channelOf is a connection handle that is a tunnel channel.
func (t *testee) channelOf(r request, name string) (*conn, *tunnel.Connection, error) {
	handle, err := r.mustString(name)
	if err != nil {
		return nil, nil, err
	}
	object, ok := t.lookup(handle)
	if !ok {
		return nil, nil, fail("unknown_handle", "%s", handle)
	}
	c, ok := object.(*conn)
	if !ok {
		return nil, nil, invalid("%s is not a connection", handle)
	}
	ch, ok := c.Conn.(*tunnel.Connection)
	if !ok {
		return nil, nil, invalid("%s is not a tunnel channel", handle)
	}
	return c, ch, nil
}

// lazyChannel reads consume with lazy as the default, a channel's own.
func (r request) lazyChannel() (bool, error) {
	mode, err := r.string("consume")
	if err != nil {
		return false, err
	}
	switch mode {
	case "", "lazy":
		return true, nil
	case "eager":
		return false, nil
	}
	return false, invalid("consume is eager or lazy")
}

func (t *testee) tunnelOps() map[string]func(request) (any, error) {
	return map[string]func(request) (any, error){
		"tunnel.over": func(r request) (any, error) {
			p, err := t.peerOf(r, "on")
			if err != nil {
				return nil, err
			}
			raw, err := r.object("options")
			if err != nil {
				return nil, err
			}
			options := tunnel.Options{}
			for key, value := range raw {
				var n int64
				if err := json.Unmarshal(value, &n); err != nil {
					return nil, invalid("options.%s is an integer", key)
				}
				switch key {
				case "window":
					options.Window = int(n)
				case "max_frame_bytes":
					options.MaxFrameBytes = n
				case "accept_capacity":
					options.AcceptCapacity = int(n)
				default:
					return nil, unsupported("tunnel option " + key)
				}
			}
			tn, err := tunnel.New(p.Peer, options)
			if err != nil {
				return nil, invalid("%v", err)
			}
			return map[string]any{"handle": t.mint("t", &tunnelOn{tn})}, nil
		},
		"tunnel.open": func(r request) (any, error) {
			tn, err := t.tunnelOf(r, "on")
			if err != nil {
				return nil, err
			}
			family, err := r.mustString("family")
			if err != nil {
				return nil, err
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			lazy, err := r.lazyChannel()
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), within)
			defer cancel()
			ch, err := tn.OpenConnection(ctx, family)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return nil, fail("timeout", "the open was not answered within %s", within)
				}
				return nil, tunnelError(err)
			}
			return map[string]any{"handle": t.mint("ch", wrap(ch, lazy)), "id": ch.ID}, nil
		},
		"tunnel.accept": func(r request) (any, error) {
			tn, err := t.tunnelOf(r, "on")
			if err != nil {
				return nil, err
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			lazy, err := r.lazyChannel()
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), within)
			defer cancel()
			ch, err := tn.AcceptConnection(ctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return nil, fail("timeout", "nothing was opened within %s", within)
				}
				return nil, tunnelError(err)
			}
			return map[string]any{"handle": t.mint("ch", wrap(ch, lazy)), "id": ch.ID, "family": ch.Family}, nil
		},
	}
}

// tunnelError maps a refused or ended open onto the driver's codes: the
// public error's code when the other side refused, disconnected when the
// tunnel's peer ended.
func tunnelError(err error) *failure {
	var public *runtime.PublicError
	if errors.As(err, &public) {
		return fail(public.Code, "%s", public.Message)
	}
	if errors.Is(err, duplex.ErrClosed) || errors.Is(err, runtime.ErrClosed) {
		return fail("disconnected", "%v", err)
	}
	return fail("failed", "%v", err)
}
