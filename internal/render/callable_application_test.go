package render

import (
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestCallableApplicationKeepsOriginAndSubstitutesSignature(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"source": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"},{"name":"S","of":"live"}]`),
			"live.json":     `{"types":{"Captured":{"kind":"callable","request":"Item","result":"S.Job"}}}`,
		},
		"consumer": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"V"},{"name":"R","of":"live"}]`),
			"live.json":     `{"imports":["source"]}`,
		},
	}))
	family := Build(analysis.Resolve(world, "consumer"))
	scope := []model.Parameter{{Name: "V"}, {Name: "R", Of: "live"}}
	application := model.Apply{Family: "source", Name: "Captured", With: map[string]model.Filler{
		"Item": {Type: model.Array{Elem: model.Named{Name: "V"}}},
		"S":    {Type: model.Named{Name: "R"}},
	}}
	got, ok := family.Apply(application, scope)
	if !ok {
		t.Fatal("callable application was not resolved")
	}
	if got.Origin.Family != "source" || got.Origin.Declaration != "Captured" {
		t.Fatalf("application changed nominal origin: %+v", got.Origin)
	}
	if !reflect.DeepEqual(got.Request, model.Array{Elem: model.Named{Name: "V"}}) || !reflect.DeepEqual(got.Result, model.Drawn{Parameter: "R", Name: "Job"}) {
		t.Fatalf("signature did not retain caller scope: request=%#v result=%#v", got.Request, got.Result)
	}
	if want := []Use{{Parameter: "V"}, {Parameter: "R", Type: "Job"}}; !reflect.DeepEqual(got.Uses, want) {
		t.Fatalf("applied signature uses %v, want %v", got.Uses, want)
	}
	if len(got.Arguments) != 2 {
		t.Fatalf("nominal application lost arguments: %+v", got.Arguments)
	}
}

func TestCallableAliasViewKeepsPublicNameAndOriginalApplication(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"functions": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json":     `{"types":{"Function":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"}}}`,
		},
		"payload": {"model.json": `{"nightseam":2,"types":{"Input":{"kind":"record","fields":[]}}}`},
		"consumer": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json": `{"imports":["functions","payload"],"types":{
				"Closed":{"kind":"alias","type":{"apply":"functions.Function","with":{"A":"payload.Input","B":{"array":"integer"}}}},
				"Twice":{"kind":"alias","type":"Closed"},
				"Generic":{"kind":"alias","parameters":[{"name":"X"}],"type":{"apply":"functions.Function","with":{"A":"X","B":"string"}}}
			}}`,
		},
	}))
	family := Build(analysis.Resolve(world, "consumer"))
	for _, name := range []string{"Closed", "Twice", "Generic"} {
		original := family.Type(name)
		view, ok := family.CallableView(original)
		if !ok {
			t.Fatalf("%s was not resolved as callable", name)
		}
		if view.Name != name || view.Declaration != original.Declaration || view.Kind != model.KindCallable || view.Origin.Family != "functions" || view.Origin.Declaration != "Function" || len(view.Arguments) != 2 {
			t.Fatalf("alias metadata lost public declaration or nominal application: %+v", view)
		}
		if !reflect.DeepEqual(view.Uses, original.Uses) || !reflect.DeepEqual(view.Parameters, original.Parameters) {
			t.Fatalf("alias lost its own type scope: %+v", view)
		}
		if name == "Generic" {
			if !reflect.DeepEqual(view.Request, model.Named{Name: "X"}) || !reflect.DeepEqual(view.Result, model.Primitive("string")) {
				t.Fatalf("generic alias signature: %#v -> %#v", view.Request, view.Result)
			}
		} else if !reflect.DeepEqual(view.Request, model.Imported{Family: "payload", Name: "Input"}) || !reflect.DeepEqual(view.Result, model.Array{Elem: model.Primitive("integer")}) {
			t.Fatalf("closed alias signature: %#v -> %#v", view.Request, view.Result)
		}
		if original.Kind != model.KindAlias {
			t.Fatal("view mutated source declaration")
		}
	}
}

func TestCallableAliasViewForwardsTheImplicitFamilyArgument(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"source": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"live"}]`),
			"live.json":     `{"types":{"Handler":{"kind":"callable","request":"S.Job"}}}`,
		},
		"consumer": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"R","of":"live"}]`),
			"live.json":     `{"imports":["source"],"types":{"Forwarded":{"kind":"alias","type":"source.Handler"}}}`,
		},
	}))
	family := Build(analysis.Resolve(world, "consumer"))
	view, ok := family.CallableView(family.Type("Forwarded"))
	if !ok || !reflect.DeepEqual(view.Request, model.Drawn{Parameter: "R", Name: "Job"}) {
		t.Fatalf("implicit family argument did not reach callable signature: %+v", view)
	}
}
