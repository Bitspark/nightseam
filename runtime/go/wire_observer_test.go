package runtime

import (
	"context"
	"encoding/json"
	"errors"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

// Expose only the neutral contract, with no concrete peer available to inspect.
type observedOpaqueWire struct{ bitwire.Wire }
type observedOpaqueEndpoint struct{ bitwire.Endpoint }

type cancellationObservingWire struct {
	bitwire.Wire
	observer *wireObservations
	ended    chan bool
}

func (w cancellationObservingWire) Send(path []string, message bitwire.Message) error {
	if message.Frame.Kind == bitwire.ProfileCancel {
		ended := false
		for _, event := range w.observer.snapshot() {
			if event, ok := event.(RequestEnded); ok && !event.Incoming {
				ended = true
			}
		}
		w.ended <- ended
	}
	return w.Wire.Send(path, message)
}

type wireObservations struct {
	mu     sync.Mutex
	events []ObserverEvent
	ended  chan struct{}
}

func (o *wireObservations) Observe(event ObserverEvent) {
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
	if end, ok := event.(RequestEnded); ok && end.Incoming && o.ended != nil {
		o.ended <- struct{}{}
	}
}
func (o *wireObservations) snapshot() []ObserverEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]ObserverEvent(nil), o.events...)
}

func TestWireHelperObservationsLabelOpaqueOperationsWithoutTransportEvents(t *testing.T) {
	carrier, model := &wireObservations{}, &wireObservations{ended: make(chan struct{}, 1)}
	a, b := localPair(t, Options{Observer: carrier})
	left, right := observedOpaqueWire{a}, observedOpaqueEndpoint{b}
	path := []string{"member.with.dot"}
	delivered := make(chan struct{})
	rightBinding := testBinding(t, right)
	_, err := RegisterWire(rightBinding, path, WireHandlers{Observer: model, Family: "probe",
		Request: func(_ context.Context, params json.RawMessage) (any, error) { return params, nil },
		Event: func(_ context.Context, data json.RawMessage) error {
			if string(data) != `"secret😀"` {
				t.Errorf("event data %s", data)
			}
			close(delivered)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err := CallWire(context.Background(), left, path, "secret😀", &result, WireCallOptions{Observer: model, Family: "probe"}); err != nil || result != "secret😀" {
		t.Fatalf("call %q: %v", result, err)
	}
	<-model.ended
	if err := EmitWire(context.Background(), left, path, "secret😀", WireEmitOptions{Observer: model, Family: "probe"}); err != nil {
		t.Fatal(err)
	}
	<-delivered
	name, _ := duplex.EncodePath(path)
	starts, ends, emitted, received := 0, 0, 0, 0
	for _, event := range model.snapshot() {
		var family, operation string
		switch event := event.(type) {
		case RequestStarted:
			starts++
			family, operation = event.Family, event.Method
		case RequestEnded:
			ends++
			family, operation = event.Family, event.Method
			if event.Outcome != OutcomeOK || event.Duration < 0 {
				t.Errorf("outcome %#v", event)
			}
		case EventEmitted:
			emitted++
			family, operation = event.Family, event.Name
			if event.Bytes != len(`"secret😀"`) {
				t.Errorf("emitted bytes %d", event.Bytes)
			}
		case EventDelivered:
			received++
			family, operation = event.Family, event.Name
		default:
			t.Fatalf("unexpected model event %T", event)
		}
		if family != "probe" || operation != name {
			t.Errorf("event %#v", event)
		}
	}
	if starts != 2 || ends != 2 || emitted != 1 || received != 1 {
		t.Fatalf("counts %d %d %d %d", starts, ends, emitted, received)
	}
	data, _ := json.Marshal(model.snapshot())
	if strings.Contains(string(data), "secret") {
		t.Fatal("observer received payload")
	}
	if len(carrier.snapshot()) != 0 {
		t.Fatalf("unexpected transport events %#v", carrier.snapshot())
	}
}

func TestWireHelperCompletionDistinguishesCancellationFromSameCodeRefusal(t *testing.T) {
	a, b := localPair(t, Options{})
	model := &wireObservations{ended: make(chan struct{}, 1)}
	started := make(chan struct{})
	bBinding := testBinding(t, b)
	_, err := RegisterWire(bBinding, []string{"wait"}, WireHandlers{Observer: model, Family: "probe", Request: func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	selected := cancellationObservingWire{Wire: a, observer: model, ended: make(chan bool, 1)}
	go func() {
		finished <- CallWire(ctx, selected, []string{"wait"}, nil, nil, WireCallOptions{Observer: model, Family: "probe"})
	}()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	<-model.ended
	if !<-selected.ended {
		t.Error("local completion was observed after cancellation admission")
	}
	ends := 0
	for _, event := range model.snapshot() {
		if event, ok := event.(RequestEnded); ok {
			ends++
			if event.Outcome != OutcomeCancelled || event.ErrorCode != "cancelled" {
				t.Errorf("end %#v", event)
			}
		}
	}
	if ends != 2 {
		t.Fatalf("ended %d times", ends)
	}
	_, err = HandleWire(bBinding, []string{"refuse"}, func(context.Context, json.RawMessage) (any, error) {
		return nil, &PublicError{Code: "cancelled", Message: "Application refusal"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CallWire(context.Background(), a, []string{"refuse"}, nil, nil, WireCallOptions{Observer: model, Family: "probe"}); err == nil {
		t.Fatal("missing refusal")
	}
	events := model.snapshot()
	last := events[len(events)-1].(RequestEnded)
	if last.Outcome != OutcomeErrored || last.ErrorCode != "cancelled" {
		t.Fatalf("refusal %#v", last)
	}
}

func TestWireHelperObserverCannotBreakTrafficOrDuplicatePanic(t *testing.T) {
	carrier, model := &wireObservations{}, &wireObservations{ended: make(chan struct{}, 1)}
	a, b := localPair(t, Options{Observer: carrier})
	observer := localTestObserver(func(event ObserverEvent) { model.Observe(event); panic("broken diagnostic") })
	bBinding := testBinding(t, b)
	_, err := RegisterWire(bBinding, []string{"panic"}, WireHandlers{Observer: observer, Family: "probe", Request: func(context.Context, json.RawMessage) (any, error) { panic("application panic") }})
	if err != nil {
		t.Fatal(err)
	}
	if err := CallWire(context.Background(), a, []string{"panic"}, nil, nil, WireCallOptions{Observer: observer, Family: "probe"}); err == nil {
		t.Fatal("missing panic refusal")
	}
	<-model.ended
	panics := 0
	for _, event := range carrier.snapshot() {
		if _, ok := event.(HandlerPanic); ok {
			panics++
		}
	}
	if panics != 1 {
		t.Fatalf("panic reports %d", panics)
	}
	starts, ends := 0, 0
	for _, event := range model.snapshot() {
		switch event.(type) {
		case RequestStarted:
			starts++
		case RequestEnded:
			ends++
		case HandlerPanic:
			t.Fatal("duplicate panic report")
		}
	}
	if starts != 2 || ends != 2 {
		t.Fatalf("request lifecycle %d starts %d ends", starts, ends)
	}
}

func TestWireHelperHandlerRefusalRemainsAnErrorAfterCancellation(t *testing.T) {
	a, b := localPair(t, Options{})
	model := &wireObservations{ended: make(chan struct{}, 1)}
	started := make(chan struct{})
	bBinding := testBinding(t, b)
	_, err := RegisterWire(bBinding, []string{"refuse"}, WireHandlers{Observer: model, Request: func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, &PublicError{Code: "cancelled", Message: "Application refusal"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- CallWire(ctx, a, []string{"refuse"}, nil, nil, WireCallOptions{Observer: model}) }()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	<-model.ended
	for _, event := range model.snapshot() {
		if event, ok := event.(RequestEnded); ok && event.Incoming {
			if event.Outcome != OutcomeErrored || event.ErrorCode != "cancelled" {
				t.Fatalf("refusal %#v", event)
			}
			return
		}
	}
	t.Fatal("missing incoming completion")
}

func TestWireHelperConcurrentObserverIdentitiesDoNotCollide(t *testing.T) {
	a, b := localPair(t, Options{})
	var mu sync.Mutex
	active := map[string]bool{}
	ids := map[string]bool{}
	collision, unmatched := false, false
	ended := make(chan struct{}, 4)
	observer := localTestObserver(func(event ObserverEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch event := event.(type) {
		case RequestStarted:
			if _, exists := active[event.ID]; exists {
				collision = true
			}
			active[event.ID] = event.Incoming
			ids[event.ID] = true
		case RequestEnded:
			if _, exists := active[event.ID]; !exists {
				unmatched = true
			}
			delete(active, event.ID)
			ended <- struct{}{}
		}
	})
	started := make(chan struct{}, 2)
	first, second := make(chan struct{}), make(chan struct{})
	bBinding := testBinding(t, b)
	_, err := RegisterWire(bBinding, []string{"hold"}, WireHandlers{Observer: observer, Request: func(_ context.Context, params json.RawMessage) (any, error) {
		started <- struct{}{}
		if string(params) == "1" {
			<-first
		} else {
			<-second
		}
		return params, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(value int) <-chan error {
		done := make(chan error, 1)
		go func() {
			var result int
			err := CallWire(context.Background(), a, []string{"hold"}, value, &result, WireCallOptions{Observer: observer})
			if err == nil && result != value {
				err = errors.New("wrong result")
			}
			done <- err
		}()
		return done
	}
	one, two := call(1), call(2)
	<-started
	<-started
	mu.Lock()
	count := len(active)
	mu.Unlock()
	close(second)
	if err := <-two; err != nil {
		t.Error(err)
	}
	close(first)
	if err := <-one; err != nil {
		t.Error(err)
	}
	// Returning the reply can wake CallWire before the incoming observation.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for range 4 {
		select {
		case <-ended:
		case <-deadline.C:
			t.Fatal("missing request completion observation")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if collision || unmatched || count != 4 || len(active) != 0 || len(ids) != 4 {
		t.Fatalf("observer correlation collision=%v unmatched=%v active=%d count=%d ids=%v", collision, unmatched, len(active), count, ids)
	}
	for id := range ids {
		if !strings.HasPrefix(id, "wire:") {
			t.Errorf("model observation has physical-looking id %q", id)
		}
	}
}

func TestWireHelperObservesSelectedBoundedResponseRefusal(t *testing.T) {
	for _, kind := range []string{"oversized result", "oversized public refusal", "unencodable result"} {
		t.Run(kind, func(t *testing.T) {
			a, b := localPair(t, Options{MaxFrameBytes: 512})
			bBinding := testBinding(t, b)
			model := &wireObservations{ended: make(chan struct{}, 1)}
			_, err := RegisterWire(bBinding, []string{"response"}, WireHandlers{Observer: model, Family: "probe", Request: func(context.Context, json.RawMessage) (any, error) {
				switch kind {
				case "oversized result":
					return strings.Repeat("x", 2048), nil
				case "oversized public refusal":
					return nil, &PublicError{Code: "denied", Message: "Refused", Data: json.RawMessage(`"` + strings.Repeat("x", 2048) + `"`)}
				default:
					return func() {}, nil
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			var public *PublicError
			if err := CallWire(context.Background(), a, []string{"response"}, nil, nil); !errors.As(err, &public) || public.Code != "internal" {
				t.Fatalf("reply %v", err)
			}
			<-model.ended
			for _, event := range model.snapshot() {
				if event, ok := event.(RequestEnded); ok {
					if event.Outcome != OutcomeErrored || event.ErrorCode != "internal" {
						t.Fatalf("observed reply %#v", event)
					}
					return
				}
			}
			t.Fatal("missing completion")
		})
	}
}
