package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Documents cannot invent a declaration or an implementation signature:
// every displayed fragment must occur in the package or init's stub.
func TestLanguageSnippetsComeFromGeneratedCode(t *testing.T) {
	for _, corpus := range []string{"corpus/api/contracts", "families/api/contracts", "proof"} {
		world, diagnostics := load.Checkout(os.DirFS("testdata"), corpus, []string{"go", "typescript"})
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		for _, name := range world.Names {
			f := render.Build(analysis.Resolve(analysis.World(world.Families), name))
			for _, target := range compose.Targets(module, scope, "") {
				if _, language := target.(spi.Scaffolder); !language {
					continue
				}
				t.Run(corpus+"/"+name+"/"+target.Name(), func(t *testing.T) {
					s, ok := target.(spi.Speller)
					if !ok {
						t.Fatal("language target has no Speller")
					}
					document := doc.Build(f, map[string]spi.Speller{target.Name(): s})
					documentTypes := map[string]*doc.Type{}
					for _, group := range [][]*doc.Type{document.Types, document.Carried} {
						for _, typ := range group {
							documentTypes[typ.Name] = typ
						}
					}
					files, err := target.Render(f)
					if err != nil {
						t.Fatal(err)
					}
					var generated strings.Builder
					for _, file := range files {
						generated.Write(file.Data)
					}
					for _, typ := range f.Types {
						language := documentTypes[typ.Name].Languages[target.Name()]
						declaration := language.Declare
						if declaration == "" || !strings.Contains(generated.String(), declaration) {
							t.Errorf("%s declaration is absent from generated code:\n%s", typ.Name, declaration)
						}
						expr := model.TypeExpr(model.Named{Name: typ.Name})
						if typ.Inline {
							expr = model.Inline{Type: typ.Declaration}
						}
						if spelling := s.Spell(f, expr); spelling == "" || language.Name != spelling {
							t.Errorf("%s document spelling %q differs from target %q", typ.Name, language.Name, spelling)
						}
					}
					stubs, err := target.(spi.Scaffolder).Scaffold(f, "api/impl/"+f.Name)
					if err != nil {
						t.Fatal(err)
					}
					var scaffold strings.Builder
					for _, stub := range stubs {
						scaffold.Write(stub.Data)
					}
					for side, operations := range map[string]doc.Side{"server": document.Server, "client": document.Client} {
						for _, m := range operations.Methods {
							invocation := m.Languages[target.Name()].Invoke
							wantHandle := target.Name() == "go" && side == "server" || target.Name() == "typescript" && side == "client"
							if wantHandle && invocation.Handle == "" {
								t.Errorf("%s %s has no init handler snippet", side, m.Name)
							}
							if invocation.Handle != "" && !strings.Contains(scaffold.String(), invocation.Handle) {
								t.Errorf("%s %s handler is absent from init:\n%s", side, m.Name, invocation.Handle)
							}
							if invocation == (spi.Invocation{}) {
								t.Errorf("%s %s has neither a call nor a handler", side, m.Name)
							}
						}
						for _, e := range operations.Events {
							if e.Languages[target.Name()].Invoke.Call == "" {
								t.Errorf("%s %s event has no call snippet", side, e.Name)
							}
						}
					}
				})
			}
		}
	}
}

func TestLanguageSpellingsUseResolvedNames(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{"x": {
		"model.json": `{"nightseam":2,"types":{
			"Input":{"kind":"record","fields":[{"name":"value","type":"T"}]},
			"Box":{"kind":"record","parameters":[{"name":"U"}],"fields":[{"name":"value","type":"U"}]},
			"Status":{"kind":"alias","type":{"literal":"ready"}}
		}}`,
		"protocol.json":   modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"run":{"request":"Input","result":"Input"}}}`),
		"go.json":         `{"names":{"Input":"Payload","Box":"Container"}}`,
		"typescript.json": `{"names":{"Input":"Payload","Box":"Container"}}`,
	}}))
	f := render.Build(analysis.Resolve(world, "x"))
	rows := []struct {
		expression     model.TypeExpr
		goName, tsName string
	}{
		{model.Named{Name: "Input"}, "protocol.Payload[T]", "Protocol.Payload<T>"},
		{model.Named{Name: "T"}, "T", "T"},
		{model.Primitive("timestamp"), "time.Time", "string"},
		{model.Nullable{Elem: model.Named{Name: "Input"}}, "runtime.Nullable[protocol.Payload[T]]", "Protocol.Payload<T> | null"},
		{model.Literal{Value: "ready"}, "protocol.LiteralReady", `"ready"`},
		{model.Apply{Name: "Box", With: map[string]model.Filler{"U": {Type: model.Primitive("string")}}}, "protocol.Container[string]", "Protocol.Container<string>"},
	}
	for _, target := range compose.Targets(module, scope, "") {
		s, ok := target.(spi.Speller)
		if !ok {
			continue
		}
		for _, row := range rows {
			want := row.goName
			if target.Name() == "typescript" {
				want = row.tsName
			}
			if got := s.Spell(f, row.expression); got != want {
				t.Errorf("%s: %s: got %q, want %q", target.Name(), model.String(row.expression), got, want)
			}
		}
	}
}

func TestLanguageInlineSpellingsFollowNamingTable(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/naming.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Derived []struct {
			Path []string
			Name string
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Derived) == 0 {
		t.Fatal("no derived naming rows")
	}
	for _, row := range table.Derived {
		shape := map[string]any{"kind": "record", "fields": []any{map[string]any{"name": "value", "type": "string"}}}
		for i := len(row.Path) - 1; i > 0; i-- {
			shape = map[string]any{"kind": "record", "fields": []any{map[string]any{"name": row.Path[i], "type": shape}}}
		}
		encoded, err := json.Marshal(map[string]any{"nightseam": 2, "types": map[string]any{row.Path[0]: shape}})
		if err != nil {
			t.Fatal(err)
		}
		overrides, err := json.Marshal(map[string]any{"names": map[string]string{row.Path[0]: "Root"}})
		if err != nil {
			t.Fatal(err)
		}
		world := analysis.World(modeltest.World(map[string]map[string]string{"x": {"model.json": string(encoded), "go.json": string(overrides), "typescript.json": string(overrides)}}))
		f := render.Build(analysis.Resolve(world, "x"))
		derived := f.Type(row.Name)
		if derived == nil {
			t.Fatalf("derived %s absent", row.Name)
		}
		for _, target := range compose.Targets(module, scope, "") {
			s, ok := target.(spi.Speller)
			if !ok {
				continue
			}
			prefix := "protocol."
			if target.Name() == "typescript" {
				prefix = "Protocol."
			}
			if got := s.Spell(f, model.Inline{Type: derived.Declaration}); got != prefix+row.Name {
				t.Errorf("%s: inline %v spells %q, want %q", target.Name(), row.Path, got, prefix+row.Name)
			}
		}
	}
}
