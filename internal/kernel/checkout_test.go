package kernel

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// index is a target that renders one file of the checkout as a whole — the
// names of every family — and nothing of any family: the kind of target a
// page across families is.
type index struct{ refuse bool }

func (*index) Name() string                              { return "index" }
func (*index) Consumes() []spi.Concern                   { return []spi.Concern{spi.Model} }
func (*index) Roots() []string                           { return []string{"api/index"} }
func (*index) Check(*render.Family) []diag.Diagnostic    { return nil }
func (*index) Render(*render.Family) ([]spi.File, error) { return nil, nil }
func (*index) Owns(family string) []string {
	if family == "" {
		return []string{"api/index"}
	}
	return nil
}
func (*index) Family(p string) (string, bool) {
	if p == "api/index/families.txt" || p == "api/index/other.txt" {
		return "", true
	}
	return "", false
}
func (i *index) RenderCheckout(w *render.World) ([]spi.File, error) {
	var names []string
	for _, f := range w.Families {
		names = append(names, f.Name)
	}
	path := "api/index/families.txt"
	if i.refuse {
		path = "elsewhere/families.txt"
	}
	return []spi.File{{Path: path, Data: []byte(strings.Join(names, "\n") + "\n")}}, nil
}

func checkoutWorld(t *testing.T, families map[string]string) (fstest.MapFS, *World) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, model := range families {
		fsys["api/contracts/"+name+"/model.json"] = &fstest.MapFile{Data: []byte(model)}
	}
	return fsys, Load(fsys, "api/contracts", []string{"index"})
}

// TestRenderCheckoutRendersEveryFamilyWhole: a target that renders the
// checkout as a whole sees every family the checkout has, in name order,
// and its file lies under what it owns for the checkout, family "".
func TestRenderCheckoutRendersEveryFamilyWhole(t *testing.T) {
	_, world := checkoutWorld(t, map[string]string{
		"b": `{"nightseam": 2, "types": {}}`,
		"a": `{"nightseam": 2, "types": {}}`,
	})
	result, err := New(&index{}).RenderCheckout(world)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.Files["api/index/families.txt"]); got != "a\nb\n" {
		t.Fatalf("the checkout's file holds %q", got)
	}
	if _, err := New(&index{refuse: true}).RenderCheckout(world); err == nil || !strings.Contains(err.Error(), "outside the directories the target owns for the checkout") {
		t.Fatalf("a file outside what the target owns for the checkout was not refused: %v", err)
	}
}

// TestRenderCheckoutIsWholeOrNotAtAll: a family with a diagnostic refuses
// the checkout's rendering, naming itself; and where no target renders the
// checkout as a whole nothing is rendered and nothing is validated, so a
// checkout of families alone is held one family at a time, as before.
func TestRenderCheckoutIsWholeOrNotAtAll(t *testing.T) {
	_, world := checkoutWorld(t, map[string]string{
		"good": `{"nightseam": 2, "types": {}}`,
		"bad":  `{"nightseam": 2, "types": {"X": {"kind": "record", "fields": [{"name": "y", "type": "Nope"}]}}}`,
	})
	if _, err := New(&index{}).RenderCheckout(world); err == nil || !strings.HasPrefix(err.Error(), "bad: invalid family:") {
		t.Fatalf("an invalid family did not refuse the checkout: %v", err)
	}
	result, err := New().RenderCheckout(world)
	if err != nil || len(result.Files) != 0 {
		t.Fatalf("a kernel with no checkout renderer rendered %v, %v", result.Files, err)
	}
}

// TestStaleHoldsTheCheckoutsFiles: a file of the checkout as a whole that
// nothing rendered this run is stale, since the checkout is rendered on
// every run; one that was rendered is current; and a family removed does
// not make the checkout's file a leftover, since the checkout never ceases
// to exist.
func TestStaleHoldsTheCheckoutsFiles(t *testing.T) {
	fsys, world := checkoutWorld(t, map[string]string{"a": `{"nightseam": 2, "types": {}}`})
	fsys["api/index/families.txt"] = &fstest.MapFile{Data: []byte("a\nremoved\n")}
	fsys["api/index/other.txt"] = &fstest.MapFile{Data: []byte("an old page\n")}
	fsys["api/index/tool.lock"] = &fstest.MapFile{Data: []byte("nobody's\n")}
	k := New(&index{})
	result, err := k.RenderCheckout(world)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := k.Stale(fsys, world, []string{"a"}, result.Files)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(stale)
	if len(stale) != 1 || stale[0] != "api/index/other.txt" {
		t.Fatalf("stale is %v: the rendered file of the checkout is current, the one nothing rendered is stale, the file that is nobody's is left alone", stale)
	}
	stale, err = k.Stale(fsys, world, []string{"a"}, map[string][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(stale)
	if len(stale) != 2 || stale[0] != "api/index/families.txt" || stale[1] != "api/index/other.txt" {
		t.Fatalf("stale is %v: a checkout file nothing rendered is stale whatever families were chosen", stale)
	}
}
