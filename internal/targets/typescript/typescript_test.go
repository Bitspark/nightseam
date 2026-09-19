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
			"session.json":  `{}`,
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
		"thenable client method":           {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "typescript.json": `{"names": {"run": "then"}}`}, "reserved_name", "typescript.json#/names/run"},
		"a method named then":              {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"server": {"methods": {"then": {"result": "string"}}}`)}, "reserved_name", "protocol.json#/server/methods/then"},
		"event helper collision":           {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "typescript.json": `{"names": {"run": "onChanged"}}`}, "generated_name_collision", "protocol.json#/server/events/changed"},
		"constructor collision":            {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "typescript.json": `{"names": {"run": "constructor"}}`}, "reserved_name", "typescript.json#/names/run"},
		"TypeScript operation collision":   {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"result": "string"}, "other": {"result": "string"}}}`), "typescript.json": `{"names": {"other": "run"}}`}, "generated_name_collision", "protocol.json#/server/methods/run"},
		"reserved type":                    {map[string]string{"model.json": m(`"Client": {"kind": "record", "fields": []}`)}, "reserved_name", "model.json#/types/Client"},
		"global type":                      {map[string]string{"model.json": m(`"Array": {"kind": "alias", "type": "string"}`)}, "reserved_name", "model.json#/types/Array"},
		"error member collision":           {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"errors": {"not-found": "", "not_found": ""}`)}, "generated_name_collision", "protocol.json#/errors/not_found"},
		"a name that is not an identifier": {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "typescript.json": `{"names": {"run": "run it"}}`}, "invalid_name", "typescript.json#/names/run"},
		"a parameter named as a type":      {map[string]string{"model.json": m(`"S": {"kind": "enum", "values": ["v"]}`), "protocol.json": modeltest.Protocol(`"parameters": [{"name": "S", "of": "session"}], "types": {"F": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "generated_name_collision", "protocol.json#/parameters/0/name"},
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

// TestRenderPlacesTheClient: the four files land where the layout says,
// the client class is spelled, the manifest is named under the scope, and
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
	if got := strings.Join(paths, " "); got != "api/ts/x-client/src/types.ts api/ts/x-client/src/index.ts api/ts/x-client/package.json api/ts/x-client/tsconfig.json" {
		t.Fatalf("placed at %s", got)
	}
	index := string(files[1].Data)
	if !strings.HasPrefix(index, "// Code generated by nightseam. DO NOT EDIT.\n") || !strings.Contains(index, "async run(params: Protocol.Input, options?: CallOptions): Promise<Protocol.Result>") {
		t.Fatalf("the client is not spelled:\n%s", index)
	}
	if !strings.Contains(string(files[2].Data), `"name":"@example/x-client"`) || !strings.Contains(string(files[2].Data), `"@nightseam/runtime":"`+DefaultRuntimeVersion+`"`) {
		t.Fatalf("the manifest is wrong: %s", files[2].Data)
	}
	for _, bad := range []Config{{}, {Scope: "example"}, {Scope: "@example", Runtime: "bad name!"}, {Scope: "@example", Layout: "no-family"}, {Scope: "@example", Layout: "../{family}"}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("config %+v was accepted", bad)
		}
	}
	placed, err := New(Config{Scope: "@example", Place: map[string]string{"x": "gen/ts/{family}"}}).Render(f)
	if err != nil || placed[0].Path != "gen/ts/x/src/types.ts" {
		t.Fatalf("placement is not honoured: %v", err)
	}
}

// TestClientLabelsEveryNameWithItsFamily: where the client makes the peer —
// dial and attach; open resolves the handle and attaches — it labels every
// method and event of the family, both sides, with the family's name,
// beside whatever the caller labelled, and declares nothing at the module
// to do it.
func TestClientLabelsEveryNameWithItsFamily(t *testing.T) {
	f := family(map[string]string{
		"model.json":    fixtureModel,
		"protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"request": "Input", "result": "Result"}}, "events": {"changed": {"type": "Result"}}}, "client": {"methods": {"back": {"result": "Result"}}, "events": {"noticed": {"type": "Result"}}}`),
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	index := string(files[1].Data)
	const labels = `{ ...options, families: { ...options.families, "run": "x", "back": "x", "changed": "x", "noticed": "x" } }`
	if got := strings.Count(index, "new DuplexPeer("+labels+")"); got != 2 {
		t.Errorf("the labels are passed to %d of the two peers the client makes:\n%s", got, index)
	}
	if strings.Contains(index, "new DuplexPeer(options)") {
		t.Error("a peer is made with options carrying no labels")
	}
}
