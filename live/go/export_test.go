package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Bitspark/nightseam/live/go"
)

func TestExportValueTracksOnlyItsOwnAllocations(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	echo := func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil }
	var retained live.Reference
	var captured *live.Scope
	_, err := p.A.ExportValue(func(scope *live.Scope) (json.RawMessage, error) {
		captured = scope
		if _, err := scope.Export("test/Call", echo); err != nil {
			t.Fatal(err)
		}
		// An overlapping conversion through the root is independent, even if
		// another conversion on this connection has not finished yet.
		done := make(chan struct{})
		go func() {
			defer close(done)
			retained, _ = p.A.Export("test/Call", echo)
		}()
		<-done
		_, err := scope.ExportValue(func(nested *live.Scope) (json.RawMessage, error) {
			ref, err := nested.Export("test/Call", echo)
			if err != nil {
				return nil, err
			}
			return json.Marshal(ref)
		})
		if err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("later conversion failed")
	})
	if err == nil || p.A.Counts().Exports != 1 {
		t.Fatalf("rollback: %v, %+v", err, p.A.Counts())
	}
	invoke, err := captured.Import(retained, "test/Call")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := invoke(context.Background(), json.RawMessage(`7`)); err != nil || string(raw) != "7" {
		t.Fatalf("retained binding: %s %v", raw, err)
	}
	// Captured views cannot retain a completed batch or roll back later work.
	raw, err := captured.ExportValue(func(next *live.Scope) (json.RawMessage, error) {
		ref, err := next.Export("test/Call", echo)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ref)
	})
	if err != nil || p.A.Counts().Exports != 2 {
		t.Fatalf("fresh conversion: %v, %+v", err, p.A.Counts())
	}
	ref, err := p.A.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := captured.Release(ref); err != nil {
		t.Fatal(err)
	}
	if p.A.Counts().Exports != 1 {
		t.Fatal(p.A.Counts())
	}
	if err := captured.Close(); err != nil {
		t.Fatal(err)
	}
	if _, found := live.ScopeOf(p.A.Peer()); found {
		t.Fatal("closing a conversion view left the closed scope registered")
	}
}

func TestExportValueUnwindsInvalidJSONAndPanic(t *testing.T) {
	p := over(t, live.Options{MaxExports: 1})
	defer p.Close()
	for _, panicAfterExport := range []bool{false, true} {
		for range 3 {
			func() {
				defer func() {
					if recovered := recover(); panicAfterExport && recovered != "conversion panic" {
						t.Errorf("panic changed: %v", recovered)
					}
				}()
				_, err := p.A.ExportValue(func(scope *live.Scope) (json.RawMessage, error) {
					_, err := scope.Export("test/Call", func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
					if err != nil {
						t.Fatal(err)
					}
					if panicAfterExport {
						panic("conversion panic")
					}
					return json.RawMessage(`{`), nil
				})
				if err == nil {
					t.Error("invalid JSON succeeded")
				}
			}()
			if p.A.Counts().Exports != 0 {
				t.Fatal(p.A.Counts())
			}
		}
	}
}
