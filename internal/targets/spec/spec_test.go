package spec

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// TestRenderWritesTheDocument: one document per family, under api/spec,
// carrying every tier — the entity with its key and constraints, the
// sides, the errors, the session — and nothing a target could collide with.
func TestRenderWritesTheDocument(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json":    `{"nightseam": 2, "types": {"Account": {"kind": "entity", "key": "id", "description": "An account.", "fields": [{"name": "id", "type": "string"}, {"name": "email", "type": "string", "unique": true, "pattern": "@"}]}, "Status": {"kind": "enum", "values": ["on", "off"]}}}`,
			"protocol.json": modeltest.Protocol(`"server": {"methods": {"get": {"request": "Account", "result": {"array": "Account"}, "errors": ["not_found"], "description": "Gets one | or more."}}, "events": {"changed": {"type": {"ref": "Account"}}}}, "errors": {"not_found": "No such account."}`),
			"session.json":  `{"decides": ["get"], "conversation": {"event": "changed", "path": "id"}}`,
		},
	}))
	f := render.Build(analysis.Resolve(world, "x"))
	target := New(Config{})
	if diagnostics := target.Check(f); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	files, err := target.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "api/spec/x/README.md" {
		t.Fatalf("rendered %v", files)
	}
	text := string(files[0].Data)
	for _, want := range []string{
		"# x", "declared in `model.json`, `protocol.json`, `session.json`",
		"An entity, identified by `id`. An account.", "| `email` | `string` | required | unique, matches `@` |",
		"One of `on`, `off`.", "| `get` | `Account` | array of `Account` | `not_found` | Gets one " + "\\" + "| or more. |",
		"| `changed` | reference to `Account` |", "| `not_found` | No such account. |",
		"- **Decides**: `get`", "the id arrives in the `changed` event, at `id` of its data",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the document lacks %q:\n%s", want, text)
		}
	}
	if family, ok := target.Family("api/spec/x/README.md"); !ok || family != "x" {
		t.Fatal("the document is not known as x's")
	}
	if _, ok := target.Family("api/spec/README.md"); ok {
		t.Fatal("a file at the root is nobody's")
	}
	if got := target.Roots(); len(got) != 1 || got[0] != "api/spec" {
		t.Fatalf("roots are %v", got)
	}
	if len(Reserved()) != 0 {
		t.Fatal("the spec reserves a name")
	}
}
