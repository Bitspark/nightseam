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

// TestSlotsAreChecked: a slot names a family of the world or the session
// role, never its own contract; a slot of the session role needs a member.
func TestSlotsAreChecked(t *testing.T) {
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
	if len(codes) != 0 {
		t.Fatalf("a slotted contract within its world was refused: %v", codes)
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

// TestGenericsFollowTheSessionSlots: a type holding a slot of the session
// role is generic in the kinds it holds, and so is a type that refers to it,
// through an alias or across families; a slot of a named family is not; the
// family is generic in the union; a substituted contract is plain.
func TestGenericsFollowTheSessionSlots(t *testing.T) {
	api, diagnostics := Parse(carrierFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	g := api.Generics()
	for name, want := range map[string]string{"Frame": "envelope", "Frames": "envelope", "Attachment": "connection"} {
		if got := strings.Join(g.Types[name], ","); got != want {
			t.Errorf("%s is generic in %q, want %q", name, got, want)
		}
	}
	for _, plain := range []string{EnvelopeType, HandleType} {
		if _, generic := g.Types[plain]; generic {
			t.Errorf("%s is generic", plain)
		}
	}
	if got := strings.Join(g.Family, ","); got != "envelope,connection" || !g.Generic() {
		t.Errorf("the family is generic in %q", got)
	}
	if got := strings.Join(api.SlotFamilies(), ","); got != "probe" {
		t.Errorf("the slots name %q", got)
	}
	if got := strings.Join(api.References(), ","); got != "probe" {
		t.Errorf("the contract refers to %q", got)
	}
	v := contractFixture(t)
	v["imports"] = []any{"carrier"}
	fields(v, "Input")[0].(map[string]any)["type"] = "carrier.Frames"
	other, diagnostics := Parse(v)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	other.Imported = map[string]API{"carrier": api}
	og := other.Generics()
	if strings.Join(og.Types["Input"], ",") != "envelope" || strings.Join(og.Family, ",") != "envelope" {
		t.Errorf("a family referring to a generic type of another is generic in %v, %v", og.Types, og.Family)
	}
	if strings.Join(og.Imported["carrier"]["Frames"], ",") != "envelope" {
		t.Error("the imported family's generics are not carried")
	}
	substituted, diagnostics := Parse(Substitute(carrierFixture(t), "probe"))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if sg := substituted.Generics(); sg.Generic() || len(sg.Types) != 0 {
		t.Errorf("the substituted contract is generic: %v", sg)
	}
	if got := strings.Join(substituted.References(), ","); got != "probe" {
		t.Errorf("the substituted contract refers to %q", got)
	}
}
