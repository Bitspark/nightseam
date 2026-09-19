package runtime

import (
	"encoding/json"
	"os"
	"testing"
)

// TestValidatorConformance: the validator agrees with the conformance table
// every runtime is held to, case by case, on the wire description of a
// family and of one it refers to — on the verdict, and on the diagnostic
// where the row states one, which is the string the other runtime's
// validator prints for the same value.
func TestValidatorConformance(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/validator.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Wire     json.RawMessage
		Imported map[string]json.RawMessage
		Patterns []struct {
			Pattern string
			Valid   bool
		}
		PatternValues []struct {
			Pattern, Value string
			Valid          bool
		}
		Equivalence []struct {
			Generic, Bound json.RawMessage
			Values         []json.RawMessage
		}
		Cases []struct {
			Expression json.RawMessage
			Value      json.RawMessage
			Valid      bool
			Message    string
			Slots      map[string]struct {
				Type   json.RawMessage
				Family string
			}
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	for _, row := range table.Patterns {
		description, err := json.Marshal(map[string]any{"types": map[string]any{
			"Probe": map[string]any{"kind": "alias", "type": map[string]any{"array": map[string]any{"nullable": map[string]any{
				"kind": "record", "fields": []any{map[string]any{"name": "tag", "type": "string", "pattern": row.Pattern}},
			}}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewSchema(description, nil)
		if (err == nil) != row.Valid {
			t.Errorf("pattern %q: valid=%v, got %v", row.Pattern, row.Valid, err)
		}
	}
	for _, row := range table.PatternValues {
		description, err := json.Marshal(map[string]any{"types": map[string]any{"Probe": map[string]any{
			"kind": "record", "fields": []any{map[string]any{"name": "text", "type": "string", "required": true, "pattern": row.Pattern}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		schema, err := NewSchema(description, nil)
		if err != nil {
			t.Errorf("pattern %q: %v", row.Pattern, err)
			continue
		}
		err = schema.ValidateValue("Probe", map[string]any{"text": row.Value})
		if (err == nil) != row.Valid {
			t.Errorf("pattern %q on %q: valid=%v, got %v", row.Pattern, row.Value, row.Valid, err)
		}
	}
	imported := map[string]*Schema{}
	for family, wire := range table.Imported {
		schema, err := NewSchema(wire, imported)
		if err != nil {
			t.Fatal(err)
		}
		imported[family] = schema
	}
	schema, err := NewSchema(table.Wire, imported)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range table.Cases {
		types := map[string]any{}
		families := map[string]*Schema{}
		for name, slot := range c.Slots {
			if slot.Family != "" {
				families[name] = imported[slot.Family]
			} else {
				types[name] = MustTypeExpression(string(slot.Type))
			}
		}
		err := schema.Bind(types, families).ValidateExpressionRaw(MustTypeExpression(string(c.Expression)), c.Value)
		if (err == nil) != c.Valid {
			t.Errorf("%s against %s: valid=%v, got %v", c.Value, c.Expression, c.Valid, err)
			continue
		}
		if c.Message != "" && err.Error() != c.Message {
			t.Errorf("%s against %s: message %q, got %q", c.Value, c.Expression, c.Message, err)
		}
	}
	for _, law := range table.Equivalence {
		for _, value := range law.Values {
			generic := schema.ValidateExpressionRaw(MustTypeExpression(string(law.Generic)), value)
			bound := schema.ValidateExpressionRaw(MustTypeExpression(string(law.Bound)), value)
			if (generic == nil) != (bound == nil) {
				t.Errorf("generic %s and bound %s disagree on %s: %v / %v", law.Generic, law.Bound, value, generic, bound)
			}
		}
	}
	if err := schema.ValidateValue("Status", "on"); err != nil {
		t.Fatal(err)
	}
	if got := schema.Fields("Payload"); len(got) != 5 || got[0] != "text" {
		t.Fatalf("fields are %v", got)
	}
	if err := schema.ValidateExpressionRaw("Status", []byte(`"on" "off"`)); err == nil {
		t.Fatal("trailing values were accepted")
	}
}
