package runtime

import (
	"context"
	"encoding/json"
	"errors"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

var preparedIdentity = DeclarationIdentity{Path: "service", Digest: strings.Repeat("a", 64)}

type observedIdentityWire struct {
	bitwire.Endpoint
	observed chan bitwire.ProfileKind
}

func (w observedIdentityWire) Receive(receiver bitwire.Receiver) (func(), error) {
	original := receiver.Message
	receiver.Message = func(path []string, message bitwire.Message) {
		w.observed <- message.Frame.Kind
		original(path, message)
	}
	return w.Endpoint.Receive(receiver)
}
func identityWait(t *testing.T, signal <-chan bitwire.ProfileKind, kind bitwire.ProfileKind) {
	t.Helper()
	select {
	case actual := <-signal:
		if actual != kind {
			t.Fatalf("delivery %s, want %s", actual, kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not arrive")
	}
}
func installWireIdentity(t *testing.T, wire bitwire.Endpoint, identity DeclarationIdentity) {
	t.Helper()
	handler, err := IdentityHandler(identity)
	if err != nil {
		t.Fatal(err)
	}
	_, err = HandleWire(testBinding(t, wire), []string{IdentityMethod}, func(ctx context.Context, raw json.RawMessage) (any, error) { return handler(ctx, nil, raw) })
	if err != nil {
		t.Fatal(err)
	}
}
func requireIdentityCode(t *testing.T, err error, code string) {
	t.Helper()
	var public *PublicError
	if !errors.As(err, &public) || public.Code != code {
		t.Fatalf("error %v, want %s", err, code)
	}
}

func TestIdentityPreparationHoldsFirstEventThroughCheckAndBinding(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "absent"}[absent], func(t *testing.T) {
			near, far := localPair(t, Options{})
			if !absent {
				installWireIdentity(t, far, preparedIdentity)
			}
			observed := make(chan bitwire.ProfileKind, 8)
			p, err := PrepareIdentity(observedIdentityWire{near, observed}, preparedIdentity, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			delivered := make(chan int, 2)
			_, err = RegisterWire(p.Wire(), []string{"event"}, WireHandlers{Event: func(_ context.Context, raw json.RawMessage) error {
				var value int
				_ = json.Unmarshal(raw, &value)
				delivered <- value
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err = EmitWire(context.Background(), far, []string{"event"}, 1); err != nil {
				t.Fatal(err)
			}
			identityWait(t, observed, bitwire.ProfileEvent)
			// The local root's event delivery is held. Identity's response returns
			// through its original return address, never through that event FIFO.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err = p.Check(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-delivered:
				t.Fatal("model ran before binding")
			default:
			}
			if err = p.Ready(); err != nil {
				t.Fatal(err)
			}
			select {
			case value := <-delivered:
				if value != 1 {
					t.Fatal(value)
				}
			case <-ctx.Done():
				t.Fatal("first event lost")
			}
		})
	}
}

func TestIdentityPreparationMismatchDiscardsEventsAndPreservesSharedCarrier(t *testing.T) {
	near, far := localPair(t, Options{})
	installWireIdentity(t, far, DeclarationIdentity{Path: "service", Digest: strings.Repeat("b", 64)})
	root := testBinding(t, near)
	model := root.Select(nil)
	observed := make(chan bitwire.ProfileKind, 8)
	p, err := PrepareIdentity(observedIdentityWire{model, observed}, preparedIdentity, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var effects atomic.Int32
	_, _ = RegisterWire(p.Wire(), []string{"event"}, WireHandlers{Event: func(context.Context, json.RawMessage) error { effects.Add(1); return nil }})
	_, _ = HandleWire(root, []string{"unrelated"}, func(context.Context, json.RawMessage) (any, error) { return 7, nil })
	_ = EmitWire(context.Background(), far, []string{"event"}, 1)
	identityWait(t, observed, bitwire.ProfileEvent)
	requireIdentityCode(t, p.Check(context.Background()), "contract_mismatch")
	requireIdentityCode(t, p.Ready(), "contract_mismatch")
	var result int
	if err = CallWire(context.Background(), far, []string{"unrelated"}, nil, &result); err != nil || result != 7 {
		t.Fatalf("shared carrier: %d, %v", result, err)
	}
	if effects.Load() != 0 {
		t.Fatal("mismatched model dispatched")
	}
	// All owned registrations were detached, permitting another interpreter.
	if _, err = RegisterWire(root, []string{"event"}, WireHandlers{Event: func(context.Context, json.RawMessage) error { return nil }}); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityPreparationDefersRequestsWithoutBlockingRootAndPreservesCancel(t *testing.T) {
	near, far := localPair(t, Options{})
	installWireIdentity(t, far, preparedIdentity)
	root := testBinding(t, near)
	model := root.Select(nil)
	observed := make(chan bitwire.ProfileKind, 8)
	p, err := PrepareIdentity(observedIdentityWire{model, observed}, preparedIdentity, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var effects atomic.Int32
	_, _ = HandleWire(p.Wire(), []string{"model"}, func(context.Context, json.RawMessage) (any, error) { effects.Add(1); return 11, nil })
	_, _ = HandleWire(root, []string{"unrelated"}, func(context.Context, json.RawMessage) (any, error) { return 7, nil })
	ctx, cancel := context.WithCancel(context.Background())
	answer := make(chan error, 1)
	go func() { answer <- CallWire(ctx, far, []string{"model"}, nil, nil) }()
	identityWait(t, observed, bitwire.ProfileRequest)
	var result int
	if err = CallWire(context.Background(), far, []string{"unrelated"}, nil, &result); err != nil || result != 7 {
		t.Fatalf("root blocked: %d, %v", result, err)
	}
	cancel()
	if !errors.Is(<-answer, context.Canceled) {
		t.Fatal("request did not cancel")
	}
	identityWait(t, observed, bitwire.ProfileCancel)
	if err = p.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = p.Ready(); err != nil {
		t.Fatal(err)
	}
	if err = CallWire(context.Background(), far, []string{"model"}, nil, &result); err != nil || result != 11 {
		t.Fatalf("ready model: %d, %v", result, err)
	}
	if effects.Load() != 1 {
		t.Fatalf("cancelled request dispatched: %d", effects.Load())
	}
}

func TestIdentityPreparationBoundsUnstartedAndUnboundFactories(t *testing.T) {
	for _, check := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstarted", true: "unbound"}[check], func(t *testing.T) {
			near, far := localPair(t, Options{})
			installWireIdentity(t, far, preparedIdentity)
			p, err := PrepareIdentity(near, preparedIdentity, Options{RequestTimeout: 250 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			_, _ = HandleWire(p.Wire(), []string{"model"}, func(context.Context, json.RawMessage) (any, error) {
				t.Error("expired model dispatched")
				return nil, nil
			})
			if check {
				if err = p.Check(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-p.released:
			case <-time.After(5 * time.Second):
				t.Fatal("preparation never expired")
			}
			if !errors.Is(p.Ready(), context.DeadlineExceeded) {
				t.Fatalf("expiry: %v", p.Ready())
			}
			binding := testBinding(t, near)
			if _, err = HandleWire(binding, []string{"model"}, func(context.Context, json.RawMessage) (any, error) { return nil, nil }); err != nil {
				t.Fatal(err)
			}
			_ = binding.Close(duplex.CodeNormal, "rebind")
			replacement, err := PrepareIdentity(near, preparedIdentity, Options{})
			if err != nil {
				t.Fatalf("identity responder leaked: %v", err)
			}
			replacement.Close()
		})
	}
}

func TestIdentityPreparationMismatchRefusesDeferredRequest(t *testing.T) {
	near, far := localPair(t, Options{})
	installWireIdentity(t, far, DeclarationIdentity{Path: "service", Digest: strings.Repeat("b", 64)})
	observed := make(chan bitwire.ProfileKind, 8)
	p, err := PrepareIdentity(observedIdentityWire{near, observed}, preparedIdentity, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, _ = HandleWire(p.Wire(), []string{"model"}, func(context.Context, json.RawMessage) (any, error) {
		t.Error("mismatched request dispatched")
		return nil, nil
	})
	answer := make(chan error, 1)
	go func() { answer <- CallWire(context.Background(), far, []string{"model"}, nil, nil) }()
	identityWait(t, observed, bitwire.ProfileRequest)
	requireIdentityCode(t, p.Check(context.Background()), "contract_mismatch")
	requireIdentityCode(t, <-answer, "contract_mismatch")
}

func TestIdentityPreparationCancellationAndCarrierCloseReleaseDispatch(t *testing.T) {
	for _, mode := range []string{"cancel", "carrier", "close"} {
		t.Run(mode, func(t *testing.T) {
			near, far := localPair(t, Options{})
			p, err := PrepareIdentity(near, preparedIdentity, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			_, _ = HandleWire(p.Wire(), []string{"model"}, func(context.Context, json.RawMessage) (any, error) {
				t.Error("abandoned model dispatched")
				return nil, nil
			})
			switch mode {
			case "cancel":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if !errors.Is(p.Check(ctx), context.Canceled) {
					t.Fatal("check ignored cancellation")
				}
			case "carrier":
				_ = far.Close(duplex.CodeNormal, "")
			case "close":
				p.Close()
			}
			select {
			case <-p.released:
			case <-time.After(5 * time.Second):
				t.Fatal("preparation remained blocked")
			}
			if p.Ready() == nil {
				t.Fatal("abandoned preparation became ready")
			}
		})
	}
}

func TestIdentityPreparationBoundsDeferredRequests(t *testing.T) {
	near, far := localPair(t, Options{MaxConcurrentHandlers: 8})
	installWireIdentity(t, far, preparedIdentity)
	observed := make(chan bitwire.ProfileKind, 8)
	p, err := PrepareIdentity(observedIdentityWire{near, observed}, preparedIdentity, Options{MaxConcurrentHandlers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, _ = HandleWire(p.Wire(), []string{"model"}, func(context.Context, json.RawMessage) (any, error) { return 1, nil })
	first := make(chan error, 1)
	go func() { first <- CallWire(context.Background(), far, []string{"model"}, nil, nil) }()
	identityWait(t, observed, bitwire.ProfileRequest)
	requireIdentityCode(t, CallWire(context.Background(), far, []string{"model"}, nil, nil), "busy")
	if err = p.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = p.Ready(); err != nil {
		t.Fatal(err)
	}
	if err = <-first; err != nil {
		t.Fatal(err)
	}
}
