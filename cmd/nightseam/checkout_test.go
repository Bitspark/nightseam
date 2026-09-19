package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/spi"
)

// roster is a writer of one page of the checkout as a whole: every family's
// name, one per line — what a page across families is to the tool, which
// has no such writer of its own yet.
type roster struct{}

func (roster) Name() string                           { return "roster" }
func (roster) Layout() doc.Layout                     { return doc.Layout{Checkout: []string{"api/spec/families.txt"}} }
func (roster) Family(*doc.Family) ([]spi.File, error) { return nil, nil }
func (roster) Checkout(c *doc.Checkout) ([]spi.File, error) {
	var names []string
	for _, f := range c.Families {
		names = append(names, f.Name)
	}
	return []spi.File{{Path: "api/spec/families.txt", Data: []byte(strings.Join(names, "\n") + "\n")}}, nil
}

// TestCheckoutPagesAreRenderedWhole: a target that renders the checkout as
// a whole has its page written by generate beside the families', held by
// check — stale when it moves, current when it does not — and rewritten
// rather than removed when a family goes, since the checkout remains.
func TestCheckoutPagesAreRenderedWhole(t *testing.T) {
	previous := toolTargets
	toolTargets = func(module, scope, sibling string) []spi.Target {
		return append(compose.Targets(module, scope, sibling), doc.Target(roster{}))
	}
	defer func() { toolTargets = previous }()

	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFamily(t, root, "codex")
	page := filepath.Join(root, "api", "spec", "families.txt")
	if out, _, err := run(t, root, "generate"); err != nil || !strings.Contains(out, "generated api/spec/families.txt") {
		t.Fatalf("generate did not write the checkout's page: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(page); err != nil || string(data) != "codex\nprobe\n" {
		t.Fatalf("the page holds %q, %v", data, err)
	}
	if out, errs, err := run(t, root, "check"); err != nil || out != "" || errs != "" {
		t.Fatalf("check after generate: %v\n%s%s", err, out, errs)
	}
	if err := os.WriteFile(page, []byte("moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := run(t, root, "check"); err == nil || !strings.Contains(errs, "stale generated output: api/spec/families.txt") {
		t.Fatalf("check passed a page that moved: %v\n%s", err, errs)
	}
	// One family named: its files are rendered, and the checkout's page with
	// them, since the checkout is rendered on every run.
	if out, _, err := run(t, root, "generate", "probe"); err != nil || !strings.Contains(out, "generated api/spec/families.txt") {
		t.Fatalf("generate of one family did not restore the checkout's page: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(root, "api", "contracts", "codex")); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, root, "generate")
	if err != nil || !strings.Contains(out, "removed api/spec/codex/README.md") || strings.Contains(out, "removed api/spec/families.txt") {
		t.Fatalf("a family removed did not take its own pages and leave the checkout's: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(page); err != nil || string(data) != "probe\n" {
		t.Fatalf("the page holds %q after the family went, %v", data, err)
	}
	// An invalid family refuses the checkout's page, naming itself, even
	// where another family alone was named.
	writeFixture(t, root, "api/contracts/codex/model.json", []byte(`{"nightseam": 2, "types": {"X": {"kind": "record", "fields": [{"name": "y", "type": "Nope"}]}}}`))
	if _, _, err := run(t, root, "generate", "probe"); err == nil || !strings.Contains(err.Error(), "codex: invalid family") {
		t.Fatalf("an invalid family did not refuse the checkout's page: %v", err)
	}
}
