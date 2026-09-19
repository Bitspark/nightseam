package doc

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// names is a writer of one page of the checkout as a whole — every
// family's name — and one page per family, its name: the smallest writer
// of both units.
type names struct{ layout Layout }

func (*names) Name() string     { return "names" }
func (n *names) Layout() Layout { return n.layout }
func (n *names) Family(f *Family) ([]spi.File, error) {
	if n.layout.Family == "" {
		return nil, nil
	}
	return []spi.File{{Path: n.layout.Dir(f.Name) + "/name.txt", Data: []byte(f.Name + "\n")}}, nil
}
func (n *names) Checkout(c *Checkout) ([]spi.File, error) {
	if len(n.layout.Checkout) == 0 {
		return nil, nil
	}
	var all []string
	for _, f := range c.Families {
		all = append(all, f.Name)
	}
	return []spi.File{{Path: n.layout.Checkout[0], Data: []byte(strings.Join(all, "\n") + "\n")}}, nil
}

func world(t *testing.T) *render.World {
	t.Helper()
	w := analysis.World(modeltest.World(map[string]map[string]string{
		"b": {"model.json": `{"nightseam": 2, "types": {}}`},
		"a": {"model.json": `{"nightseam": 2, "types": {}}`},
	}))
	return &render.World{Families: []*render.Family{render.Build(analysis.Resolve(w, "a")), render.Build(analysis.Resolve(w, "b"))}}
}

// TestTargetAnswersForBothUnits: a writer of pages per family and of the
// checkout, made a target, owns a family's directory for the family and the
// checkout's pages' directory for the checkout, knows each page as its
// family's or as the checkout's, and renders each unit through the writer.
func TestTargetAnswersForBothUnits(t *testing.T) {
	target := Target(&names{Layout{Family: "api/names/{family}", Checkout: []string{"api/names/index.txt", "api/all/every.txt"}}})
	if got := target.Owns("a"); len(got) != 1 || got[0] != "api/names/a" {
		t.Fatalf("owns %v for a", got)
	}
	if got := target.Owns(""); len(got) != 2 || got[0] != "api/all" || got[1] != "api/names" {
		t.Fatalf("owns %v for the checkout", got)
	}
	if got := target.Roots(); len(got) != 2 || got[0] != "api/all" || got[1] != "api/names" {
		t.Fatalf("roots are %v", got)
	}
	if family, ok := target.Family("api/names/a/name.txt"); !ok || family != "a" {
		t.Fatal("a family's page is not known as its own")
	}
	if family, ok := target.Family("api/names/index.txt"); !ok || family != "" {
		t.Fatal("a page of the checkout is not known as the checkout's")
	}
	if _, ok := target.Family("api/names/README.md"); ok {
		t.Fatal("a file that is no page is somebody's")
	}
	w := world(t)
	files, err := target.Render(w.Families[0])
	if err != nil || len(files) != 1 || files[0].Path != "api/names/a/name.txt" || string(files[0].Data) != "a\n" {
		t.Fatalf("rendered %v, %v for a", files, err)
	}
	files, err = target.(spi.CheckoutRenderer).RenderCheckout(w)
	if err != nil || len(files) != 1 || files[0].Path != "api/names/index.txt" || string(files[0].Data) != "a\nb\n" {
		t.Fatalf("rendered %v, %v for the checkout", files, err)
	}
}

// TestTargetOfOneUnitOwnsNothingOfTheOther: a writer with no pages of a unit
// owns nothing for it, renders nothing for it, and its layout refuses a
// page path that names a family or escapes the checkout.
func TestTargetOfOneUnitOwnsNothingOfTheOther(t *testing.T) {
	whole := Target(&names{Layout{Checkout: []string{"api/all/every.txt"}}})
	if got := whole.Owns("a"); len(got) != 0 {
		t.Fatalf("a writer of the checkout alone owns %v for a family", got)
	}
	if _, ok := whole.Family("api/all/a/name.txt"); ok {
		t.Fatal("a writer of the checkout alone knows a family's page")
	}
	if files, err := whole.Render(world(t).Families[0]); err != nil || len(files) != 0 {
		t.Fatalf("a writer of the checkout alone rendered %v, %v for a family", files, err)
	}
	each := Target(&names{Layout{Family: "api/names/{family}"}})
	if got := each.Owns(""); len(got) != 0 {
		t.Fatalf("a writer of families alone owns %v for the checkout", got)
	}
	if files, err := each.(spi.CheckoutRenderer).RenderCheckout(world(t)); err != nil || len(files) != 0 {
		t.Fatalf("a writer of families alone rendered %v, %v for the checkout", files, err)
	}
	for _, bad := range []Layout{{Checkout: []string{"api/{family}/index.txt"}}, {Checkout: []string{"../index.txt"}}, {Family: "api/names"}, {Place: map[string]string{"a": "docs/a"}}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("layout %v was not refused", bad)
		}
	}
}
