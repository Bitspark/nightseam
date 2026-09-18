package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryGoModuleIsOfferedUpdates holds .github/dependabot.yml to the Go
// modules on disk. A gomod entry covers the module in one directory and does
// not enter a nested one, so a module added beside otel/go would be offered
// no update at all — and would say nothing about it, an unoffered module
// looking exactly like one with nothing to offer. Dependabot's configuration
// runs nothing, so the list cannot be derived the way scripts/packages.mjs
// derives the published packages; it is written by hand and held here, the
// way TestVersionsMoveInLockstep holds the versions. The check runs both
// ways: a module with no entry, and an entry naming no module.
func TestEveryGoModuleIsOfferedUpdates(t *testing.T) {
	root := repositoryRoot(t)
	config, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}

	// The root module, and every nested one, as Dependabot spells a
	// directory: absolute from the repository root, with forward slashes.
	modules := map[string]bool{"/": false}
	nested, err := filepath.Glob(filepath.Join(root, "*", "go", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	// A glob that matched nothing would hold nothing and say so by passing.
	if len(nested) == 0 {
		t.Fatal("found no nested Go module under */go; otel/go is one")
	}
	for _, file := range nested {
		directory, err := filepath.Rel(root, filepath.Dir(file))
		if err != nil {
			t.Fatal(err)
		}
		modules["/"+filepath.ToSlash(directory)] = false
	}

	for _, directory := range gomodDirectories(t, string(config)) {
		if _, ok := modules[directory]; !ok {
			t.Errorf("dependabot.yml offers updates for the gomod directory %s, which holds no go.mod", directory)
			continue
		}
		modules[directory] = true
	}
	for directory, covered := range modules {
		if !covered {
			t.Errorf("no gomod entry in dependabot.yml names %s; its requirements are never offered an update", directory)
		}
	}
}

// gomodDirectories reads the directory of every gomod update out of the
// configuration. Dependabot's file is small and flat — a list of entries,
// each opening with its ecosystem — and reading it this way costs the root
// module a YAML dependency it does not otherwise have, which is the same
// trade the rest of this file makes.
func gomodDirectories(t *testing.T, config string) []string {
	t.Helper()
	var directories []string
	ecosystem := ""
	for _, line := range strings.Split(config, "\n") {
		field := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(field, "- package-ecosystem:"):
			ecosystem = strings.TrimSpace(strings.TrimPrefix(field, "- package-ecosystem:"))
		case strings.HasPrefix(field, "directory:") && ecosystem == "gomod":
			directories = append(directories, strings.TrimSpace(strings.TrimPrefix(field, "directory:")))
		}
	}
	if len(directories) == 0 {
		t.Fatal("dependabot.yml declares no gomod update at all")
	}
	return directories
}
