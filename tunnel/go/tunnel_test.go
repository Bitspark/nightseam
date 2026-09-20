package tunnel_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/duplex/go/duplextest"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// peers is two outer peers over a pipe, with a tunnel each.
func peers(t *testing.T, options tunnel.Options) (client, server *tunnel.Tunnel) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, b := duplex.Pipe(8 << 20)
	pa, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{MaxFrameBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{MaxFrameBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pa.Close(); pb.Close() })
	client, err = tunnel.New(pa, options)
	if err != nil {
		t.Fatal(err)
	}
	server, err = tunnel.New(pb, options)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

// pair opens one channel from the client and accepts it at the server.
func pair(t *testing.T, client, server *tunnel.Tunnel) (opened, accepted *tunnel.Channel) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan *tunnel.Channel, 1)
	go func() {
		c, err := server.Accept(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- c
	}()
	opened, err := client.Open(ctx, "probe")
	if err != nil {
		t.Fatal(err)
	}
	accepted = <-done
	if accepted == nil || accepted.ID != opened.ID || accepted.Family != "probe" {
		t.Fatalf("accepted %+v for %+v", accepted, opened)
	}
	return opened, accepted
}

// TestChannelIsAConn holds a channel over a pipe to the seam.
func TestChannelIsAConn(t *testing.T) {
	duplextest.Run(t, func(t *testing.T, limit int64) (duplex.Conn, duplex.Conn) {
		client, server := peers(t, tunnel.Options{MaxFrameBytes: limit})
		a, b := pair(t, client, server)
		return a, b
	})
}

// TestChannelIsAConnOverWebSocket holds a channel over a WebSocket to the
// seam: the outer peers are a real server and a real client.
func TestChannelIsAConnOverWebSocket(t *testing.T) {
	duplextest.Run(t, func(t *testing.T, limit int64) (duplex.Conn, duplex.Conn) {
		options := tunnel.Options{MaxFrameBytes: limit}
		accepted := make(chan *tunnel.Channel, 1)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tn *tunnel.Tunnel
			peer, err := runtime.Accept(w, r, runtime.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
				CheckOrigin:  func(*http.Request) bool { return true },
				Options: runtime.Options{MaxFrameBytes: 4 << 20, Prepare: func(peer *runtime.Peer) (err error) {
					tn, err = tunnel.New(peer, options)
					return err
				}},
			})
			if err != nil {
				return
			}
			c, err := tn.Accept(peer.Context())
			if err != nil {
				return
			}
			accepted <- c
			<-peer.Done()
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		var tn *tunnel.Tunnel
		peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{
			Options: runtime.Options{MaxFrameBytes: 4 << 20, Prepare: func(peer *runtime.Peer) (err error) {
				tn, err = tunnel.New(peer, options)
				return err
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { peer.Close() })
		opened, err := tn.Open(ctx, "probe")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case c := <-accepted:
			return opened, c
		case <-ctx.Done():
			t.Fatal("the server accepted no channel")
			return nil, nil
		}
	})
}

// TestBothSidesOpen: each side opens, ids never collide, and a handle
// resolves on either side to the channel it names.
func TestBothSidesOpen(t *testing.T) {
	client, server := peers(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fromClient, err := client.Open(ctx, "probe")
	if err != nil {
		t.Fatal(err)
	}
	fromServer, err := server.Open(ctx, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if fromClient.ID%2 != 1 || fromServer.ID%2 != 0 {
		t.Fatalf("ids %d and %d are not the openers' parities", fromClient.ID, fromServer.ID)
	}
	atServer, ok := server.Channel(fromClient.ID)
	if !ok || atServer.Family != "probe" {
		t.Fatalf("the server resolved %+v", atServer)
	}
	atClient, ok := client.Channel(fromServer.ID)
	if !ok || atClient.Family != "codex" {
		t.Fatalf("the client resolved %+v", atClient)
	}
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	if c, err := server.Accept(short); err == nil {
		t.Fatalf("a resolved channel was accepted again: %d", c.ID)
	}
	stop()
	if err := fromServer.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("down")}); err != nil {
		t.Fatal(err)
	}
	if frame, err := atClient.Receive(ctx); err != nil || string(frame.Data) != "down" {
		t.Fatalf("received %q, %v", frame.Data, err)
	}
	if _, ok := client.Channel(99); ok {
		t.Fatal("an unknown id resolved")
	}
}

// TestOuterCloseEndsEveryChannel: when the connection carrying the channels
// goes, each channel sees going away.
func TestOuterCloseEndsEveryChannel(t *testing.T) {
	client, server := peers(t, tunnel.Options{})
	a, b := pair(t, client, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Peer().Close()
	for _, c := range []*tunnel.Channel{a, b} {
		_, err := c.Receive(ctx)
		var closed *duplex.CloseError
		if !errors.As(err, &closed) || closed.Code != duplex.CodeGoingAway {
			t.Fatalf("channel %d ended with %v", c.ID, err)
		}
	}
}

// TestCreditPacesTheSender: with a window of two, a third send waits until
// the receiver takes a frame, and only that channel waits.
func TestCreditPacesTheSender(t *testing.T) {
	client, server := peers(t, tunnel.Options{Window: 2})
	a, b := pair(t, client, server)
	other, otherAccepted := pair(t, client, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if err := a.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	err := a.Send(short, duplex.Frame{Kind: duplex.Text, Data: []byte("third")})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the third frame did not wait for credit: %v", err)
	}
	if err := other.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("elsewhere")}); err != nil {
		t.Fatalf("another channel waited too: %v", err)
	}
	if frame, err := otherAccepted.Receive(ctx); err != nil || string(frame.Data) != "elsewhere" {
		t.Fatalf("the other channel received %q, %v", frame.Data, err)
	}
	if _, err := b.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("third")}); err != nil {
		t.Fatalf("credit did not return: %v", err)
	}
	for _, want := range []string{"x", "third"} {
		if frame, err := b.Receive(ctx); err != nil || string(frame.Data) != want {
			t.Fatalf("received %q, %v; want %q", frame.Data, err, want)
		}
	}
}

// TestAPeerRunsOverAChannel: a peer of the profile speaks over a channel as
// over any transport, a call and an event both ways.
func TestAPeerRunsOverAChannel(t *testing.T) {
	client, server := peers(t, tunnel.Options{})
	a, b := pair(t, client, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events := make(chan string, 1)
	inner, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{Handlers: map[string]runtime.Handler{
		"echo": func(_ context.Context, _ *runtime.Peer, params json.RawMessage) (any, error) { return params, nil },
	}, Events: map[string]runtime.EventHandler{
		"hello": func(_ context.Context, _ *runtime.Peer, data json.RawMessage) { events <- string(data) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	outer, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	var result map[string]any
	if err := outer.Call(ctx, "echo", map[string]any{"through": "the tunnel"}, &result); err != nil {
		t.Fatal(err)
	}
	if result["through"] != "the tunnel" {
		t.Fatalf("echoed %v", result)
	}
	if err := outer.Emit(ctx, "hello", "there"); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-events:
		if data != `"there"` {
			t.Fatalf("event %s", data)
		}
	case <-ctx.Done():
		t.Fatal("no event")
	}
}

// TestOpenIsRefusedWhenNobodyAccepts: beyond the accept capacity an open
// fails with channel_refused, and the tunnel stands.
func TestOpenIsRefusedWhenNobodyAccepts(t *testing.T) {
	client, _ := peers(t, tunnel.Options{AcceptCapacity: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Open(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	_, err := client.Open(ctx, "probe")
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != tunnel.ErrorRefused {
		t.Fatalf("the second open: %v", err)
	}
	if _, err := client.Open(ctx, ""); err == nil {
		t.Fatal("a channel of no family opened")
	}
}

// TestAFrameBeyondTheWindowEndsTheChannel: the receiver's inbox is one window
// deep, so a sender that ignores the credit it was granted is refused rather
// than held without bound. The frame beyond it goes out on the outer peer
// itself, which is the only way past this side's own credit and is what a peer
// of another making may do; nothing here receives, so the window stays full.
func TestAFrameBeyondTheWindowEndsTheChannel(t *testing.T) {
	client, server := peers(t, tunnel.Options{Window: 2})
	a, _ := pair(t, client, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if err := a.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Peer().Emit(ctx, tunnel.FrameEvent, map[string]any{"channel": a.ID, "text": "beyond"}); err != nil {
		t.Fatal(err)
	}
	// The sender learns of the refusal from its own end of the channel.
	var closed *duplex.CloseError
	if _, err := a.Receive(ctx); !errors.As(err, &closed) {
		t.Fatalf("the sender's channel ended with %v", err)
	}
	if closed.Code != duplex.CodeProtocolError || closed.Reason != "a frame beyond the window of 2" {
		t.Fatalf("the channel ended with %d %q", int(closed.Code), closed.Reason)
	}
}
