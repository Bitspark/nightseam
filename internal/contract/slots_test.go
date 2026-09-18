package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// carrierFixture is a contract generic in one parameter, with one slot of
// each kind of it, a slot of a named family, and an alias over a slotted
// record.
func carrierFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{"schema_version":1,"profile":"nightseam.duplex/1","name":"carrier",
"parameters":[{"name":"S","of":"session"}],
"types":{
 "Frame":{"kind":"record","fields":[{"name":"sequence","type":"integer"},{"name":"message","type":{"envelope":"S"}}]},
 "Attachment":{"kind":"record","fields":[{"name":"connection","type":{"connection":"S"}}]},
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

// TestSlotsAreChecked: a slot names a parameter this family declares or a
// family of the world, never its own contract; a parameter of the session
// role needs a family that declares it.
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

// TestSubstituteFillsEverySlot: the left path of the diagram. A slot of a
// bound parameter takes its family, a named slot keeps its own, every slot becomes a
// reference to that family's Envelope or Handle, and the families join the
// imports; the result has no slot and parses and checks as a plain family.
func TestSubstituteFillsEverySlot(t *testing.T) {
	substituted := Substitute(carrierFixture(t), map[string]string{"S": "probe"})
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
	other := Substitute(carrierFixture(t), map[string]string{"S": "other"})
	data, _ = json.Marshal(other)
	if !strings.Contains(string(data), `"result":"probe.Envelope"`) || !strings.Contains(string(data), `"imports":["other","probe"]`) {
		t.Fatalf("a named slot did not keep its family, or the imports are wrong:\n%s", data)
	}
	// Substitution leaves its input untouched.
	original := carrierFixture(t)
	Substitute(original, map[string]string{"S": "probe"})
	if _, _, ok := Slot(original["types"].(map[string]any)["Frame"].(map[string]any)["fields"].([]any)[1].(map[string]any)["type"]); !ok {
		t.Fatal("Substitute mutated its input")
	}
}

// spell renders uses as parameter:kind, comma separated, for comparison.
func spell(uses []Use) string {
	out := make([]string, len(uses))
	for i, use := range uses {
		out[i] = use.Parameter + ":" + use.Kind
	}
	return strings.Join(out, ",")
}

// TestGenericsFollowTheParameterSlots: a type holding a slot of a parameter
// is generic in it at the kinds it holds, and so is a type that refers to
// it, through an alias or across families; a slot of a named family is not;
// the family is generic in the union; a substituted contract is plain.
func TestGenericsFollowTheParameterSlots(t *testing.T) {
	api, diagnostics := Parse(carrierFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	g := api.Generics()
	for name, want := range map[string]string{"Frame": "S:envelope", "Frames": "S:envelope", "Attachment": "S:connection"} {
		if got := spell(g.Types[name]); got != want {
			t.Errorf("%s is generic in %q, want %q", name, got, want)
		}
	}
	for _, plain := range []string{EnvelopeType, HandleType} {
		if _, generic := g.Types[plain]; generic {
			t.Errorf("%s is generic", plain)
		}
	}
	if got := spell(g.Family); got != "S:envelope,S:connection" || !g.Generic() {
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
	v["parameters"] = []any{map[string]any{"name": "T", "of": "session"}}
	fields(v, "Input")[0].(map[string]any)["type"] = "carrier.Frames"
	other, diagnostics := Parse(v)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	other.Imported = map[string]API{"carrier": api}
	og := other.Generics()
	if spell(og.Types["Input"]) != "T:envelope" || spell(og.Family) != "T:envelope" {
		t.Errorf("a family referring to a generic type of another is generic in %v, %v", og.Types, og.Family)
	}
	if spell(og.Imported["carrier"]["Frames"]) != "S:envelope" {
		t.Error("the imported family's generics are not carried")
	}
	substituted, diagnostics := Parse(Substitute(carrierFixture(t), map[string]string{"S": "probe"}))
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

// twoParameterFixture is a contract generic in two parameters: S fills an
// envelope slot and a connection slot, T only an envelope slot, and a named
// family fills a third. Nothing collapses them into one another.
func twoParameterFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{"schema_version":1,"profile":"nightseam.duplex/1","name":"carrier",
"parameters":[{"name":"S","of":"session"},{"name":"T","of":"session"}],
"types":{
 "Frame":{"kind":"record","fields":[{"name":"message","type":{"envelope":"S"}},{"name":"back","type":{"connection":"S"}}]},
 "Echo":{"kind":"record","fields":[{"name":"heard","type":{"envelope":"T"}}]},
 "Both":{"kind":"record","fields":[{"name":"frame","type":"Frame"},{"name":"echo","type":{"array":"Echo"}}]},
 "Plain":{"kind":"record","fields":[{"name":"of","type":{"envelope":"probe"}}]}
},
"methods":[{"name":"relay","go_name":"Relay","ts_name":"relay","direction":"client_to_server","request":{"envelope":"T"},"result":"Both"}],
"events":[]}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestTwoParametersStayApart: each parameter is its own axis. A type is
// generic in the parameters it names and no others, at the kinds it names
// them at; the family is the union in declaration order; a slot of a named
// family is generic in nothing; and a request may itself hold a slot.
func TestTwoParametersStayApart(t *testing.T) {
	api, diagnostics := Parse(twoParameterFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	api.Families = []string{"carrier", "probe"}
	api.Sessions = []string{"probe"}
	if problems := Check(api); len(problems) != 0 {
		t.Fatalf("a contract of two parameters was refused: %+v", problems)
	}
	g := api.Generics()
	for name, want := range map[string]string{
		"Frame": "S:envelope,S:connection",
		"Echo":  "T:envelope",
		"Both":  "S:envelope,S:connection,T:envelope",
		"Plain": "",
	} {
		if got := spell(g.Types[name]); got != want {
			t.Errorf("%s is generic in %q, want %q", name, got, want)
		}
	}
	if got := spell(g.Family); got != "S:envelope,S:connection,T:envelope" {
		t.Errorf("the family is generic in %q", got)
	}
	// The request holds a slot of T, which is why the family is generic in it
	// even though no type of the family names T at an envelope but Echo.
	if got := spell(api.Generics().Family); !strings.Contains(got, "T:envelope") {
		t.Errorf("a slot in a request did not reach the family: %q", got)
	}
}

// TestParametersAreCheckedAndBoundOneByOne: an undeclared parameter, a
// declared one no slot names and a duplicate are each reported; a binding
// fills only the parameters it names, and a partial one stays generic in
// the rest.
func TestParametersAreCheckedAndBoundOneByOne(t *testing.T) {
	world := func(v map[string]any) API {
		api, diagnostics := Parse(v)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		api.Families = []string{"carrier", "probe"}
		api.Sessions = []string{"probe"}
		return api
	}
	v := twoParameterFixture(t)
	v["types"].(map[string]any)["Echo"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = map[string]any{"envelope": "U"}
	codes := map[string]bool{}
	for _, d := range Check(world(v)) {
		codes[d.Code] = true
	}
	if !codes["unresolved_parameter"] {
		t.Fatalf("an undeclared parameter was not reported: %v", codes)
	}
	// T is still named by the method's request, so it is still used: a slot
	// in a request counts, which is what the widened request buys.
	if codes["unused_parameter"] {
		t.Fatalf("a parameter named only by a request was called unused: %v", codes)
	}
	delete(v["methods"].([]any)[0].(map[string]any), "request")
	codes = map[string]bool{}
	for _, d := range Check(world(v)) {
		codes[d.Code] = true
	}
	if !codes["unused_parameter"] {
		t.Fatalf("a parameter no slot names was not reported: %v", codes)
	}
	v = twoParameterFixture(t)
	v["parameters"] = append(v["parameters"].([]any), map[string]any{"name": "S", "of": "session"})
	found := false
	for _, d := range Check(world(v)) {
		found = found || d.Code == "duplicate_parameter"
	}
	if !found {
		t.Fatal("a parameter declared twice was not reported")
	}
	// Binding both parameters to different families leaves no slot of either.
	full := Substitute(twoParameterFixture(t), map[string]string{"S": "probe", "T": "codex"})
	data, _ := json.Marshal(full)
	text := string(data)
	for _, want := range []string{`"type":{"envelope":"probe"}`, `"type":{"connection":"probe"}`, `"type":{"envelope":"codex"}`} {
		if strings.Contains(text, want) {
			t.Errorf("a bound slot survived substitution: %s in\n%s", want, text)
		}
	}
	for _, want := range []string{`"probe.Envelope"`, `"probe.Handle"`, `"codex.Envelope"`, `"imports":["codex","probe"]`} {
		if !strings.Contains(text, want) {
			t.Errorf("the substituted contract lacks %s:\n%s", want, text)
		}
	}
	if _, ok := full["parameters"]; ok {
		t.Error("a fully bound contract still declares parameters")
	}
	// Binding one parameter leaves the other, and the contract generic in it.
	partial := Substitute(twoParameterFixture(t), map[string]string{"S": "probe"})
	api, diagnostics := Parse(partial)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if got := spell(api.Generics().Family); got != "T:envelope" {
		t.Fatalf("a partial binding left the contract generic in %q", got)
	}
	if names := api.ParameterNames(); len(names) != 1 || names[0] != "T" {
		t.Fatalf("a partial binding left the parameters %v", names)
	}
}
