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
// in tier files under testdata/corpus, and what every target renders for
// it is held under testdata/golden, file for file. The invalid cases under
// testdata/invalid are each a checkout the tool must refuse, holding what
// validate says about it, word for word. The families under
// testdata/families are what the slow fixtures and the in-memory tests
// render.
//
// The golden files are the oracle a target has whether or not there is a
// compiler to hand its output to: a change to a renderer shows up as a diff
// of them, which is what a reviewer reads. When the change is meant, rewrite
// them from the current output and commit the diff:
//
//	go test ./cmd/nightseam -run 'Golden|Invalid|Surface|Reserved' -update
var update = flag.Bool("update", false, "rewrite testdata/golden and every diagnostics.txt from the current output")

const corpusRoot = "testdata/corpus"
const goldenRoot = "testdata/golden"
const invalidRoot = "testdata/invalid"
const familiesRoot = "testdata/families"
const familiesGoldenRoot = "testdata/golden-families"

// TestCorpusIsValid: every family of the corpus validates as one world.
func TestCorpusIsValid(t *testing.T) {
	for _, root := range []string{corpusRoot, familiesRoot} {
		out, errs, err := run(t, root, "validate")
		if err != nil || errs != "" {
			t.Fatalf("%s does not validate: %v\n%s%s", root, err, errs, out)
		}
		names, err := (&app{root: root}).families()
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("%d families; valid\n", len(names)); out != want {
			t.Fatalf("validate reported %q, not %q", out, want)
		}
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
	holdGolden(t, goldenRoot, files)
}

// TestFamiliesRenderGolden: what every target renders for the families the
// fixtures use — a family generic in two session families, one drawing a
// type beyond the two every family carries, one whose wire names differ
// from its language names — is exactly the files under
// testdata/golden-families.
func TestFamiliesRenderGolden(t *testing.T) {
	holdGolden(t, familiesGoldenRoot, renderV2(t, familiesRoot))
}

// holdGolden compares rendered files against a golden tree, or rewrites the
// tree under -update.
func holdGolden(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	if *update {
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		for p, data := range files {
			writeFixture(t, root, p, data)
		}
		t.Logf("rewrote %d files under %s", len(files), root)
		return
	}
	for p, data := range files {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if os.IsNotExist(err) {
			t.Errorf("%s has no file under %s; run with -update", p, root)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(data) {
			t.Errorf("%s differs:\n%s", p, diff(string(want), string(data)))
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, produced := files[filepath.ToSlash(rel)]; !produced {
			t.Errorf("%s under %s is produced by nothing; run with -update", rel, root)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestInvalidCorpusIsRefused: each case under testdata/invalid is a checkout
// validate refuses, and what it says — every diagnostic with its file,
// pointer and code, then the error that stopped it — is held in the case's
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
