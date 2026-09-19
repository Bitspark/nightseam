package render

import (
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestAppliedInheritanceRetainsBoundMembersAndOriginalDeclarations(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {"model.json": `{"nightseam":2,"types":{
          "Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]},
          "Result":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","variants":{"ok":"T"}}
        }}`},
		"child": {"model.json": `{"nightseam":2,"imports":["base"],"types":{
          "Box":{"kind":"record","parameters":[{"name":"Item"}],"extends":[{"apply":"base.Box","with":{"T":{"array":{"nullable":"Item"}}}}],"fields":[]},
          "Result":{"kind":"union","parameters":[{"name":"Item"}],"tag":"kind","extends":[{"apply":"base.Result","with":{"T":{"array":{"nullable":"Item"}}}}],"variants":{"none":{"empty":true}}}
        }}`},
	}))
	r := Build(analysis.Resolve(world, "child"))
	box := r.Type("Box")
	if len(box.Fields) != 1 || model.String(box.Fields[0].Type) != `{"array":{"nullable":"Item"}}` || box.Fields[0].Origin.Family != "base" || model.String(box.Fields[0].DeclaredType) != model.String(box.Fields[0].Type) {
		t.Fatalf("inherited field: %+v", box.Fields)
	}
	result := r.Type("Result")
	if len(result.Bases) != 1 || result.Bases[0].Type.Origin.Family != "base" || result.Bases[0].Edge.Name != "base.Result" {
		t.Fatalf("base provenance: %+v", result.Bases)
	}
	v := result.Variants[0]
	if v.Origin.Family != "base" || v.Form != VariantValue || len(v.Arguments) != 1 || model.String(v.Arguments[0].Type) != `{"array":{"nullable":"Item"}}` {
		t.Fatalf("inherited variant: %+v", v)
	}
	if result.Variants[1].Form != VariantEmpty {
		t.Fatal("no-payload arm did not become tag-only")
	}
	bound, ok := r.Apply(model.Apply{Name: "Result", With: map[string]model.Filler{"Item": {Type: model.Primitive("string")}}}, nil)
	if !ok || model.String(bound.Variants[0].Type) != `{"array":{"nullable":"string"}}` || len(bound.Uses) != 0 {
		t.Fatalf("bound inherited union: %+v", bound)
	}
	if model.String(bound.Variants[0].Arguments[0].Type) != `{"array":{"nullable":"string"}}` || model.String(bound.Bases[0].Type.Variants[0].Type) != `{"array":{"nullable":"string"}}` {
		t.Fatal("bound union lost its inherited wrapper arguments")
	}
	if r.ReferencedFamily("base").Type("Result").Declaration != world["base"].Types["Result"] {
		t.Fatal("source family identity was lost")
	}
	if model.String(world["base"].Types["Result"].Variants[0].Type) != `"T"` {
		t.Fatal("binding changed the original declaration")
	}
}

func TestAppliedSideFactsBindPayloadsAndKeepReferenceMeaning(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base":  {"model.json": `{"nightseam":2,"types":{"Entity":{"kind":"entity","key":"id","fields":[{"name":"id","type":"integer"}]}}}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"read":{"result":"T"},"selected":{"result":{"ref":"Entity"}}},"events":{"changed":{"type":"T"}}}`)},
		"child": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"}],"server":{"extends":[{"apply":"base","with":{"T":{"array":{"nullable":"Item"}}}}]}`)},
	}))
	r := Build(analysis.Resolve(world, "child"))
	m := r.Server.Methods[0]
	if model.String(m.Result) != `{"array":{"nullable":"Item"}}` || model.String(m.BoundDeclaration.Result) != model.String(m.Result) || model.String(m.Declaration.Result) != `"T"` || m.Scope[0].Name != "Item" {
		t.Fatalf("bound method: %+v", m)
	}
	selected := r.Server.Methods[1]
	if model.String(selected.Result) != `"integer"` || model.String(selected.BoundDeclaration.Result) != `{"ref":"Entity"}` || selected.Origin.Family != "base" {
		t.Fatalf("bound ref: %+v", selected)
	}
	e := r.Server.Events[0]
	if model.String(e.BoundDeclaration.Type) != `{"array":{"nullable":"Item"}}` || e.Declaration != &world["base"].Protocol.Server.Events[0] {
		t.Fatalf("bound event: %+v", e)
	}
	if !reflect.DeepEqual(r.Uses, []Use{{Parameter: "Item"}}) {
		t.Fatalf("inherited side uses: %v", r.Uses)
	}
}

func TestRecordPayloadFactsSeparateRecordsFromOtherObjectForms(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{
         "Row":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
         "Alias":{"kind":"alias","type":{"apply":"Row","with":{"T":"string"}}},
         "Other":{"kind":"union","tag":"type","variants":{"none":{"empty":true}}},
         "U":{"kind":"union","tag":"kind","variants":{"alias":"Alias","applied":{"apply":"Row","with":{"T":"integer"}},"map":{"map":"string"},"nullable":{"nullable":"Alias"},"union":"Other","inline":{"kind":"record","fields":[]},"none":{"empty":true}}}
        }}`},
	}))
	r := Build(analysis.Resolve(world, "x"))
	for _, v := range r.Type("U").Variants {
		wantRecord := v.Tag == "alias" || v.Tag == "applied" || v.Tag == "inline"
		if (v.Payload != nil) != wantRecord {
			t.Errorf("%s record payload = %+v", v.Tag, v.Payload)
		}
		if v.Tag == "alias" && model.String(v.Payload.Fields[0].Type) != `"string"` {
			t.Fatal("alias did not preserve applied record arguments")
		}
		if v.Tag != "none" && v.Form != VariantValue {
			t.Errorf("%s is not adjacent", v.Tag)
		}
	}
}

func TestInheritedReferencesRetainTheArgumentOwner(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {"model.json": `{"nightseam":2,"types":{
          "Entity":{"kind":"entity","key":"id","fields":[{"name":"id","type":"integer"}]},
          "Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"owned","type":{"ref":"Entity"}},{"name":"supplied","type":"T"}]}
        }}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"owned":{"result":{"ref":"Entity"}},"supplied":{"result":"T"}},"events":{"changed":{"type":"T"}}}`)},
		"child": {"model.json": `{"nightseam":2,"imports":["base"],"types":{
          "Entity":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]},
          "Box":{"kind":"record","extends":[{"apply":"base.Box","with":{"T":{"ref":"Entity"}}}],"fields":[]}
        }}`, "protocol.json": modeltest.Protocol(`"server":{"extends":[{"apply":"base","with":{"T":{"ref":"Entity"}}}]}`)},
	}))
	r := Build(analysis.Resolve(world, "child"))
	for _, field := range r.Type("Box").Fields {
		want := "base"
		if field.Name == "supplied" {
			want = "child"
		}
		if ref, ok := field.DeclaredType.(model.Ref); !ok || ref.Family != want {
			t.Errorf("%s declaration reference = %#v, want %s", field.Name, field.DeclaredType, want)
		}
	}
	for _, method := range r.Server.Methods {
		want := "base"
		if method.Name == "supplied" {
			want = "child"
		}
		if ref, ok := method.BoundDeclaration.Result.(model.Ref); !ok || ref.Family != want {
			t.Errorf("%s result reference = %#v, want %s", method.Name, method.BoundDeclaration.Result, want)
		}
	}
	if r.Server.Events[0].BoundDeclaration.Type.(model.Ref).Family != "child" {
		t.Fatal("event argument captured by the base's entity")
	}
}

func TestLocalInheritanceBindsNestedFamilyCaptures(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"types":{
          "Nested":{"kind":"record","fields":[{"name":"item","type":"T"}]},
          "Base":{"kind":"record","fields":[{"name":"nested","type":"Nested"}]},
          "Child":{"kind":"record","extends":[{"apply":"Base","with":{"T":"string"}}],"fields":[]}
        }`)},
	}))
	f := analysis.Resolve(world, "x")
	r := Build(f)
	for _, expression := range []model.TypeExpr{r.Type("Child").Fields[0].Type, f.FlattenedFields("Child")[0].Type} {
		application, ok := expression.(model.Apply)
		if !ok || application.Name != "Nested" || model.String(application.With["T"].Type) != `"string"` {
			t.Fatalf("nested local capture = %s", model.String(expression))
		}
		view, ok := r.Apply(application, nil)
		if !ok || model.String(view.Fields[0].Type) != `"string"` || len(view.Uses) != 0 {
			t.Fatalf("bound nested view = %+v", view)
		}
	}
}

func TestInheritedTypesExposeTransitivePackageReferences(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"values": {"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[]}}}`},
		"base":   {"model.json": `{"nightseam":2,"imports":["values"],"types":{"Record":{"kind":"record","fields":[{"name":"payload","type":"values.Payload"}]},"Union":{"kind":"union","tag":"kind","variants":{"text":"string"}}}}`},
		"middle": {"model.json": `{"nightseam":2,"imports":["base"],"types":{"Record":{"kind":"record","extends":["base.Record"],"fields":[]},"Union":{"kind":"union","tag":"kind","extends":["base.Union"],"variants":{}}}}`},
		"child":  {"model.json": `{"nightseam":2,"imports":["middle"],"types":{"Record":{"kind":"record","extends":["middle.Record"],"fields":[]},"Union":{"kind":"union","tag":"kind","extends":["middle.Union"],"variants":{}}}}`},
	}))
	r := Build(analysis.Resolve(world, "child"))
	if !reflect.DeepEqual(r.References, []string{"base", "middle", "values"}) {
		t.Fatalf("inherited wrapper and field packages = %v", r.References)
	}
}
