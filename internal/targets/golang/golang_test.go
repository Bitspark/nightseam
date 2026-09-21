package golang

import (
	"go/format"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// family builds one family from inline tier files, with a protocol family
// beside it for a parameter to bind, and renders it for the target.
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
	"Input": {"kind": "record", "fields": [{"name": "text", "type": "string"}, {"name": "message", "type": "string"}]},
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
	return New(Config{Module: "example.test/m"}).Check(family(files))
}

func has(diagnostics []diag.Diagnostic, code, at string) bool {
	for _, d := range diagnostics {
		if d.Code == code && d.At().String() == at {
			return true
		}
	}
	return false
}

// TestCheckRejectsWhatGoCannotGenerate breaks the fixture one way at a time
// and expects the target to name it, at the declaration or the override at
// fault.
func TestCheckRejectsWhatGoCannotGenerate(t *testing.T) {
	m := func(types string) string { return `{"nightseam": 2, "types": {` + types + `}}` }
	for name, c := range map[string]struct {
		files    map[string]string
		code, at string
	}{
		"Go field collision":              {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "go.json": `{"names": {"Input.message": "Text"}}`}, "generated_name_collision", "go.json#/names/Input.message"},
		"reserved codec field":            {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "go.json": `{"names": {"Input.text": "MarshalJSON"}}`}, "reserved_name", "go.json#/names/Input.text"},
		"unexported override":             {map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``), "go.json": `{"names": {"Input.text": "text"}}`}, "invalid_name", "go.json#/names/Input.text"},
		"extension storage field":         {map[string]string{"model.json": m(`"Input": {"kind": "record", "open": true, "fields": [{"name": "additional_fields", "type": "string"}]}`)}, "reserved_name", "model.json#/types/Input/fields/0"},
		"model declaration collision":     {map[string]string{"model.json": m(`"ServerModel": {"kind": "record", "fields": []}`), "protocol.json": modeltest.Protocol(``)}, "reserved_name", "model.json#/types/ServerModel"},
		"event facet collision":           {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"server":{"events":{"changed":{"type":"Result"},"other":{"type":"Result"}}}`), "go.json": `{"names":{"other":"Changed"}}`}, "generated_name_collision", "go.json#/names/other"},
		"Go operation collision":          {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"result": "string"}, "other": {"result": "string"}}}`), "go.json": `{"names": {"other": "Run"}}`}, "generated_name_collision", "protocol.json#/server/methods/run"},
		"reserved type":                   {map[string]string{"model.json": m(`"Tag": {"kind": "record", "fields": []}`)}, "reserved_name", "model.json#/types/Tag"},
		"a type that is a keyword":        {map[string]string{"model.json": m(`"Func": {"kind": "record", "fields": []}`), "go.json": `{"names": {"Func": "func"}}`}, "invalid_name", "go.json#/names/Func"},
		"enum generated collision":        {map[string]string{"model.json": m(`"Status": {"kind": "enum", "values": ["in-progress", "in_progress"]}`)}, "generated_name_collision", "model.json#/types/Status/values/1"},
		"enum type collision":             {map[string]string{"model.json": m(`"Status": {"kind": "enum", "values": [""]}`)}, "generated_name_collision", "model.json#/types/Status/values/0"},
		"enum constant reserved":          {map[string]string{"model.json": m(`"Validate": {"kind": "enum", "values": ["raw"]}`)}, "reserved_name", "model.json#/types/Validate/values/0"},
		"error constant collision":        {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"errors": {"not-found": "", "not_found": ""}`)}, "generated_name_collision", "protocol.json#/errors/not_found"},
		"error constant is a type":        {map[string]string{"model.json": m(`"ErrorDenied": {"kind": "record", "fields": []}`), "protocol.json": modeltest.Protocol(`"errors": {"denied": ""}`)}, "generated_name_collision", "protocol.json#/errors/denied"},
		"error code with no identifier":   {map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(`"errors": {"---": ""}`)}, "invalid_name", "protocol.json#/errors/---"},
		"a type parameter that is a type": {map[string]string{"model.json": m(`"SEnvelope": {"kind": "record", "fields": []}`), "protocol.json": modeltest.Protocol(`"parameters": [{"name": "S", "of": "protocol"}], "types": {"Frame": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "generated_name_collision", "protocol.json#/parameters/0/name"},
		"a diamond in Go":                 {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "x", "type": "string"}]}, "B": {"kind": "record", "extends": ["A"], "fields": []}, "C": {"kind": "record", "extends": ["A"], "fields": []}, "D": {"kind": "record", "extends": ["B", "C"], "fields": []}`)}, "generated_name_collision", "model.json#/types/A/fields/0"},
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

// TestNaming: the convention derives Go names from declared names, and an
// override replaces it where the file says so.
func TestNaming(t *testing.T) {
	f := family(map[string]string{
		"model.json":    `{"nightseam": 2, "types": {"Item": {"kind": "record", "fields": [{"name": "work_item_id", "type": "string"}, {"name": "url", "type": "string"}]}, "Status": {"kind": "enum", "values": ["context.example", "done"]}}}`,
		"protocol.json": modeltest.Protocol(`"server": {"methods": {"no_args": {"result": "string"}}, "events": {"frame.relayed": {"type": "string"}}}, "errors": {"not_found": ""}`),
		"go.json":       `{"names": {"Item.url": "Link", "no_args": "Nothing"}}`,
	})
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for got, want := range map[string]string{
		p.fields["Item.work_item_id"]:         "WorkItemID",
		p.fields["Item.url"]:                  "Link",
		p.constants["Status.context.example"]: "StatusContextExample",
		p.constants["Status.done"]:            "StatusDone",
		p.operations["no_args"]:               "Nothing",
		p.operations["frame.relayed"]:         "FrameRelayed",
		p.errors["not_found"]:                 "ErrorNotFound",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// TestRenderPlacesAndFormats: the four files land where the layout says,
// gofmt has nothing to change in them, each opens with the header, the
// binding imports the protocol package at the module, and a config the
// target cannot place with is refused.
func TestRenderPlacesAndFormats(t *testing.T) {
	f := family(map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``)})
	files, err := New(Config{Module: "example.test/m"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
		formatted, err := format.Source(file.Data)
		if err != nil {
			t.Fatalf("%s: %v", file.Path, err)
		}
		if string(formatted) != string(file.Data) {
			t.Errorf("%s is not gofmt-clean", file.Path)
		}
		if !strings.HasPrefix(string(file.Data), "// Code generated by nightseam. DO NOT EDIT.\n") {
			t.Errorf("%s lacks the header", file.Path)
		}
	}
	if got := strings.Join(paths, " "); got != "api/go/x-protocol/types_generated.go api/go/x-protocol/validation_generated.go api/go/x-binding/binding_generated.go api/go/x-client/client_generated.go" {
		t.Fatalf("placed at %s", got)
	}
	if !strings.Contains(string(files[2].Data), `protocol "example.test/m/api/go/x-protocol"`) {
		t.Fatal("the binding does not import the protocol package at the module")
	}
	for _, bad := range []Config{{}, {Module: "bad path"}, {Module: "m", Runtime: "/abs"}, {Module: "m", Layout: Layout{Protocol: "no-family"}}, {Module: "m", Layout: Layout{Protocol: "../{family}"}}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("config %+v was accepted", bad)
		}
	}
	placed, err := New(Config{Module: "example.test/m", Place: map[string]Layout{"x": {Protocol: "gen/{family}/p", Binding: "gen/{family}/b", Client: "gen/{family}/c"}}}).Render(f)
	if err != nil || placed[0].Path != "gen/x/p/types_generated.go" || placed[3].Path != "gen/x/c/client_generated.go" {
		t.Fatalf("placement is not honoured: %v %v", err, placed)
	}
}

// bothSides is a family with a method and an event on each side, which is
// what a label map must cover: a peer observes the names it sends as well
// as the names it receives.
func bothSides() *render.Family {
	return family(map[string]string{
		"model.json":    fixtureModel,
		"protocol.json": modeltest.Protocol(`"server": {"methods": {"run": {"request": "Input", "result": "Result"}}, "events": {"changed": {"type": "Result"}}}, "client": {"methods": {"back": {"result": "Result"}}, "events": {"noticed": {"type": "Result"}}}`),
	})
}

// Each ToWire labels the encoded paths of both sides with the family's name,
// retaining whatever the host already labelled.
func TestToWireLabelsEveryNameWithItsFamily(t *testing.T) {
	files, err := New(Config{Module: "example.test/m"}).Render(bothSides())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files[2:] {
		source := string(file.Data)
		for _, want := range []string{
			"families := map[string]string{}",
			"for name, existing := range options.Families {",
			`families["3:run"] = "x"`,
			`families["4:back"] = "x"`,
			`families["7:changed"] = "x"`,
			`families["7:noticed"] = "x"`,
			"options.Families = families",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s does not label: %s", file.Path, want)
			}
		}
	}
}

// TestReservedNamesAreWhatTheTargetEmits: every identifier the generated
// packages declare of themselves is reserved, and nothing else is.
func TestReservedNamesAreWhatTheTargetEmits(t *testing.T) {
	reserved := Reserved()
	for _, ident := range []string{"Tag", "Server", "Client", "ServerModel", "ClientMethods", "ServerEvents", "ToWire", "FromWire", "IsError"} {
		found := false
		for _, r := range reserved {
			found = found || r == ident
		}
		if !found {
			t.Errorf("%s is emitted and not reserved", ident)
		}
	}
	for _, free := range []string{"Handler", "Dial", "Attach", "Open", "Serve", "Remote", "Peer", "Close", "API", "Optional", "Call", "Notify"} {
		for _, r := range reserved {
			if r == free {
				t.Errorf("%s is reserved and never emitted", free)
			}
		}
	}
}
