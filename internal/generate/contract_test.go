package generate

import (
	"encoding/json"
	"strings"
	"testing"
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

func TestContractDefaultsAndEmbeddedSchema(t *testing.T) {
	api, diagnostics := Parse(contractFixture(t))
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	fields := api.Types["Input"].Fields
	if !fields[0].Required || fields[1].Required || !fields[1].Nullable {
		t.Fatalf("wrong presence defaults: %+v", fields)
	}
	first := EmbeddedSchema()
	first[0] = 'x'
	if !json.Valid(EmbeddedSchema()) {
		t.Fatal("caller mutated embedded schema")
	}
	if DefaultGoName("work_item_id") != "WorkItemID" {
		t.Fatal("unexpected field name conversion")
	}
}

func TestContractValidationRejectsUnsupportedContracts(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"unknown top field", "schema_validation", func(v map[string]any) { v["typo"] = true }},
		{"unknown nested field", "schema_validation", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["typo"] = true }},
		{"unresolved reference", "unresolved_type", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["type"] = "Missing" }},
		{"recursive array", "cyclic_type", func(v map[string]any) {
			fields(v, "Input")[0].(map[string]any)["type"] = map[string]any{"array": "Input"}
		}},
		{"primitive request", "invalid_request", func(v map[string]any) {
			v["types"].(map[string]any)["Input"] = map[string]any{"kind": "alias", "type": "string"}
		}},
		{"wire field collision", "field_collision", func(v map[string]any) { fields(v, "Input")[1].(map[string]any)["name"] = "message" }},
		{"Go field collision", "generated_name_collision", func(v map[string]any) { fields(v, "Input")[1].(map[string]any)["go_name"] = "Message" }},
		{"reserved codec field", "reserved_name", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["go_name"] = "MarshalJSON" }},
		{"invalid direction", "schema_validation", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["direction"] = "both" }},
		{"reserved method", "reserved_name", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "Close" }},
		{"thenable client method", "reserved_name", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["ts_name"] = "then" }},
		{"event helper collision", "generated_name_collision", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "OnChanged" }},
		{"peer field collision", "generated_name_collision", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["go_name"] = "Peer" }},
		{"TypeScript event helper collision", "generated_name_collision", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["ts_name"] = "onChanged" }},
		{"reserved type", "reserved_name", func(v map[string]any) {
			v["types"].(map[string]any)["Client"] = map[string]any{"kind": "record", "fields": []any{}}
		}},
		{"enum duplicate", "schema_validation", func(v map[string]any) {
			v["types"].(map[string]any)["Status"] = map[string]any{"kind": "enum", "values": []any{"a", "a"}}
		}},
		{"enum generated collision", "generated_name_collision", func(v map[string]any) {
			v["types"].(map[string]any)["Status"] = map[string]any{"kind": "enum", "values": []any{"in-progress", "in_progress"}}
		}},
		{"enum type collision", "generated_name_collision", func(v map[string]any) {
			v["types"].(map[string]any)["Status"] = map[string]any{"kind": "enum", "values": []any{""}}
		}},
		{"ambiguous expression", "schema_validation", func(v map[string]any) {
			fields(v, "Input")[0].(map[string]any)["type"] = map[string]any{"array": "string", "map": "string"}
		}},
		{"non-finite JSON", "invalid_json", func(v map[string]any) { v["schema_version"] = json.Number("1.2.3") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := contractFixture(t)
			tt.mutate(v)
			diagnostics := Validate(v)
			for _, d := range diagnostics {
				if d.Code == tt.code {
					return
				}
			}
			t.Fatalf("expected %s, got %+v", tt.code, diagnostics)
		})
	}
}

func fields(v map[string]any, name string) []any {
	return v["types"].(map[string]any)[name].(map[string]any)["fields"].([]any)
}

func TestContractInheritanceAndAliases(t *testing.T) {
	v := contractFixture(t)
	types := v["types"].(map[string]any)
	types["Extended"] = map[string]any{"kind": "record", "extends": []any{"Input"}, "fields": []any{map[string]any{"name": "additional", "type": map[string]any{"map": "Result"}}}}
	types["ExtendedInput"] = map[string]any{"kind": "alias", "type": "Extended"}
	if diagnostics := Validate(v); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	types["Extended"].(map[string]any)["fields"] = []any{map[string]any{"name": "message", "type": "string"}}
	if diagnostics := Validate(v); len(diagnostics) == 0 {
		t.Fatal("accepted inherited collision")
	}
	types["Extended"].(map[string]any)["extends"] = []any{"Result", "Input"}
	types["Extended"].(map[string]any)["fields"] = []any{}
	types["Input"].(map[string]any)["extends"] = []any{"Result"}
	if diagnostics := Validate(v); len(diagnostics) == 0 {
		t.Fatal("accepted diamond field duplication")
	}
}

func TestContractDescriptions(t *testing.T) {
	v := contractFixture(t)
	types := v["types"].(map[string]any)
	types["Input"].(map[string]any)["description"] = "Input fields."
	fields(v, "Input")[0].(map[string]any)["description"] = "Message to send."
	v["methods"].([]any)[0].(map[string]any)["description"] = "Run the operation."
	v["events"].([]any)[0].(map[string]any)["description"] = "Announce changes."
	types["Status"] = map[string]any{"kind": "enum", "values": []any{"ready"}, "description": "Possible statuses."}
	types["Message"] = map[string]any{"kind": "alias", "type": "string", "description": "A message."}
	api, diagnostics := Parse(v)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if api.Types["Input"].Description != "Input fields." || api.Types["Input"].Fields[0].Description != "Message to send." || api.Methods[0].Description != "Run the operation." || api.Events[0].Description != "Announce changes." || api.Types["Status"].Description != "Possible statuses." || api.Types["Message"].Description != "A message." {
		t.Fatalf("descriptions were not preserved: %+v", api)
	}
	baseline, diagnostics := Parse(contractFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	data, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"description"`) {
		t.Fatalf("omitted descriptions were introduced into canonical contract: %s", data)
	}
	fields(v, "Input")[0].(map[string]any)["description"] = false
	if diagnostics := Validate(v); len(diagnostics) == 0 {
		t.Fatal("non-string description accepted")
	}
}

func TestContractRejectsRemovedDialect(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"removed profile", func(v map[string]any) { v["profile"] = "workbench-v1" }},
		{"raw JSON type", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["type"] = "raw_json" }},
		{"native int type", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["type"] = "int" }},
		{"native int64 type", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["type"] = "int64" }},
		{"native pointer hint", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["go_pointer"] = false }},
		{"omit empty hint", func(v map[string]any) { fields(v, "Input")[0].(map[string]any)["omit_empty"] = false }},
		{"no params hint", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["legacy_no_params"] = false }},
		{"subscription hint", func(v map[string]any) { v["methods"].([]any)[0].(map[string]any)["legacy_subscription"] = false }},
		{"TypeScript only aliases", func(v map[string]any) { v["ts_aliases"] = map[string]any{"ExtraInput": "Input"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := contractFixture(t)
			test.mutate(v)
			diagnostics := Validate(v)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "schema_validation" {
					return
				}
			}
			t.Fatalf("expected schema rejection, got %+v", diagnostics)
		})
	}
	for _, direction := range []string{"client_to_server", "server_to_client"} {
		v := contractFixture(t)
		method := v["methods"].([]any)[0].(map[string]any)
		delete(method, "request")
		method["direction"] = direction
		if diagnostics := Validate(v); len(diagnostics) != 0 {
			t.Fatalf("parameterless duplex method rejected: %+v", diagnostics)
		}
	}
}

func TestContractDiagnosticsDeterministicAndLocated(t *testing.T) {
	v := contractFixture(t)
	fields(v, "Input")[0].(map[string]any)["unknown"] = true
	fields(v, "Result")[0].(map[string]any)["unknown"] = true
	a, _ := json.Marshal(Validate(v))
	b, _ := json.Marshal(Validate(v))
	if string(a) != string(b) {
		t.Fatalf("nondeterministic diagnostics: %s versus %s", a, b)
	}
	if !strings.Contains(string(a), "/types/Input") || !strings.Contains(string(a), "/types/Result") {
		t.Fatalf("missing diagnostic paths: %s", a)
	}
}

func TestContractOperationNamespace(t *testing.T) {
	v := contractFixture(t)
	m := v["methods"].([]any)[0].(map[string]any)
	v["methods"] = append(v["methods"].([]any), map[string]any{"name": m["name"], "go_name": m["go_name"], "ts_name": m["ts_name"], "direction": "server_to_client", "result": "Result"})
	if diagnostics := Validate(v); len(diagnostics) != 0 {
		t.Fatal("opposite directions should have independent operation namespaces", diagnostics)
	}
	v["methods"].([]any)[1].(map[string]any)["direction"] = "client_to_server"
	if diagnostics := Validate(v); len(diagnostics) == 0 {
		t.Fatal("duplicate operation accepted")
	}
}
