package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	matrixOnce      sync.Once
	matrix          *Matrix
	matrixProfiles  *Profiles
	matrixRoot      string
	matrixLanguages []string
	matrixOut       string
)

// RemoveOut removes what the run built and rendered, once its testees have
// stopped. TestMain calls it last.
func RemoveOut() {
	if matrixOut != "" {
		_ = os.RemoveAll(matrixOut)
	}
}

// WriteMatrix writes the matrix of everything the run held, as
// conformance/matrix.json, and says it on stderr — when the run was the
// whole suite: every profile held for every language with a testee. A run
// of some tests, or some scenarios, says its matrix and writes nothing, so
// the file on disk is never a part of a run read as the whole. TestMain
// calls it once the tests are done.
func WriteMatrix() error {
	if matrix == nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, "the matrix of this run:\n%s", matrix)
	if file := os.Getenv("NIGHTSEAM_MATRIX_SUMMARY"); file != "" {
		if err := writeSummary(file, matrix, matrixProfiles); err != nil {
			return err
		}
	}
	if missing := matrix.Missing(matrixLanguages); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "not written to matrix.json: the run did not hold %s\n", strings.Join(missing, ", "))
		return nil
	}
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
	// Observe, when set, is told every outcome as it happens, beside the
	// matrix: what a test that asks more of a run than its counts reads.
	Observe func(sc Scenario, a, b string, o Outcome)
	places  Places
	// rendered is where each language's probe rendering lies, once prepared.
	rendered map[string]string
	// strict keeps nightly's per-scenario failures independent of tiers.
	strict bool
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
	// Every run builds and renders into a directory of its own, since two
	// runs may share one checkout — a binary a running testee holds cannot
	// be rebuilt over on Windows, and a rendering half written is nobody's.
	// It lies under conformance/ so that a rendered TypeScript package
	// resolves the workspace's packages by walking up. TestMain removes it.
	out := filepath.Join(root, ".out", fmt.Sprint(os.Getpid()))
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// One matrix for the whole run, whichever tests open a suite: TestMain
	// writes it once they are done.
	matrixOnce.Do(func() {
		matrix = NewMatrix(profiles)
		matrixProfiles = profiles
		matrixRoot = root
		matrixLanguages = languagesOf(recipes)
		matrixOut = out
	})
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
func (s *Suite) Languages() []string { return languagesOf(s.Recipes) }

func languagesOf(recipes map[string]Recipe) []string {
	var out []string
	for language := range recipes {
		if language != "go" {
			out = append(out, language)
		}
	}
	sort.Strings(out)
	if _, ok := recipes["go"]; ok {
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
	unavailable := ""
	if generated {
		for _, language := range []string{a, b} {
			if s.Recipes[language].Generated == nil {
				unavailable = fmt.Sprintf("the %s testee has no generated target", language)
				break
			}
		}
	}
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
		// NIGHTSEAM_PRETEND_DIAL_ONLY names a language the runner treats as
		// unable to listen, whatever its testee said: how the suite proves a
		// dial-only language is held in every role, with a testee that can.
		if !generated && os.Getenv("NIGHTSEAM_PRETEND_DIAL_ONLY") == language {
			var features []string
			for _, f := range testee.Hello.Features {
				if f != "listen" {
					features = append(features, f)
				}
			}
			testee.Hello.Features = features
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
			s.one(t, ctx, sc, a, b, start, unavailable)
		}
	}
}

// one runs a scenario as one subtest, its outcome into the matrix.
func (s *Suite) one(t *testing.T, ctx context.Context, sc Scenario, a, b string, start func(string) *Testee, unavailable string) {
	t.Helper()
	{
		t.Run(sc.Layer+"/"+sc.Name, func(t *testing.T) {
			outcome := Outcome{Skipped: unavailable}
			if unavailable == "" {
				ta, tb := start(a), start(b)
				outcome = Run(ctx, ta, tb, sc)
			}
			// The cell is the non-reference language's, on whichever side it
			// stands; go with go is the reference's own row.
			held := a
			if a == "go" && b != "go" {
				held = b
			}
			s.reportOutcome(t, sc, a, b, held, outcome)
		})
	}
}

type scenarioReporter interface {
	Helper()
	Skip(...any)
	Fatalf(string, ...any)
	Logf(string, ...any)
}

func (s *Suite) reportOutcome(t scenarioReporter, sc Scenario, a, b, held string, outcome Outcome) {
	t.Helper()
	s.Matrix.Record(held, s.Placed[sc.Key()], outcome)
	if s.Observe != nil {
		s.Observe(sc, a, b, outcome)
	}
	if outcome.Skipped != "" {
		profile := s.Placed[sc.Key()]
		tier := s.Profiles.Tiers[fmt.Sprint(s.Profiles.Languages[held].Tier)]
		if s.strict && slices.Contains(tier.Requires, profile) {
			t.Fatalf("%s: required %s/%s scenario skipped: %s", sc.File, held, profile, outcome.Skipped)
			return
		}
		t.Skip(outcome.Skipped)
		return
	}
	if outcome.Failed != nil {
		if s.strict || slices.Contains(s.Matrix.Blocking(s.Profiles), held) {
			t.Fatalf("%s (a: %s, b: %s)\n%v", sc.File, a, b, outcome.Failed)
		} else {
			t.Logf("nonblocking %s/%s failure: %s (a: %s, b: %s)\n%v", held, s.Placed[sc.Key()], sc.File, a, b, outcome.Failed)
		}
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
