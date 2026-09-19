package golang

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func TestTypeParametersDoNotRequireFamilyTags(t *testing.T) {
	fam := family(map[string]string{
		"model.json":    `{"nightseam":2,"types":{"TTag":{"kind":"record","fields":[]}}}`,
		"protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"echo":{"request":"T","result":"T"}}}`),
	})
	files, err := New(Config{Module: "example.test/m"}).Render(fam)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.Contains(string(file.Data), "runtime.Of[TTag]") {
			t.Fatal("a type parameter acquired a family constraint")
		}
		if strings.HasSuffix(file.Path, "client_generated.go") && !strings.Contains(string(file.Data), "func Dial[T any]") {
			t.Fatalf("type-only entry signature is missing: %s", file.Data)
		}
	}
}

func TestImportedGoOverridesAreCheckedInTheirSourceFamily(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {
			"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"value","type":"string"}]}}}`,
			"go.json":    `{"names":{"Payload.value":"hidden"}}`,
		},
		"left":  {"model.json": `{"nightseam":2,"imports":["base"],"types":{"Payload":{"kind":"record","extends":["base.Payload"],"fields":[]}}}`},
		"right": {"model.json": `{"nightseam":2,"imports":["base"],"types":{"Payload":{"kind":"record","extends":["base.Payload"],"fields":[]}}}`},
		"child": {"model.json": `{"nightseam":2,"imports":["left","right"],"types":{
			"Left":{"kind":"record","extends":["left.Payload"],"fields":[]},
			"Right":{"kind":"record","extends":["right.Payload"],"fields":[]}
		}}`},
	}))
	_, diagnostics := newPlan(render.Build(analysis.Resolve(world, "child")))
	if len(diagnostics) == 1 {
		diagnostic := diagnostics[0]
		if diagnostic.Family == "base" && diagnostic.Code == "invalid_name" && diagnostic.At().String() == "go.json#/names/Payload.value" {
			return
		}
	}
	t.Fatalf("an inherited unexported Go field was not refused at its source: %v", diagnostics)
}

func TestMixedParametersConstrainOnlyFamilyDraws(t *testing.T) {
	fam := family(map[string]string{
		"protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"},{"name":"S","of":"protocol"}],"types":{"Input":{"kind":"record","fields":[{"name":"item","type":"T"},{"name":"frame","type":"S.Envelope"}]}},"server":{"methods":{"echo":{"request":"Input","result":"Input"}}}`),
	})
	files, err := New(Config{Module: "example.test/m"}).Render(fam)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Path, "client_generated.go") && !strings.Contains(string(file.Data), "func Dial[T any, SEnvelope runtime.Of[STag], STag any]") {
			t.Fatalf("mixed entry signature is missing: %s", file.Data)
		}
	}
}

func TestGoParametersRespectDeclarationAndEntryScopes(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"own parameter shadows package type":  {"model.json": `{"nightseam":2,"types":{"T":{"kind":"record","fields":[]},"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]}}}`},
		"entry parameter shadows client type": {"protocol.json": modeltest.Protocol(`"parameters":[{"name":"Client"}],"server":{"methods":{"echo":{"result":"Client"}}}`)},
	} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := newPlan(family(files))
			found := false
			for _, diagnostic := range diagnostics {
				found = found || diagnostic.Code == "generated_name_collision"
			}
			if !found {
				t.Fatalf("a shadowed generated name was accepted: %v", diagnostics)
			}
		})
	}
	_, diagnostics := newPlan(family(map[string]string{"model.json": `{"nightseam":2,"types":{
		"Left":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
		"Right":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
		"Open":{"kind":"record","fields":[]},"Client":{"kind":"record","fields":[]}
	}}`}))
	if len(diagnostics) != 0 {
		t.Fatalf("names in distinct Go scopes collided: %v", diagnostics)
	}
}
