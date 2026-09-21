package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/live/go/livetest"
	"github.com/Bitspark/nightseam/runtime/go"
)

// This is the construction, not an alternate public live API. The descriptor
// and active owner enter Import before the invocation is exposed as a Wire.
// A path alone does not replace that checked interpretation or its lifetime.
func checkedBindingWire(t *testing.T, owner *live.Owner, ref live.Reference, contract string) (duplex.Wire, error) {
	t.Helper()
	invoke, err := owner.Import(ref, contract, "")
	if err != nil {
		return nil, err
	}
	var descriptor struct {
		Binding string `json:"binding"`
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		t.Fatal(err)
	}
	access, binding, err := runtime.NewWirePair(runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = access.Close(duplex.CodeNormal, "") })
	detach, err := runtime.HandleWire(binding, []string{descriptor.Binding}, func(ctx context.Context, raw json.RawMessage) (any, error) {
		return invoke(ctx, raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(detach)
	// The nonce-containing binding is one opaque path component.
	return duplex.At(access, []string{descriptor.Binding}), nil
}

func socketBindingScopes(t *testing.T) livetest.Pair {
	t.Helper()
	ready := make(chan *live.Scope, 1)
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Options: runtime.Options{Prepare: func(p *runtime.Peer) error {
			s, err := live.Over(p, live.Options{})
			if err == nil {
				ready <- s
			}
			return err
		}},
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	var a *live.Scope
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{
		Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) { a, err = live.Over(p, live.Options{}); return err }},
	})
	if err != nil {
		server.Close()
		cancel()
		t.Fatal(err)
	}
	b := <-ready
	return livetest.Pair{A: a, B: b, Close: func() { _ = peer.Close(); _ = b.Peer().Close(); cancel(); server.Close() }}
}

func requireWireCode(t *testing.T, err error, code string) {
	t.Helper()
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestLiveBindingWireConstructionRetainsGuardsAndReleaseBarrier(t *testing.T) {
	for _, carrier := range []string{"local", "socket"} {
		t.Run(carrier, func(t *testing.T) {
			p := over(t, live.Options{})
			if carrier == "socket" {
				p.Close()
				p = socketBindingScopes(t)
			}
			defer p.Close()
			holder := p.A.Owner().Child()
			target := p.A.Owner().Child()
			if carrier == "socket" {
				target = p.B.Owner().Child()
			}
			var guards, effects atomic.Int32
			var permitted atomic.Bool
			entered, finish := make(chan struct{}), make(chan struct{})
			ref, err := target.Export("test/Guarded", "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
				guards.Add(1)
				if !permitted.Load() {
					return nil, &runtime.PublicError{Code: "denied", Message: "guard denied"}
				}
				effects.Add(1)
				close(entered)
				<-finish
				return raw, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if carrier == "socket" {
				raw, _ := json.Marshal(ref)
				ref, err = p.A.Decode(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			wire, err := checkedBindingWire(t, holder, ref, "test/Guarded")
			if err != nil {
				t.Fatal(err)
			}
			wire = duplex.At(duplex.Mount(map[string]duplex.Wire{"outer": duplex.Mount(map[string]duplex.Wire{"binding": wire})}), []string{"outer", "binding"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requireWireCode(t, runtime.CallWire(ctx, wire, nil, 1, nil), "denied")
			if effects.Load() != 0 {
				t.Fatal("wire bypassed the explicit guard")
			}
			permitted.Store(true)
			done := make(chan error, 1)
			go func() {
				var value int
				err := runtime.CallWire(ctx, wire, nil, 42, &value)
				if err == nil && value != 42 {
					err = errors.New("wrong result")
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("binding never entered")
			}
			if target.Counts() != (live.Counts{Exports: 1}) {
				t.Fatal("target allocation was not owned")
			}
			wantHolder := live.Counts{}
			if carrier == "socket" {
				wantHolder.Imports = 1
			}
			if holder.Counts() != wantHolder {
				t.Fatalf("holder allocations: %+v", holder.Counts())
			}
			if carrier == "socket" {
				_ = holder.Release()
			} else {
				_ = target.Release()
			}
			requireWireCode(t, runtime.CallWire(ctx, wire, nil, 43, nil), live.ErrorReferenceReleased)
			close(finish)
			if err := <-done; err != nil {
				t.Fatalf("release cancelled an admitted invocation: %v", err)
			}
			if guards.Load() != 2 || effects.Load() != 1 {
				t.Fatalf("guards=%d effects=%d", guards.Load(), effects.Load())
			}
			_ = holder.Release()
			_ = target.Release()
			if p.A.Counts() != (live.Counts{}) || p.B.Counts() != (live.Counts{}) {
				t.Fatalf("counts before teardown: %+v %+v", p.A.Counts(), p.B.Counts())
			}
		})
	}
}

func TestLiveBindingWireConstructionChecksInterpretationAndNonce(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	var effects atomic.Int32
	ref, err := p.B.Owner().Export("test/Checked", "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { effects.Add(1); return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	holder := p.A.Owner().Child()
	_, err = checkedBindingWire(t, holder, ref, "test/Checked")
	requireWireCode(t, err, live.ErrorReferenceForeign)
	raw, _ := json.Marshal(ref)
	arrived, err := p.A.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = checkedBindingWire(t, holder, arrived, "test/Other")
	requireWireCode(t, err, live.ErrorContractMismatch)
	if holder.Counts() != (live.Counts{}) || effects.Load() != 0 {
		t.Fatal("invalid interpretation allocated or dispatched")
	}
	unknown, err := p.A.Decode(json.RawMessage(`{"binding":"0000000000000000.1","contract":"test/Checked"}`))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := checkedBindingWire(t, holder, unknown, "test/Checked")
	if err != nil {
		t.Fatal(err)
	}
	requireWireCode(t, runtime.CallWire(context.Background(), wire, nil, 1, nil), live.ErrorReferenceUnknown)
	if effects.Load() != 0 || holder.Counts() != (live.Counts{Imports: 1}) {
		t.Fatal("unknown nonce reached a binding or changed attachment accounting")
	}
	_ = holder.Release()
	_ = p.B.Owner().Release()
	if p.A.Counts() != (live.Counts{}) || p.B.Counts() != (live.Counts{}) {
		t.Fatal("counts not zero before teardown")
	}
}

func TestLiveBindingWireConstructionRetainsUncertainPublication(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	target, outgoing := p.A.Owner().Child(), p.A.Owner().Child()
	retained := make(chan live.Invoke, 1)
	ref, err := target.Export("test/Retain", "", func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		arrived, err := p.A.Decode(raw)
		if err != nil {
			return nil, err
		}
		invoke, err := target.Import(arrived, "test/Callback", "")
		if err != nil {
			return nil, err
		}
		retained <- invoke
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := checkedBindingWire(t, p.A.Owner(), ref, "test/Retain")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := outgoing.PublishValue(func(batch *live.Owner) (json.RawMessage, error) {
			callback, err := batch.Export("test/Callback", "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
				callbacks.Add(1)
				return raw, nil
			})
			if err != nil {
				return nil, err
			}
			return json.Marshal(callback)
		}, func(raw json.RawMessage) (json.RawMessage, error) {
			var result json.RawMessage
			err := runtime.CallWire(ctx, wire, nil, raw, &result)
			return result, err
		})
		done <- err
	}()
	var callback live.Invoke
	select {
	case callback = <-retained:
	case <-time.After(5 * time.Second):
		t.Fatal("callback was not retained")
	}
	cancel()
	err = <-done
	var proof *runtime.UnpublishedError
	if err == nil || errors.As(err, &proof) {
		t.Fatalf("dispatched cancellation claimed non-publication: %v", err)
	}
	if outgoing.Counts() != (live.Counts{Exports: 1}) {
		t.Fatal("uncertain publication reclaimed the callback")
	}
	if raw, err := callback(context.Background(), json.RawMessage(`51`)); err != nil || string(raw) != "51" || callbacks.Load() != 1 {
		t.Fatalf("retained guarded callback: %s %v calls=%d", raw, err, callbacks.Load())
	}
	_ = outgoing.Release()
	_ = target.Release()
	if p.A.Counts() != (live.Counts{}) || p.B.Counts() != (live.Counts{}) {
		t.Fatal("counts not zero before teardown")
	}
}

func TestLiveBindingWireConstructionForwardRetainsIndependentLifetime(t *testing.T) {
	origin, destination := over(t, live.Options{}), over(t, live.Options{})
	defer origin.Close()
	defer destination.Close()
	sourceOwner, sourceHolder := origin.A.Owner().Child(), origin.B.Owner().Child()
	forwardOwner, forwardHolder := destination.A.Owner().Child(), destination.B.Owner().Child()
	var calls atomic.Int32
	ref, err := sourceOwner.Export("test/Forward", "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { calls.Add(1); return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(ref)
	arrived, err := origin.B.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := checkedBindingWire(t, sourceHolder, arrived, "test/Forward")
	if err != nil {
		t.Fatal(err)
	}
	// Opaque JSON only: nested reference positions still require generated
	// conversions. Forwarding cannot discover those positions from a Wire.
	forwarded, err := forwardOwner.Export("test/Forward", "", func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var result json.RawMessage
		err := runtime.CallWire(ctx, imported, nil, raw, &result)
		return result, err
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(forwarded)
	arrived, err = destination.B.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	access, err := checkedBindingWire(t, forwardHolder, arrived, "test/Forward")
	if err != nil {
		t.Fatal(err)
	}
	var value int
	if err := runtime.CallWire(context.Background(), access, nil, 73, &value); err != nil || value != 73 || calls.Load() != 1 {
		t.Fatalf("forwarded value=%d calls=%d err=%v", value, calls.Load(), err)
	}
	if origin.A.Counts() != (live.Counts{Exports: 1}) || origin.B.Counts() != (live.Counts{Imports: 1}) || destination.A.Counts() != (live.Counts{Exports: 1}) || destination.B.Counts() != (live.Counts{Imports: 1}) {
		t.Fatal("forwarding changed allocation ownership")
	}
	_ = forwardHolder.Release()
	_ = forwardOwner.Release()
	requireWireCode(t, runtime.CallWire(context.Background(), access, nil, 74, nil), live.ErrorReferenceReleased)
	if err := runtime.CallWire(context.Background(), imported, nil, 75, &value); err != nil || value != 75 || calls.Load() != 2 {
		t.Fatalf("origin after forward release=%d calls=%d err=%v", value, calls.Load(), err)
	}
	if origin.A.Counts() != (live.Counts{Exports: 1}) || origin.B.Counts() != (live.Counts{Imports: 1}) || destination.A.Counts() != (live.Counts{}) || destination.B.Counts() != (live.Counts{}) {
		t.Fatal("forward release crossed its scope")
	}
	_ = sourceHolder.Release()
	_ = sourceOwner.Release()
	if origin.A.Counts() != (live.Counts{}) || origin.B.Counts() != (live.Counts{}) {
		t.Fatal("counts not zero before teardown")
	}
}

func TestLiveBindingWireConstructionCloseIsNotRelease(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	owner := p.A.Owner().Child()
	entered, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	ref, err := owner.Export("test/Close", "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-finish
		}
		return raw, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := checkedBindingWire(t, owner, ref, "test/Close")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.CallWire(context.Background(), wire, nil, 81, nil) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("binding never entered")
	}
	_ = wire.Close(duplex.CodeNormal, "end this exposure")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("carrier close preserved an outstanding reply")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("carrier close did not settle the outstanding call")
	}
	close(finish)
	if owner.Counts() != (live.Counts{Exports: 1}) {
		t.Fatal("wire close silently became owner release")
	}
	// The same live binding is still reachable through its intact scope. This
	// is the concrete mismatch in replacing release with a routing carrier close.
	invoke, err := owner.Import(ref, "test/Close", "")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := invoke(context.Background(), json.RawMessage(`82`)); err != nil || string(raw) != "82" || calls.Load() != 2 {
		t.Fatalf("scope binding after wire close: %s %v calls=%d", raw, err, calls.Load())
	}
	_ = owner.Release()
	if p.A.Counts() != (live.Counts{}) || p.B.Counts() != (live.Counts{}) {
		t.Fatal("counts not zero before teardown")
	}
}
