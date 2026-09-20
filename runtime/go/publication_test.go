package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

func wantUnpublished(t *testing.T, err error, want bool) {
	t.Helper()
	var unpublished *ws.UnpublishedError
	if got := errors.As(err, &unpublished); got != want || err == nil {
		t.Fatalf("publication proof: %v, want unpublished=%v", err, want)
	}
}

func TestUnpublishedProofIsLocalToTheSendAttempt(t *testing.T) {
	entered, finish := make(chan struct{}, 1), make(chan struct{})
	client, server := newPair(t, ws.Options{Handlers: map[string]ws.Handler{
		"wait": func(c context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
			entered <- struct{}{}
			select {
			case <-finish:
				return nil, nil
			case <-c.Done():
				return nil, c.Err()
			}
		},
		"busy": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
			return nil, &ws.PublicError{Code: "busy", Message: "retained before refusing"}
		},
		"nested": func(c context.Context, peer *ws.Peer, _ json.RawMessage) (any, error) {
			withdrawn, cancel := context.WithCancel(c)
			cancel()
			return nil, peer.Call(withdrawn, "never.sent", nil, nil)
		},
	}}, ws.Options{MaxPendingRequests: 1, MaxFrameBytes: 512})
	_ = server
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.Call(ctx, "wait", nil, nil)
	wantUnpublished(t, err, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wrapping lost cancellation identity: %v", err)
	}
	wantUnpublished(t, client.Call(context.Background(), "wait", make(chan int), nil), true)
	wantUnpublished(t, client.Call(context.Background(), "wait", strings.Repeat("x", 1024), nil), true)
	wantUnpublished(t, client.Emit(context.Background(), "event", make(chan int)), true)
	wantUnpublished(t, client.Emit(context.Background(), "event", strings.Repeat("x", 1024)), true)

	done := make(chan error, 1)
	go func() { done <- client.Call(context.Background(), "wait", nil, nil) }()
	receive(t, entered)
	err = client.Call(context.Background(), "busy", nil, nil)
	wantUnpublished(t, err, true)
	var public *ws.PublicError
	if !errors.As(err, &public) || public.Code != "busy" {
		t.Fatalf("wrapping lost public refusal: %v", err)
	}
	close(finish)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}

	// The same public code received from the other side carries no proof.
	err = client.Call(context.Background(), "busy", nil, nil)
	wantUnpublished(t, err, false)
	if !errors.As(err, &public) || public.Code != "busy" {
		t.Fatal(err)
	}
	// A marker from a nested, definitely-unsent call must not cross the wire
	// as proof about the request whose implementation has already run.
	err = client.Call(context.Background(), "nested", nil, nil)
	wantUnpublished(t, err, false)
	if !errors.As(err, &public) || public.Code != "cancelled" {
		t.Fatal(err)
	}
}

type publicationWriteFailure struct {
	duplex.Conn
	wrote chan struct{}
	err   error
}

func (c *publicationWriteFailure) Send(context.Context, duplex.Frame) error {
	close(c.wrote)
	return c.err
}

func TestQueuedWriteFailureHasNoUnpublishedProof(t *testing.T) {
	near, far := duplex.Pipe(1 << 20)
	defer far.Abort()
	cause := errors.New("transport failed after accepting a queued frame")
	connection := &publicationWriteFailure{Conn: near, wrote: make(chan struct{}), err: cause}
	peer, err := ws.NewPeer(context.Background(), connection, ws.ClientRole, ws.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	err = peer.Call(context.Background(), "supply", nil, nil)
	receive(t, connection.wrote)
	wantUnpublished(t, err, false)
	if !errors.Is(err, cause) {
		t.Fatalf("transport cause lost: %v", err)
	}
}
