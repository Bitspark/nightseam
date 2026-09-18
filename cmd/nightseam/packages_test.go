package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPublishedPackageIsTested holds every published TypeScript package
// to having tests of its own and running them. `pnpm -r test` runs the script
// where there is one and passes over a package where there is none, so a
// package with no script is not a package that fails: it is one nobody hears
// about, and @nightseam/duplex — the seam every other package runs over —
// published that way for two releases. The parity rule of COLLABORATION.md
// names the suites by name and is therefore read rather than run; this is the
// half of it a test can hold. The packages are found the way
// scripts/packages.mjs and TestVersionsMoveInLockstep find them, so a
// component added beside the others is held to the same rule without anyone
// remembering to name it here.
//
// It asks three things of each: a test script, a file the script names for
// every test file in the package, and every file the script names present on
// disk — the last because a renamed test file leaves a script that runs
// nothing until node refuses to start, which is a failure of the tier that
// runs it and not of the tier that reads it.
func TestEveryPublishedPackageIsTested(t *testing.T) {
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
		directory := filepath.Dir(file)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Name    string            `json:"name"`
			Private bool              `json:"private"`
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Private {
			continue
		}
		script := manifest.Scripts["test"]
		if strings.TrimSpace(script) == "" {
			t.Errorf("%s has no test script; pnpm -r test passes over it in silence", manifest.Name)
			continue
		}
		named := map[string]bool{}
		for _, word := range strings.Fields(script) {
			if !strings.HasSuffix(word, ".test.ts") {
				continue
			}
			named[filepath.ToSlash(word)] = true
			if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(word))); err != nil {
				t.Errorf("%s runs %s, which is not there", manifest.Name, word)
			}
		}
		if len(named) == 0 {
			t.Errorf("%s has a test script naming no test file: %s", manifest.Name, script)
		}
		onDisk, err := filepath.Glob(filepath.Join(directory, "src", "*.test.ts"))
		if err != nil {
			t.Fatal(err)
		}
		if len(onDisk) == 0 {
			t.Errorf("%s has no test file under src; its language's half of the parity rule is unheld", manifest.Name)
		}
		for _, test := range onDisk {
			relative, err := filepath.Rel(directory, test)
			if err != nil {
				t.Fatal(err)
			}
			if !named[filepath.ToSlash(relative)] {
				t.Errorf("%s does not run %s; a test file nobody runs holds nothing", manifest.Name, filepath.ToSlash(relative))
			}
		}
	}
}
