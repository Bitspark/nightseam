package kernel

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

func TestImplicitFamilyRenderingAndLifetime(t *testing.T) {
	files := fstest.MapFS{
		"api/contracts/a/model.json":    {Data: []byte(`{"nightseam":2}`)},
		"api/contracts/a/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1"}`)},
		"api/contracts/a/session.json":  {Data: []byte(`{}`)},
		"api/contracts/b/model.json":    {Data: []byte(`{"nightseam":2}`)},
		"api/contracts/b/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1"}`)},
	}
	k := New(implicitTarget{})
	world := k.Load(files, "api/contracts")
	result, err := k.Render(world, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 || result.Files["generated/a/source"] == nil || result.Files["generated/session/source"] == nil {
		t.Fatalf("a session family must generate its built-in payload package: %v", result.Files)
	}
	for path, data := range result.Files {
		files[path] = &fstest.MapFile{Data: data}
	}
	files["generated/session/obsolete"] = &fstest.MapFile{Data: []byte("old output")}
	stale, err := k.Stale(files, world, []string{"a"}, result.Files)
	if err != nil || !slices.Equal(stale, []string{"generated/session/obsolete"}) {
		t.Fatalf("regenerating a dependency must remove its obsolete outputs: %v, %v", stale, err)
	}
	plain, err := k.Render(world, "b")
	if err != nil || len(plain.Files) != 1 || plain.Files["generated/b/source"] == nil {
		t.Fatalf("a protocol-only family generated an implicit session package: %v, %v", plain, err)
	}
	stale, err = k.Stale(files, world, []string{"b"}, plain.Files)
	if err != nil || len(stale) != 0 {
		t.Fatalf("partial generation must preserve a dependency another family still needs: %v, %v", stale, err)
	}
	delete(files, "api/contracts/a/session.json")
	world = k.Load(files, "api/contracts")
	stale, err = k.Stale(files, world, []string{"b"}, plain.Files)
	if err != nil || !slices.Equal(stale, []string{"generated/session/obsolete", "generated/session/source"}) {
		t.Fatalf("a dependency nobody needs must become stale: %v, %v", stale, err)
	}
	files["api/contracts/a/session.json"] = &fstest.MapFile{Data: []byte(`{}`)}
	refusing := New(implicitTarget{refuse: "session"})
	problems := refusing.Validate(refusing.Load(files, "api/contracts"), "a")
	if len(problems) != 1 || problems[0].Code != "cannot_render" || problems[0].Family != "session" {
		t.Fatalf("a target's refusal of the built-in must refuse the dependent family: %v", problems)
	}
}

// A target with no language semantics isolates ownership, dependency and
// stale-file behavior from the two language renderers' implementation.
type implicitTarget struct{ refuse string }

func (implicitTarget) Name() string                { return "fixture" }
func (implicitTarget) Consumes() []spi.Concern     { return []spi.Concern{spi.Model} }
func (implicitTarget) Owns(family string) []string { return []string{"generated/" + family} }
func (implicitTarget) Roots() []string             { return []string{"generated"} }
func (implicitTarget) Family(path string) (string, bool) {
	return spi.MatchPattern("generated/{family}", path)
}
func (target implicitTarget) Check(f *render.Family) []diag.Diagnostic {
	if f.Name == target.refuse {
		return []diag.Diagnostic{{Family: f.Name, Code: "cannot_render", Message: "fixture refusal"}}
	}
	return nil
}
func (implicitTarget) Render(f *render.Family) ([]spi.File, error) {
	return []spi.File{{Path: "generated/" + f.Name + "/source", Data: []byte(strings.Join(f.References, ",") + "\n")}}, nil
}
