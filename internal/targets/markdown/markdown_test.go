package markdown

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// TestWritesThePage: one page per family, under api/spec, carrying every
// tier — the entity with its key and constraints, an example of it and
// where it is used, the sides with each operation on the wire, the errors,
// the session — and nothing a target could collide with; and the writer,
// made a target, answers what the kernel asks of one.
func TestWritesThePage(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json":    `{"nightseam": 2, "types": {"Account": {"kind": "entity", "key": "id", "description": "An account.", "fields": [{"name": "id", "type": "string"}, {"name": "email", "type": "string", "unique": true, "pattern": "@"}]}, "Status": {"kind": "enum", "values": ["on", "off"]}}}`,
			"protocol.json": modeltest.Protocol(`"server": {"methods": {"get": {"request": "Account", "result": {"array": "Account"}, "errors": ["not_found"], "description": "Gets one | or more."}}, "events": {"changed": {"type": {"ref": "Account"}}}}, "errors": {"not_found": "No such account."}`),
			"session.json":  `{"decides": ["get"], "conversation": {"event": "changed", "path": "id"}}`,
		},
	}))
	f := render.Build(analysis.Resolve(world, "x"))
	target := doc.Target(New(Config{}), nil)
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
		"For example:", `  "email": "@"`, "Used by `get` (request, result), `changed` (data).",
		"### `get` on the wire", "The client sends:", `  "method": "get",`, `  "id": "c:1",`, "The server answers:",
		"Or refuses with `not_found`:", `    "message": "No such account."`, "### `changed` on the wire", "The server emits:", `  "event": "changed",`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the page lacks %q:\n%s", want, text)
		}
	}
	if target.Name() != Name {
		t.Fatalf("the target is named %q", target.Name())
	}
	if family, ok := target.Family("api/spec/x/README.md"); !ok || family != "x" {
		t.Fatal("the page is not known as x's")
	}
	if _, ok := target.Family("api/spec/README.md"); ok {
		t.Fatal("a file at the root is nobody's")
	}
	if got := target.Roots(); len(got) != 1 || got[0] != "api/spec" {
		t.Fatalf("roots are %v", got)
	}
	if got := target.Owns("x"); len(got) != 1 || got[0] != "api/spec/x" {
		t.Fatalf("owns %v", got)
	}
	if len(Reserved()) != 0 {
		t.Fatal("the writer reserves a name")
	}
}

// TestPlacedFamiliesLiveWhereTheConfigSays: a family the config places is
// found at its own pattern and nowhere else, and a pattern that names no
// family is refused before a page is written.
func TestPlacedFamiliesLiveWhereTheConfigSays(t *testing.T) {
	target := doc.Target(New(Config{Place: map[string]string{"x": "docs/{family}"}}), nil)
	if family, ok := target.Family("docs/x/README.md"); !ok || family != "x" {
		t.Fatal("the placed page is not known as x's")
	}
	if _, ok := target.Family("api/spec/x/README.md"); ok {
		t.Fatal("a placed family is still found at the default layout")
	}
	if got := target.Roots(); len(got) != 2 || got[0] != "api/spec" || got[1] != "docs" {
		t.Fatalf("roots are %v", got)
	}
	world := analysis.World(modeltest.World(map[string]map[string]string{"x": {"model.json": `{"nightseam": 2, "types": {}}`}}))
	f := render.Build(analysis.Resolve(world, "x"))
	bad := doc.Target(New(Config{Layout: "docs/spec"}), nil)
	if diagnostics := bad.Check(f); len(diagnostics) != 1 || diagnostics[0].Code != "invalid_config" {
		t.Fatalf("a layout without {family} was not refused: %v", diagnostics)
	}
}
