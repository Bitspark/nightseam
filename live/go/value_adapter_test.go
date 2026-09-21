package live_test

import (
	"testing"

	"github.com/Bitspark/nightseam/live/go"
)

func TestJSONValueAdapterNeedsNoLiveOwner(t *testing.T) {
	adapter := live.JSONAdapter[string]()
	if adapter.Live || adapter.Binding.Schema == nil {
		t.Fatal("JSON adapter lost its data binding")
	}
	p := over(t, live.Options{})
	defer p.Close()
	owner := p.A.Owner().Child()
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*live.Owner{nil, owner} {
		raw, err := adapter.Export(owner, "held")
		if err != nil {
			t.Fatal(err)
		}
		value, err := adapter.Import(owner, raw)
		if err != nil || value != "held" {
			t.Fatalf("value %q: %v", value, err)
		}
		if _, err := adapter.Import(owner, []byte("42")); err == nil {
			t.Fatal("binding validation bypassed")
		}
		if _, err := adapter.Export(owner, string([]byte{255})); err == nil {
			t.Fatal("invalid Unicode exported")
		}
	}
	if p.A.Counts() != (live.Counts{}) {
		t.Fatal("scalar conversion allocated a binding")
	}
}
