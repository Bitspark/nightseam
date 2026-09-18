package otel_test

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

const nightseam = "github.com/Bitspark/nightseam"

// TestImportDirection holds the seam this module exists for: the adapter
// imports the runtime and nothing else of Nightseam, and the root module
// imports nothing of OpenTelemetry — neither a package of it nor a line of it
// in go.mod, the core module's dependency-freedom being a released promise
// (RELEASING.md). The direction is one way, which is why the adapter is a
// module of its own and is released by a tag of its own.
//
// What this package's own tests import is another matter: they speak over a
// tunnel and a session to hold the adapter to the traffic of a relay, which
// is what a test of an adapter must do and what the package itself never does.
func TestImportDirection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	type pkg struct {
		ImportPath                         string
		Imports, TestImports, XTestImports []string
	}
	adapter, err := exec.CommandContext(ctx, "go", "list", "-json", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	var adapted pkg
	if err := json.Unmarshal(adapter, &adapted); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, imported := range adapted.Imports {
		if !strings.HasPrefix(imported, nightseam) {
			continue
		}
		if imported != nightseam+"/runtime/go" {
			t.Errorf("the adapter imports %s; it binds to the runtime and to nothing else of Nightseam", imported)
			continue
		}
		seen = true
	}
	if !seen {
		t.Error("the adapter imports no runtime; it is an adapter of one")
	}

	root := checkout(t)
	listed := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	listed.Dir = root
	listed.Env = append(os.Environ(), "GOWORK=off")
	packages, err := listed.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(packages))
	for {
		var p pkg
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, imports := range [][]string{p.Imports, p.TestImports, p.XTestImports} {
			for _, imported := range imports {
				if strings.HasPrefix(imported, "go.opentelemetry.io/") || strings.HasPrefix(imported, nightseam+"/otel/") {
					t.Errorf("%s imports %s; the core module depends on nothing and chooses no backend", p.ImportPath, imported)
				}
			}
		}
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(module), "opentelemetry") {
		t.Error("the root go.mod names OpenTelemetry; the core module's dependency-freedom is what is published")
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
