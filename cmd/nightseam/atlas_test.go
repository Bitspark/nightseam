package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/doc"
)

// The proof's complete page holds every settled form and every variant's
// canonical example beside the regular checkout goldens.
func TestAtlasProofGolden(t *testing.T) {
	k := compose.Kernel(module, scope, "")
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
	_, embedded, ok := strings.Cut(string(data), `<script type="application/json" id="nightseam-document">`)
	if !ok {
		t.Fatal("atlas document is missing")
	}
	embedded, _, _ = strings.Cut(embedded, "</script>")
	var document struct {
		Families []struct {
			Name  string
			Types []struct {
				Name      string
				Languages map[string]doc.Language
			}
		}
	}
	if err := json.Unmarshal([]byte(embedded), &document); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, family := range document.Families {
		if family.Name != "probe" {
			continue
		}
		for _, typ := range family.Types {
			if typ.Name != "Payload" {
				continue
			}
			found = true
			for _, language := range []string{"go", "typescript"} {
				if spelling := typ.Languages[language]; spelling.Name == "" || spelling.Declare == "" {
					t.Errorf("atlas lost the %s type metadata", language)
				}
			}
		}
	}
	if !found {
		t.Fatal("atlas lost the probe payload")
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
