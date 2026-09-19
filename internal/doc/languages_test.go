package doc

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

type futureLanguage struct{}

func (futureLanguage) Spell(f *render.Family, e model.TypeExpr) string {
	return f.Name + ":" + model.String(e)
}
func (futureLanguage) Declare(_ *render.Family, t *model.Type) string { return "declare " + t.Kind }
func (futureLanguage) Invoke(_ *render.Family, side, op string) spi.Invocation {
	return spi.Invocation{Call: side + ":" + op, Handle: "handle " + op}
}

func TestDocumentAsksAnUnknownLanguageForEveryDeclaration(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{"x": {
		"model.json":    `{"nightseam":2,"types":{"Input":{"kind":"record","fields":[]}}}`,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"run":{"request":"Input","result":"Input"}},"events":{"changed":{"type":"Input"}}},"client":{"methods":{"ask":{"result":"Input"}},"events":{"seen":{"type":"Input"}}}`),
	}}))
	f := render.Build(analysis.Resolve(world, "x"))
	spellers := map[string]spi.Speller{"future": futureLanguage{}}
	d := Build(f, spellers)
	for _, group := range [][]*Type{d.Types, d.Carried} {
		for _, typ := range group {
			language := typ.Languages["future"]
			if language.Name != "x:"+model.String(model.Named{Name: typ.Name}) || language.Declare != "declare "+typ.Kind {
				t.Errorf("%s lost language metadata: %+v", typ.Name, language)
			}
		}
	}
	for name, side := range map[string]Side{"server": d.Server, "client": d.Client} {
		for _, method := range side.Methods {
			if got := method.Languages["future"].Invoke; got != spellers["future"].Invoke(f, name, method.Name) {
				t.Errorf("%s %s: %+v", name, method.Name, got)
			}
		}
		for _, event := range side.Events {
			if got := event.Languages["future"].Invoke; got != spellers["future"].Invoke(f, name, event.Name) {
				t.Errorf("%s %s: %+v", name, event.Name, got)
			}
		}
	}
	checkout := BuildCheckout(&render.World{Families: []*render.Family{f}}, spellers)
	if checkout.Families[0].Types[0].Languages["future"].Name == "" {
		t.Fatal("checkout dropped its spellers")
	}
}
