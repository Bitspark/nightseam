package kernel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/legacy/contract"
	"github.com/Bitspark/nightseam/internal/legacy/spi"
)

func contractFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{"schema_version":1,"profile":"nightseam.duplex/1","name":"example","types":{"Input":{"kind":"record","fields":[{"name":"message","type":"string"}]},"Result":{"kind":"record","fields":[{"name":"ok","type":"boolean"}]}},"methods":[{"name":"example.run","go_name":"Run","ts_name":"run","direction":"client_to_server","request":"Input","result":"Result"}],"events":[]}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// fake is a language that answers what the test tells it to, and records
// what it was asked.
type fake struct {
	name        string
	diagnostics []contract.Diagnostic
	files       []spi.File
	checked     int
	rendered    int
}

func (f *fake) Name() string { return f.name }
func (f *fake) Check(contract.API) []contract.Diagnostic {
	f.checked++
	return f.diagnostics
}
func (f *fake) Render(contract.API) ([]spi.File, error) {
	f.rendered++
	return f.files, nil
}

// TestValidateMergesEveryView: the contract's diagnostics and each language's
// arrive as one sorted set, and a language is asked only once.
func TestValidateMergesEveryView(t *testing.T) {
	v := contractFixture(t)
	v["types"].(map[string]any)["Input"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = "Missing"
	a := &fake{name: "a", diagnostics: []contract.Diagnostic{{Code: "reserved_name", Pointer: "/types/Result", Message: "a"}}}
	b := &fake{name: "b", diagnostics: []contract.Diagnostic{{Code: "reserved_name", Pointer: "/methods/0/ts_name", Message: "b"}}}
	diagnostics := Validate(v, a, b)
	var got []string
	for _, d := range diagnostics {
		got = append(got, d.Code+" "+d.Pointer)
	}
	want := "reserved_name /methods/0/ts_name|unresolved_type /types/Input/fields/0/type|reserved_name /types/Result"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %v", got)
	}
	if a.checked != 1 || b.checked != 1 || a.rendered != 0 || b.rendered != 0 {
		t.Fatalf("Validate asked a %d/%d, b %d/%d", a.checked, a.rendered, b.checked, b.rendered)
	}
	if _, err := Generate(v, a, b); err == nil || !strings.Contains(err.Error(), "invalid API contract: /methods/0/ts_name: b") {
		t.Fatalf("Generate did not fail on the first diagnostic: %v", err)
	}
	if a.rendered != 0 || b.rendered != 0 {
		t.Fatal("a refused contract was rendered")
	}
}

// TestSchemaFailureAsksNoLanguage: a contract the schema refuses is not
// well-formed enough to show to a language.
func TestSchemaFailureAsksNoLanguage(t *testing.T) {
	v := contractFixture(t)
	v["typo"] = true
	a := &fake{name: "a", diagnostics: []contract.Diagnostic{{Code: "reserved_name", Pointer: "/types/Result", Message: "a"}}}
	diagnostics := Validate(v, a)
	if a.checked != 0 {
		t.Fatal("a language saw a contract the schema refused")
	}
	for _, d := range diagnostics {
		if d.Code != "schema_validation" {
			t.Fatalf("schema failure carried more than the schema's diagnostics: %+v", diagnostics)
		}
	}
	if len(diagnostics) == 0 {
		t.Fatal("no schema diagnostic")
	}
}

// TestGenerateMergesAndHoldsPaths: files of every language arrive keyed by
// path, a path two languages render is refused, and a path that leaves the
// repository is refused whatever the language says.
func TestGenerateMergesAndHoldsPaths(t *testing.T) {
	v := contractFixture(t)
	a := &fake{name: "a", files: []spi.File{{Path: "api/go/x/a.go", Data: []byte("a")}}}
	b := &fake{name: "b", files: []spi.File{{Path: "api/ts/x/b.ts", Data: []byte("b")}}}
	first, err := Generate(v, a, b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(v, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("generation is not deterministic")
	}
	if len(first.Files) != 2 || string(first.Files["api/go/x/a.go"]) != "a" || string(first.Files["api/ts/x/b.ts"]) != "b" || first.Recipe != spi.Recipe || first.RuntimeVersion != spi.RuntimeVersion {
		t.Fatalf("unexpected result %+v", first)
	}
	if _, err := Generate(v, a, a); err == nil || !strings.Contains(err.Error(), "conflicting generated output api/go/x/a.go") {
		t.Fatalf("two languages rendered one path: %v", err)
	}
	for _, path := range []string{"../outside/x.go", "api/../../x.go", "/abs/x.go", "api\\go\\x.go", "c:/x.go", "api//x.go", "api/./x.go", "", "."} {
		c := &fake{name: "c", files: []spi.File{{Path: path, Data: []byte("c")}}}
		if _, err := Generate(v, c); err == nil || !strings.Contains(err.Error(), "invalid output path") {
			t.Errorf("accepted path %q: %v", path, err)
		}
	}
	if _, err := Generate(v); err != nil {
		t.Fatalf("no languages is a valid composition: %v", err)
	}
}
