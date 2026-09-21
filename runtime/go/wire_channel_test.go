package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

type wireChannelHarness struct {
	ctx                          context.Context
	client, server               *tunnel.Tunnel
	clientLog, serverLog         *recorder
	siblingClient, siblingServer *ws.Peer
	outerCalls, siblingCalls     atomic.Int32
}

func newWireChannelHarness(t *testing.T) *wireChannelHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	h := &wireChannelHarness{ctx: ctx, clientLog: &recorder{}, serverLog: &recorder{}}
	client, server := newPair(t, ws.Options{Observer: h.serverLog}, ws.Options{Observer: h.clientLog})
	var err error
	h.client, err = tunnel.New(client, tunnel.Options{Window: 1})
	if err != nil {
		t.Fatal(err)
	}
	h.server, err = tunnel.New(server, tunnel.Options{Window: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Handle("outer.echo", func(_ context.Context, _ *ws.Peer, data json.RawMessage) (any, error) {
		h.outerCalls.Add(1)
		return data, nil
	}); err != nil {
		t.Fatal(err)
	}
	a, b := h.open(t)
	h.siblingClient = newWireChannelPeer(t, ctx, a, ws.ClientRole, ws.Options{})
	h.siblingServer = newWireChannelPeer(t, ctx, b, ws.ServerRole, ws.Options{})
	_, err = ws.HandleWire(h.siblingServer.Wire(), []string{"echo"}, func(_ context.Context, data json.RawMessage) (any, error) {
		h.siblingCalls.Add(1)
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.healthy(t)
	return h
}

func (h *wireChannelHarness) open(t *testing.T) (*tunnel.Connection, *tunnel.Connection) {
	t.Helper()
	a, err := h.client.OpenConnection(h.ctx, "wire-channel-test", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.server.AcceptConnection(h.ctx)
	if err != nil || b.ID != a.ID {
		t.Fatalf("accept = %v, %v; opened %d", b, err, a.ID)
	}
	return a, b
}

func newWireChannelPeer(t *testing.T, ctx context.Context, channel *tunnel.Connection, role ws.Role, options ws.Options) *ws.Peer {
	t.Helper()
	peer, err := ws.NewPeer(ctx, channel, role, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return peer
}

func (h *wireChannelHarness) healthy(t *testing.T) {
	t.Helper()
	beforeOuter, beforeSibling := h.outerCalls.Load(), h.siblingCalls.Load()
	var outer, sibling string
	if err := h.client.Peer().Call(h.ctx, "outer.echo", "outer alive", &outer); err != nil || outer != "outer alive" {
		t.Fatalf("outer round trip = %q, %v", outer, err)
	}
	if err := ws.CallWire(h.ctx, h.siblingClient.Wire(), []string{"echo"}, "sibling alive", &sibling); err != nil || sibling != "sibling alive" {
		t.Fatalf("sibling round trip = %q, %v", sibling, err)
	}
	if h.outerCalls.Load() != beforeOuter+1 || h.siblingCalls.Load() != beforeSibling+1 {
		t.Fatal("healthy carriers did not dispatch each call exactly once")
	}
	for _, peer := range []*ws.Peer{h.client.Peer(), h.server.Peer(), h.siblingClient, h.siblingServer} {
		if err := peer.Err(); err != nil {
			t.Fatalf("unrelated carrier ended: %v", err)
		}
	}
}

func wireChannelCount(log *recorder, matches func(ws.ObserverEvent) bool) int {
	count := 0
	for _, event := range log.all() {
		if matches(event) {
			count++
		}
	}
	return count
}

func wireChannelAwait(t *testing.T, log *recorder, count int, matches func(ws.ObserverEvent) bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		next := log.next()
		if wireChannelCount(log, matches) >= count {
			return
		}
		select {
		case <-next:
		case <-deadline.C:
			t.Fatal("observer did not reach the channel barrier")
		}
	}
}

func wireChannelClosed(event ws.ObserverEvent) bool {
	_, ok := event.(ws.ConnectionClosed)
	return ok
}

func wireChannelPressure(event ws.ObserverEvent) bool {
	pressure, ok := event.(ws.Backpressure)
	return ok && pressure.Stalled
}

// A selected mounted view uses the inner peer already carried by this channel.
// No new channel or peer is introduced by selecting or mounting it.
func wireChannelView(peer *ws.Peer) duplex.Wire {
	return duplex.At(duplex.Mount(map[string]duplex.Wire{
		"destination": duplex.At(peer.Wire(), []string{"events"}),
	}), []string{"destination"})
}

// The receiver takes nothing. One frame spends the window, the next blocks
// the transport on credit, and the last two occupy its bounded output queue.
// Observer barriers establish each phase without depending on scheduler speed.
func wireChannelFill(t *testing.T, h *wireChannelHarness, channel *tunnel.Connection, wire duplex.Wire, log *recorder) {
	t.Helper()
	for sequence := range 4 {
		if err := ws.EmitWire(h.ctx, wire, []string{"item"}, sequence); err != nil {
			t.Fatalf("accepted prefix item %d: %v", sequence, err)
		}
		switch sequence {
		case 0:
			wireChannelAwait(t, log, 1, func(event ws.ObserverEvent) bool { _, ok := event.(ws.FrameSent); return ok })
		case 1:
			wireChannelAwait(t, h.clientLog, 1, func(event ws.ObserverEvent) bool {
				stall, ok := event.(tunnel.CreditStall)
				return ok && stall.ID == channel.ID
			})
		default:
			wireChannelAwait(t, log, sequence+1, func(event ws.ObserverEvent) bool { _, ok := event.(ws.EventEmitted); return ok })
		}
	}
}

func TestWireChannelNeverReadingEndsOnlyItsOwnCarrier(t *testing.T) {
	h := newWireChannelHarness(t)
	a, _ := h.open(t)
	log := &recorder{}
	peer := newWireChannelPeer(t, h.ctx, a, ws.ClientRole, ws.Options{QueueCapacity: 2, WriteTimeout: 5 * time.Second, Observer: log})
	wire := wireChannelView(peer)
	wireChannelFill(t, h, a, wire, log)
	finished := make(chan error, 1)
	go func() { finished <- ws.EmitWire(h.ctx, wire, []string{"item"}, 4) }()
	select {
	case err := <-finished:
		if err != nil && !errors.Is(err, ws.ErrBackpressure) {
			t.Fatalf("overflow admission = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("admission waited for the unread channel")
	}
	select {
	case <-peer.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("full channel waited for its transport deadline")
	}
	if !errors.Is(peer.Err(), ws.ErrBackpressure) {
		t.Fatalf("carrier ended with %v, want backpressure", peer.Err())
	}
	wireChannelAwait(t, log, 1, wireChannelClosed)
	wireChannelAwait(t, h.serverLog, 1, func(event ws.ObserverEvent) bool {
		closed, ok := event.(tunnel.ChannelClosed)
		return ok && closed.ID == a.ID
	})
	if wireChannelCount(log, wireChannelPressure) != 1 || wireChannelCount(log, wireChannelClosed) != 1 {
		t.Fatal("overflow did not report exactly one terminal pressure and close")
	}
	h.healthy(t)
}

func TestWireChannelSlowReaderKeepsAcceptedOrderAndSiblingProgress(t *testing.T) {
	h := newWireChannelHarness(t)
	a, b := h.open(t)
	log := &recorder{}
	peer := newWireChannelPeer(t, h.ctx, a, ws.ClientRole, ws.Options{QueueCapacity: 2, WriteTimeout: 5 * time.Second, Observer: log})
	wireChannelFill(t, h, a, wireChannelView(peer), log)
	h.healthy(t) // The other channel progresses while this one lacks credit.
	name, err := duplex.EncodePath([]string{"events", "item"})
	if err != nil {
		t.Fatal(err)
	}
	for want := range 4 {
		frame, err := b.Receive(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Kind  string `json:"kind"`
			Event string `json:"event"`
			Data  int    `json:"data"`
		}
		if err := json.Unmarshal(frame.Data, &envelope); err != nil {
			t.Fatal(err)
		}
		if frame.Kind != duplex.Text || envelope.Kind != "event" || envelope.Event != name || envelope.Data != want {
			t.Fatalf("accepted event %d = %+v", want, envelope)
		}
	}
	if peer.Err() != nil || wireChannelCount(log, wireChannelPressure) != 0 || wireChannelCount(log, wireChannelClosed) != 0 {
		t.Fatalf("slow reader lost a healthy carrier: %v", peer.Err())
	}
	h.healthy(t)
}

func TestWireChannelFailureEndsItsBlockedWriterAndPreservesSiblings(t *testing.T) {
	h := newWireChannelHarness(t)
	a, b := h.open(t)
	log := &recorder{}
	peer := newWireChannelPeer(t, h.ctx, a, ws.ClientRole, ws.Options{QueueCapacity: 2, WriteTimeout: 5 * time.Second, Observer: log})
	wire := wireChannelView(peer)
	wireChannelFill(t, h, a, wire, log)
	if err := b.Close(h.ctx, duplex.CodeInternalError, "destination failed"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peer.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("failed channel left its writer blocked on credit")
	}
	var closed *duplex.CloseError
	if !errors.As(peer.Err(), &closed) || closed.Code != duplex.CodeInternalError || closed.Reason != "destination failed" {
		t.Fatalf("carrier failure = %v", peer.Err())
	}
	wireChannelAwait(t, log, 1, wireChannelClosed)
	if wireChannelCount(log, wireChannelClosed) != 1 || wireChannelCount(log, wireChannelPressure) != 0 {
		t.Fatal("carrier failure was reported more than once or as queue pressure")
	}
	if err := ws.EmitWire(h.ctx, wire, []string{"item"}, 99); err == nil {
		t.Fatal("failed carrier accepted another frame")
	}
	h.healthy(t)
}
