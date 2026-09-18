package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The corpus is a checkout of families written as a consumer writes them,
// in layer files under testdata/corpus, and what every target renders for
// it is held under testdata/golden, file for file. The invalid cases under
// testdata/invalid are each a checkout the tool must refuse, holding what
// validate says about it, word for word.
//
// The golden files are the oracle a target has whether or not there is a
// compiler to hand its output to: a change to a renderer shows up as a diff
// of them, which is what a reviewer reads. When the change is meant, rewrite
// them from the current output and commit the diff:
//
//	go test ./cmd/nightseam -run 'Golden|Invalid' -update
var update = flag.Bool("update", false, "rewrite testdata/golden and every diagnostics.txt from the current output")

const corpusRoot = "testdata/corpus"
const goldenRoot = "testdata/golden"
const invalidRoot = "testdata/invalid"

// TestCorpusIsValid: every family of the corpus validates as one world.
func TestCorpusIsValid(t *testing.T) {
	out, errs, err := run(t, corpusRoot, "validate")
	if err != nil || errs != "" {
		t.Fatalf("the corpus does not validate: %v\n%s%s", err, errs, out)
	}
	names, err := (&app{root: corpusRoot}).families()
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d contracts; valid\n", len(names)); out != want {
		t.Fatalf("validate reported %q, not %q", out, want)
	}
}

// TestCorpusRendersGolden: what every target renders for the corpus is
// exactly the golden files — no file differs, none is missing, and none is
// left over from a target that no longer renders it.
func TestCorpusRendersGolden(t *testing.T) {
	a := &app{root: corpusRoot, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.RemoveAll(goldenRoot); err != nil {
			t.Fatal(err)
		}
		for p, data := range files {
			writeFixture(t, goldenRoot, p, data)
		}
		t.Logf("rewrote %d golden files under %s", len(files), goldenRoot)
		return
	}
	for p, data := range files {
		want, err := os.ReadFile(filepath.Join(goldenRoot, filepath.FromSlash(p)))
		if os.IsNotExist(err) {
			t.Errorf("%s is rendered but has no golden file; run with -update", p)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(data) {
			t.Errorf("%s differs from its golden file:\n%s", p, diff(string(want), string(data)))
		}
	}
	err = filepath.WalkDir(goldenRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(goldenRoot, path)
		if err != nil {
			return err
		}
		if _, rendered := files[filepath.ToSlash(rel)]; !rendered {
			t.Errorf("%s is a golden file nothing renders; run with -update", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestInvalidCorpusIsRefused: each case under testdata/invalid is a checkout
// validate refuses, and what it says — every diagnostic with its pointer
// and code, or the error that stopped it — is held in the case's
// diagnostics.txt.
func TestInvalidCorpusIsRefused(t *testing.T) {
	entries, err := os.ReadDir(invalidRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			root := filepath.Join(invalidRoot, entry.Name())
			out, errs, err := run(t, root, "validate")
			if err == nil {
				t.Fatalf("validate accepted the case:\n%s", out)
			}
			var report strings.Builder
			report.WriteString(errs)
			fmt.Fprintf(&report, "error: %v\n", err)
			if out != "" {
				fmt.Fprintf(&report, "stdout: %s", out)
			}
			// A message that names the checkout names it the same way on
			// every platform.
			got := strings.NewReplacer(filepath.ToSlash(root), "<root>", root, "<root>").Replace(report.String())
			expectation := filepath.Join(root, "diagnostics.txt")
			if *update {
				if err := os.WriteFile(expectation, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(expectation)
			if os.IsNotExist(err) {
				t.Fatalf("no diagnostics.txt; run with -update. validate said:\n%s", got)
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Errorf("validate said something else than diagnostics.txt:\n%s", diff(string(want), got))
			}
		})
	}
}

// diff shows the first line at which two texts part, with context, which is
// enough to find the change in a file a reviewer will read in full anyway.
func diff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	at := 0
	for at < len(wantLines) && at < len(gotLines) && wantLines[at] == gotLines[at] {
		at++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "at line %d\n", at+1)
	from := max(at-2, 0)
	for i := from; i < at && i < len(wantLines); i++ {
		fmt.Fprintf(&b, "  %s\n", wantLines[i])
	}
	for i := at; i < min(at+3, len(wantLines)); i++ {
		fmt.Fprintf(&b, "- %s\n", wantLines[i])
	}
	for i := at; i < min(at+3, len(gotLines)); i++ {
		fmt.Fprintf(&b, "+ %s\n", gotLines[i])
	}
	return b.String()
}
