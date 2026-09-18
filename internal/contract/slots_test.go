package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// carrierFixture is a contract with one slot of each kind, of the session
// role and of a named family, and an alias over a slotted record.
func carrierFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{"schema_version":1,"profile":"nightseam.duplex/1","name":"carrier",
"types":{
 "Frame":{"kind":"record","fields":[{"name":"sequence","type":"integer"},{"name":"message","type":{"envelope":"session"}}]},
 "Attachment":{"kind":"record","fields":[{"name":"connection","type":{"connection":"session"}}]},
 "Frames":{"kind":"alias","type":{"array":"Frame"}}
},
"methods":[{"name":"relay","go_name":"Relay","ts_name":"relay","direction":"client_to_server","request":"Frame","result":{"envelope":"probe"}}],
"events":[{"name":"frame.relayed","go_name":"FrameRelayed","ts_name":"frameRelayed","direction":"server_to_client","type":"Frame"}]}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func probeFixture(t *testing.T, role string) API {
	t.Helper()
	v := contractFixture(t)
	v["name"] = "probe"
	if role != "" {
		v["role"] = role
	}
	api, diagnostics := Parse(v)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	return api
}

// TestEveryFamilyCarriesEnvelopeAndHandle: the two types every family
// carries are injected on parse and may not be declared.
func TestEveryFamilyCarriesEnvelopeAndHandle(t *testing.T) {
	api, diagnostics := Parse(contractFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, name := range []string{EnvelopeType, HandleType} {
		if api.Types[name].Kind != "record" {
			t.Errorf("%s is not carried", name)
		}
	}
	v := contractFixture(t)
	v["types"].(map[string]any)[EnvelopeType] = map[string]any{"kind": "alias", "type": "string"}
	if _, diagnostics := Parse(v); len(diagnostics) != 1 || diagnostics[0].Code != "reserved_name" || diagnostics[0].Pointer != "/types/Envelope" {
		t.Fatalf("a declared Envelope was accepted: %+v", diagnostics)
	}
}

// TestSlotsAreCheckedAndUnsupportedUntilSubstituted: a slot names a family
// of the world or the session role, never its own contract, and no slot
// renders until a family is substituted into it.
func TestSlotsAreCheckedAndUnsupportedUntilSubstituted(t *testing.T) {
	api, diagnostics := Parse(carrierFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	api.Families = []string{"carrier", "probe"}
	api.Sessions = []string{"probe"}
	codes := map[string]int{}
	for _, d := range Check(api) {
		codes[d.Code]++
	}
	if codes["unsupported_slot"] != 3 || codes["unresolved_type"] != 0 {
		t.Fatalf("expected three unsupported slots and nothing unresolved, got %v", codes)
	}
	api.Sessions = nil
	found := false
	for _, d := range Check(api) {
		found = found || (d.Code == "unresolved_type" && strings.Contains(d.Message, "session role"))
	}
	if !found {
		t.Fatal("a session slot with no session family in the world was not reported")
	}
	v := carrierFixture(t)
	v["types"].(map[string]any)["Frame"].(map[string]any)["fields"].([]any)[1].(map[string]any)["type"] = map[string]any{"envelope": "carrier"}
	api, _ = Parse(v)
	api.Families = []string{"carrier"}
	found = false
	for _, d := range Check(api) {
		found = found || d.Code == "self_slot"
	}
	if !found {
		t.Fatal("a slot of the declaring family was not reported")
	}
}

// TestImportsResolveWithinTheWorld: a family.Type reference needs the family
// imported, the family known, and the type declared there; an imported
// family imports nothing; a family does not import itself.
func TestImportsResolveWithinTheWorld(t *testing.T) {
	v := contractFixture(t)
	v["imports"] = []any{"probe"}
	fields(v, "Input")[0].(map[string]any)["type"] = "probe.Result"
	api, diagnostics := Parse(v)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if problems := Check(api); len(problems) == 0 || problems[0].Code != "unresolved_import" {
		t.Fatalf("an import outside the world passed: %+v", problems)
	}
	api.Imported = map[string]API{"probe": probeFixture(t, "")}
	if problems := Check(api); len(problems) != 0 {
		t.Fatalf("a resolved import was reported: %+v", problems)
	}
	fields(v, "Input")[0].(map[string]any)["type"] = "probe.Missing"
	api, _ = Parse(v)
	api.Imported = map[string]API{"probe": probeFixture(t, "")}
	if problems := Check(api); len(problems) != 1 || problems[0].Code != "unresolved_type" {
		t.Fatalf("a missing imported type passed: %+v", problems)
	}
	fields(v, "Input")[0].(map[string]any)["type"] = "other.Result"
	api, _ = Parse(v)
	api.Imported = map[string]API{"probe": probeFixture(t, "")}
	if problems := Check(api); len(problems) != 1 || problems[0].Code != "unresolved_type" {
		t.Fatalf("a reference to an unimported family passed: %+v", problems)
	}
	nested := probeFixture(t, "")
	nested.Imports = []string{"third"}
	fields(v, "Input")[0].(map[string]any)["type"] = "probe.Result"
	api, _ = Parse(v)
	api.Imported = map[string]API{"probe": nested}
	if problems := Check(api); len(problems) != 1 || problems[0].Code != "nested_import" {
		t.Fatalf("a nested import passed: %+v", problems)
	}
	v["imports"] = []any{"example"}
	api, _ = Parse(v)
	if problems := Check(api); len(problems) == 0 || problems[0].Code != "self_import" {
		t.Fatalf("a self import passed: %+v", problems)
	}
}

// TestSubstituteFillsEverySlot: the left path of the diagram. Session slots
// take the family given, a named slot keeps its own, every slot becomes a
// reference to that family's Envelope or Handle, and the families join the
// imports; the result has no slot and parses and checks as a plain family.
func TestSubstituteFillsEverySlot(t *testing.T) {
	substituted := Substitute(carrierFixture(t), "probe")
	data, _ := json.Marshal(substituted)
	text := string(data)
	for _, want := range []string{`"type":"probe.Envelope"`, `"type":"probe.Handle"`, `"result":"probe.Envelope"`, `"imports":["probe"]`} {
		if !strings.Contains(text, want) {
			t.Errorf("substituted contract lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `{"envelope":`) || strings.Contains(text, `{"connection":`) {
		t.Fatalf("a slot survived substitution:\n%s", text)
	}
	api, diagnostics := Parse(substituted)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	api.Families = []string{"carrier", "probe"}
	api.Sessions = []string{"probe"}
	api.Imported = map[string]API{"probe": probeFixture(t, SessionRole)}
	if problems := Check(api); len(problems) != 0 {
		t.Fatalf("the substituted contract does not check: %+v", problems)
	}
	// A named slot of another family keeps that family whatever is substituted.
	other := Substitute(carrierFixture(t), "other")
	data, _ = json.Marshal(other)
	if !strings.Contains(string(data), `"result":"probe.Envelope"`) || !strings.Contains(string(data), `"imports":["other","probe"]`) {
		t.Fatalf("a named slot did not keep its family, or the imports are wrong:\n%s", data)
	}
	// Substitution leaves its input untouched.
	original := carrierFixture(t)
	Substitute(original, "probe")
	if _, _, ok := Slot(original["types"].(map[string]any)["Frame"].(map[string]any)["fields"].([]any)[1].(map[string]any)["type"]); !ok {
		t.Fatal("Substitute mutated its input")
	}
}
