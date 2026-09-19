package conformance

import (
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
			"session":       {Layers: []string{"session"}},
			"observability": {Needs: []string{"observer", "propagator"}},
		},
		Tiers: map[string]Tier{
			"1": {Requires: []string{"core", "generator", "session", "observability"}, OnFailure: "stop"},
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
	if err := p.HoldToTier("go", core); err == nil || !strings.Contains(err.Error(), "session") {
		t.Fatalf("a tier 1 testee without session was not refused: %v", err)
	}
	if err := p.HoldToTier("fourth", core); err != nil {
		t.Fatalf("a tier 4 testee holding core was refused: %v", err)
	}
	if err := p.HoldToTier("nobody", core); err != nil {
		t.Fatalf("a language of no tier was held to something: %v", err)
	}
	// The generated layer is a second testee's, so the first is not held to it.
	full := Hello{Driver: 1, Layers: []string{"seam", "peer", "tunnel", "session"}, Features: []string{"observer", "propagator"}}
	if err := p.HoldToTier("go", full); err != nil {
		t.Fatal(err)
	}
}

// TestVerdictsFollowTheTierTable: one verdict per onFailure value, from a
// row with one failure in a required profile, and ok without.
func TestVerdictsFollowTheTierTable(t *testing.T) {
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
	m.Record("fourth", "session", failed)
	if got := m.Verdict(p, "fourth"); got != "provisional" {
		t.Errorf("a tier 4 failure in session became %s", got)
	}
	ok := NewMatrix(p)
	ok.Record("go", "core", Outcome{})
	ok.Record("go", "session", Outcome{Skipped: "not today"})
	if got := ok.Verdict(p, "go"); got != "ok" {
		t.Errorf("a row of passes and skips is %s", got)
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
	if !strings.Contains(string(first), `"verdict": "ok"`) || !strings.Contains(string(first), `"tier": 1`) {
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
