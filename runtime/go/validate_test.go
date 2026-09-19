package runtime

import (
	"encoding/json"
	"os"
	"testing"
)

// TestValidatorConformance: the validator agrees with the conformance table
// every runtime is held to, case by case, on the wire description of a
// family and of one it refers to.
func TestValidatorConformance(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/validator.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Wire     json.RawMessage
		Imported map[string]json.RawMessage
		Cases    []struct {
			Expression json.RawMessage
			Value      json.RawMessage
			Valid      bool
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	imported := map[string]func(string, []byte) error{}
	for family, wire := range table.Imported {
		schema, err := NewSchema(wire, nil)
		if err != nil {
			t.Fatal(err)
		}
		imported[family] = schema.ValidateRaw
	}
	schema, err := NewSchema(table.Wire, imported)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range table.Cases {
		err := schema.ValidateExpressionRaw(MustTypeExpression(string(c.Expression)), c.Value)
		if (err == nil) != c.Valid {
			t.Errorf("%s against %s: valid=%v, got %v", c.Value, c.Expression, c.Valid, err)
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
