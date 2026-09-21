package runtime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type identityFixture struct {
	Name           string                       `json:"name"`
	Declaration    string                       `json:"declaration"`
	Digest         string                       `json:"digest"`
	Sources        map[string]map[string]string `json:"sources"`
	Family         string                       `json:"family"`
	WireExpression json.RawMessage              `json:"wireExpression"`
	Families       map[string]struct {
		Wire, Declaration, Digest string
		Imports                   []string
	} `json:"families"`
}

func TestClosedDeclarationRenderedExpressions(t *testing.T) {
	checked := 0
	for name, row := range identityFixtures(t) {
		if len(row.Families) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			schemas := map[string]*Schema{}
			for name, family := range row.Families {
				schemas[name] = MustSchema(family.Wire, family.Digest, nil).MustWithDeclaration(family.Declaration)
			}
			for name, family := range row.Families {
				for _, imported := range family.Imports {
					schemas[name].imported[imported] = schemas[imported]
				}
			}
			var got string
			var err error
			if len(row.WireExpression) > 0 && string(row.WireExpression) != "null" {
				got, err = (TypeBinding{Schema: schemas[row.Family], Type: MustTypeExpression(string(row.WireExpression))}).Declaration()
			} else {
				got, err = schemas[row.Family].BoundDeclaration()
			}
			graph, readErr := readDeclaration(row.Declaration)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !closedFixtureRoot(graph, graph.Root) {
				if err == nil {
					t.Fatalf("open template acquired identity: %s", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != row.Declaration {
				t.Fatalf("rendered expression differs\ngot  %s\nwant %s", got, row.Declaration)
			}
			checked++
		})
	}
	if checked < 30 {
		t.Fatalf("only %d rendered expression cases checked", checked)
	}
}

func closedFixtureRoot(graph declarationGraph, value any) bool {
	switch value := value.(type) {
	case map[string]any:
		if _, parameter := value["parameter"]; parameter {
			return false
		}
		if nested, ok := value["graph"]; ok {
			data, _ := json.Marshal(nested)
			inner, err := readDeclaration(string(data))
			return err == nil && closedFixtureRoot(inner, inner.Root)
		}
		if path, ok := value["ref"].(string); ok {
			definition, _ := graph.Definitions[path].(map[string]any)
			return len(declarationParameters(definition)) == 0
		}
		for _, child := range value {
			if !closedFixtureRoot(graph, child) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !closedFixtureRoot(graph, child) {
				return false
			}
		}
	}
	return true
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

func TestClosedDeclarationStructuralArguments(t *testing.T) {
	rows := identityFixtures(t)
	first := fixtureSchema(t, rows["same named argument first revision"])
	second := fixtureSchema(t, rows["same named argument second revision"])
	shape := MustTypeExpression(`{"kind":"record","fields":[{"name":"a","type":"A","required":true},{"name":"b","type":{"array":"B"},"required":true,"length":{"min":1}}]}`)
	schema := MustSchema(`{"types":{"Alias":{"kind":"alias","parameters":[{"name":"A"},{"name":"B"}],"type":{"kind":"record","fields":[{"name":"a","type":"A","required":true},{"name":"b","type":{"array":"B"},"required":true,"length":{"min":1}}]}}}}`, "", nil)
	binding := schema.Bind(map[string]any{"A": TypeBinding{Schema: first, Type: "Job"}, "B": TypeBinding{Schema: second, Type: "Job"}}, nil)
	direct, err := (TypeBinding{Schema: binding, Type: shape}).Declaration()
	if err != nil {
		t.Fatal(err)
	}
	alias, err := (TypeBinding{Schema: binding, Type: "Alias"}).Declaration()
	if err != nil {
		t.Fatal(err)
	}
	if direct != alias {
		t.Fatalf("inline alias changed meaning\n%s\n%s", direct, alias)
	}
	if !strings.Contains(direct, rows["same named argument first revision"].Declaration) || !strings.Contains(direct, rows["same named argument second revision"].Declaration) {
		t.Fatal("structural fields lost separate revision scopes")
	}
	if err := binding.ValidateExpressionRaw(shape, []byte(`{"a":{"value":"one"},"b":[{"value":2}]}`)); err != nil {
		t.Fatal(err)
	}
	integer := TypeArgument[int64]()
	array := TypeArgument[[]int64]()
	primitive, err := integer.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	container, err := array.Declaration()
	if err != nil || container != `{"definitions":{},"root":{"array":{"graph":`+primitive+`}},"version":1}` {
		t.Fatalf("array scope: %s %v", container, err)
	}
	if _, err := TypeArgument[struct{ Field string }]().Declaration(); err == nil {
		t.Fatal("host codec silently invented declaration provenance")
	}
}

func TestClosedFamilyIdentityAndValidationUseTheSameNestedBindings(t *testing.T) {
	rows := identityFixtures(t)
	graph, _ := readDeclaration(rows["generic family template"].Declaration)
	familyNode := graph.Definitions["same"].(map[string]any)
	familyNode["types"] = map[string]any{"Box": map[string]any{"ref": "same/Box"}}
	graph.Definitions["same/Box"] = map[string]any{"kind": "record", "parameters": []any{}, "captures": []any{map[string]any{"name": "A", "of": ""}}, "open": false, "fields": []any{map[string]any{"name": "value", "type": map[string]any{"parameter": "same/A"}, "required": true, "nullable": false, "unique": false}}}
	document, _ := graph.document()
	family := MustSchema(`{"types":{"Box":{"kind":"record","fields":[{"name":"value","type":"A","required":true}]}},"parameters":[{"name":"A"},{"name":"B"}]}`, declarationHash(document), nil).MustWithDeclaration(document)
	consumer := MustSchema(`{"types":{"Draw":{"kind":"alias","type":"F.Box"}},"parameters":[{"name":"F","of":"protocol"}]}`, "", nil)
	boundFamily := family.Bind(map[string]any{"A": "integer", "B": "string"}, nil)
	if _, err := boundFamily.DeclarationDigest(); err != nil {
		t.Fatal(err)
	}
	bound := consumer.Bind(nil, map[string]*Schema{"F": boundFamily})
	if err := bound.ValidateRaw("Draw", []byte(`{"value":42}`)); err != nil {
		t.Fatal(err)
	}
	if err := bound.ValidateRaw("Draw", []byte(`{"value":"wrong"}`)); err == nil {
		t.Fatal("nested family binding ignored during validation")
	}
}
