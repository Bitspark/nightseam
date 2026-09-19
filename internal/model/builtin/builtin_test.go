package builtin

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
)

// TestBuiltinsDecode: every built-in family reads as a family of the
// declaration language, and the profile's declares exactly the two types
// every family with a protocol carries.
func TestBuiltinsDecode(t *testing.T) {
	families := Families()
	for _, name := range []string{"duplex", "tunnel", "session"} {
		if _, ok := families[name]; !ok {
			t.Fatalf("the built-in %s family is missing", name)
		}
	}
	duplex := families["duplex"]
	if duplex.Protocol != nil {
		t.Error("the profile's family has a protocol tier, which would carry itself")
	}
	var names []string
	for name := range duplex.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != 2 || !model.Carried(names[0]) || !model.Carried(names[1]) {
		t.Fatalf("duplex declares %v, and the carried types are Envelope and Handle", names)
	}
	for _, name := range names {
		if got := duplex.Types[name].At.File; got != Locate("duplex", model.ModelFile) {
			t.Errorf("%s is located at %s, which a reader would take for their own file", name, got)
		}
	}
}

// TestEnvelopeIsTheProfilesMessage: the envelope spells one
// nightseam.duplex/1 message field for field — the trace context it carries
// among them, optional strings like any other, and the meta a request or an
// event carries, an optional flat map of strings — and the handle is a
// channel reference.
func TestEnvelopeIsTheProfilesMessage(t *testing.T) {
	duplex := Families()["duplex"]
	var names []string
	for _, field := range duplex.Types[model.EnvelopeType].Fields {
		names = append(names, field.Name)
	}
	want := []string{"version", "kind", "id", "method", "params", "result", "error", "event", "data", "traceparent", "tracestate", "meta"}
	if len(names) != len(want) {
		t.Fatalf("the envelope's fields are %v", names)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("the envelope's fields are %v", names)
		}
	}
	for _, field := range duplex.Types[model.EnvelopeType].Fields {
		switch field.Name {
		case "traceparent", "tracestate":
			if field.Required || !model.Equal(field.Type, model.Primitive("string")) {
				t.Errorf("%s is %+v, not an optional string", field.Name, field)
			}
		case "meta":
			if field.Required || !model.Equal(field.Type, model.Map{Elem: model.Primitive("string")}) {
				t.Errorf("meta is %+v, not an optional map of strings", field)
			}
		}
	}
	if fields := duplex.Types[model.HandleType].Fields; len(fields) != 1 || fields[0].Name != "channel" {
		t.Fatalf("the handle's fields are %v", fields)
	}
}

// TestFramesTableIsDuplexsDeclaration holds conformance/tables/frames.json
// — every envelope a peer accepts or refuses — to the built-in duplex
// family's Envelope rather than to a list written by hand beside it: a
// frame the table calls valid carries only members the declaration has and
// every member it requires, and every member of the declaration is
// exercised by some valid row. The two move together or the test says so.
func TestFramesTableIsDuplexsDeclaration(t *testing.T) {
	data, err := os.ReadFile("../../../conformance/tables/frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Rows []struct {
			Name  string
			Frame string
			Valid bool
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	envelope := Families()["duplex"].Types[model.EnvelopeType]
	declared := map[string]bool{}
	required := map[string]bool{}
	for _, field := range envelope.Fields {
		declared[field.Name] = true
		if field.Required {
			required[field.Name] = true
		}
	}
	exercised := map[string]bool{}
	rows := 0
	for _, row := range table.Rows {
		var frame map[string]json.RawMessage
		if err := json.Unmarshal([]byte(row.Frame), &frame); err != nil {
			continue // a frame that is not an object is the table's own case
		}
		if !row.Valid {
			continue
		}
		rows++
		for member := range frame {
			if !declared[member] {
				t.Errorf("the valid row %q carries %s, which duplex.Envelope does not declare", row.Name, member)
			}
			exercised[member] = true
		}
		for member := range required {
			if _, present := frame[member]; !present {
				t.Errorf("the valid row %q leaves out %s, which duplex.Envelope requires", row.Name, member)
			}
		}
	}
	if rows == 0 {
		t.Fatal("the frames table has no valid rows")
	}
	for _, field := range envelope.Fields {
		if !exercised[field.Name] {
			t.Errorf("duplex.Envelope declares %s and no valid row of the frames table carries it", field.Name)
		}
	}
}
