package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex"
)

// TestPeerSpeaksTheProfileOverAnyConnection: two peers over an in-memory
// pipe, no socket anywhere, complete a call, a reverse call, an event and a
// cancellation, and a closed pipe ends both. The profile is written to the
// seam, not to a WebSocket.
func TestPeerSpeaksTheProfileOverAnyConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientConn, serverConn := duplex.Pipe(1 << 20)
	blocked := make(chan struct{})
	server, err := NewPeer(ctx, serverConn, ServerRole, Options{Handlers: map[string]Handler{
		"echo": func(ctx context.Context, p *Peer, raw json.RawMessage) (any, error) {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			var back string
			if err := p.Call(ctx, "reverse", s, &back); err != nil {
				return nil, err
			}
			return back, nil
		},
		"block": func(ctx context.Context, p *Peer, raw json.RawMessage) (any, error) {
			<-ctx.Done()
			close(blocked)
			return nil, ctx.Err()
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	observed := make(chan Event, 1)
	client, err := NewPeer(ctx, clientConn, ClientRole, Options{
		Handlers: map[string]Handler{"reverse": func(ctx context.Context, p *Peer, raw json.RawMessage) (any, error) {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			runes := []rune(s)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		}},
		Events: map[string]EventHandler{"changed": func(ctx context.Context, p *Peer, raw json.RawMessage) {
			observed <- Event{Name: "changed", Data: raw}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var result string
	if err := client.Call(ctx, "echo", "seam", &result); err != nil {
		t.Fatal(err)
	}
	if result != "maes" {
		t.Fatalf("a call and its reverse call over the pipe returned %q", result)
	}
	if err := server.Emit(ctx, "changed", map[string]int{"count": 7}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-observed:
		if string(event.Data) != `{"count":7}` {
			t.Fatalf("the event arrived as %s", event.Data)
		}
	case <-ctx.Done():
		t.Fatal("no event arrived over the pipe")
	}
	short, cancelShort := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShort()
	if err := client.Call(short, "block", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a cancelled call returned %v", err)
	}
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("the cancellation never reached the handler over the pipe")
	}
	if err := clientConn.Close(ctx, duplex.CodeNormal, "done"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.Done():
	case <-ctx.Done():
		t.Fatal("closing the connection did not end the server peer")
	}
	var closed *duplex.CloseError
	if err := server.Err(); !errors.As(err, &closed) || closed.Code != duplex.CodeNormal || closed.Reason != "done" {
		t.Fatalf("the server peer ended with %v, not the close it was sent", err)
	}
}

// TestPeerRefusesAFrameOverItsLimit: a connection whose maker set a laxer
// limit than the peer's still cannot hand the peer an oversized frame.
func TestPeerRefusesAFrameOverItsLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	near, far := duplex.Pipe(1 << 20)
	peer, err := NewPeer(ctx, near, ClientRole, Options{MaxFrameBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	big := make([]byte, 600)
	for i := range big {
		big[i] = ' '
	}
	copy(big, `{"version":1,"kind":"event","event":"e","data":1`)
	big[len(big)-1] = '}'
	if err := far.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: big}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peer.Done():
	case <-ctx.Done():
		t.Fatal("the peer accepted a frame over its limit")
	}
	if err := peer.Err(); err == nil || err.Error() != "duplex frame exceeds size limit" {
		t.Fatalf("the peer ended with %v", err)
	}
}
