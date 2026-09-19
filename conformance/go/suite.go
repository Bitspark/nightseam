package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

var (
	matrixOnce     sync.Once
	matrix         *Matrix
	matrixProfiles *Profiles
	matrixRoot     string
)

// WriteMatrix writes the matrix of everything the run held, as
// conformance/matrix.json, and says it on stderr; nothing, when no suite
// was opened. TestMain calls it once the tests are done.
func WriteMatrix() error {
	if matrix == nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, "the matrix of this run:\n%s", matrix)
	return matrix.Write(matrixProfiles, filepath.Join(matrixRoot, "matrix.json"))
}

// Suite is one run of the scenarios: the recipes that take part, built
// once, and the places their placeholders stand for.
type Suite struct {
	Root      string
	Checkout  string
	Recipes   map[string]Recipe
	Scenarios []Scenario
	Profiles  *Profiles
	// Placed is each scenario's profile, by layer/name.
	Placed map[string]string
	Matrix *Matrix
	places Places
	// rendered is where each language's probe rendering lies, once prepared.
	rendered map[string]string
}

// Open reads the scenarios and the recipes of the checkout, holds every
// recipe's toolchains to the path, and builds every testee once. Under
// -short it skips, since a testee is a process; without, a toolchain that
// is missing fails, as every fixture of this repository does.
func Open(t *testing.T) *Suite {
	t.Helper()
	if testing.Short() {
		t.Skip("the conformance suite runs testees; skipped under -short")
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := Recipes(root)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadProfiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Cover(scenarios); err != nil {
		t.Fatal(err)
	}
	placed := map[string]string{}
	for _, sc := range scenarios {
		profile, err := profiles.Place(sc)
		if err != nil {
			t.Fatal(err)
		}
		placed[sc.Key()] = profile
	}
	out := filepath.Join(root, ".out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// One matrix for the whole run, whichever tests open a suite: TestMain
	// writes it once they are done.
	matrixOnce.Do(func() { matrix = NewMatrix(profiles); matrixProfiles = profiles; matrixRoot = root })
	s := &Suite{Root: root, Checkout: filepath.Dir(root), Recipes: recipes, Scenarios: scenarios, Profiles: profiles, Placed: placed, Matrix: matrix, places: Places{Checkout: filepath.Dir(root), Out: out}, rendered: map[string]string{}}
	t.Cleanup(func() {
		if blocking := s.Matrix.Blocking(s.Profiles); len(blocking) > 0 {
			t.Errorf("the tier table stops a release on: %v", blocking)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, language := range s.Languages() {
		recipe := recipes[language]
		if err := recipe.CheckToolchains(); err != nil {
			t.Fatal(err)
		}
		if err := recipe.RunBuild(ctx, s.places, false); err != nil {
			t.Fatalf("build the %s testee: %v", language, err)
		}
	}
	return s
}

// Languages is every language with a recipe, sorted, Go first.
func (s *Suite) Languages() []string {
	var out []string
	for language := range s.Recipes {
		if language != "go" {
			out = append(out, language)
		}
	}
	sort.Strings(out)
	if _, ok := s.Recipes["go"]; ok {
		out = append([]string{"go"}, out...)
	}
	return out
}

// pair runs every scenario of the layers that are not generated with
// language a on side a and language b on side b, one subtest each; a testee
// that dies is started again for the next scenario.
func (s *Suite) pair(t *testing.T, a, b string) {
	t.Helper()
	s.run(t, a, b, false, func(sc Scenario) bool { return sc.Layer != "generated" })
}

// runGenerated runs the generated scenarios with each side's generated
// testee, over the rendering PrepareGenerated laid for it.
func (s *Suite) runGenerated(t *testing.T, a, b string) {
	t.Helper()
	s.run(t, a, b, true, func(sc Scenario) bool { return sc.Layer == "generated" })
}

func (s *Suite) run(t *testing.T, a, b string, generated bool, keep func(Scenario) bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testees := map[string]*Testee{}
	start := func(language string) *Testee {
		if testee := testees[language]; testee != nil && testee.Dead() == nil {
			return testee
		}
		places := s.places
		if generated {
			places.Rendered = s.rendered[language]
		}
		testee, err := Start(ctx, s.Recipes[language], places, generated)
		if err != nil {
			t.Fatalf("start the %s testee: %v", language, err)
		}
		// A testee of a tier is held to what the tier requires: a layer it
		// lacks fails the run rather than skipping its scenarios. The
		// generated testee carries the generated layer alone.
		if !generated {
			if err := s.Profiles.HoldToTier(language, testee.Hello); err != nil {
				testee.Kill()
				t.Fatal(err)
			}
		}
		testees[language] = testee
		return testee
	}
	defer func() {
		for _, testee := range testees {
			_ = testee.Stop()
		}
	}()
	// One process per language even when a language is on both sides: the
	// two sides then share one testee, which the handles keep apart.
	for _, sc := range s.Scenarios {
		if !keep(sc) {
			continue
		}
		runs := []Scenario{sc}
		if sc.Mirror {
			runs = append(runs, sc.Mirrored())
		}
		for _, sc := range runs {
			s.one(t, ctx, sc, a, b, start)
		}
	}
}

// one runs a scenario as one subtest, its outcome into the matrix.
func (s *Suite) one(t *testing.T, ctx context.Context, sc Scenario, a, b string, start func(string) *Testee) {
	t.Helper()
	{
		t.Run(sc.Layer+"/"+sc.Name, func(t *testing.T) {
			ta, tb := start(a), start(b)
			outcome := Run(ctx, ta, tb, sc)
			// The cell is the non-reference language's, on whichever side it
			// stands; go with go is the reference's own row.
			held := a
			if a == "go" && b != "go" {
				held = b
			}
			s.Matrix.Record(held, s.Placed[sc.Key()], outcome)
			if outcome.Skipped != "" {
				t.Skip(outcome.Skipped)
			}
			if outcome.Failed != nil {
				t.Fatalf("%s (a: %s, b: %s)\n%v", sc.File, a, b, outcome.Failed)
			}
		})
	}
}

// Pairings are the ordered pairs a run holds: the star, every language
// against Go on either side; or the matrix, every pair.
func (s *Suite) Pairings(matrix bool) [][2]string {
	languages := s.Languages()
	var out [][2]string
	for _, a := range languages {
		for _, b := range languages {
			if matrix || a == "go" || b == "go" {
				out = append(out, [2]string{a, b})
			}
		}
	}
	return out
}

func pairName(a, b string) string { return fmt.Sprintf("%s-with-%s", a, b) }
