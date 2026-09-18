package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSurfaceOfCheckout is a tool, not a test: with NIGHTSEAM_SURFACE set to
// a checkout and NIGHTSEAM_AGAINST to another, it compares the exported Go
// surface of every generated file the two have in common, so that a real
// consumer's regeneration can be held to what it had.
func TestSurfaceOfCheckout(t *testing.T) {
	from, against := os.Getenv("NIGHTSEAM_SURFACE"), os.Getenv("NIGHTSEAM_AGAINST")
	if from == "" || against == "" {
		t.Skip("set NIGHTSEAM_SURFACE and NIGHTSEAM_AGAINST")
	}
	err := filepath.WalkDir(filepath.Join(from, "api/go"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, "_generated.go") {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		mine, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		theirs, err := os.ReadFile(filepath.Join(against, rel))
		if err != nil {
			t.Logf("%s: not in the other checkout", rel)
			return nil
		}
		want := strings.Join(goSurface(t, rel, string(theirs)), "\n")
		got := strings.Join(goSurface(t, rel, string(mine)), "\n")
		if want != got {
			t.Errorf("%s:\n%s", filepath.ToSlash(rel), diff(want, got))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
