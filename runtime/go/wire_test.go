package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

type wireReplySink struct{ replies chan duplex.ProfileFrame }

func TestWireCancellationRetainsExecutingHandlerBudget(t *testing.T) {
	for _, mode := range []string{"cancel", "caller-deadline", "receiver-deadline"} {
		t.Run(mode, func(t *testing.T) {
			options := ws.Options{MaxConcurrentHandlers: 1}
			if mode == "receiver-deadline" {
				options.RequestTimeout = 100 * time.Millisecond
			}
			client, server := newPair(t, options, ws.Options{})
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var released sync.Once
			t.Cleanup(func() { released.Do(func() { close(release) }) })
			var calls atomic.Int32
			_, err := ws.HandleWire(server.Wire(), []string{"hold"}, func(ctx context.Context, _ json.RawMessage) (any, error) {
				if calls.Add(1) == 1 {
					close(entered)
					<-ctx.Done()
					close(cancelled)
					<-release
				}
				return "finished", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "caller-deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- ws.CallWire(ctx, client.Wire(), []string{"hold"}, nil, nil) }()
			receive(t, entered)
			if mode == "cancel" {
				cancel()
			}
			if mode != "receiver-deadline" {
				if err := receive(t, result); !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("caller cancellation = %v", err)
				}
			}
			receive(t, cancelled)
			// Repeated round trips keep exercising admission while the first body
			// is explicitly held, independent of when its wrapper is scheduled.
			for range 10 {
				err := ws.CallWire(context.Background(), client.Wire(), []string{"hold"}, nil, nil)
				var public *ws.PublicError
				if !errors.As(err, &public) || public.Code != "busy" {
					t.Fatalf("request admitted while cancelled body still runs: calls=%d, err=%v", calls.Load(), err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("executed %d bodies at a limit of one", calls.Load())
			}
			released.Do(func() { close(release) })
			if mode == "receiver-deadline" {
				var public *ws.PublicError
				if err := receive(t, result); !errors.As(err, &public) || public.Code != "cancelled" {
					t.Fatalf("receiver deadline = %v", err)
				}
			}
			ready, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			for {
				err := ws.CallWire(ready, client.Wire(), []string{"hold"}, nil, nil)
				if err == nil {
					break
				}
				var public *ws.PublicError
				if !errors.As(err, &public) || public.Code != "busy" {
					t.Fatal(err)
				}
			}
		})
	}
}

type wireVerifiedKey struct{}
type wireContextPropagator struct{ ws.Propagator }

func (p wireContextPropagator) Extract(ctx context.Context, trace ws.Trace) context.Context {
	return context.WithValue(p.Propagator.Extract(ctx, trace), wireVerifiedKey{}, true)
}

func TestWireKeepsReceivedContextWithoutForwardingApplicationMetadata(t *testing.T) {
	client, server := newPair(t, ws.Options{Propagator: wireContextPropagator{ws.DefaultPropagator}}, ws.Options{})
	_, err := ws.HandleWire(client.Wire(), []string{"reverse"}, func(ctx context.Context, _ json.RawMessage) (any, error) { return ws.MetaFrom(ctx), nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.HandleWire(server.Wire(), []string{"check"}, func(ctx context.Context, _ json.RawMessage) (any, error) {
		var reverse ws.Meta
		if err := ws.CallWire(ctx, server.Wire(), []string{"reverse"}, nil, &reverse); err != nil {
			return nil, err
		}
		return map[string]any{"verified": ctx.Value(wireVerifiedKey{}) == true, "received": ws.MetaFrom(ctx), "reverse": reverse}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Verified bool
		Received ws.Meta
		Reverse  ws.Meta
	}
	if err := ws.CallWire(ws.WithMeta(context.Background(), ws.Meta{"credential": "one-call"}), client.Wire(), []string{"check"}, nil, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Verified || got.Received["credential"] != "one-call" || len(got.Reverse) != 0 {
		t.Fatalf("wire request context = %+v", got)
	}
}

func TestWireHandlerPanicStaysPrivateAndObserved(t *testing.T) {
	observed := &recorder{}
	client, server := newPair(t, ws.Options{Observer: observed}, ws.Options{})
	_, err := ws.HandleWire(server.Wire(), []string{"panic"}, func(context.Context, json.RawMessage) (any, error) { panic("private failure") })
	if err != nil {
		t.Fatal(err)
	}
	err = ws.CallWire(context.Background(), client.Wire(), []string{"panic"}, nil, nil)
	var public *ws.PublicError
	if !errors.As(err, &public) || public.Code != "internal" || public.Message != "Internal error" {
		t.Fatalf("panic response = %v", err)
	}
	count := 0
	for _, event := range observed.all() {
		if failure, ok := event.(ws.HandlerPanic); ok {
			count++
			if failure.Value != "private failure" {
				t.Fatalf("panic observation = %+v", failure)
			}
		}
	}
	if count != 1 {
		t.Fatalf("panic observations = %d", count)
	}
	if server.Err() != nil {
		t.Fatalf("panic ended carrier: %v", server.Err())
	}
}

func (s *wireReplySink) Send(_ []string, message duplex.Message) error {
	s.replies <- message.Frame
	return nil
}
func (*wireReplySink) Receive([]string, duplex.Receiver) (func(), error) {
	return nil, duplex.ErrReceiverExists
}
func (*wireReplySink) Close(duplex.Code, string) error { return nil }

func TestWirePreservesRequestAndEventAdmissionOrder(t *testing.T) {
	observed := &recorder{}
	client, server := newPair(t, ws.Options{}, ws.Options{Observer: observed})
	sink := &wireReplySink{replies: make(chan duplex.ProfileFrame, 40)}
	address := &duplex.ReturnAddress{Wire: sink}
	wire := client.Wire()
	var want []string
	for i := range 40 {
		path := []string{"ordered", fmt.Sprint(i)}
		_, err := ws.HandleWire(server.Wire(), path, func(context.Context, json.RawMessage) (any, error) { return nil, nil })
		if err != nil {
			t.Fatal(err)
		}
		name, _ := duplex.EncodePath(path)
		for _, kind := range []duplex.ProfileKind{duplex.ProfileRequest, duplex.ProfileEvent} {
			frame := duplex.ProfileFrame{Version: 1, Kind: kind}
			if kind == duplex.ProfileRequest {
				frame.ID = fmt.Sprintf("c:%d", i+1)
				frame.Params = json.RawMessage("{}")
			} else {
				frame.Data = json.RawMessage("null")
			}
			if err := wire.Send(path, duplex.Message{Frame: frame, Return: address}); err != nil {
				t.Fatal(err)
			}
			want = append(want, string(kind)+" "+name)
		}
	}
	for range 40 {
		receive(t, sink.replies)
	}
	var got []string
	for _, event := range observed.all() {
		if sent, ok := event.(ws.FrameSent); ok && (sent.Kind == "request" || sent.Kind == "event") {
			got = append(got, sent.Kind+" "+sent.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("per-wire send order changed:\n got %v\nwant %v", got, want)
	}
}

func TestWirePreservesCancellationBeforeTheFollowingEvent(t *testing.T) {
	observed := &recorder{}
	client, server := newPair(t, ws.Options{}, ws.Options{Observer: observed})
	started := make(chan struct{})
	eventReceived := make(chan struct{})
	afterName, _ := duplex.EncodePath([]string{"after"})
	if err := server.HandleEvent(afterName, func(context.Context, *ws.Peer, json.RawMessage) { close(eventReceived) }); err != nil {
		t.Fatal(err)
	}
	_, err := ws.HandleWire(server.Wire(), []string{"wait"}, func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &wireReplySink{replies: make(chan duplex.ProfileFrame, 1)}
	address := &duplex.ReturnAddress{Wire: sink}
	wire := client.Wire()
	if err := wire.Send([]string{"wait"}, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileRequest, ID: "c:1", Params: json.RawMessage("{}")}, Return: address}); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	if err := wire.Send([]string{"wait"}, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileCancel, ID: "c:1"}, Return: address}); err != nil {
		t.Fatal(err)
	}
	if err := ws.EmitWire(context.Background(), wire, []string{"after"}, nil); err != nil {
		t.Fatal(err)
	}
	receive(t, sink.replies)
	receive(t, eventReceived)
	var kinds []string
	for _, event := range observed.all() {
		if sent, ok := event.(ws.FrameSent); ok {
			kinds = append(kinds, sent.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"request", "cancel", "event"}) {
		t.Fatalf("send order = %v", kinds)
	}
}

func TestMountedWireKeepsIndependentOriginsAndCancellation(t *testing.T) {
	client, server := newPair(t, ws.Options{}, ws.Options{})
	started := make(chan string, 2)
	finished := make(chan string, 2)
	allow := make(chan struct{})
	defer close(allow)
	_, err := ws.HandleWire(server.Wire(), []string{"worker", "run"}, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, err
		}
		started <- name
		select {
		case <-ctx.Done():
			finished <- name
			return nil, ctx.Err()
		case <-allow:
			return name, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both views select the same existing carrier. Each CallWire has its own
	// local return address, so cancelling one cannot cancel the other's id.
	wire := duplex.At(duplex.Mount(map[string]duplex.Wire{"service": client.Wire()}), []string{"service", "worker"})
	first, cancelFirst := context.WithCancel(context.Background())
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelFirst()
	defer cancelSecond()
	var calls sync.WaitGroup
	results := make(chan error, 2)
	for _, call := range []struct {
		ctx  context.Context
		name string
	}{{first, "first"}, {second, "second"}} {
		calls.Add(1)
		go func() {
			defer calls.Done()
			results <- ws.CallWire(call.ctx, wire, []string{"run"}, call.name, nil)
		}()
	}
	receive(t, started)
	receive(t, started)
	cancelFirst()
	if err := receive(t, results); !errors.Is(err, context.Canceled) {
		t.Fatalf("first cancellation = %v", err)
	}
	if name := receive(t, finished); name != "first" {
		t.Fatalf("cancelled %q, want first", name)
	}
	var echoed string
	_, err = ws.HandleWire(server.Wire(), []string{"worker", "echo"}, func(_ context.Context, value json.RawMessage) (any, error) { return value, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.CallWire(context.Background(), wire, []string{"echo"}, "still open", &echoed); err != nil || echoed != "still open" {
		t.Fatalf("sibling call after cancellation = %q, %v", echoed, err)
	}
	cancelSecond()
	if err := receive(t, results); !errors.Is(err, context.Canceled) {
		t.Fatalf("second cancellation = %v", err)
	}
	if name := receive(t, finished); name != "second" {
		t.Fatalf("second cancellation reached %q", name)
	}
	calls.Wait()
}

func TestWirePathPreservesOpaqueSegmentsOverTheExistingEnvelope(t *testing.T) {
	client, server := newPair(t, ws.Options{}, ws.Options{})
	for _, route := range []struct {
		path []string
		want string
	}{{[]string{"a.b"}, "one segment"}, {[]string{"a", "b"}, "two segments"}, {[]string{""}, "empty segment"}} {
		_, err := ws.HandleWire(server.Wire(), route.path, func(context.Context, json.RawMessage) (any, error) { return route.want, nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, route := range []struct {
		path []string
		want string
	}{{[]string{"a.b"}, "one segment"}, {[]string{"a", "b"}, "two segments"}, {[]string{""}, "empty segment"}} {
		var got string
		if err := ws.CallWire(context.Background(), client.Wire(), route.path, nil, &got); err != nil || got != route.want {
			t.Fatalf("path %q = %q, %v; want %q", route.path, got, err, route.want)
		}
		if err := ws.CallWire(context.Background(), duplex.At(client.Wire(), route.path), nil, nil, &got); err != nil || got != route.want {
			t.Fatalf("selected leaf %q = %q, %v; want %q", route.path, got, err, route.want)
		}
	}
}
