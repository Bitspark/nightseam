package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// TestVersionsMoveInLockstep holds every spelling of the version to one
// number: the four TypeScript packages and the version the generator writes
// into a generated client's manifest. The Go module's version is its tag,
// which the release workflow holds to the same number (RELEASING.md).
func TestVersionsMoveInLockstep(t *testing.T) {
	root := repositoryRoot(t)
	for _, directory := range []string{"duplex/ts", "runtime/ts", "tunnel/ts", "session/ts"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(directory), "package.json"))
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
