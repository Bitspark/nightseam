package runtime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type identityFixture struct {
	Name        string                       `json:"name"`
	Declaration string                       `json:"declaration"`
	Digest      string                       `json:"digest"`
	Sources     map[string]map[string]string `json:"sources"`
}

func identityFixtures(t *testing.T) map[string]identityFixture {
	t.Helper()
	data, err := os.ReadFile("../../conformance/tables/declaration-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []identityFixture `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	rows := map[string]identityFixture{}
	for _, row := range table.Cases {
		rows[row.Name] = row
	}
	return rows
}

// Supply the generated provenance for the selected fixtures without invoking a
// generator from a runtime unit test. Extra source types cannot enter a selected
// argument's graph: only the fixture's selected closure is made available.
func fixtureSchema(t *testing.T, row identityFixture) *Schema {
	t.Helper()
	graph, err := readDeclaration(row.Declaration)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]any{}
	var parameters []any
	for _, file := range []string{"model.json", "protocol.json", "live.json"} {
		if source := row.Sources["same"][file]; source != "" {
			var value map[string]any
			if err := json.Unmarshal([]byte(source), &value); err != nil {
				t.Fatal(err)
			}
			if declarations, ok := value["types"].(map[string]any); ok {
				for name, def := range declarations {
					types[name] = def
				}
			}
			if list, ok := value["parameters"].([]any); ok {
				parameters = list
			}
		}
	}
	if graph.Definitions["same"] == nil {
		graph.Definitions["same"] = map[string]any{"kind": "family", "parameters": []any{}, "types": map[string]any{}}
		graph.Root = map[string]any{"ref": "same"}
	}
	document, err := graph.document()
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(map[string]any{"types": types, "parameters": parameters})
	return MustSchema(string(wire), declarationHash(document), nil).MustWithDeclaration(document)
}

func TestClosedDeclarationIdentitySharedBytes(t *testing.T) {
	rows := identityFixtures(t)
	for name, row := range rows {
		t.Run(name, func(t *testing.T) {
			graph, err := readDeclaration(row.Declaration)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := graph.document()
			if err != nil || encoded != row.Declaration {
				t.Fatalf("canonical bytes changed: %v", err)
			}
			if declarationHash(encoded) != row.Digest {
				t.Fatal("shared SHA-256 differs")
			}
		})
	}
}

func TestClosedDeclarationIdentityBindings(t *testing.T) {
	rows := identityFixtures(t)
	family := fixtureSchema(t, rows["generic family template"])
	callable := fixtureSchema(t, rows["callable template"])
	first := fixtureSchema(t, rows["same named argument first revision"])
	second := fixtureSchema(t, rows["same named argument second revision"])
	builtin := MustSchema(`{"types":{}}`, "", nil)
	integer := TypeBinding{Schema: builtin, Type: "integer"}
	text := TypeBinding{Schema: builtin, Type: "string"}
	job1 := TypeBinding{Schema: first, Type: "Job"}
	job2 := TypeBinding{Schema: second, Type: "Job"}
	check := func(name, got string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if got != rows[name].Declaration {
			t.Fatalf("%s differs\ngot %s\nwant %s", name, got, rows[name].Declaration)
		}
	}
	got, err := family.Bind(map[string]any{"A": integer, "B": text}, nil).BoundDeclaration()
	check("bound generic family", got, err)
	got, err = family.Bind(map[string]any{"A": text, "B": integer}, nil).BoundDeclaration()
	check("bound generic family changed arguments", got, err)
	got, err = (TypeBinding{Schema: callable.Bind(map[string]any{"A": job1, "B": job2}, nil), Type: "Function"}).Declaration()
	check("same path distinct revisions in separate arguments", got, err)
	got, err = (TypeBinding{Schema: callable.Bind(map[string]any{"A": job2, "B": job1}, nil), Type: "Function"}).Declaration()
	check("same path distinct revisions argument order changed", got, err)
	got, err = (TypeBinding{Schema: callable, Type: "Alias"}).Declaration()
	check("application baseline", got, err)
	got, err = family.Bind(map[string]any{"A": job1, "B": integer}, nil).BoundDeclaration()
	check("bound generic family stable argument closure", got, err)

	// A supplied generic value keeps its own nested type bindings.
	nested := TypeBinding{Schema: callable.Bind(map[string]any{"A": integer, "B": text}, nil), Type: "Function"}
	closed := family.Bind(map[string]any{"A": nested, "B": job1}, nil)
	outer, err := closed.BoundDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outer, rows["application baseline"].Declaration) {
		t.Fatal("nested application provenance lost")
	}
	digest, err := closed.DeclarationDigest()
	if err != nil || digest != declarationHash(outer) {
		t.Fatalf("closed digest: %s %v", digest, err)
	}
	other := family.Bind(map[string]any{"A": nested, "B": job2}, nil)
	otherDigest, err := other.DeclarationDigest()
	if err != nil || digest == otherDigest {
		t.Fatal("nested same-path revision collapsed")
	}
	if _, err := family.DeclarationDigest(); err == nil || !strings.Contains(err.Error(), "missing required binding A") {
		t.Fatalf("missing slot: %v", err)
	}
	if _, err := family.Bind(map[string]any{"A": integer}, nil).DeclarationDigest(); err == nil {
		t.Fatal("partial binding acquired identity")
	}
	if _, err := (TypeBinding{Schema: MustSchema(`{"types":{"Job":{"kind":"record"}}}`, "", nil), Type: "Job"}).Declaration(); err == nil {
		t.Fatal("missing provenance acquired identity")
	}
	if _, err := family.WithDeclaration(strings.Replace(family.declaration, `"version":1`, `"version":2`, 1)); err == nil {
		t.Fatal("bad version accepted")
	}
	if _, err := MustSchema(`{"types":{}}`, strings.Repeat("a", 64), nil).WithDeclaration(rows["generic family template"].Declaration); err == nil {
		t.Fatal("wrong digest accepted")
	}
}

func TestClosedDeclarationFamilyBindings(t *testing.T) {
	rows := identityFixtures(t)
	family := fixtureSchema(t, rows["generic family template"])
	graph, _ := readDeclaration(family.declaration)
	definition := graph.Definitions["same"].(map[string]any)
	definition["parameters"] = []any{map[string]any{"name": "F", "of": "protocol"}}
	document, _ := graph.document()
	consumer := MustSchema(`{"types":{},"parameters":[{"name":"F","of":"protocol"}]}`, declarationHash(document), nil).MustWithDeclaration(document)
	bound := family.Bind(map[string]any{"A": "integer", "B": "string"}, nil)
	got, err := consumer.Bind(nil, map[string]*Schema{"F": bound}).BoundDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, rows["bound generic family"].Declaration) {
		t.Fatal("bound family argument lost its slots")
	}
	if _, err := consumer.Bind(nil, map[string]*Schema{"F": family}).BoundDeclaration(); err == nil {
		t.Fatal("unbound nested family acquired identity")
	}
}
