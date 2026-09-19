package golang

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestGoUnionNamesMatchTable(t *testing.T) {
	data, err := os.ReadFile("../../../conformance/tables/naming.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		GoUnions struct {
			Rows []struct{ Name, Override, Tag, KindType, Field, Constant string }
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.GoUnions.Rows) == 0 {
		t.Fatal("the Go union naming table is empty")
	}
	for _, row := range table.GoUnions.Rows {
		t.Run(row.Name+"/"+row.Tag+"/"+row.Override, func(t *testing.T) {
			files := map[string]string{
				"model.json": fmt.Sprintf(`{"nightseam":2,"types":{%q:{"kind":"union","tag":"kind","variants":{%q:"string"}}}}`, row.Name, row.Tag),
			}
			if row.Override != "" {
				files["go.json"] = fmt.Sprintf(`{"names":{%q:%q}}`, row.Name, row.Override)
			}
			p, diagnostics := newPlan(family(files))
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			u := p.unions[row.Name]
			for _, pair := range [][2]string{
				{u.kind, row.KindType}, {u.fields[row.Tag], row.Field}, {u.constants[row.Tag], row.Constant},
			} {
				if pair[0] != pair[1] {
					t.Errorf("got %q, table says %q", pair[0], pair[1])
				}
			}
		})
	}
}

// These names share the namespaces of the generated concrete union and
// its kind constants, even though the wire codec is a separate concern.
func TestUnionPlanRejectsCollisions(t *testing.T) {
	for name, c := range map[string]struct {
		types, overrides, code, at string
	}{
		"kind type":           {`"Part":{"kind":"union","tag":"kind","variants":{"text":"string"}},"PartKind":{"kind":"record","fields":[]}`, "", "generated_name_collision", "model.json#/types/Part"},
		"kind constant":       {`"Part":{"kind":"union","tag":"kind","variants":{"text":"string"}},"PartKindText":{"kind":"record","fields":[]}`, "", "generated_name_collision", "model.json#/types/Part/variants/text"},
		"enum constant":       {`"Part":{"kind":"union","tag":"kind","variants":{"t_ext":"string"}},"PartKindT":{"kind":"enum","values":["ext"]}`, "", "generated_name_collision", "model.json#/types/PartKindT/values/0"},
		"normalized variants": {`"Part":{"kind":"union","tag":"kind","variants":{"a-b":"string","a_b":"string"}}`, "", "generated_name_collision", "model.json#/types/Part/variants/a_b"},
		"kind method":         {`"Part":{"kind":"union","tag":"kind","variants":{"kind":"string"}}`, "", "reserved_name", "model.json#/types/Part/variants/kind"},
		"family method":       {`"Part":{"kind":"union","tag":"kind","variants":{"of":"string"}}`, "", "reserved_name", "model.json#/types/Part/variants/of"},
		"marshal method":      {`"Part":{"kind":"union","tag":"kind","variants":{"marshal_json":"string"}}`, "", "reserved_name", "model.json#/types/Part/variants/marshal_json"},
		"unmarshal method":    {`"Part":{"kind":"union","tag":"kind","variants":{"unmarshal_json":"string"}}`, "", "reserved_name", "model.json#/types/Part/variants/unmarshal_json"},
		"empty identifier":    {`"Part":{"kind":"union","tag":"kind","variants":{"---":"string"}}`, "", "invalid_name", "model.json#/types/Part/variants/---"},
		"numeric identifier":  {`"Part":{"kind":"union","tag":"kind","variants":{"3d":"string"}}`, "", "invalid_name", "model.json#/types/Part/variants/3d"},
		"renamed kind type":   {`"Part":{"kind":"union","tag":"kind","variants":{"text":"string"}},"MessageKind":{"kind":"record","fields":[]}`, `{"names":{"Part":"Message"}}`, "generated_name_collision", "go.json#/names/Part"},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"model.json": `{"nightseam":2,"types":{` + c.types + `}}`}
			if c.overrides != "" {
				files["go.json"] = c.overrides
			}
			_, diagnostics := newPlan(family(files))
			if !has(diagnostics, c.code, c.at) {
				t.Fatalf("expected %s at %s, got %v", c.code, c.at, diagnostics)
			}
		})
	}
}

func TestUnionNamesStayInTheirOwnNamespaces(t *testing.T) {
	f := family(map[string]string{
		"model.json": `{"nightseam":2,"types":{
			"Kind":{"kind":"record","fields":[{"name":"kind","type":"string"}]},
			"Left":{"kind":"union","tag":"kind","variants":{"text":"string"}},
			"Right":{"kind":"union","tag":"kind","variants":{"text":"string"}}
		}}`,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"kind":{"result":"string"}}}`),
	})
	if _, diagnostics := newPlan(f); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}
