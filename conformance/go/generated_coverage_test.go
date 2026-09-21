package conformance

import (
	"slices"
	"testing"
)

func TestGeneratedCoverageIncludesARecipeWithoutATarget(t *testing.T) {
	profiles := tiered()
	s := &Suite{
		Recipes:   map[string]Recipe{"go": {}, "fourth": {}},
		Profiles:  profiles,
		Matrix:    NewMatrix(profiles),
		Scenarios: []Scenario{{Layer: "generated", Name: "ordinary"}, {Layer: "generated", Name: "mirrored", Mirror: true}},
		Placed:    map[string]string{"generated/ordinary": "generator", "generated/mirrored": "generator"},
	}
	// Neither recipe can build or start a process: missing generated support
	// must be represented by the same scenario inventory without inventing one.
	if got := s.PrepareGenerated(t); !slices.Equal(got, []string{"go", "fourth"}) {
		t.Fatalf("generated inventory = %v, want every registered language", got)
	}
	s.run(t, "go", "fourth", true, func(Scenario) bool { return false })
	if s.Matrix.rows["fourth"] != nil {
		t.Fatal("a filtered-out run invented coverage")
	}
	for _, pair := range [][2]string{{"go", "fourth"}, {"fourth", "go"}} {
		t.Run(pairName(pair[0], pair[1]), func(t *testing.T) {
			s.runGenerated(t, pair[0], pair[1])
		})
	}
	cell := s.Matrix.rows["fourth"]["generator"]
	if cell == nil || cell.Passed != 0 || cell.Failed != 0 || cell.Skipped != 6 {
		t.Fatalf("missing target coverage = %+v, want six explicit skips", cell)
	}
	for _, profile := range []string{"core", "tunnel", "observability"} {
		s.Matrix.Record("fourth", profile, Outcome{})
	}
	if missing := s.Matrix.Missing([]string{"fourth"}); len(missing) != 0 {
		t.Fatalf("complete core-only run cannot write its matrix: %v", missing)
	}
	if verdict := s.Matrix.Verdict(profiles, "fourth"); verdict != "ok" {
		t.Fatalf("tier 4 optional generated coverage = %s, want ok", verdict)
	}
	profiles.Languages["fourth"] = Language{Tier: 2}
	if verdict := s.Matrix.Verdict(profiles, "fourth"); verdict != "blocking" {
		t.Fatalf("tier 2 missing generated coverage = %s, want blocking", verdict)
	}
}
