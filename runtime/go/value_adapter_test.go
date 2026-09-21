package runtime_test

import (
	"context"
	"testing"

	"github.com/Bitspark/nightseam/runtime/go"
)

func TestJSONValueAdapterValidatesWithoutEnvironment(t *testing.T) {
	adapter := runtime.JSONAdapter[string]()
	if adapter.NeedsContext || adapter.Binding.Schema == nil {
		t.Fatal("ordinary data lost its declaration or acquired a context requirement")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		raw, err := adapter.Export(ctx, "held")
		if err != nil {
			t.Fatal(err)
		}
		value, err := adapter.Import(ctx, raw)
		if err != nil || value != "held" {
			t.Fatalf("round trip: %q, %v", value, err)
		}
		if _, err := adapter.Import(ctx, []byte("42")); err == nil {
			t.Fatal("import bypassed declaration validation")
		}
		if _, err := adapter.Export(ctx, string([]byte{255})); err == nil {
			t.Fatal("export accepted invalid Unicode")
		}
	}
}
