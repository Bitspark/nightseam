package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

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
