package doc

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

func TestExampleArrayLengthBelongsToTheArray(t *testing.T) {
	w := analysis.World(modeltest.World(map[string]map[string]string{"x": {"model.json": `{
		"nightseam":2,"types":{"Lists":{"kind":"record","fields":[
			{"name":"many","type":{"array":"string"},"length":{"min":3,"max":3}},
			{"name":"empty","type":{"array":"string"},"length":{"max":0}},
			{"name":"nested","type":{"array":{"array":"string"}},"length":{"min":2}}
		]}}}`}}))
	for name := range w {
		if ds := check.Family(analysis.Resolve(w, name)); len(ds) != 0 {
			t.Fatal(ds)
		}
	}
	f := render.Build(analysis.Resolve(w, "x"))
	got := typed(Build(f, nil), "Lists").Example
	want := `{"many":["‹many›","‹many›","‹many›"],"empty":[],"nested":[["‹nested›"],["‹nested›"]]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if err := runtime.MustSchema(f.Wire, nil).ValidateRaw("Lists", got); err != nil {
		t.Fatal(err)
	}
}

func TestExampleAppliedFamilyRetainsTheCallersBindings(t *testing.T) {
	w := analysis.World(modeltest.World(map[string]map[string]string{
		"item": {"protocol.json": modeltest.Protocol(""), "model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"value","type":{"literal":"bound"}}]}}}`},
		"box":  {"protocol.json": `{"profile":"nightseam.duplex/1","parameters":[{"name":"S","of":"protocol"}],"types":{"Payload":{"kind":"record","fields":[{"name":"other","type":"boolean"}]},"Box":{"kind":"record","fields":[{"name":"payload","type":"S.Payload"}]}}}`},
		"x": {"protocol.json": `{"profile":"nightseam.duplex/1","imports":["item","box"],"types":{"Payload":{"kind":"record","fields":[{"name":"caller","type":"string"}]},
			"Page":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"array":"T"}}]},
			"Bound":{"kind":"alias","type":{"apply":"Page","with":{"T":{"apply":"box.Box","with":{"S":"item"}}}}}
		}}`},
	}))
	for name := range w {
		if ds := check.Family(analysis.Resolve(w, name)); len(ds) != 0 {
			t.Fatal(ds)
		}
	}
	f := render.Build(analysis.Resolve(w, "x"))
	got := typed(Build(f, nil), "Bound").Example
	want := `{"items":[{"payload":{"value":"bound"}}]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
	schemas := map[string]*runtime.Schema{}
	for _, name := range []string{"item", "box", "x"} {
		schemas[name] = runtime.MustSchema(render.Build(analysis.Resolve(w, name)).Wire, schemas)
	}
	if err := schemas["x"].ValidateRaw("Bound", got); err != nil {
		t.Fatal(err)
	}
}
