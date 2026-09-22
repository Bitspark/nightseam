package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tiered is a profiles table for the tests: the shape of profiles.json,
// with a language at every tier.
func tiered() *Profiles {
	return &Profiles{
		Profiles: map[string]Profile{
			"core":          {Layers: []string{"seam", "peer"}},
			"generator":     {Layers: []string{"generated"}},
			"tunnel":        {Layers: []string{"tunnel"}},
			"observability": {Needs: []string{"observer", "propagator"}},
		},
		Tiers: map[string]Tier{
			"1": {Requires: []string{"core", "generator", "tunnel", "observability"}, OnFailure: "stop"},
			"2": {Requires: []string{"core", "generator"}, OnFailure: "stop"},
			"3": {Requires: []string{"core", "generator"}, OnFailure: "provisional"},
			"4": {Requires: []string{"core"}, OnFailure: "provisional"},
		},
		Languages: map[string]Language{"go": {Tier: 1, Reference: true}, "second": {Tier: 2}, "third": {Tier: 3}, "fourth": {Tier: 4}},
	}
}

// TestATesteeIsHeldToItsTier: a hello without a layer the tier requires
// fails at tier 1 and passes at tier 4, where the layer is not required.
func TestATesteeIsHeldToItsTier(t *testing.T) {
	p := tiered()
	core := Hello{Driver: 1, Layers: []string{"seam", "peer"}, Features: []string{"listen"}}
	if err := p.HoldToTier("go", core); err == nil || !strings.Contains(err.Error(), "tunnel") {
		t.Fatalf("a tier 1 testee without the tunnel was not refused: %v", err)
	}
	if err := p.HoldToTier("fourth", core); err != nil {
		t.Fatalf("a tier 4 testee holding core was refused: %v", err)
	}
	if err := p.HoldToTier("nobody", core); err != nil {
		t.Fatalf("a language of no tier was held to something: %v", err)
	}
	// The generated layer is a second testee's, so the first is not held to it.
	full := Hello{Driver: 1, Layers: []string{"seam", "peer", "tunnel"}, Features: []string{"observer", "propagator"}}
	if err := p.HoldToTier("go", full); err != nil {
		t.Fatal(err)
	}
}

// TestVerdictsFollowTheTierTable: one verdict per onFailure value, from a
// row with one failure or skip in a required profile, and ok without either.
func TestVerdictsFollowTheTierTable(t *testing.T) {
	t.Setenv("NIGHTSEAM_MATRIX", "")
	p := tiered()
	m := NewMatrix(p)
	failed := Outcome{Failed: &Failure{Reason: "x"}}
	for _, language := range []string{"go", "second", "third", "fourth"} {
		m.Record(language, "core", Outcome{})
		m.Record(language, "core", failed)
	}
	m.Record("clean", "core", Outcome{})
	want := map[string]string{"go": "blocking", "second": "blocking", "third": "provisional", "fourth": "provisional", "clean": "provisional"}
	for language, verdict := range want {
		if got := m.Verdict(p, language); got != verdict {
			t.Errorf("%s: verdict %s, want %s", language, got, verdict)
		}
	}
	if blocking := m.Blocking(p); strings.Join(blocking, ",") != "go,second" {
		t.Errorf("blocking = %v", blocking)
	}
	// A failure outside what the tier requires does not count against it.
	m.Record("fourth", "tunnel", failed)
	if got := m.Verdict(p, "fourth"); got != "provisional" {
		t.Errorf("a tier 4 failure in the tunnel became %s", got)
	}
	ok := NewMatrix(p)
	ok.Record("go", "core", Outcome{})
	ok.Record("go", "tunnel", Outcome{})
	if got := ok.Verdict(p, "go"); got != "ok" {
		t.Errorf("a row of passes is %s", got)
	}
	for _, language := range []string{"go", "second", "third", "fourth"} {
		t.Run(language+"/required-skip", func(t *testing.T) {
			m := NewMatrix(p)
			m.Record(language, "core", Outcome{})
			m.Record(language, "core", Outcome{Skipped: "missing required operation"})
			if got := m.Verdict(p, language); got != want[language] {
				t.Errorf("required skip: verdict %s, want %s", got, want[language])
			}
		})
	}
	// A testee that would not build is the row's own state and not a cell's,
	// so the verdict is the tier's disposition however the cells that did run
	// read: a green row cannot stand for a language the run never held, and a
	// row of nothing at all is the same absence with nothing beside it.
	for _, language := range []string{"go", "second", "third", "fourth"} {
		t.Run(language+"/absent-testee", func(t *testing.T) {
			tier := p.Tiers[fmt.Sprint(p.Languages[language].Tier)]
			green := NewMatrix(p)
			for _, profile := range tier.Requires {
				green.Record(language, profile, Outcome{})
			}
			if got := green.Verdict(p, language); got != "ok" {
				t.Fatalf("the row before the absence is %s", got)
			}
			for _, m := range []*Matrix{green, NewMatrix(p)} {
				m.recordAbsent(language, "runtime", "the fixture compiler refused the testee")
				if got := m.Verdict(p, language); got != want[language] {
					t.Errorf("absent testee: verdict %s, want %s", got, want[language])
				}
				if got, blocks := strings.Join(m.Blocking(p), ","), want[language] == "blocking"; (got == language) != blocks {
					t.Errorf("absent testee: blocking %q, want %s among them: %v", got, language, blocks)
				}
			}
			if table := green.String(); !strings.Contains(table, "runtime testee absent") || !strings.Contains(table, "the fixture compiler refused the testee") {
				t.Errorf("the matrix does not say what is absent or why:\n%s", table)
			}
		})
	}
	for _, c := range []struct {
		language, profile, verdict string
		blocking                   bool
	}{
		{"fourth", "core", "provisional", false},
		{"third", "generator", "provisional", false},
		{"go", "core", "blocking", true},
		{"second", "core", "blocking", true},
		{"second", "live", "ok", false},
	} {
		t.Run("CI/"+c.language+"/"+c.profile, func(t *testing.T) {
			p := tiered()
			p.Profiles["live"] = Profile{Layers: []string{"live"}}
			m := NewMatrix(p)
			for language := range p.Languages {
				for profile := range p.Profiles {
					m.Record(language, profile, Outcome{})
				}
			}
			sc := Scenario{Layer: "peer", Name: "fixture", File: "fixture.json"}
			s := &Suite{Profiles: p, Matrix: m, Placed: map[string]string{sc.Key(): c.profile}}
			reporter := &verdictReporter{}
			s.reportOutcome(reporter, sc, "go", c.language, c.language, Outcome{Failed: &Failure{Reason: "fixture refusal"}})
			if reporter.failed != c.blocking || (len(m.Blocking(p)) > 0) != c.blocking {
				t.Fatalf("star failed=%v, blocking=%v; want blocking=%v", reporter.failed, m.Blocking(p), c.blocking)
			}
			if got := m.Verdict(p, c.language); got != c.verdict {
				t.Fatalf("verdict=%s, want %s", got, c.verdict)
			}
			if cell := m.rows[c.language][c.profile]; cell.Passed != 1 || cell.Failed != 1 {
				t.Fatalf("failure lost from matrix: %+v", cell)
			}
			if !strings.Contains(reporter.output, "fixture refusal") {
				t.Fatalf("scenario diagnostic lost: %s", reporter.output)
			}
			summary := m.summary(p)
			if !strings.Contains(summary, "`"+c.language+"/"+c.profile+"`: 1 failed") || !strings.Contains(summary, "| "+c.language+" |") {
				t.Fatalf("summary lost the red cell or matrix row:\n%s", summary)
			}
			if c.verdict == "provisional" && !strings.Contains(summary, "(provisional)") {
				t.Fatalf("summary lost provisional label:\n%s", summary)
			}
			if c.language == "second" && c.profile == "live" && !strings.Contains(summary, "next minor release") {
				t.Fatalf("summary lost lag note:\n%s", summary)
			}
			// Nightly mode also holds star/generated pairings, regardless of
			// whether TestMatrix itself is selected in this test process.
			t.Run("nightly", func(t *testing.T) {
				t.Setenv("NIGHTSEAM_MATRIX", "1")
				nightly := &verdictReporter{}
				s.reportOutcome(nightly, sc, "go", c.language, c.language, Outcome{Failed: &Failure{Reason: "fixture refusal"}})
				if !nightly.failed {
					t.Fatal("nightly passed a failed star/generated scenario")
				}
			})
		})
	}
}

type verdictReporter struct {
	failed bool
	output string
}

func (*verdictReporter) Helper()            {}
func (r *verdictReporter) Skip(args ...any) { r.output += fmt.Sprint(args...) }
func (r *verdictReporter) Fatalf(format string, args ...any) {
	r.failed = true
	r.output += fmt.Sprintf(format, args...)
}
func (r *verdictReporter) Logf(format string, args ...any) { r.output += fmt.Sprintf(format, args...) }

func TestNightlyRetainsRequiredSkipFailures(t *testing.T) {
	t.Setenv("NIGHTSEAM_MATRIX", "1")
	p := tiered()
	sc := Scenario{Layer: "peer", Name: "fixture"}
	for _, profile := range []string{"core", "tunnel"} {
		s := &Suite{Profiles: p, Matrix: NewMatrix(p), Placed: map[string]string{sc.Key(): profile}}
		r := &verdictReporter{}
		s.reportOutcome(r, sc, "third", "fourth", "fourth", Outcome{Skipped: "missing operation"})
		if r.failed != (profile == "core") {
			t.Fatalf("nightly %s skip failed=%v", profile, r.failed)
		}
	}
}

func TestSummaryPublishesLatestRunWithRequiredSkips(t *testing.T) {
	p := tiered()
	m := NewMatrix(p)
	m.Record("fourth", "core", Outcome{Skipped: "missing operation"})
	file := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(file, []byte("stale run"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSummary(file, m, p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale run") || !strings.Contains(string(data), "`fourth/core`: 0 failed, 1 skipped (provisional)") {
		t.Fatalf("summary=%s", data)
	}
}

func TestRequiredGeneratedServerRoleCannotSkip(t *testing.T) {
	p := tiered()
	p.Languages["typescript"] = Language{Tier: 1}
	m := NewMatrix(p)
	m.Record("typescript", "generator", Outcome{})
	m.Record("typescript", "generator", Outcome{Skipped: "the typescript testee does not support gen.serve: generated server binding absent"})
	if got := m.Verdict(p, "typescript"); got != "blocking" {
		t.Fatalf("missing required gen.serve role: verdict %s, want blocking", got)
	}
	if got := strings.Join(m.Blocking(p), ","); got != "typescript" {
		t.Errorf("blocking languages = %s, want typescript", got)
	}
	cell := m.rows["typescript"]["generator"]
	if cell.Passed != 1 || cell.Skipped != 1 || cell.Failed != 0 {
		t.Errorf("the unsupported role must remain visible as a skip: %+v", cell)
	}
}

func TestOptionalProfileSkipsRemainInformational(t *testing.T) {
	p := tiered()
	for _, language := range []string{"second", "third", "fourth"} {
		t.Run(language, func(t *testing.T) {
			m := NewMatrix(p)
			m.Record(language, "core", Outcome{})
			m.Record(language, "tunnel", Outcome{Skipped: "optional profile absent"})
			if got := m.Verdict(p, language); got != "ok" {
				t.Errorf("optional skip: verdict %s, want ok", got)
			}
		})
	}
}

// TestTheMatrixIsWrittenTheSameTwice: matrix.json of one run is byte for
// byte that of another, whatever order the outcomes came in.
func TestTheMatrixIsWrittenTheSameTwice(t *testing.T) {
	p := tiered()
	dir := t.TempDir()
	write := func(name string, order []string) []byte {
		m := NewMatrix(p)
		for _, language := range order {
			m.Record(language, "core", Outcome{})
			m.Record(language, "generator", Outcome{Skipped: "s"})
		}
		file := filepath.Join(dir, name)
		if err := m.Write(p, file); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := write("a.json", []string{"go", "second", "third"})
	second := write("b.json", []string{"third", "go", "second"})
	if string(first) != string(second) {
		t.Fatalf("the matrix differs by the order outcomes came in:\n%s\n%s", first, second)
	}
	if !strings.Contains(string(first), `"verdict": "blocking"`) || !strings.Contains(string(first), `"verdict": "provisional"`) || !strings.Contains(string(first), `"tier": 1`) {
		t.Fatalf("the matrix lacks its verdicts or tiers:\n%s", first)
	}
}

// TestProfilesAreHeldToThemselves: the checked-in profiles.json loads, and
// one that names a tier requiring an unknown profile, or a language with no
// testee, is refused.
func TestProfilesAreHeldToThemselves(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfiles(root); err != nil {
		t.Fatal(err)
	}
	broken := t.TempDir()
	copyFile := func(name string) {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(broken, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copyFile("profiles.schema.json")
	for _, c := range []struct{ name, json, want string }{
		{"a tier requiring an unknown profile", `{"profiles":{"core":{"layers":["seam"],"promise":"wire"}},"tiers":{"1":{"requires":["core","nope"],"onFailure":"stop"}},"languages":{}}`, "nope"},
		{"a language with no testee", `{"profiles":{"core":{"layers":["seam"],"promise":"wire"}},"tiers":{"1":{"requires":["core"],"onFailure":"stop"}},"languages":{"cobol":{"tier":1}}}`, "cobol"},
		{"a language of an unknown tier", `{"profiles":{"core":{"layers":["seam"],"promise":"wire"}},"tiers":{"1":{"requires":["core"],"onFailure":"stop"}},"languages":{"go":{"tier":9}}}`, "tier 9"},
		{"a profile of nothing", `{"profiles":{"core":{"promise":"wire"}},"tiers":{},"languages":{}}`, "schema"},
	} {
		if err := os.WriteFile(filepath.Join(broken, "profiles.json"), []byte(c.json), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadProfiles(broken)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s was not refused for %q: %v", c.name, c.want, err)
		}
	}
}

// A missing optional capability belongs to its participant, on either side.
// It must not turn the other participant's required profile red.
func TestNightlyAttributesSkippedCapabilities(t *testing.T) {
	t.Setenv("NIGHTSEAM_MATRIX", "1")
	for _, missing := range []string{"go", "fourth"} {
		for _, pair := range [][2]string{{"go", "fourth"}, {"fourth", "go"}} {
			t.Run(strings.Join(pair[:], "-")+"/"+missing, func(t *testing.T) {
				p := tiered()
				sc := Scenario{Layer: "tunnel", Name: "fixture"}
				s := &Suite{Profiles: p, Matrix: NewMatrix(p), Placed: map[string]string{sc.Key(): "tunnel"}}
				r := &verdictReporter{}
				s.reportOutcome(r, sc, pair[0], pair[1], pair[0], Outcome{Skipped: "missing tunnel", SkippedBy: missing})
				if r.failed != (missing == "go") {
					t.Fatalf("missing %s: failed=%v, output=%s", missing, r.failed, r.output)
				}
				if s.Matrix.rows[missing]["tunnel"].Skipped != 1 {
					t.Fatal("missing participant lost its skip")
				}
				other := "go"
				if missing == "go" {
					other = "fourth"
				}
				if len(s.Matrix.rows[other]) != 0 {
					t.Fatalf("uninvolved participant received a result: %v", s.Matrix.rows[other])
				}
			})
		}
	}
}

func TestRunNamesTheParticipantMissingACapability(t *testing.T) {
	for _, side := range []string{"a", "b"} {
		a := &Testee{Language: "first", Hello: Hello{Layers: []string{"peer"}}}
		b := &Testee{Language: "second", Hello: Hello{Layers: []string{"peer"}}}
		outcome := Run(context.Background(), a, b, Scenario{Steps: []Step{{On: side, Op: "tunnel.open"}}})
		want := "first"
		if side == "b" {
			want = "second"
		}
		if outcome.SkippedBy != want || outcome.Skipped == "" {
			t.Fatalf("side %s: %+v", side, outcome)
		}
	}
}
