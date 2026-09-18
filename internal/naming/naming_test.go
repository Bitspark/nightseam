package naming

import (
	"encoding/json"
	"os"
	"testing"
)

// TestConventionsAreTheTable holds the two conventions to the naming table
// every language's target is held to: conformance/tables/naming.json, a
// column per language. A target for a third language adds its column there
// and holds its own convention to it the same way.
func TestConventionsAreTheTable(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/naming.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Rows []struct{ Wire, Go, TypeScript string }
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) == 0 {
		t.Fatal("the naming table is empty")
	}
	for _, row := range table.Rows {
		if got := UpperCamel(row.Wire); got != row.Go {
			t.Errorf("%s: Go spells it %s, the table says %s", row.Wire, got, row.Go)
		}
		if got := LowerCamel(row.Wire); got != row.TypeScript {
			t.Errorf("%s: TypeScript spells it %s, the table says %s", row.Wire, got, row.TypeScript)
		}
	}
}
