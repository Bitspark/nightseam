package golang

import (
	"encoding/json"
	"go/format"
	"strings"
	"testing"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
)

func contractFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"example","types":{"Input":{"kind":"record","fields":[{"name":"message","type":"string"},{"name":"count","type":"integer","required":false,"nullable":true}]},"Result":{"kind":"record","fields":[{"name":"ok","type":"boolean"}]}},"methods":[{"name":"example.run","go_name":"Run","ts_name":"run","direction":"client_to_server","request":"Input","result":"Result"}],"events":[{"name":"example.changed","go_name":"Changed","ts_name":"changed","direction":"server_to_client","type":"Result"}]}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fields(v map[string]any, name string) []any {
	return v["types"].(map[string]any)[name].(map[string]any)["fields"].([]any)
}

// check parses a contract the schema accepts and returns what Go refuses in it.
func check(t *testing.T, v map[string]any) []contract.Diagnostic {
	t.Helper()
	api, diagnostics := contract.Parse(v)
	if len(diagnostics) != 0 {
		t.Fatalf("schema refused the fixture: %+v", diagnostics)
	}
	return New(Options{}).Check(api)
}

// TestCheckRejectsWhatGoCannotGenerate breaks the fixture one way at a time
// and expects the Go language to name it, at the pointer of the name.
func TestCheckRejectsWhatGoCannotGenerate(t *testing.T) {
	tests := []struct {
		name, code, pointer string
		mutate              func(map[string]any)
	}{
		{"Go field collision", "generated_name_collision", "/types/Input/fields/1/go_name", func(v map[string]any) { fields(v, "Input")[1].(map[string]any)["go_name"] = "Message" }},
		{"reserved codec field", "reserved_name", "/types/Input/fields/0/go_name", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["go_name"] = "MarshalJSON" }},
		{"unexported default name", "invalid_name", "/types/Input/fields/0/go_name", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["name"] = "_" }},
		{"extension storage field", "reserved_name", "/types/Input/fields/0/go_name", func(v map[string]any) {
			v["types"].(map[string]any)["Input"].(map[string]any)["open"] = true
			fields(v, "Input")[0].(map[string]any)["name"] = "additional_fields"
		}},
		{"reserved method", "reserved_name", "/methods/0/go_name", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "Close" }},
		{"event helper collision", "generated_name_collision", "/events/0/go_name", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "OnChanged" }},
		{"peer field collision", "generated_name_collision", "/methods/0/go_name", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "Peer" }},
		{"Go operation collision", "operation_collision", "/methods/1/go_name", func(v map[string]any) {
			v["methods"] = append(v["methods"].([]any), map[string]any{"name": "other", "go_name": "Run", "ts_name": "other", "direction": "client_to_server", "result": "Result"})
		}},
		{"reserved type", "reserved_name", "/types/Client", func(v map[string]any) {
			v["types"].(map[string]any)["Client"] = map[string]any{"kind": "record", "fields": []any{}}
		}},
		{"enum generated collision", "generated_name_collision", "/types/Status/values/1", func(v map[string]any) {
			v["types"].(map[string]any)["Status"] = map[string]any{"kind": "enum", "values": []any{"in-progress", "in_progress"}}
		}},
		{"enum type collision", "generated_name_collision", "/types/Status/values/0", func(v map[string]any) {
			v["types"].(map[string]any)["Status"] = map[string]any{"kind": "enum", "values": []any{""}}
		}},
		{"enum constant reserved", "reserved_name", "/types/Validate/values/0", func(v map[string]any) {
			v["types"].(map[string]any)["Validate"] = map[string]any{"kind": "enum", "values": []any{"raw"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := contractFixture(t)
			tt.mutate(v)
			diagnostics := check(t, v)
			for _, d := range diagnostics {
				if d.Code == tt.code && d.Pointer == tt.pointer {
					return
				}
			}
			t.Fatalf("expected %s at %s, got %+v", tt.code, tt.pointer, diagnostics)
		})
	}
	if diagnostics := check(t, contractFixture(t)); len(diagnostics) != 0 {
		t.Fatalf("the fixture is refused: %+v", diagnostics)
	}
}

// TestCheckSurvivesWhatTheContractRefuses: the kernel runs every language's
// Check alongside the contract's, so a cycle or an unknown parent must not
// stop Go from reporting its own findings.
func TestCheckSurvivesWhatTheContractRefuses(t *testing.T) {
	v := contractFixture(t)
	types := v["types"].(map[string]any)
	types["Input"].(map[string]any)["extends"] = []any{"Input", "Missing"}
	fields(v, "Input")[0].(map[string]any)["go_name"] = "MarshalJSON"
	found := false
	for _, d := range check(t, v) {
		found = found || d.Code == "reserved_name"
	}
	if !found {
		t.Fatal("a cyclic contract hid the Go diagnostics")
	}
}

func TestNaming(t *testing.T) {
	if DefaultGoName("work_item_id") != "WorkItemID" {
		t.Fatal("unexpected field name conversion")
	}
	for value, want := range map[string]string{"": "Status", "in-progress": "StatusInProgress", "context.example": "StatusContextExample", "id": "StatusID", "Api_Key": "StatusAPIKey", "9lives": "Status9lives", "ready": "StatusReady"} {
		if got := enumConstantName("Status", value); got != want {
			t.Errorf("enumConstantName(Status, %q) = %q, want %q", value, got, want)
		}
	}
}

// TestRenderPlacesAndFormats renders the fixture alone, as Go, and holds the
// package layout, the gofmt-cleanliness of every file and the option rules.
func TestRenderPlacesAndFormats(t *testing.T) {
	api, diagnostics := contract.Parse(contractFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	files, err := New(Options{}).Render(api)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
		formatted, err := format.Source(file.Data)
		if err != nil {
			t.Fatalf("%s does not parse: %v", file.Path, err)
		}
		if string(formatted) != string(file.Data) {
			t.Errorf("%s is not gofmt-clean", file.Path)
		}
		if !strings.HasPrefix(string(file.Data), "// Code generated by dev websocket-api/1. DO NOT EDIT.\n") {
			t.Errorf("%s lacks the generated header", file.Path)
		}
	}
	if strings.Join(paths, " ") != "api/go/example-protocol/types_generated.go api/go/example-protocol/validation_generated.go api/go/example-binding/binding_generated.go api/go/example-client/client_generated.go" {
		t.Fatalf("unexpected layout: %v", paths)
	}
	if !strings.Contains(string(files[2].Data), `"github.com/Bitspark/nighthall/api/go/example-protocol"`) {
		t.Fatal("binding does not import the protocol package at the default module")
	}
	for _, options := range []Options{{ClientPath: "../outside"}, {Module: "/abs"}, {ProtocolPath: "api/go/x", BindingPath: "api/go/x"}, {ClientPath: "api/go/x/"}, {BindingPath: "api/go/x:y"}} {
		if _, err := New(options).Render(api); err == nil {
			t.Errorf("accepted options %+v", options)
		}
	}
	files, err = New(Options{Module: "example.test/generated", ProtocolPath: "gen/protocol", BindingPath: "gen/binding", ClientPath: "gen/client"}).Render(api)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Path != "gen/protocol/types_generated.go" || !strings.Contains(string(files[3].Data), `"example.test/generated/gen/protocol"`) {
		t.Fatalf("options were not honoured: %s\n%s", files[0].Path, files[3].Data)
	}
}
