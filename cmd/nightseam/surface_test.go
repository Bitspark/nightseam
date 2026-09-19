package main

import (
	"github.com/Bitspark/nightseam/internal/surface"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The surface of a generated Go package is what a consumer can name of it:
// every exported type with its fields and tags, every exported function,
// method, constant and variable with its signature. It is held under
// testdata/surface per generated file, so that a change to how the
// generator spells the packages' API is a diff a reviewer reads, apart from
// the bytes of the files — and so that a second generator can be held to
// the same API as the first, identifier for identifier.
const surfaceRoot = "testdata/surface"

// TestCorpusSurfaceIsGolden: the exported surface of every Go file the corpus
// renders is exactly what testdata/surface holds.
func TestCorpusSurfaceIsGolden(t *testing.T) {
	a := &app{root: corpusRoot, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	surfaces := map[string]string{}
	for p, data := range files {
		if strings.HasSuffix(p, ".go") {
			surfaces[p+".txt"] = strings.Join(goSurface(t, p, string(data)), "\n") + "\n"
		}
	}
	if *update {
		if err := os.RemoveAll(surfaceRoot); err != nil {
			t.Fatal(err)
		}
		for p, text := range surfaces {
			writeFixture(t, surfaceRoot, p, []byte(text))
		}
		t.Logf("rewrote %d surface files under %s", len(surfaces), surfaceRoot)
		return
	}
	for p, text := range surfaces {
		want, err := os.ReadFile(filepath.Join(surfaceRoot, filepath.FromSlash(p)))
		if os.IsNotExist(err) {
			t.Errorf("%s has no surface file; run with -update", p)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != text {
			t.Errorf("the surface of %s changed:\n%s", strings.TrimSuffix(p, ".txt"), diff(string(want), text))
		}
	}
	err = filepath.WalkDir(surfaceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(surfaceRoot, path)
		if err != nil {
			return err
		}
		if _, rendered := surfaces[filepath.ToSlash(rel)]; !rendered {
			t.Errorf("%s is a surface file nothing renders; run with -update", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func goSurface(t *testing.T, name, source string) []string {
	t.Helper()
	declarations, err := surface.Declarations(name, source)
	if err != nil {
		t.Fatal(err)
	}
	return declarations
}
