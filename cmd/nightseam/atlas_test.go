package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/targets/atlas"
)

// The proof's complete page holds every settled form and every variant's
// canonical example beside the regular checkout goldens.
func TestAtlasProofGolden(t *testing.T) {
	k := kernel.New(doc.Target(atlas.New(atlas.Config{})))
	world := k.Load(os.DirFS("testdata"), "proof")
	result, err := k.RenderCheckout(world)
	if err != nil {
		t.Fatal(err)
	}
	holdGolden(t, "testdata/golden-proof-atlas", result.Files)
}

func TestAtlasConfigAndCheckoutOwnership(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFixture(t, root, "api/contracts/nightseam.json", []byte(`{"targets":{"atlas":{"title":"Our protocol","tokens":{"--lamp":"#864"}}}}`))
	if out, errs, err := run(t, root, "generate"); err != nil {
		t.Fatalf("%v\n%s%s", err, out, errs)
	}
	page := filepath.Join(root, "api", "spec", "index.html")
	data, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(data), "<title>Our protocol</title>") || !strings.Contains(string(data), "--lamp: #864;") {
		t.Fatalf("config did not reach atlas: %v", err)
	}
	if _, errs, err := run(t, root, "check"); err != nil {
		t.Fatalf("check: %v\n%s", err, errs)
	}
	if err := os.WriteFile(page, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := run(t, root, "check"); err == nil || !strings.Contains(errs, "stale generated output: api/spec/index.html") {
		t.Fatalf("stale atlas was not reported: %v\n%s", err, errs)
	}
	writeFixture(t, root, "api/contracts/nightseam.json", []byte(`{"disabled":["atlas"]}`))
	if _, errs, err := run(t, root, "check"); err != nil {
		t.Fatalf("disabled atlas still checked: %v\n%s", err, errs)
	}
}
