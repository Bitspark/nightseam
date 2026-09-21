package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	nightseam = "github.com/Bitspark/nightseam"
	archon    = "github.com/Bitspark/archon/"
)

// TestImportDirection holds the seam this module exists for: the profile
// imports Archon — core for the Ed25519 floor and the envelope, sdk for
// possession and login — and of Nightseam only the runtime it composes onto;
// the root module imports nothing of Archon and nothing of this module,
// neither a package of it nor a line of it in go.mod, the core module's
// dependency-freedom being a released promise (RELEASING.md). The direction
// is one way, which is why the profile is a module of its own and is
// released by a tag of its own. A consumer that adopts no authority profile
// installs no identity layer.
func TestImportDirection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	type pkg struct {
		ImportPath                         string
		Imports, TestImports, XTestImports []string
	}
	own, err := exec.CommandContext(ctx, "go", "list", "-json", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(own))
	for {
		var p pkg
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, imported := range p.Imports {
			switch {
			case strings.HasPrefix(imported, archon):
			case strings.HasPrefix(imported, nightseam+"/auth/"):
			case imported == nightseam+"/runtime/go", imported == nightseam+"/duplex/go", imported == nightseam+"/live/go":
			case strings.HasPrefix(imported, nightseam):
				t.Errorf("%s imports %s; the profile composes onto the runtime, the wire and the live scope, and onto nothing else of Nightseam", p.ImportPath, imported)
			}
		}
	}

	root := checkout(t)
	listed := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	listed.Dir = root
	listed.Env = append(os.Environ(), "GOWORK=off")
	packages, err := listed.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(bytes.NewReader(packages))
	for {
		var p pkg
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, imports := range [][]string{p.Imports, p.TestImports, p.XTestImports} {
			for _, imported := range imports {
				if strings.HasPrefix(imported, archon) || strings.HasPrefix(imported, nightseam+"/auth/") {
					t.Errorf("%s imports %s; the core module depends on nothing and carries no identity layer", p.ImportPath, imported)
				}
			}
		}
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(module), "archon") {
		t.Error("the root go.mod names Archon; the core module's dependency-freedom is what is published")
	}
}

// checkout is the nearest ancestor of the test's directory that holds the
// root module's go.mod, so moving this module does not move what it is held
// against.
func checkout(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module "+nightseam+"\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod of the root module above the test directory")
		}
		dir = parent
	}
}
