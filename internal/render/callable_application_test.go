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
