package typescript

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func family(files map[string]string) *render.Family {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": files,
		"probe": {
			"model.json":    `{"nightseam": 2, "types": {"Payload": {"kind": "record", "fields": [{"name": "text", "type": "string"}]}}}`,
			"protocol.json": modeltest.Protocol(``),
		},
	}))
	return render.Build(analysis.Resolve(world, "x"))
}

const fixtureModel = `{"nightseam": 2, "types": {
	"Input": {"kind": "record", "fields": [{"name": "text", "type": "string"}]},
	"Result": {"kind": "record", "fields": [{"name": "ok", "type": "boolean"}]}
}}`

func fixtureProtocol(sections string) string {
	base := `"server": {"methods": {"run": {"request": "Input", "result": "Result"}}, "events": {"changed": {"type": "Result"}}}, "errors": {"denied": "Denied."}`
	if sections != "" {
		base += ", " + sections
	}
	return modeltest.Protocol(base)
}

func check(files map[string]string) []diag.Diagnostic {
	return New(Config{Scope: "@example"}).Check(family(files))
}

func has(diagnostics []diag.Diagnostic, code, at string) bool {
	for _, d := range diagnostics {
		if d.Code == code && d.At().String() == at {
			return true
		}
	}
	return false
}

// TestCheckRejectsWhatTypeScriptCannotGenerate breaks the fixture one way
// at a time and expects the target to name it.
func TestCheckRejectsWhatTypeScriptCannotGenerate(t *testing.T) {
	m := func(types string) string { return `{"nightseam": 2, "types": {` + types + `}}` }
	for name, c := range map[string]struct {
		files    map[string]string
		code, at string
	}{
		"TypeScript operation collision":   {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"result": "string"}, "other": {"result": "string"}}}`), "typescript.json": `{"names": {"other": "run"}}`}, "generated_name_collision", "protocol.json#/server/methods/run"},
		"reserved type":                    {map[string]string{"model.json": m(`"Client": {"kind": "record", "fields": []}`)}, "reserved_name", "model.json#/types/Client"},
		"global type":                      {map[string]string{"model.json": m(`"Array": {"kind": "alias", "type": "string"}`)}, "reserved_name", "model.json#/types/Array"},
		"error member collision":           {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"errors": {"not-found": "", "not_found": ""}`)}, "generated_name_collision", "protocol.json#/errors/not_found"},
		"a name that is not an identifier": {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "typescript.json": `{"names": {"run": "run it"}}`}, "invalid_name", "typescript.json#/names/run"},
		"a parameter named as a type":      {map[string]string{"model.json": m(`"S": {"kind": "enum", "values": ["v"]}`), "protocol.json": modeltest.Protocol(`"parameters": [{"name": "S", "of": "protocol"}], "types": {"F": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "generated_name_collision", "protocol.json#/parameters/0/name"},
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := check(c.files)
			if !has(diagnostics, c.code, c.at) {
				t.Fatalf("expected %s at %s, got %v", c.code, c.at, diagnostics)
			}
		})
	}
	if diagnostics := check(map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``)}); len(diagnostics) != 0 {
		t.Fatalf("the fixture is refused: %v", diagnostics)
	}
}

// Methods are inside their own facet, so then is not a Promise hook and
// constructor/close/onChanged are not class members or generated event helpers.
func TestMethodFacetsAcceptFormerClientMemberNames(t *testing.T) {
	for _, name := range []string{"then", "constructor", "close", "onChanged"} {
		t.Run(name, func(t *testing.T) {
			for _, override := range []bool{false, true} {
				declaration := name
				if name == "onChanged" {
					declaration = "on_changed"
				}
				files := map[string]string{"model.json": fixtureModel}
				if override {
					declaration = "run"
					files["typescript.json"] = `{"names":{"run":"` + name + `"}}`
				}
				files["protocol.json"] = modeltest.Protocol(`"server":{"methods":{"` + declaration + `":{"request":"Input","result":"Result"}},"events":{"changed":{"type":"Result"}}}`)
				f := family(files)
				if diagnostics := New(Config{Scope: "@example"}).Check(f); len(diagnostics) != 0 {
					t.Fatalf("safe facet method refused (override=%v): %v", override, diagnostics)
				}
				output, err := New(Config{Scope: "@example"}).Render(f)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(output[0].Data), name+"(params: Input, context?: WireModelContext): Result | Promise<Result>") {
					t.Fatalf("safe facet method missing (override=%v):\n%s", override, output[0].Data)
				}
			}
		})
	}
}

// TestNaming: members are lower camel by convention, error members too,
// quoted when not an identifier; an override replaces the convention.
func TestNaming(t *testing.T) {
	f := family(map[string]string{
		"model.json":      fixtureModel,
		"protocol.json":   modeltest.Protocol(`"server": {"methods": {"no_args": {"result": "string"}, "work.get": {"result": "string"}}, "events": {"frame.relayed": {"type": "string"}}}, "errors": {"not_found": "", "429-too-many": "", "---": ""}`),
		"typescript.json": `{"names": {"work.get": "getWorkItem"}}`,
	})
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for got, want := range map[string]string{
		p.operations["no_args"]:       "noArgs",
		p.operations["work.get"]:      "getWorkItem",
		p.operations["frame.relayed"]: "frameRelayed",
		p.errors["not_found"]:         "notFound",
		p.errors["429-too-many"]:      `"429-too-many"`,
		p.errors["---"]:               `"---"`,
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// TestRenderPlacesTheClient: the files land where the layout says,
// the client model adapter is spelled, the manifest is named under the scope, and
// a config the target cannot place with is refused.
func TestRenderPlacesTheClient(t *testing.T) {
	f := family(map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``)})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	if got := strings.Join(paths, " "); got != "api/ts/x-client/src/types.ts api/ts/x-client/src/index.ts api/ts/x-client/package.json api/ts/x-client/tsconfig.json api/ts/x-client/src/test.ts api/ts/x-binding/src/index.ts api/ts/x-binding/package.json api/ts/x-binding/tsconfig.json api/ts/x-binding/src/test.ts" {
		t.Fatalf("placed at %s", got)
	}
	index := string(files[1].Data)
	if !strings.HasPrefix(index, "// Code generated by nightseam. DO NOT EDIT.\n") || !strings.Contains(index, "export function toWire(model: Protocol.ClientModel, context: AdapterContext): Wire") || !strings.Contains(index, "export async function fromWire(wire: Wire, context: AdapterContext): Promise<Protocol.ClientModel>") {
		t.Fatalf("the client model adapter is not spelled:\n%s", index)
	}
	if !strings.Contains(string(files[0].Data), "run(params: Input, context?: WireModelContext): Result | Promise<Result>") {
		t.Fatalf("shared method signature is missing:\n%s", files[0].Data)
	}
	if !strings.Contains(string(files[2].Data), `"name":"@example/x-client"`) || !strings.Contains(string(files[2].Data), `"@nightseam/runtime":"`+DefaultRuntimeVersion+`"`) {
		t.Fatalf("the manifest is wrong: %s", files[2].Data)
	}
	for _, bad := range []Config{{}, {Scope: "example"}, {Scope: "@example", Runtime: "bad name!"}, {Scope: "@example", Layout: Layout{Client: "no-family"}}, {Scope: "@example", Layout: Layout{Client: "../{family}"}}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("config %+v was accepted", bad)
		}
	}
	placed, err := New(Config{Scope: "@example", Place: map[string]Layout{"x": {Client: "gen/ts/{family}"}}}).Render(f)
	if err != nil || placed[0].Path != "gen/ts/x/src/types.ts" {
		t.Fatalf("placement is not honoured: %v", err)
	}
}

// Each model bridge makes one local wire pair and labels the family's names,
// preserving caller labels; the generated adapter creates no concrete peer.
func TestWireAdaptersLabelEveryNameWithItsFamily(t *testing.T) {
	f := family(map[string]string{
		"model.json":    fixtureModel,
		"protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"request": "Input", "result": "Result"}}, "events": {"changed": {"type": "Result"}}}, "client": {"methods": {"back": {"result": "Result"}}, "events": {"noticed": {"type": "Result"}}}`),
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	const labels = `{ ...adapter.options, families: { ...adapter.options.families, [encodePath(["run"])]: "x", [encodePath(["back"])]: "x", [encodePath(["changed"])]: "x", [encodePath(["noticed"])]: "x" } }`
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/src/index.ts") {
			continue
		}
		index := string(file.Data)
		if got := strings.Count(index, "wirePair("+labels+")"); got != 1 {
			t.Errorf("%s labels %d local pairs, want one:\n%s", file.Path, got, index)
		}
		if strings.Count(index, "wirePair(") != 1 {
			t.Errorf("%s creates extra or unlabelled local wires", file.Path)
		}
		if strings.Contains(index, "new DuplexPeer") {
			t.Errorf("%s creates a concrete peer", file.Path)
		}
	}
}
