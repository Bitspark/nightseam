package doc

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Writer renders the document in one format. It declares its unit by what
// it answers: pages of one family, pages of the checkout as a whole, or
// both. A writer that has no pages of a unit answers nil for it.
type Writer interface {
	// Name identifies the writer: what its target is called, and so what
	// its config in the checkout's nightseam.json is keyed by.
	Name() string
	// Layout says where the writer's pages go.
	Layout() Layout
	// Family renders the pages of one family.
	Family(f *Family) ([]spi.File, error)
	// Checkout renders the pages of the checkout as a whole.
	Checkout(c *Checkout) ([]spi.File, error)
}

// Layout is where a writer's pages live, relative to the checkout. Family
// is a pattern over the family's name, {family}, the directory a family's
// pages are under; Place puts named families elsewhere, each at a pattern
// of its own; Checkout names the checkout's pages, each an exact path. A
// writer with no pages of a unit leaves that part empty.
type Layout struct {
	Family   string
	Place    map[string]string
	Checkout []string
}

var pathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./{}-]*$`)

// Validate reports what is wrong with the layout.
func (l Layout) Validate() error {
	patterns := []string{}
	if l.Family != "" || len(l.Place) > 0 {
		patterns = append(patterns, l.Family)
	}
	for _, placed := range l.Place {
		patterns = append(patterns, placed)
	}
	for _, pattern := range patterns {
		if !strings.Contains(pattern, "{family}") {
			return fmt.Errorf("a layout pattern names the family with {family}: %q", pattern)
		}
		if !validPath(strings.ReplaceAll(pattern, "{family}", "x")) {
			return fmt.Errorf("invalid output path pattern %q", pattern)
		}
	}
	for _, p := range l.Checkout {
		if strings.Contains(p, "{family}") || !validPath(p) {
			return fmt.Errorf("invalid checkout page path %q", p)
		}
	}
	return nil
}

func validPath(p string) bool {
	return p != "" && p != "." && p != ".." && !strings.Contains(p, "\\") && !strings.Contains(p, ":") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.HasPrefix(p, "../") && pathPattern.MatchString(p)
}

// Dir is the directory a family's pages are under.
func (l Layout) Dir(family string) string {
	if placed, ok := l.Place[family]; ok {
		return strings.ReplaceAll(placed, "{family}", family)
	}
	return strings.ReplaceAll(l.Family, "{family}", family)
}

// Target makes a writer a target of the generator: what the kernel asks of
// a target — what it owns, where its files are found, whose a file is,
// what it refuses, what it renders — is answered here once, from the
// writer's layout, so that a writer implements none of it and every format
// is held by the kernel the same way.
func Target(w Writer, spellers map[string]spi.Speller) spi.Target {
	return &target{w: w, layout: w.Layout(), spellers: spellers}
}

type target struct {
	spellers map[string]spi.Speller
	w        Writer
	layout   Layout
}

func (t *target) Name() string { return t.w.Name() }

// Consumes is every concern: a document documents all of them.
func (*target) Consumes() []spi.Concern { return []spi.Concern{spi.Model, spi.Protocol, spi.Live} }

// Owns is the directory a family's pages are under, or, for the checkout
// as a whole, the directories its pages lie in.
func (t *target) Owns(family string) []string {
	if family == "" {
		return t.checkoutDirs()
	}
	if t.layout.Family == "" {
		return nil
	}
	return []string{t.layout.Dir(family)}
}

func (t *target) checkoutDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, p := range t.layout.Checkout {
		if dir := path.Dir(p); !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

func (t *target) Roots() []string {
	seen := map[string]bool{}
	var roots []string
	add := func(root string) {
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	if t.layout.Family != "" {
		add(spi.PatternRoot(t.layout.Family))
	}
	for _, placed := range t.layout.Place {
		add(spi.PatternRoot(placed))
	}
	for _, dir := range t.checkoutDirs() {
		add(dir)
	}
	sort.Strings(roots)
	return roots
}

// Family answers a family's page with the family, and a page of the
// checkout as a whole with "".
func (t *target) Family(p string) (string, bool) {
	for _, page := range t.layout.Checkout {
		if p == page {
			return "", true
		}
	}
	for family, placed := range t.layout.Place {
		if matched, ok := spi.MatchPattern(placed, p); ok && matched == family {
			return family, true
		}
	}
	if t.layout.Family == "" {
		return "", false
	}
	if family, ok := spi.MatchPattern(t.layout.Family, p); ok {
		if _, placed := t.layout.Place[family]; !placed {
			return family, true
		}
	}
	return "", false
}

// Check refuses a layout that cannot be written to, and nothing else: the
// document describes every form the neutral checks accept, and a writer
// declares no identifier a family could collide with.
func (t *target) Check(f *render.Family) []diag.Diagnostic {
	if err := t.layout.Validate(); err != nil {
		return []diag.Diagnostic{{Family: f.Name, Code: "invalid_config", Message: err.Error()}}
	}
	return nil
}

// Render writes the pages of one family.
func (t *target) Render(f *render.Family) ([]spi.File, error) {
	if err := t.layout.Validate(); err != nil {
		return nil, err
	}
	return t.w.Family(Build(f, t.spellers))
}

// RenderCheckout writes the pages of the checkout as a whole.
func (t *target) RenderCheckout(w *render.World) ([]spi.File, error) {
	if err := t.layout.Validate(); err != nil {
		return nil, err
	}
	return t.w.Checkout(BuildCheckout(w, t.spellers))
}
