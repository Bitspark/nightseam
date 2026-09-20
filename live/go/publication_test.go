package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func TestRefusedInvocationUnwindsUnpublishedArguments(t *testing.T) {
	for _, mode := range []string{"local-released", "remote-released", "local-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			seen := &recorder{}
			p := overObserved(t, live.Options{MaxExports: 3}, seen)
			defer p.Close()
			outgoing := p.A.Owner().Child()
			holder := p.A.Owner().Child()
			defer outgoing.Release()
			defer holder.Release()
			target := holder
			if mode == "remote-released" {
				target = p.B.Owner().Child()
				defer target.Release()
			}
			var dispatched atomic.Int32
			ref, err := target.Export("test/Target", func(context.Context, json.RawMessage) (json.RawMessage, error) {
				dispatched.Add(1)
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "remote-released" {
				raw, _ := json.Marshal(ref)
				ref, err = p.A.Decode(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			invoke, err := holder.Import(ref, "test/Target")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "local-cancelled" {
				cancel()
			} else {
				_ = holder.Release()
			}
			echo := func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil }
			prior, err := outgoing.Export("test/Call", echo)
			if err != nil {
				t.Fatal(err)
			}
			for range 12 {
				_, err := outgoing.PublishValue(func(batch *live.Owner) (json.RawMessage, error) {
					ref, err := batch.Export("test/Call", echo)
					if err != nil {
						return nil, err
					}
					return json.Marshal(ref)
				}, func(raw json.RawMessage) (json.RawMessage, error) { return invoke(ctx, raw) })
				var proof *runtime.UnpublishedError
				if !errors.As(err, &proof) {
					t.Fatalf("pre-dispatch refusal carries no proof: %v", err)
				}
				if outgoing.Counts() != (live.Counts{Exports: 1}) {
					t.Fatalf("refusal retained fresh arguments: %+v", outgoing.Counts())
				}
			}
			if dispatched.Load() != 0 {
				t.Fatal("refused invocation dispatched")
			}
			for _, event := range seen.events() {
				if e, ok := event.(runtime.FrameSent); ok && e.Kind == "request" {
					t.Fatal("refused invocation sent a frame")
				}
			}
			kept, err := outgoing.Import(prior, "test/Call")
			if err != nil {
				t.Fatal(err)
			}
			if raw, err := kept(context.Background(), json.RawMessage(`49`)); err != nil || string(raw) != "49" {
				t.Fatalf("prior export unusable: %s, %v", raw, err)
			}
		})
	}
}

func TestPublishValueOnlyUnwindsItsFreshUnsentExports(t *testing.T) {
	seen := &recorder{}
	p := overObserved(t, live.Options{MaxExports: 2}, seen)
	defer p.Close()
	owner := p.A.Owner().Child()
	defer owner.Release()
	echo := func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil }
	prior, err := owner.Export("test/Call", echo)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 12 {
		_, err := owner.PublishValue(func(batch *live.Owner) (json.RawMessage, error) {
			ref, err := batch.Export("test/Call", echo)
			if err != nil {
				return nil, err
			}
			return json.Marshal(ref)
		}, func(raw json.RawMessage) (json.RawMessage, error) {
			return nil, p.A.Peer().Call(ctx, "unsent", raw, nil)
		})
		var proof *runtime.UnpublishedError
		if !errors.As(err, &proof) {
			t.Fatalf("missing proof: %v", err)
		}
		if owner.Counts() != (live.Counts{Exports: 1}) {
			t.Fatalf("publication leaked or reclaimed prior export: %+v", owner.Counts())
		}
	}
	for _, event := range seen.events() {
		if e, ok := event.(runtime.EventEmitted); ok && e.Name == live.ReleaseEvent {
			t.Fatal("unsent rollback emitted live.release")
		}
	}
	invoke, err := owner.Import(prior, "test/Call")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := invoke(context.Background(), json.RawMessage(`47`)); err != nil || string(got) != "47" {
		t.Fatalf("prior export changed: %s, %v", got, err)
	}
}

func TestDispatchedReleaseRefusalRetainsArguments(t *testing.T) {
	for _, remote := range []bool{false, true} {
		p := over(t, live.Options{})
		outgoing, holder := p.A.Owner().Child(), p.A.Owner().Child()
		target := holder
		if remote {
			target = p.B.Owner().Child()
		}
		retained := make(chan live.Invoke, 1)
		ref, err := target.Export("test/Target", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			ref, err := target.Scope().Decode(raw)
			if err != nil {
				return nil, err
			}
			alias, err := target.Import(ref, "test/Call")
			if err != nil {
				return nil, err
			}
			retained <- alias
			return nil, &runtime.PublicError{Code: live.ErrorReferenceReleased, Message: "implementation refused after retention"}
		})
		if err != nil {
			t.Fatal(err)
		}
		if remote {
			raw, _ := json.Marshal(ref)
			ref, err = p.A.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
		}
		invoke, err := holder.Import(ref, "test/Target")
		if err != nil {
			t.Fatal(err)
		}
		_, err = outgoing.PublishValue(func(batch *live.Owner) (json.RawMessage, error) {
			ref, err := batch.Export("test/Call", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
			if err != nil {
				return nil, err
			}
			return json.Marshal(ref)
		}, func(raw json.RawMessage) (json.RawMessage, error) { return invoke(context.Background(), raw) })
		var proof *runtime.UnpublishedError
		var public *runtime.PublicError
		if errors.As(err, &proof) || !errors.As(err, &public) || public.Code != live.ErrorReferenceReleased {
			t.Fatalf("implementation refusal classified as unsent: %v", err)
		}
		if outgoing.Counts() != (live.Counts{Exports: 1}) {
			t.Fatal("dispatched request callback was reclaimed")
		}
		var alias live.Invoke
		select {
		case alias = <-retained:
		case <-time.After(5 * time.Second):
			t.Fatal("implementation never retained the callback")
		}
		if raw, err := alias(context.Background(), json.RawMessage(`51`)); err != nil || string(raw) != "51" {
			t.Fatalf("retained callback failed: %s, %v", raw, err)
		}
		_ = outgoing.Release()
		_ = holder.Release()
		_ = target.Release()
		p.Close()
	}
}

func TestPublishValueUnknownPublisherFailureRetains(t *testing.T) {
	for _, panics := range []bool{false, true} {
		p := over(t, live.Options{})
		owner := p.A.Owner().Child()
		cause := errors.New("publisher outcome unknown")
		func() {
			defer func() {
				if got := recover(); (got != nil) != panics {
					t.Fatalf("publisher panic changed: %v", got)
				}
			}()
			_, err := owner.PublishValue(func(batch *live.Owner) (json.RawMessage, error) {
				ref, err := batch.Export("test/Call", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
				if err != nil {
					return nil, err
				}
				return json.Marshal(ref)
			}, func(json.RawMessage) (json.RawMessage, error) {
				if panics {
					panic(cause)
				}
				return nil, cause
			})
			if !errors.Is(err, cause) {
				t.Fatalf("publisher cause changed: %v", err)
			}
		}()
		if owner.Counts() != (live.Counts{Exports: 1}) {
			t.Fatal("unknown publication was reclaimed")
		}
		_ = owner.Release()
		if p.A.Counts() != (live.Counts{}) {
			t.Fatal("owner release left retained export")
		}
		p.Close()
	}
}
