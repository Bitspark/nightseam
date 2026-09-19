package main

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/targets/spec"
)

// An inherited member is described in the derived declaration's parameter
// scope, with the explicit binding still visible at the inheritance edge.
func TestSpecificationBindsInheritedTypeParameters(t *testing.T) {
	files := fstest.MapFS{
		"contracts/x/model.json": {Data: []byte(`{"nightseam":2,"types":{
			"Base":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
			"Forwarded":{"kind":"record","parameters":[{"name":"Item"}],"extends":[{"apply":"Base","with":{"T":"Item"}}],"fields":[]},
			"Bound":{"kind":"record","extends":[{"apply":"Base","with":{"T":{"array":{"nullable":"string"}}}}],"fields":[]},
			"Choice":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","variants":{"item":"T"}},
			"Extended":{"kind":"union","parameters":[{"name":"Item"}],"extends":[{"apply":"Choice","with":{"T":"Item"}}],"tag":"kind","variants":{"count":"integer"}}
		}}`)},
	}
	document := renderSpecificationFixture(t, files, "x")
	for _, test := range []struct {
		name string
		want []string
	}{
		{"Forwarded", []string{"Extends `Base` with T=`Item`.", "| `item` | `Item` |", "inherited from `Base`"}},
		{"Bound", []string{"Extends `Base` with T=array of nullable `string`.", "| `item` | array of nullable `string` |"}},
		{"Extended", []string{"Extends `Choice` with T=`Item`.", "| `\"item\"` | `Item` | `Choice` |", "| `\"count\"` | `integer` | `Extended` |"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			section := specificationSection(t, document, "### "+test.name)
			for _, want := range test.want {
				if !strings.Contains(section, want) {
					t.Errorf("the inherited declaration omits %q:\n%s", want, section)
				}
			}
		})
	}
}

// Binding a side must substitute operation types without lowering away an
// entity reference or qualifying a forwarded parameter as a source type.
func TestSpecificationBindsInheritedSideParameters(t *testing.T) {
	files := fstest.MapFS{
		"contracts/base/model.json": {Data: []byte(`{"nightseam":2,"types":{"Entity":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]}}}`)},
		"contracts/base/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{
			"methods":{"echo":{"request":"T","result":"T"}},
			"events":{"changed":{"type":"T"},"selected":{"type":{"ref":"Entity"}}}
		}}`)},
		"contracts/child/model.json": {Data: []byte(`{"nightseam":2,"imports":["base"]}`)},
		"contracts/child/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"Item"}],"server":{
			"extends":[{"apply":"base","with":{"T":{"array":{"nullable":"Item"}}}}]
		}}`)},
	}
	document := renderSpecificationFixture(t, files, "child")
	for _, want := range []string{
		"Extends the server side of `base` with T=array of nullable `Item`.",
		"| `echo` | array of nullable `Item` | array of nullable `Item` |",
		"| `changed` | array of nullable `Item` |",
		"| `selected` | reference to `base.Entity` |",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the applied side omits %q:\n%s", want, document)
		}
	}
}

func renderSpecificationFixture(t *testing.T, files fstest.MapFS, family string) string {
	t.Helper()
	k := kernel.New(spec.New(spec.Config{}))
	world := k.Load(files, "contracts")
	result, err := k.Render(world, family)
	if err != nil {
		t.Fatal(err)
	}
	return string(result.Files["api/spec/"+family+"/README.md"])
}
