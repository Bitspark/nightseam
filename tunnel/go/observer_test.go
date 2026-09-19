package tunnel_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// carried stands for everything a channel carries. It travels in the frames a
// channel exchanges and in the calls a peer makes over it, and it must appear
// in nothing either tunnel's observer is told.
const carried = "channel-payload-sentinel-4bf92f35"

// recorder keeps what it was told, which is what a test needs and what no
// adapter should do. Observe is called from whichever goroutine the event
// happened on — a handler, a caller, the reader — so it holds a lock of its
// own.
type recorder struct {
	changed  chan struct{}
	mu       sync.Mutex
	observed []runtime.ObserverEvent
}

func (r *recorder) Observe(event runtime.ObserverEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observed = append(r.observed, event)
	if r.changed != nil {
		close(r.changed)
		r.changed = nil
	}
}

func (r *recorder) all() []runtime.ObserverEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runtime.ObserverEvent(nil), r.observed...)
}

// lines is what the tunnel told this observer, in order and rendered so that a
// sequence reads as one; the runtime's own events reach the same observer and
// are the runtime's suite to hold, not this one's.
func (r *recorder) lines() []string {
	lines := []string{}
	for _, event := range r.all() {
		if rendered := channelLine(event); rendered != "" {
			lines = append(lines, rendered)
		}
	}
	return lines
}

// next captures a notification before reading the state, so no update is lost.
func (r *recorder) next() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changed == nil {
		r.changed = make(chan struct{})
	}
	return r.changed
}

// await waits until the tunnel told this observer at least count things, so
// that a test never reads a sequence a tunnel is still writing.
func (r *recorder) await(t *testing.T, count int) []string {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		next := r.next()
		if lines := r.lines(); len(lines) >= count {
			return lines
		}
		select {
		case <-next:
		case <-deadline.C:
			t.Fatalf("the tunnel told of %d things, want %d:\n%s", len(r.lines()), count, strings.Join(r.lines(), "\n"))
		}
	}
}

// channelLine is the type switch a consumer dispatches by, which is also how
// this suite reads a sequence: every event of the tunnel's surface has a case
// here, and an event of another layer none.
func channelLine(event runtime.ObserverEvent) string {
	switch e := event.(type) {
	case tunnel.ChannelOpened:
		return fmt.Sprintf("opened %s id=%d opener=%t", e.Family, e.ID, e.Opener)
	case tunnel.ChannelAccepted:
		return fmt.Sprintf("accepted %s id=%d", e.Family, e.ID)
	case tunnel.ChannelClosed:
		return fmt.Sprintf("closed %s id=%d code=%d reason=%q", e.Family, e.ID, e.Code, e.Reason)
	case tunnel.CreditStall:
		return fmt.Sprintf("stalled %s id=%d waiting=%d", e.Family, e.ID, e.Waiting)
	case tunnel.OpenRefused:
		return fmt.Sprintf("refused %s reason=%q", e.Family, e.Reason)
	}
	return ""
}

// heard is the first thing of one kind an observer was told, and whether it
// was told one.
func heard[T runtime.ObserverEvent](r *recorder) (T, bool) {
	for _, event := range r.all() {
		if match, ok := event.(T); ok {
			return match, true
		}
	}
	var zero T
	return zero, false
}

func same(t *testing.T, side string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s was told\n%s\nwant\n%s", side, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// watched is two outer peers over a pipe, each with a tunnel and an observer
// of its own: a tunnel takes no observer of its own, observing through the
// peer it runs over, so the tunnel's events land on the peer's.
func watched(t *testing.T, options tunnel.Options) (client, server *tunnel.Tunnel, atClient, atServer *recorder) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, b := duplex.Pipe(8 << 20)
	atClient, atServer = &recorder{}, &recorder{}
	pa, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{Observer: atClient})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{Observer: atServer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pa.Close(); pb.Close() })
	if client, err = tunnel.New(pa, options); err != nil {
		t.Fatal(err)
	}
	if server, err = tunnel.New(pb, options); err != nil {
		t.Fatal(err)
	}
	return client, server, atClient, atServer
}

// opening opens a channel from one tunnel and takes it at the other, so that
// what a test then reads is a channel both sides hold.
func opening(t *testing.T, ctx context.Context, from, to *tunnel.Tunnel, family string) (opened, accepted *tunnel.Channel) {
	t.Helper()
	taken := make(chan *tunnel.Channel, 1)
	go func() {
		c, err := to.Accept(ctx)
		if err != nil {
			t.Error(err)
		}
		taken <- c
	}()
	opened, err := from.Open(ctx, family)
	if err != nil {
		t.Fatal(err)
	}
	return opened, <-taken
}

// A channel opened, accepted, exchanged over and closed is told of on both
// sides, in that order and with the id, the family and the sequence the opener
// named — and the frames that crossed it are told of by neither tunnel: what a
// channel carries is the business of the peers speaking over it.
func TestAChannelIsToldOfOnBothSides(t *testing.T) {
	client, server, atClient, atServer := watched(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened, inbound := opening(t, ctx, client, server, "probe")
	for _, exchange := range []struct {
		from, to *tunnel.Channel
		data     string
	}{{opened, inbound, "up " + carried}, {inbound, opened, "down " + carried}} {
		if err := exchange.from.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte(exchange.data)}); err != nil {
			t.Fatal(err)
		}
		if frame, err := exchange.to.Receive(ctx); err != nil || string(frame.Data) != exchange.data {
			t.Fatalf("received %q, %v", frame.Data, err)
		}
	}
	if err := opened.Close(ctx, duplex.CodeNormal, "the work is done"); err != nil {
		t.Fatal(err)
	}
	id := opened.ID
	same(t, "the opener", atClient.await(t, 2), []string{
		fmt.Sprintf("opened probe id=%d opener=true", id),
		fmt.Sprintf("closed probe id=%d code=%d reason=%q", id, duplex.CodeNormal, "the work is done"),
	})
	same(t, "the accepter", atServer.await(t, 3), []string{
		fmt.Sprintf("opened probe id=%d opener=false", id),
		fmt.Sprintf("accepted probe id=%d", id),
		fmt.Sprintf("closed probe id=%d code=%d reason=%q", id, duplex.CodeNormal, "the work is done"),
	})
	noPayload(t, "the opener", atClient)
	noPayload(t, "the accepter", atServer)
}

// A send that finds the other side's window full says that it waits, and says
// how many senders on the channel then wait.
func TestAStallIsToldOf(t *testing.T) {
	client, server, atClient, _ := watched(t, tunnel.Options{Window: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened, _ := opening(t, ctx, client, server, "probe")
	for i := 0; i < 2; i++ {
		if err := opened.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := heard[tunnel.CreditStall](atClient); ok {
		t.Fatal("a send within the window stalled")
	}
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	err := opened.Send(short, duplex.Frame{Kind: duplex.Text, Data: []byte("third " + carried)})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the third frame did not wait for credit: %v", err)
	}
	stall, ok := heard[tunnel.CreditStall](atClient)
	if !ok {
		t.Fatalf("no stall among %s", strings.Join(atClient.lines(), ", "))
	}
	if stall.Family != "probe" || stall.ID != opened.ID || stall.Waiting != 1 || stall.At.IsZero() {
		t.Fatalf("stalled %+v", stall)
	}
	noPayload(t, "the sender", atClient)
}

// An open that opens nothing is told of on the side that refused it and on the
// side whose open was refused, and it names no channel.
func TestARefusedOpenIsToldOf(t *testing.T) {
	client, _, atClient, atServer := watched(t, tunnel.Options{AcceptCapacity: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Open(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Open(ctx, "probe"); err == nil {
		t.Fatal("the second open was not refused")
	}
	refused, ok := heard[tunnel.OpenRefused](atClient)
	if !ok || refused.Family != "probe" || !strings.Contains(refused.Reason, tunnel.ErrorRefused) {
		t.Fatalf("the opener was told %+v, %t, among %s", refused, ok, strings.Join(atClient.lines(), ", "))
	}
	atRefuser, ok := heard[tunnel.OpenRefused](atServer)
	if !ok || atRefuser.Family != "probe" || !strings.Contains(atRefuser.Reason, "no room") {
		t.Fatalf("the refuser was told %+v, %t", atRefuser, ok)
	}
	// An open this side will not make at all is refused here and told of here.
	if _, err := client.Open(ctx, ""); err == nil {
		t.Fatal("a channel of no family opened")
	}
	lines := atClient.lines()
	if last, want := lines[len(lines)-1], "refused  reason=\"a channel is opened for a family\""; last != want {
		t.Fatalf("the opener was last told %q, want %q", last, want)
	}
}

// A channel's own peer takes its own observer through its options, as a peer
// over any transport does: what the peers say inside the channel reaches that
// one, and the tunnel's observer hears the channel open, accept and close and
// nothing of what crossed it.
func TestAChannelsPeerTakesItsOwnObserver(t *testing.T) {
	client, server, atClient, atServer := watched(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened, inbound := opening(t, ctx, client, server, "probe")
	inside := &recorder{}
	served, err := runtime.NewPeer(ctx, inbound, runtime.ServerRole, runtime.Options{Handlers: map[string]runtime.Handler{
		"echo": func(_ context.Context, _ *runtime.Peer, params json.RawMessage) (any, error) { return params, nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer served.Close()
	caller, err := runtime.NewPeer(ctx, opened, runtime.ClientRole, runtime.Options{Observer: inside})
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	var result map[string]string
	if err := caller.Call(ctx, "echo", map[string]string{"secret": carried}, &result); err != nil {
		t.Fatal(err)
	}
	if result["secret"] != carried {
		t.Fatalf("echoed %v", result)
	}
	ended, ok := heard[runtime.RequestEnded](inside)
	if !ok || ended.Method != "echo" || ended.Outcome != runtime.OutcomeOK {
		t.Fatalf("the channel's own peer was told %+v, %t", ended, ok)
	}
	for side, r := range map[string]*recorder{"the opener": atClient, "the accepter": atServer} {
		if started, ok := heard[runtime.RequestStarted](r); ok && started.Method == "echo" {
			t.Fatalf("%s heard a call made inside the channel: %+v", side, started)
		}
		noPayload(t, side, r)
	}
}

// noPayload is the rule of the whole surface: everything an observer of a
// tunnel was told, marshalled and rendered, names nothing the channels
// carried.
func noPayload(t *testing.T, side string, r *recorder) {
	t.Helper()
	for _, event := range r.all() {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("%s: marshal %T: %v", side, event, err)
		}
		if strings.Contains(string(encoded), carried) {
			t.Fatalf("%s: %T carried a payload: %s", side, event, encoded)
		}
		if rendered := fmt.Sprintf("%+v", event); strings.Contains(rendered, carried) {
			t.Fatalf("%s: %T carried a payload beyond its marshalled members: %s", side, event, rendered)
		}
	}
}
