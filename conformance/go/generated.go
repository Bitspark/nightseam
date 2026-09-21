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

// renderTestees renders the families a generated testee links: the probe
// and the proof for the type language, the worker for live values and the
// combinator for callables that take and answer callables.
func renderTestees(checkout, dir string) error {
	k := compose.Kernel(GeneratedModule, GeneratedScope, "")
	corpus := filepath.Join(checkout, "cmd", "nightseam", "testdata", "corpus")
	world := k.Load(os.DirFS(corpus), "api/contracts")
	// The corpus has the probe and the worker; the proof and the combinator
	// are declared among the families, and are lifted in beside them.
	families := k.Load(os.DirFS(filepath.Join(checkout, "cmd", "nightseam", "testdata", "families")), "api/contracts")
	for _, name := range []string{"proof", "boxes", "combinator"} {
		if families.Families[name] == nil {
			return fmt.Errorf("render the testees: no %s family among the families", name)
		}
		world.Families[name] = families.Families[name]
		world.Problems[name] = families.Problems[name]
		world.Names = append(world.Names, name)
	}
	if world.Families["worker"] == nil {
		return fmt.Errorf("render the testees: the corpus has no worker family")
	}
	owners := k.Load(os.DirFS(filepath.Join(checkout, "cmd", "nightseam", "testdata", "live-owners")), "api/contracts")
	world.Families["owners"] = owners.Families["owners"]
	world.Problems["owners"] = owners.Problems["owners"]
	world.Names = append(world.Names, "owners")
	publication := k.Load(os.DirFS(filepath.Join(checkout, "conformance", "corpora", "live-publication")), "api/contracts")
	world.Families["publication"] = publication.Families["publication"]
	world.Problems["publication"] = publication.Problems["publication"]
	world.Names = append(world.Names, "publication")
	cell := k.Load(os.DirFS(filepath.Join(checkout, "conformance", "corpora", "wire-cell")), "api/contracts")
	world.Families["cell"] = cell.Families["cell"]
	world.Problems["cell"] = cell.Problems["cell"]
	world.Names = append(world.Names, "cell")
	for _, name := range []string{"probe", "proof", "worker", "boxes", "combinator", "owners", "publication", "cell"} {
		result, err := k.Render(world, name)
		if err != nil {
			return fmt.Errorf("render the testees: %w", err)
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
		if err := renderTestees(s.Checkout, places.Rendered); err != nil {
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
