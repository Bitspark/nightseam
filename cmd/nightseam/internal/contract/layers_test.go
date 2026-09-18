package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func layerFiles(t *testing.T) map[string]map[string]any {
	t.Helper()
	parse := func(text string) map[string]any {
		var value map[string]any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	return map[string]map[string]any{
		LayerDTO: parse(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"example","layer":"dto","imports":["records"],
			"types":{"Input":{"kind":"record","fields":[{"name":"message","type":"string"}]},"Result":{"kind":"record","fields":[{"name":"ok","type":"boolean"}]}}}`),
		LayerRPC: parse(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"example","layer":"rpc",
			"types":{"Frame":{"kind":"record","fields":[{"name":"message","type":{"envelope":"probe"}}]}},
			"methods":[{"name":"example.run","go_name":"Run","ts_name":"run","direction":"client_to_server","request":"Input","result":"Result"},
			           {"name":"example.ask","go_name":"Ask","ts_name":"ask","direction":"server_to_client","request":"Input","result":"Result"}],
			"events":[{"name":"example.changed","go_name":"Changed","ts_name":"changed","direction":"server_to_client","type":"Result"}],"errors":[]}`),
		LayerSess: parse(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"example","layer":"sess",
			"types":{"Attachment":{"kind":"record","fields":[{"name":"connection","type":{"connection":"probe"}}]}},
			"session":{"decides":["example.run"],"asks":["example.ask"],"conversation":{"event":"example.changed","path":"id"}}}`),
	}
}

// TestMergeJoinsLayers: the layer files of a family merge into one contract
// that records each type's layer, carries the rpc layer's operations and
// the sess layer's session, and is a session family for having a sess.
func TestMergeJoinsLayers(t *testing.T) {
	merged, diagnostics := Merge(layerFiles(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	api, problems := Parse(merged)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if api.Role != SessionRole || api.Layer != "" || len(api.Methods) != 2 || len(api.Events) != 1 || api.Session == nil {
		t.Fatalf("merged contract is not whole: %+v", api)
	}
	for name, layer := range map[string]string{"Input": LayerDTO, "Result": LayerDTO, "Frame": LayerRPC, "Attachment": LayerSess} {
		if api.Layers[name] != layer {
			t.Errorf("%s is recorded in %q, not %s", name, api.Layers[name], layer)
		}
	}
	if len(api.Imports) != 1 || api.Imports[0] != "records" {
		t.Errorf("imports were not merged: %v", api.Imports)
	}
	api.Families = []string{"example", "probe", "records"}
	api.Sessions = []string{"probe"}
	api.Imported = map[string]API{"records": probeFixture(t, "")}
	for _, d := range Check(api) {
		if d.Code != "unsupported_slot" {
			t.Errorf("a well-layered family was reported: %+v", d)
		}
	}
}

// TestMergeRefusesWhatIsNotALayerFile: the wrong layer, another family, a
// section a layer does not carry, a type declared twice.
func TestMergeRefusesWhatIsNotALayerFile(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(files map[string]map[string]any)
	}{
		{"wrong layer", "wrong_layer", func(f map[string]map[string]any) { f[LayerDTO]["layer"] = "rpc" }},
		{"another family", "wrong_family", func(f map[string]map[string]any) { f[LayerRPC]["name"] = "other" }},
		{"operations in dto", "wrong_section", func(f map[string]map[string]any) { f[LayerDTO]["methods"] = []any{} }},
		{"session in rpc", "wrong_section", func(f map[string]map[string]any) { f[LayerRPC]["session"] = map[string]any{} }},
		{"unknown section", "unknown_section", func(f map[string]map[string]any) { f[LayerSess]["typo"] = true }},
		{"type declared twice", "duplicate_type", func(f map[string]map[string]any) {
			f[LayerRPC]["types"].(map[string]any)["Input"] = map[string]any{"kind": "alias", "type": "string"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := layerFiles(t)
			tc.mutate(files)
			_, diagnostics := Merge(files)
			for _, d := range diagnostics {
				if d.Code == tc.code {
					return
				}
			}
			t.Fatalf("expected %s, got %+v", tc.code, diagnostics)
		})
	}
}

// TestDirectionRuleHoldsPerDeclaration: a declaration refers to its own
// layer or a lower one. Each case moves one reference upward and expects
// the violation at that reference; the last cases are the permitted ones.
func TestDirectionRuleHoldsPerDeclaration(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(files map[string]map[string]any)
		pointer string
	}{
		{"dto refers to rpc", func(f map[string]map[string]any) {
			f[LayerDTO]["types"].(map[string]any)["Input"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = "Frame"
		}, "/types/Input/fields/0/type"},
		{"rpc result refers to sess", func(f map[string]map[string]any) {
			f[LayerRPC]["methods"].([]any)[0].(map[string]any)["result"] = "Attachment"
		}, "/methods/0/result"},
		{"event refers to sess", func(f map[string]map[string]any) {
			f[LayerRPC]["events"].([]any)[0].(map[string]any)["type"] = "Attachment"
		}, "/events/0/type"},
		{"request declared in sess", func(f map[string]map[string]any) {
			f[LayerRPC]["methods"].([]any)[0].(map[string]any)["request"] = "Attachment"
		}, "/methods/0/request"},
		{"envelope slot in dto", func(f map[string]map[string]any) {
			f[LayerDTO]["types"].(map[string]any)["Input"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = map[string]any{"envelope": "probe"}
		}, "/types/Input/fields/0/type"},
		{"connection slot in rpc", func(f map[string]map[string]any) {
			f[LayerRPC]["types"].(map[string]any)["Frame"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = map[string]any{"connection": "probe"}
		}, "/types/Frame/fields/0/type"},
		{"imported sess type in dto", func(f map[string]map[string]any) {
			f[LayerDTO]["types"].(map[string]any)["Input"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = "records.Session"
		}, "/types/Input/fields/0/type"},
	}
	imported := probeFixture(t, "")
	imported.Types["Session"] = Type{Kind: "record", Fields: []Field{{Name: "id", Type: "string", Required: true}}}
	imported.Layers = map[string]string{"Session": LayerSess, "Input": LayerDTO, "Result": LayerDTO}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := layerFiles(t)
			tc.mutate(files)
			merged, diagnostics := Merge(files)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			api, problems := Parse(merged)
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			api.Families = []string{"example", "probe", "records"}
			api.Sessions = []string{"probe"}
			api.Imported = map[string]API{"records": imported}
			for _, d := range Check(api) {
				if d.Code == "layer_violation" && d.Pointer == tc.pointer {
					return
				}
			}
			t.Fatalf("no layer_violation at %s: %+v", tc.pointer, Check(api))
		})
	}
	// Permitted: an rpc type holding an envelope slot, a sess type holding a
	// connection slot and an rpc type, an rpc operation over dto types.
	merged, _ := Merge(layerFiles(t))
	api, _ := Parse(merged)
	api.Families = []string{"example", "probe", "records"}
	api.Sessions = []string{"probe"}
	api.Imported = map[string]API{"records": imported}
	for _, d := range Check(api) {
		if d.Code == "layer_violation" {
			t.Errorf("a permitted reference was refused: %+v", d)
		}
	}
	// A contract merged by hand, with no record of layers, has no direction.
	delete(merged, "layers")
	api, _ = Parse(merged)
	api.Families = []string{"example", "probe", "records"}
	api.Sessions = []string{"probe"}
	api.Imported = map[string]API{"records": imported}
	for _, d := range Check(api) {
		if d.Code == "layer_violation" {
			t.Errorf("an unlayered contract was held to a direction: %+v", d)
		}
	}
}

// TestSessionNamesItsOperations: decides and asks name the family's methods,
// asks the server's, and the conversation arrives in one of its events.
func TestSessionNamesItsOperations(t *testing.T) {
	cases := []struct {
		name, code, pointer string
		mutate              func(session map[string]any)
	}{
		{"decides an unknown method", "unknown_operation", "/session/decides/0", func(s map[string]any) { s["decides"] = []any{"example.nope"} }},
		{"asks an unknown method", "unknown_operation", "/session/asks/0", func(s map[string]any) { s["asks"] = []any{"example.nope"} }},
		{"asks a client method", "invalid_direction", "/session/asks/0", func(s map[string]any) { s["asks"] = []any{"example.run"} }},
		{"conversation in no event", "unknown_operation", "/session/conversation/event", func(s map[string]any) {
			s["conversation"] = map[string]any{"event": "example.nope", "path": "id"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := layerFiles(t)
			tc.mutate(files[LayerSess]["session"].(map[string]any))
			merged, _ := Merge(files)
			api, problems := Parse(merged)
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			api.Families = []string{"example", "probe"}
			api.Sessions = []string{"probe"}
			for _, d := range Check(api) {
				if d.Code == tc.code && d.Pointer == tc.pointer {
					return
				}
			}
			t.Fatalf("expected %s at %s, got %+v", tc.code, tc.pointer, Check(api))
		})
	}
	if !strings.Contains(string(EmbeddedSchema()), `"session"`) {
		t.Fatal("the schema does not know the session section")
	}
}
