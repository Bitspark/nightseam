package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/targets/spec"
)

// The spec target alone renders through the checked kernel, so its complete
// output is held even while the language targets still refuse new forms.
func TestProofSpecificationGolden(t *testing.T) {
	k := kernel.New(spec.New(spec.Config{}))
	world := k.Load(os.DirFS("testdata"), "proof")
	files := map[string][]byte{}
	for _, name := range world.Names {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range result.Files {
			files[path] = data
		}
	}
	holdGolden(t, "testdata/golden-proof-spec", files)
}

// The proof uses the complete language, so a readable own-declarations
// document is insufficient: inherited members and derived inline shapes
// must be present, named and linked by their actual type expressions.
func TestProofSpecificationIsComplete(t *testing.T) {
	family := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	target := spec.New(spec.Config{})
	if diagnostics := target.Check(family); len(diagnostics) != 0 {
		t.Errorf("the spec target refuses the proof family: %v", diagnostics)
	}
	files, err := target.Render(family)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, name := range []string{"Carried", "Option", "OptionNone", "Page", "Part", "PartImage", "Parts", "PartsRequest", "Result", "RichPart", "RichPartTable", "TextPart"} {
		if count := strings.Count(document, "\n### "+name+"\n"); count != 1 {
			t.Errorf("type %s has %d declarations in the specification, want one", name, count)
		}
	}

	rich := specificationSection(t, document, "### RichPart")
	if !strings.Contains(rich, "Extends `Part`.") {
		t.Error("the extended union does not name its declared base")
	}
	for _, tag := range []string{"count", "image", "table", "text"} {
		if count := strings.Count(rich, "| `\""+tag+"\"` |"); count != 1 {
			t.Errorf("RichPart describes variant %s %d times, want once including inherited variants", tag, count)
		}
	}
	for _, want := range []string{
		"| `parts` | `PartsRequest` |",
		"| `echo` | `probe.Payload` | `probe.Payload` | `denied` |",
		"| `changed` | `probe.Payload` |",
		"| `denied` | The caller is denied. |",
		"| `rows` | array of nullable `string` | required |",
		"- **S**: a family with the `protocol` tier",
		"- **Item**: a type.",
		"- **T**: a type.",
		"- **E**: a type.",
		"An alias of `Page` with T=`Part`.",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the proof specification omits %q", want)
		}
	}
}

// The proof family's inline declarations are records. A nested union and
// enum need the same materialization even through collection/null wrappers.
func TestSpecificationNamesNestedInlineShapes(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{"State":{"kind":"record","fields":[
			{"name":"items","type":{"map":{"array":{"nullable":{"kind":"union","tag":"kind","value":"content","variants":{
				"summary":"string",
				"detail":{"kind":"record","fields":[{"name":"mode","type":{"kind":"enum","values":["compact","full"]}}]}
			}}}}}}
		]}}}`},
	}))
	family := analysis.Resolve(world, "x")
	if diagnostics := check.Family(family); len(diagnostics) != 0 {
		t.Fatalf("the inline fixture is invalid: %v", diagnostics)
	}
	target := spec.New(spec.Config{})
	facts := render.Build(family)
	if diagnostics := target.Check(facts); len(diagnostics) != 0 {
		t.Errorf("the spec target refuses nested inline shapes: %v", diagnostics)
	}
	files, err := target.Render(facts)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"### StateItems\n",
		"### StateItemsDetail\n",
		"### StateItemsDetailMode\n",
		"| `items` | map of array of nullable `StateItems` |",
		"| `\"detail\"` | `StateItemsDetail` |",
		"| `mode` | `StateItemsDetailMode` |",
		"One of `compact`, `full`.",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the nested-inline specification omits %q", want)
		}
	}
}

func TestSpecificationNamesItsActualSourceDirectory(t *testing.T) {
	files := fstest.MapFS{
		"custom/contracts/x/model.json": {Data: []byte(`{"nightseam":2,"types":{"Item":{"kind":"record","fields":[{"name":"id","type":"string"}]}}}`)},
	}
	k := kernel.New(spec.New(spec.Config{}))
	world := k.Load(files, "custom/contracts")
	result, err := k.Render(world, "x")
	if err != nil {
		t.Fatal(err)
	}
	document := string(result.Files["api/spec/x/README.md"])
	if !strings.Contains(document, "from custom/contracts/x/") || !strings.Contains(document, "`custom/contracts/x/model.json`") {
		t.Fatalf("the specification names a different source than the one loaded:\n%s", document)
	}
}

// Lowering an inherited entity reference to its key type preserves its wire
// value but loses what the declaration says that value identifies.
func TestSpecificationPreservesInheritedEntityReferences(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {
			"model.json":    `{"nightseam":2,"types":{"Item":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]}}}`,
			"protocol.json": modeltest.Protocol(`"server":{"events":{"selected":{"type":{"ref":"Item"}}}}`),
		},
		"child": {
			"model.json":    `{"nightseam":2,"imports":["base"]}`,
			"protocol.json": modeltest.Protocol(`"server":{"extends":["base"]}`),
		},
	}))
	for _, name := range []string{"base", "child"} {
		if diagnostics := check.Family(analysis.Resolve(world, name)); len(diagnostics) != 0 {
			t.Fatalf("the reference fixture is invalid in %s: %v", name, diagnostics)
		}
	}
	files, err := spec.New(spec.Config{}).Render(render.Build(analysis.Resolve(world, "child")))
	if err != nil {
		t.Fatal(err)
	}
	if document := string(files[0].Data); !strings.Contains(document, "| `selected` | reference to `base.Item` |") {
		t.Fatalf("the inherited event loses its entity-reference meaning:\n%s", document)
	}
}

func specificationSection(t *testing.T, document, heading string) string {
	t.Helper()
	_, section, found := strings.Cut(document, "\n"+heading+"\n")
	if !found {
		t.Fatalf("the document lacks section %s", heading)
	}
	end := len(section)
	for _, next := range []string{"\n## ", "\n### "} {
		if index := strings.Index(section, next); index >= 0 && index < end {
			end = index
		}
	}
	return section[:end]
}
