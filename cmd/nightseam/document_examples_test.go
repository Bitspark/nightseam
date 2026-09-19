package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

//go:generate go test . -short -run ^TestDocumentExamplesTable$ -update

type documentExample struct {
	Family      string                        `json:"family"`
	Path        string                        `json:"path"`
	Expression  model.TypeExpr                `json:"expression,omitempty"`
	Value       json.RawMessage               `json:"value,omitempty"`
	Bindings    map[string]doc.ExampleBinding `json:"bindings,omitempty"`
	Unavailable *doc.ExampleUnavailable       `json:"unavailable,omitempty"`
	To          string                        `json:"to,omitempty"`
	Member      string                        `json:"member,omitempty"`
}

type documentExamples struct {
	Name    string                     `json:"name"`
	Schemas map[string]json.RawMessage `json:"schemas"`
	Rows    []documentExample          `json:"rows"`
}

func TestDocumentExamplesTable(t *testing.T) {
	// These declarations have no concrete live family in their visible
	// dependencies. Every other corpus entry, including every proof form,
	// must remain concrete; a synthesis regression cannot hide as a limit.
	expectedUnavailable := map[string]bool{
		"families/relay/types/Carried":                 false,
		"families/relay/server/methods/relay/request":  false,
		"families/relay/server/methods/relay/response": false,
	}
	var table []documentExamples
	for _, corpus := range []string{"corpus", "families"} {
		world, diagnostics := load.Checkout(os.DirFS("testdata/"+corpus), "api/contracts", []string{"go", "typescript", "markdown"})
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		resolved := analysis.World(world.Families)
		facts := map[string]*render.Family{}
		var rendered render.World
		var collect func(*render.Family)
		collect = func(f *render.Family) {
			if f == nil || facts[f.Name] != nil {
				return
			}
			facts[f.Name] = f
			for _, name := range append(append([]string{}, f.References...), f.Carries...) {
				collect(f.ReferencedFamily(name))
			}
		}
		for _, name := range world.Names {
			f := render.Build(analysis.Resolve(resolved, name))
			rendered.Families = append(rendered.Families, f)
			collect(f)
		}
		out := documentExamples{Name: corpus, Schemas: map[string]json.RawMessage{}}
		for name, f := range facts {
			out.Schemas[name] = json.RawMessage(f.Wire)
		}
		for _, f := range doc.BuildCheckout(&rendered, nil).Families {
			add := func(path string, expression model.TypeExpr, value json.RawMessage, info doc.ExampleInfo, to, member string) {
				if (value == nil) == (info.ExampleUnavailable == nil) {
					t.Fatalf("%s/%s/%s: exactly one of value or unavailability is required", corpus, f.Name, path)
				}
				if info.ExampleUnavailable != nil {
					key := corpus + "/" + f.Name + "/" + path
					if _, expected := expectedUnavailable[key]; !expected {
						t.Errorf("unexpected unavailable example %s: %s", key, info.ExampleUnavailable.Reason)
					}
					expectedUnavailable[key] = true
				}
				out.Rows = append(out.Rows, documentExample{Family: f.Name, Path: path, Expression: expression, Value: value, Bindings: info.ExampleBindings, Unavailable: info.ExampleUnavailable, To: to, Member: member})
			}
			for _, typ := range append(append([]*doc.Type{}, f.Types...), f.Carried...) {
				expr := doc.ExampleExpression(facts[f.Name].Type(typ.Name))
				add("types/"+typ.Name, expr, typ.Example, typ.ExampleInfo, "", "")
				for _, v := range typ.Variants {
					add("types/"+typ.Name+"/variants/"+v.Tag, expr, v.Example, v.ExampleInfo, "", "")
				}
			}
			for _, side := range []struct {
				name, other string
				value       doc.Side
			}{{"server", "client", f.Server}, {"client", "server", f.Client}} {
				for _, m := range side.value.Methods {
					path := side.name + "/methods/" + m.Name
					add(path+"/request", m.Request, m.Frames.Request, m.ExampleInfo, side.name, "params")
					add(path+"/response", m.Result, m.Frames.Response, m.ExampleInfo, side.other, "result")
					for i, code := range m.Errors {
						var frame json.RawMessage
						if m.ExampleUnavailable == nil {
							frame = m.Frames.Refusals[i].Frame
						}
						add(path+"/refusals/"+code, nil, frame, m.ExampleInfo, side.other, "")
					}
				}
				for _, e := range side.value.Events {
					add(side.name+"/events/"+e.Name, e.Type, e.Frame, e.ExampleInfo, side.other, "data")
				}
			}
		}
		table = append(table, out)
	}
	for path, seen := range expectedUnavailable {
		if !seen {
			t.Errorf("%s is now concrete; remove its unavailability expectation", path)
		}
	}
	encoded, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	path := "../../conformance/tables/examples.json"
	if *update {
		if err := os.WriteFile(path, encoded, 0644); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, encoded) {
		t.Fatal("examples.json differs from the corpus documents; run go generate ./cmd/nightseam")
	}
}
