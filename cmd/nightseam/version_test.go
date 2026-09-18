package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// TestVersionsMoveInLockstep holds every spelling of the version to one
// number: every published TypeScript package and the version the generator
// writes into a generated client's manifest. The Go module's version is its
// tag, which the release workflow holds to the same number (RELEASING.md).
// The packages are found rather than listed, the way scripts/packages.mjs
// finds them, so that a component added beside the others is held to the one
// version without anyone remembering to name it here.
func TestVersionsMoveInLockstep(t *testing.T) {
	root := repositoryRoot(t)
	manifests, err := filepath.Glob(filepath.Join(root, "*", "ts", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A glob that matched nothing would hold nothing and say so by passing.
	if len(manifests) < 4 {
		t.Fatalf("found %d manifests under */ts; the workspace has more than that", len(manifests))
	}
	for _, file := range manifests {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Private bool   `json:"private"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Version != typescript.DefaultRuntimeVersion {
			t.Errorf("%s is %s; the generator writes %s into generated manifests (scripts/version.mjs sets both)", manifest.Name, manifest.Version, typescript.DefaultRuntimeVersion)
		}
		if manifest.Private {
			t.Errorf("%s is private; it is published", manifest.Name)
		}
	}
}
