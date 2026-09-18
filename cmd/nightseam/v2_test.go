package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/kernel"
)

// The v2 generator, run in-process until the CLI switches to it: the v2
// corpora validate as worlds, and each case under testdata/v2/invalid is
// refused with what its diagnostics.txt holds, one line per diagnostic as
// validate will print them.

var v2Targets = []string{"go", "typescript"}

// TestV2CorporaAreValid: every family of both v2 corpora has nothing to be
// told by the model's and the concerns' checks.
func TestV2CorporaAreValid(t *testing.T) {
	for _, corpus := range []string{"corpus", "families"} {
		world := kernel.Load(os.DirFS(filepath.Join(v2Root, corpus)), "api/contracts", v2Targets)
		if len(world.Names) == 0 {
			t.Fatalf("%s holds no families", corpus)
		}
		for _, name := range world.Names {
			for _, d := range kernel.Validate(world, name) {
				t.Errorf("%s: %s", corpus, d)
			}
		}
	}
}

// TestInvalidCorpusIsRefusedV2: each case is refused, and says exactly what
// its diagnostics.txt holds.
func TestInvalidCorpusIsRefusedV2(t *testing.T) {
	root := filepath.Join(v2Root, "invalid")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			caseRoot := filepath.Join(root, entry.Name())
			world := kernel.Load(os.DirFS(caseRoot), "api/contracts", v2Targets)
			var report strings.Builder
			problems := 0
			// Every family the checkout names, and every one the loader had
			// something to say about, whether or not it could be read.
			seen := map[string]bool{}
			var names []string
			for _, name := range world.Names {
				seen[name] = true
				names = append(names, name)
			}
			for name := range world.Problems {
				if !seen[name] {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			for _, name := range names {
				for _, d := range kernel.Validate(world, name) {
					fmt.Fprintln(&report, d)
					problems++
				}
			}
			if problems == 0 {
				t.Fatal("the case was accepted")
			}
			fmt.Fprintf(&report, "error: %d problems\n", problems)
			got := report.String()
			expectation := filepath.Join(caseRoot, "diagnostics.txt")
			if *update {
				if err := os.WriteFile(expectation, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(expectation)
			if os.IsNotExist(err) {
				t.Fatalf("no diagnostics.txt; run with -update. validate says:\n%s", got)
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Errorf("validate says something else than diagnostics.txt:\n%s", diff(string(want), got))
			}
		})
	}
}

// renderV2 renders every family of a v2 checkout with the v2 kernel.
func renderV2(t *testing.T, root string) map[string][]byte {
	t.Helper()
	k := v2Kernel(module, scope)
	world := k.Load(os.DirFS(root), "api/contracts")
	files := map[string][]byte{}
	for _, name := range world.Names {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for p, data := range result.Files {
			if previous, exists := files[p]; exists && string(previous) != string(data) {
				t.Fatalf("%s is rendered twice, differently", p)
			}
			files[p] = data
		}
	}
	return files
}

// TestV2CorpusRendersGolden: what every v2 target renders for the v2 corpus
// is exactly the files under testdata/v2/golden.
func TestV2CorpusRendersGolden(t *testing.T) {
	holdGolden(t, filepath.Join(v2Root, "golden"), renderV2(t, filepath.Join(v2Root, "corpus")))
}

// TestUpgradeIsSurfaceEquivalent: the exported Go surface of every package
// v2 renders for the converted corpus is identical to what v1 rendered for
// the corpus it was converted from — identifier for identifier, signature
// for signature — so that a consumer of the one is a consumer of the other.
func TestUpgradeIsSurfaceEquivalent(t *testing.T) {
	a := &app{root: corpusRoot, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	v2 := renderV2(t, filepath.Join(v2Root, "corpus"))
	for p, data := range v1 {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		rendered, ok := v2[p]
		if !ok {
			t.Errorf("v2 does not render %s", p)
			continue
		}
		want := strings.Join(goSurface(t, p, string(data)), "\n")
		got := strings.Join(goSurface(t, p, string(rendered)), "\n")
		if want != got {
			t.Errorf("the surface of %s differs:\n%s", p, diff(want, got))
		}
	}
}

// tsExports lists what a TypeScript module exports, by name, sorted: the
// surface a consumer sees, apart from the types' shapes, which the diagram
// fixture holds under tsc.
func tsExports(source string) []string {
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^export (?:interface|type|class|const|function|async function) ([A-Za-z_$][A-Za-z0-9_$]*)`).FindAllStringSubmatch(source, -1) {
		seen[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`(?m)^export (?:type )?\{ ([^}]*) \}`).FindAllStringSubmatch(source, -1) {
		for _, name := range strings.Split(m[1], ",") {
			seen[strings.TrimSpace(name)] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestUpgradeIsExportEquivalent: every TypeScript module v2 renders for the
// converted corpus exports what v1's does, name for name, so that a
// consumer of the one is a consumer of the other; what v2 exports beyond
// that is reported. (The types' shapes are held equal by the diagram
// fixture under tsc.)
func TestUpgradeIsExportEquivalent(t *testing.T) {
	a := &app{root: corpusRoot, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	v2 := renderV2(t, filepath.Join(v2Root, "corpus"))
	for p, data := range v1 {
		if !strings.HasSuffix(p, ".ts") {
			continue
		}
		rendered, ok := v2[p]
		if !ok {
			t.Errorf("v2 does not render %s", p)
			continue
		}
		exported := map[string]bool{}
		for _, name := range tsExports(string(rendered)) {
			exported[name] = true
		}
		for _, name := range tsExports(string(data)) {
			if !exported[name] {
				t.Errorf("%s no longer exports %s", p, name)
			}
			delete(exported, name)
		}
		for name := range exported {
			t.Logf("%s also exports %s", p, name)
		}
	}
}
