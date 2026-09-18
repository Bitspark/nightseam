package main

import (
	"fmt"
	"os"
	"path/filepath"
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
