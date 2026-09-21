package doc

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

func TestGenericExamplesExposeTheirConcreteBindings(t *testing.T) {
	f := proof(t)
	carried := typed(f, "Carried")
	if carried.ExampleBindings["S"].Family != "probe" || carried.ExampleBindings["Item"].Type != "string" {
		t.Fatalf("bindings: %#v", carried.ExampleBindings)
	}
	if carried.ExampleUnavailable != nil || !strings.Contains(string(carried.Example), `"message":{`) {
		t.Fatalf("example: %s (%v)", carried.Example, carried.ExampleUnavailable)
	}
	page := typed(f, "Page")
	if page.ExampleBindings["T"].Type != "string" || page.ExampleUnavailable != nil {
		t.Fatalf("generic type example: %#v", page)
	}
}

func TestUnavailableExamplesAreAccountedForWithoutInventedJSON(t *testing.T) {
	w := analysis.World(modeltest.World(map[string]map[string]string{"x": {"protocol.json": `{
		"profile":"nightseam.duplex/1","types":{
			"Impossible":{"kind":"record","fields":[{"name":"text","type":"string","pattern":"a^"}]},
			"Large":{"kind":"record","fields":[{"name":"items","type":{"array":"string"},"length":{"min":1000000000}}]},
			"Optional":{"kind":"record","fields":[{"name":"bad","type":"Impossible","required":false}]},
			"Recursive":{"kind":"record","fields":[{"name":"next","type":{"nullable":"Recursive"}}]}
		},"server":{"methods":{"echo":{"request":"Impossible","result":"string"}},"events":{"bad":{"type":"Impossible"}}}}`}}))
	facts := render.Build(analysis.Resolve(w, "x"))
	f := Build(facts, nil)
	for _, name := range []string{"Impossible", "Large"} {
		typ := typed(f, name)
		if typ.Example != nil || typ.ExampleUnavailable == nil || typ.ExampleUnavailable.Kind != "limit" || typ.ExampleUnavailable.Reason == "" {
			t.Fatalf("%s: value %s, reason %#v", name, typ.Example, typ.ExampleUnavailable)
		}
	}
	for _, name := range []string{"Optional", "Recursive"} {
		typ := typed(f, name)
		if typ.ExampleUnavailable != nil {
			t.Fatalf("%s: %v", name, typ.ExampleUnavailable)
		}
		if err := runtime.MustSchema(facts.Wire, facts.WireDigest, nil).ValidateRaw(name, typ.Example); err != nil {
			t.Fatal(err)
		}
	}
	m := f.Server.Methods[0]
	if m.ExampleUnavailable == nil || m.Frames.Request != nil || m.Frames.Response != nil || len(m.Frames.Refusals) != 0 {
		t.Fatalf("unavailable exchange: %#v", m)
	}
	e := f.Server.Events[0]
	if e.ExampleUnavailable == nil || e.Frame != nil {
		t.Fatalf("unavailable event: %#v", e)
	}
}
