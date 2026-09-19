package runtime

import (
	"encoding/json"
	"os"
	"testing"
)

// Both validators consume the document's actual bytes and visible bindings.
// Unavailable entries remain in the table, with a reason and no invented value.
func TestDocumentExamplesConformance(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var table []struct {
		Name    string
		Schemas map[string]json.RawMessage
		Rows    []struct {
			Family, Path, To, Member string
			Expression, Value        json.RawMessage
			Bindings                 map[string]struct{ Type, Family string }
			Unavailable              *struct{ Kind, Reason string }
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	proof := 0
	for _, corpus := range table {
		schemas := map[string]*Schema{}
		for name, wire := range corpus.Schemas {
			schemas[name] = MustSchema(string(wire), schemas)
		}
		for _, row := range corpus.Rows {
			t.Run(corpus.Name+"/"+row.Family+"/"+row.Path, func(t *testing.T) {
				if row.Unavailable != nil {
					if len(row.Value) != 0 || row.Unavailable.Reason == "" || row.Unavailable.Kind != "limit" && row.Unavailable.Kind != "impossible" {
						t.Fatalf("invalid unavailability: %+v", row)
					}
					return
				}
				if !json.Valid(row.Value) {
					t.Fatal("missing example value")
				}
				if row.Family == "proof" {
					proof++
				}
				types, families := map[string]any{}, map[string]*Schema{}
				for name, binding := range row.Bindings {
					if binding.Family != "" {
						families[name] = schemas[binding.Family]
						if families[name] == nil {
							t.Fatalf("missing family binding %s", binding.Family)
						}
					} else {
						types[name] = binding.Type
					}
				}
				value := row.Value
				if row.To != "" {
					frame, err := decodeFrame(value)
					if err != nil {
						t.Fatal(err)
					}
					local, remote := "s:", "c:"
					if row.To == "client" {
						local, remote = "c:", "s:"
					}
					prefix := remote
					if frame.Kind == "response" {
						prefix = local
					}
					if frame.ID != "" && !validID(frame.ID, prefix) {
						t.Fatal("invalid frame id")
					}
					if row.Member != "" {
						var members map[string]json.RawMessage
						if err := json.Unmarshal(value, &members); err != nil {
							t.Fatal(err)
						}
						value = members[row.Member]
					}
				}
				if len(row.Expression) != 0 {
					if err := schemas[row.Family].Bind(types, families).ValidateExpressionRaw(MustTypeExpression(string(row.Expression)), value); err != nil {
						t.Fatalf("%s: %v", value, err)
					}
				}
			})
		}
	}
	if proof < 30 {
		t.Fatalf("only %d proof examples are held", proof)
	}
}
