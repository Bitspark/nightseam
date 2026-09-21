package oracle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
)

func drawnFamily() *model.Family {
	draw := model.Drawn{Parameter: "S", Name: "Job"}
	fillers := map[string]model.Filler{
		"Provider": {Type: model.Named{Name: "S"}},
		"Value":    {Type: model.Array{Elem: draw}},
	}
	side := model.Side{
		Extends: []model.Inheritance{{Name: "base", With: fillers}},
		Methods: []model.Method{{Name: "exchange", Request: draw, Result: model.Nullable{Elem: draw}}},
		Events:  []model.Event{{Name: "changed", Type: model.Map{Elem: draw}}},
	}
	return &model.Family{
		Name: "holder", Imports: []string{"base"},
		Types: map[string]*model.Type{
			"Call": {Name: "Call", Kind: model.KindCallable, Request: draw, Result: model.Apply{Family: "base", Name: "Box", With: fillers}},
			"Choice": {Name: "Choice", Kind: model.KindUnion, Variants: []model.Variant{
				{Tag: "empty"}, {Tag: "job", Type: model.Nullable{Elem: draw}},
			}},
			"Alias": {Name: "Alias", Kind: model.KindAlias, Alias: model.Map{Elem: draw}},
			"Record": {Name: "Record", Kind: model.KindRecord,
				Parameters: []model.Parameter{{Name: "T"}},
				Extends:    []model.Inheritance{{Name: "base.Box", With: fillers}},
				Fields: []model.Field{
					{Name: "job", Type: model.Array{Elem: draw}},
					{Name: "other", Type: model.Drawn{Parameter: "U", Name: "Progress"}},
					{Name: "local", Type: model.Named{Name: "T"}},
					{Name: "inline", Type: model.Inline{Type: &model.Type{Kind: model.KindUnion, Variants: []model.Variant{{Tag: "job", Type: draw}}}}},
				},
			},
		},
		Protocol: &model.Protocol{Parameters: []model.Parameter{{Name: "S", Of: "live"}, {Name: "U", Of: "live"}}, Server: side, Client: side},
		Live:     &model.Live{Server: side, Client: side},
	}
}

func TestSubstituteCompleteFamilyDraws(t *testing.T) {
	bound := Substitute(drawnFamily(), map[string]string{"S": "provider"})
	check := func(name string, got model.TypeExpr, want string) {
		t.Helper()
		if actual := model.String(got); actual != want {
			t.Errorf("%s = %s, want %s", name, actual, want)
		}
	}
	check("callable request", bound.Types["Call"].Request, `"provider.Job"`)
	check("callable result", bound.Types["Call"].Result, `{"apply":"base.Box","with":{"Provider":"provider","Value":{"array":"provider.Job"}}}`)
	check("union arm", bound.Types["Choice"].Variants[1].Type, `{"nullable":"provider.Job"}`)
	if bound.Types["Choice"].Variants[0].Type != nil {
		t.Error("empty union arm acquired a payload")
	}
	check("alias", bound.Types["Alias"].Alias, `{"map":"provider.Job"}`)
	fields := bound.Types["Record"].Fields
	check("nested field", fields[0].Type, `{"array":"provider.Job"}`)
	check("unbound family", fields[1].Type, `"U.Progress"`)
	check("type parameter", fields[2].Type, `"T"`)
	check("inline union", fields[3].Type.(model.Inline).Type.Variants[0].Type, `"provider.Job"`)
	check("type inheritance", bound.Types["Record"].Extends[0].Expression(), `{"apply":"base.Box","with":{"Provider":"provider","Value":{"array":"provider.Job"}}}`)
	for name, side := range map[string]model.Side{
		"protocol server": bound.Protocol.Server, "protocol client": bound.Protocol.Client,
		"live server": bound.Live.Server, "live client": bound.Live.Client,
	} {
		check(name+" request", side.Methods[0].Request, `"provider.Job"`)
		check(name+" result", side.Methods[0].Result, `{"nullable":"provider.Job"}`)
		check(name+" event", side.Events[0].Type, `{"map":"provider.Job"}`)
		check(name+" inherited type filler", side.Extends[0].With["Value"].Type, `{"array":"provider.Job"}`)
		if side.Extends[0].With["Provider"].Family != "provider" {
			t.Errorf("%s inherited family filler was not bound", name)
		}
	}
	if bound.Name != "holder" || bound.Types["Call"].Name != "Call" {
		t.Error("substitution renamed a nominal declaration")
	}
	if !reflect.DeepEqual(bound.Protocol.Parameters, []model.Parameter{{Name: "U", Of: "live"}}) {
		t.Errorf("remaining family parameters = %#v", bound.Protocol.Parameters)
	}
	if !reflect.DeepEqual(bound.Imports, []string{"base", "provider"}) {
		t.Errorf("imports = %v", bound.Imports)
	}
}

func TestSubstituteLeavesSourceIndependent(t *testing.T) {
	source := drawnFamily()
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	first := Substitute(source, map[string]string{"S": "first"})
	second := Substitute(source, map[string]string{"S": "second"})
	// A mutation of the left route must not change the right route or its
	// source, even where the original declaration reused an inheritance map.
	first.Types["Choice"].Variants[1].Tag = "changed"
	first.Types["Record"].Extends[0].With["Provider"] = model.Filler{Family: "changed"}
	first.Live.Server.Methods[0].Name = "changed"
	first.Live.Client.Extends[0].With["Provider"] = model.Filler{Family: "changed"}
	after, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("substituting or editing the bound route mutated the source declaration")
	}
	if second.Types["Choice"].Variants[1].Tag != "job" || second.Live.Server.Methods[0].Name != "exchange" || second.Live.Client.Extends[0].With["Provider"].Family != "second" {
		t.Fatal("independently bound routes share mutable declaration members")
	}
}
