package golang

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestLiteralNamesMatchTable(t *testing.T) {
	data, err := os.ReadFile("../../../conformance/tables/naming.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		GoLiterals struct {
			Rows []struct{ Value, Type, Constant string }
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.GoLiterals.Rows) == 0 {
		t.Fatal("no Go literal naming rows")
	}
	for _, row := range table.GoLiterals.Rows {
		p, diagnostics := newPlan(family(map[string]string{"model.json": fmt.Sprintf(`{"nightseam":2,"types":{"Alias":{"kind":"alias","type":{"literal":%q}}}}`, row.Value)}))
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if p.literals[row.Value] != (literalPlan{row.Type, row.Constant}) {
			t.Errorf("%q: got %#v, table says %s / %s", row.Value, p.literals[row.Value], row.Type, row.Constant)
		}
	}
}

func TestLiteralNamesPreserveOneTypePerValue(t *testing.T) {
	f := family(map[string]string{"model.json": `{"nightseam":2,"types":{
		"Ready":{"kind":"alias","type":{"literal":"ready"}},
		"Input":{"kind":"record","fields":[
			{"name":"status","type":{"literal":"ready"}},
			{"name":"history","type":{"array":{"literal":"ready"}}},
			{"name":"url","type":{"nullable":{"literal":"api_url"}}}
		]}
	}}`})
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(p.literals) != 2 || p.literals["ready"] != (literalPlan{"LiteralReady", "LiteralReadyValue"}) || p.literals["api_url"] != (literalPlan{"LiteralAPIURL", "LiteralAPIURLValue"}) {
		t.Fatalf("literal identities are not stable: %#v", p.literals)
	}
}

func TestLiteralNamesRejectCollisions(t *testing.T) {
	for _, extra := range []string{
		`"LiteralReady":{"kind":"record","fields":[]}`,
		`"LiteralReadyValue":{"kind":"record","fields":[]}`,
		`"Another":{"kind":"alias","type":{"literal":"Ready"}}`,
	} {
		f := family(map[string]string{"model.json": `{"nightseam":2,"types":{"Ready":{"kind":"alias","type":{"literal":"ready"}},` + extra + `}}`})
		_, diagnostics := newPlan(f)
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == "generated_name_collision"
		}
		if !found {
			t.Fatalf("literal collision accepted: %s: %v", extra, diagnostics)
		}
	}
}

func TestSchemaMetadataMemberIsReservedLocally(t *testing.T) {
	for _, types := range []string{
		`"Part":{"kind":"union","tag":"kind","variants":{"wire_type":"string"}}`,
		`"Part":{"kind":"record","fields":[{"name":"wire_type","type":"string"}]}`,
	} {
		_, diagnostics := newPlan(family(map[string]string{"model.json": `{"nightseam":2,"types":{` + types + `}}`}))
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == "reserved_name"
		}
		if !found {
			t.Fatalf("metadata member collision accepted: %s: %v", types, diagnostics)
		}
	}
	_, diagnostics := newPlan(family(map[string]string{"model.json": `{"nightseam":2,"types":{"WireType":{"kind":"record","fields":[]}}}`}))
	if len(diagnostics) != 0 {
		t.Fatalf("a method name was reserved in the package: %v", diagnostics)
	}
}
