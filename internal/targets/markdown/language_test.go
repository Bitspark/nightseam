package markdown

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// Each expression retains its declared type and parameter meaning in prose.
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
	files, err := doc.Target(New(Config{}), nil).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"A record, generic in `T`.",
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
	files, err := doc.Target(New(Config{}), nil).Render(render.Build(analysis.Resolve(world, "x")))
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
				"Choice":{"kind":"union","tag":"kind","value":"payload","variants":{"text":"Message","count":"integer","json":"json","map":{"map":"string"},"maybe":{"nullable":"Message"}}}
			}}`,
			"protocol.json": modeltest.Protocol(`"imports":["base"],"server":{"extends":["base"],"methods":{"choose":{"result":"Choice"}}}`),
		},
	}))
	files, err := doc.Target(New(Config{}), nil).Render(render.Build(analysis.Resolve(world, "x")))
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"The `kind` member identifies the variant.",
		"The complete payload is carried in `payload` beside the tag, including records, maps, JSON and null.",
		"A record's own literal tag remains inside its payload.",
		"A variant without a payload has only the tag; `payload` is absent.",
		"| `\"count\"` | `integer` |",
		"| `\"json\"` | `json` |",
		"| `\"map\"` | map of `string` |",
		"| `\"maybe\"` | nullable `Message` |",
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
			files, err := doc.Target(New(Config{}), nil).Render(family)
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
			if name == "tunnel" && !strings.Contains(document, "| `family` | `string` |") {
				t.Fatalf("the tunnel reference does not describe the family a channel speaks:\n%s", document)
			}
		})
	}
}

func TestRenderUnionDistinguishesAbsentPayload(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{"EmptyRecord":{"kind":"record","fields":[]},"Choice":{"kind":"union","tag":"kind","value":"payload","variants":{"none":{"empty":true},"maybe":{"nullable":"string"},"record":"EmptyRecord"}}}}`},
	}))
	family := render.Build(analysis.Resolve(world, "x"))
	files, err := doc.Target(New(Config{}), nil).Render(family)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[0].Data)
	for _, want := range []string{
		"| `\"none\"` | — |",
		"| `\"maybe\"` | nullable `string` |",
		"| `\"record\"` | `EmptyRecord` |",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the specification confuses payload absence with a payload value; missing %q:\n%s", want, document)
		}
	}
}
