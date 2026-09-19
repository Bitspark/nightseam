package conformance

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/compose"
)

// The generated layer: a language's second testee links what the generator
// renders for the probe and proof families and speaks them through it.
// The runner renders both into the recipe's {rendered} directory under a
// fixed module and scope, lays the language's own files from
// conformance/<lang>/generated/ beside the rendering — the testee's source,
// its module or package manifest — with {checkout} and {go} filled in text
// files, and then the recipe's generated build and run take over. The
// runner knows no language; the targets are the tool's own, composed in
// internal/compose and named nowhere else, so that the suite renders what
// the tool renders, and a language that has no target has no generated
// testee. What a testee does not consume — the specification the spec
// target lays beside the sources — it ignores.

// GeneratedModule and GeneratedScope root the rendering, as the fixtures
// under cmd/nightseam root theirs.
const GeneratedModule = "example.test/generated"
const GeneratedScope = "@example"

// renderProofAndProbe renders the shared proof against the conformance probe into dir.
func renderProofAndProbe(checkout, dir string) error {
	k := compose.Kernel(GeneratedModule, GeneratedScope, "")
	corpus := filepath.Join(checkout, "cmd", "nightseam", "testdata", "corpus")
	world := k.Load(os.DirFS(corpus), "api/contracts")
	proof := k.Load(os.DirFS(filepath.Join(checkout, "cmd", "nightseam", "testdata", "families")), "api/contracts")
	world.Families["proof"] = proof.Families["proof"]
	world.Problems["proof"] = proof.Problems["proof"]
	world.Names = append(world.Names, "proof")
	for _, name := range []string{"probe", "proof"} {
		result, err := k.Render(world, name)
		if err != nil {
			return fmt.Errorf("render proof and probe: %w", err)
		}
		for p, data := range result.Files {
			file := filepath.Join(dir, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// layGenerated copies conformance/<lang>/generated/** into dir, filling
// {checkout} and {go} in every text file: the checkout's path with slashes,
// and the go directive of its go.mod.
func layGenerated(checkout, from, dir string) error {
	goDirective, err := goDirectiveOf(filepath.Join(checkout, "go.mod"))
	if err != nil {
		return err
	}
	replacer := strings.NewReplacer("{checkout}", filepath.ToSlash(checkout), "{go}", goDirective)
	return filepath.WalkDir(from, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// A .tmpl suffix is dropped; it keeps a Go template from being a
		// package of the checkout, and says the file is filled. A file named
		// go.sum.checkout is the checkout's own go.sum, copied rather than
		// kept, so that it never drifts from the modules the rendering needs.
		target := filepath.Join(dir, strings.TrimSuffix(relative, ".tmpl"))
		if filepath.Base(relative) == "go.sum.checkout" {
			target = filepath.Join(filepath.Dir(target), "go.sum")
			if data, err = os.ReadFile(filepath.Join(checkout, "go.sum")); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(replacer.Replace(string(data))), 0o644)
	})
}

func goDirectiveOf(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return strings.TrimSpace(version), nil
		}
	}
	return "", fmt.Errorf("no go directive in %s", file)
}

// PrepareGenerated renders proof and probe and lays the language's files for every
// recipe that has a generated testee, then builds each once. The rendering
// lands where the recipe says; a recipe without a generated section is
// left out of the generated layer.
func (s *Suite) PrepareGenerated(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var languages []string
	for _, language := range s.Languages() {
		recipe := s.Recipes[language]
		if recipe.Generated == nil {
			continue
		}
		from := filepath.Join(recipe.Dir, "generated")
		if _, err := os.Stat(from); err != nil {
			t.Fatalf("the %s recipe has a generated testee but no %s to lay beside the rendering", language, from)
		}
		places := s.places
		places.Rendered = recipe.fill(recipe.Generated.Rendered, places)
		if err := os.RemoveAll(places.Rendered); err != nil {
			t.Fatal(err)
		}
		if err := renderProofAndProbe(s.Checkout, places.Rendered); err != nil {
			t.Fatal(err)
		}
		if err := layGenerated(s.Checkout, from, places.Rendered); err != nil {
			t.Fatal(err)
		}
		if err := recipe.RunBuild(ctx, places, true); err != nil {
			t.Fatalf("build the %s generated testee: %v", language, err)
		}
		s.rendered[language] = places.Rendered
		languages = append(languages, language)
	}
	return languages
}
