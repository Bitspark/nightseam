package spec

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// The complete target still refuses new forms until all of them render.
// These rendering cases hold the independent expression and parameter work
// before the shared resolution facts for unions and inline shapes land.
func TestRenderTypeExpressionsAndParameters(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json": `{"nightseam":2,"types":{
				"Page":{"kind":"record","parameters":[{"name":"T","description":"The element."}],"fields":[{"name":"items","type":{"array":{"nullable":"T"}}}]},
				"Pages":{"kind":"alias","type":{"apply":"Page","with":{"T":{"map":{"nullable":"string"}}}}},
				"State":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ready|later"}},{"name":"code","type":{"literal":"a\u0060b"}},{"name":"cache","type":{"nullable":{"array":"string"}},"required":false}]}
			}}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"},{"name":"Item","description":"The current item."}],"types":{"Frame":{"kind":"record","fields":[{"name":"envelope","type":"S.Envelope"},{"name":"item","type":"Item"}]}},"server":{"methods":{"get":{"result":"Frame"}}}`),
		},
	}))
	f := render.Build(analysis.Resolve(world, "x"))
	files, err := New(Config{}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"- **S**: a family with the `protocol` tier",
		"- **Item**: a type. The current item.",
		"- **T**: a type. The element.",
		"| `items` | array of nullable `T` | required |",
		"An alias of `Page` with T=map of nullable `string`.",
		"| `kind` | the literal `\"ready\\|later\"` | required |",
		"| `code` | the literal ``\"a`b\"`` | required |",
		"| `cache` | nullable array of `string` | optional |",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the specification lacks %q:\n%s", want, document)
		}
	}
}

func TestRenderFamilyTypeParameterWithoutFamilyDraw(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json":    `{"nightseam":2,"types":{}}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"}],"server":{"methods":{"echo":{"request":"Item","result":"Item"}}}`),
		},
	}))
	files, err := New(Config{}).Render(render.Build(analysis.Resolve(world, "x")))
	if err != nil {
		t.Fatal(err)
	}
	if document := string(files[0].Data); !strings.Contains(document, "- **Item**: a type.") {
		t.Fatalf("a type parameter disappeared without a family draw:\n%s", document)
	}
}

func TestRenderUnionAndSideDeclarations(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {"model.json": `{"nightseam":2,"types":{}}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"ping":{"result":"string"}}}`)},
		"x": {
			"model.json": `{"nightseam":2,"types":{
				"Message":{"kind":"record","fields":[{"name":"kind","type":{"literal":"text"}},{"name":"body","type":"string"}]},
				"Choice":{"kind":"union","tag":"kind","value":"payload","variants":{"text":"Message","count":"integer"}}
			}}`,
			"protocol.json": modeltest.Protocol(`"imports":["base"],"server":{"extends":["base"],"methods":{"choose":{"result":"Choice"}}}`),
		},
	}))
	files, err := New(Config{}).Render(render.Build(analysis.Resolve(world, "x")))
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"The `kind` member identifies the variant.",
		"non-object payload is carried in `payload`",
		"| `\"count\"` | `integer` |",
		"| `\"text\"` | `Message` |",
		"Extends the server side of `base`.",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the specification lacks %q:\n%s", want, document)
		}
	}
}

func TestRenderBuiltinReferences(t *testing.T) {
	world := analysis.World(builtin.Families())
	for _, name := range builtin.Names() {
		t.Run(name, func(t *testing.T) {
			family := render.Build(analysis.Resolve(world, name))
			files, err := New(Config{}).Render(family)
			if err != nil {
				t.Fatal(err)
			}
			document := string(files[0].Data)
			if !strings.Contains(document, "nightseam:"+name+"/") || strings.Contains(document, "api/contracts/"+name+"/") {
				t.Fatalf("the built-in specification claims a consumer source:\n%s", document)
			}
			for _, side := range []render.Side{family.Server, family.Client} {
				for _, event := range side.Events {
					if !strings.Contains(document, "`"+event.Name+"`") {
						t.Errorf("the built-in specification omits %s", event.Name)
					}
				}
				for _, method := range side.Methods {
					if !strings.Contains(document, "`"+method.Name+"`") {
						t.Errorf("the built-in specification omits %s", method.Name)
					}
				}
			}
			if name == "session" && !strings.Contains(document, "| `holder` | nullable `string` |") {
				t.Fatalf("the session reference does not describe its nullable holder:\n%s", document)
			}
		})
	}
}
