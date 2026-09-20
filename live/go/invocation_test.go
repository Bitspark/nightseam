package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// Moving the body off the caller's goroutine must preserve the peer's panic
// boundary and local callers' ability to recover, rather than crash the process.
func TestScopedInvocationRetainsPanicBoundary(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	reference, err := p.A.Owner().Export("probe/Panic", func(context.Context, json.RawMessage) (json.RawMessage, error) { panic("private body panic") })
	if err != nil {
		t.Fatal(err)
	}
	local, err := p.A.Owner().Import(reference, "probe/Panic")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if got := recover(); got != "private body panic" {
				t.Errorf("local panic = %v", got)
			}
		}()
		_, _ = local(context.Background(), nil)
		t.Error("local panic did not reach its caller")
	}()
	raw, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	arrived, err := p.B.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := p.B.Owner().Import(arrived, "probe/Panic")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = remote(ctx, nil)
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != "internal" || public.Message != "Internal error" {
		t.Fatalf("remote panic escaped its boundary: %v", err)
	}
}
