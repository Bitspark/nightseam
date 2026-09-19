package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// TestVersionsMoveInLockstep holds every spelling of the version to one
// number: every published TypeScript package, the version the generator
// writes into a generated client's manifest, the version every nested Go
// module requires the root module at, and what the getting-started example
// under examples/ depends on in both languages. The Go module's version is
// its tag, which the release workflow holds to the same number
// (RELEASING.md). All of them are found rather than listed, the way
// scripts/packages.mjs and scripts/version.mjs find them, so that a
// component added beside the others is held to the one version without
// anyone remembering to name it here.
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
	published := ""
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
		// A private manifest is a workspace member and not a release — the
		// conformance testee — and scripts/packages.mjs leaves it out the same way.
		if manifest.Private {
			continue
		}
		if manifest.Version != typescript.DefaultRuntimeVersion {
			t.Errorf("%s is %s; the generator writes %s into generated manifests (scripts/version.mjs sets both)", manifest.Name, manifest.Version, typescript.DefaultRuntimeVersion)
		}
		published = manifest.Version
	}

	// A component that depends on what the core module may not — otel/go, the
	// OpenTelemetry adapter — is a Go module of its own, released by a second
	// tag beside the root module's and requiring the root module at the same
	// number. Nothing in Go spells that number, a tag being a module's whole
	// release, so it is read off a manifest above. The replace beside the
	// requirement is what makes the repository build against itself; a
	// consumer ignores a dependency's replace and gets what is required.
	modules, err := filepath.Glob(filepath.Join(root, "*", "go", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) == 0 {
		t.Fatal("found no nested Go module under */go; otel/go is one")
	}
	for _, file := range modules {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		module := string(data)
		if required := "github.com/Bitspark/nightseam v" + published; !strings.Contains(module, required) {
			t.Errorf("%s does not require %q; scripts/version.mjs writes it there", file, required)
		}
		if !strings.Contains(module, "replace github.com/Bitspark/nightseam => ../..") {
			t.Errorf("%s does not replace the root module with this checkout", file)
		}
	}

	// The getting-started example under examples/ is the other way round: a
	// consumer checkout rather than a member of this one, which names the
	// released version of everything it depends on and carries neither a
	// workspace link nor a replace to stand in for one. That is what lets
	// the release smokes install it from outside the workspace — from the
	// packed tarballs before the tag, from npm and the module proxy after
	// it — so a version that drifted here is a release whose own example
	// asks for something else.
	examples, err := filepath.Glob(filepath.Join(root, "examples", "*", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("found no example under examples/*/go.mod; the getting-started is the consumer both release smokes install")
	}
	for _, file := range examples {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		module := string(data)
		if required := "github.com/Bitspark/nightseam v" + published; !strings.Contains(module, required) {
			t.Errorf("%s does not require %q; scripts/version.mjs writes it there", file, required)
		}
		if strings.Contains(module, "replace github.com/Bitspark/nightseam") {
			t.Errorf("%s replaces the root module; the example resolves what is published, which is what the smokes install it to find out", file)
		}
		err = filepath.WalkDir(filepath.Dir(file), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Name() != "package.json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var manifest struct {
				Dependencies    map[string]string `json:"dependencies"`
				DevDependencies map[string]string `json:"devDependencies"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return err
			}
			where, _ := filepath.Rel(root, path)
			for _, group := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
				for name, spec := range group {
					if !strings.HasPrefix(name, "@nightseam/") {
						continue
					}
					if spec != published {
						t.Errorf("%s depends on %s at %s, not %s; an example that resolves through a link is one nobody installed", filepath.ToSlash(where), name, spec, published)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
