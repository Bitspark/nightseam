package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A real failing command and real driver processes exercise both build paths
// and the star's continuation. The parent checks the child test process's exit
// code, so an Errorf/Fatalf cannot be mistaken for a passing provisional run.
func TestBuildOutcomeHelper(t *testing.T) {
	switch os.Getenv("NIGHTSEAM_BUILD_OUTCOME_HELPER") {
	case "":
		return
	case "fail":
		for n := 0; n < 40; n++ {
			fmt.Fprintf(os.Stderr, "build progress %d\n", n)
		}
		fmt.Fprintln(os.Stderr, "fixture compiler refused the testee")
		os.Exit(1)
	case "driver":
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var request map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
				os.Exit(2)
			}
			var answer any = map[string]any{}
			if request["op"] == "hello" {
				answer = Hello{Driver: 1, Language: os.Getenv("NIGHTSEAM_BUILD_LANGUAGE"), Layers: []string{"peer", "generated"}}
			}
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request["id"], "ok": answer}); err != nil {
				os.Exit(3)
			}
			if request["op"] == "bye" {
				os.Exit(0)
			}
		}
		os.Exit(0)
	case "suite":
	default:
		t.Fatal("unknown fixture mode")
	}

	tier, err := strconv.Atoi(os.Getenv("NIGHTSEAM_BUILD_TIER"))
	if err != nil {
		t.Fatal(err)
	}
	stage := os.Getenv("NIGHTSEAM_BUILD_STAGE")
	dir := os.Getenv("NIGHTSEAM_BUILD_OUTPUT")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	p := tiered()
	p.Profiles = map[string]Profile{"core": {Layers: []string{"peer"}}, "generator": {Layers: []string{"generated"}}}
	p.Tiers["1"] = Tier{Requires: []string{"core", "generator"}, OnFailure: "stop"}
	p.Languages = map[string]Language{"go": {Tier: 1, Reference: true}, "broken": {Tier: tier}, "healthy": {Tier: 1}}
	s := &Suite{Checkout: filepath.Dir(root), Profiles: p, Matrix: NewMatrix(p), Recipes: map[string]Recipe{},
		Scenarios: []Scenario{{Layer: "peer", Name: "native"}, {Layer: "generated", Name: "generated"}},
		Placed:    map[string]string{"peer/native": "core", "generated/generated": "generator"},
		places:    Places{Checkout: filepath.Dir(root), Out: dir}, rendered: map[string]string{},
	}
	for _, language := range []string{"go", "broken", "healthy"} {
		self := filepath.Join(dir, language)
		if err := os.MkdirAll(filepath.Join(self, "generated"), 0o755); err != nil {
			t.Fatal(err)
		}
		command := Command{Argv: []string{executable, "-test.run=^TestBuildOutcomeHelper$"}, Env: map[string]string{
			"NIGHTSEAM_BUILD_OUTCOME_HELPER": "driver", "NIGHTSEAM_BUILD_LANGUAGE": language,
		}}
		var recipe Recipe
		data, _ := json.Marshal(map[string]any{"language": language, "run": command, "generated": map[string]any{
			"rendered": "{out}/rendered-" + language, "run": command,
		}})
		if err := json.Unmarshal(data, &recipe); err != nil {
			t.Fatal(err)
		}
		recipe.Dir = self
		if language == "broken" && os.Getenv("NIGHTSEAM_BUILD_RECOVERED") == "" {
			failure := Command{Argv: command.Argv, Env: map[string]string{"NIGHTSEAM_BUILD_OUTCOME_HELPER": "fail"}}
			if stage == "runtime" {
				recipe.Build = []Command{failure}
			} else {
				recipe.Generated.Build = []Command{failure}
			}
		}
		s.Recipes[language] = recipe
	}
	t.Cleanup(func() {
		if err := s.Matrix.Write(p, filepath.Join(dir, "matrix.json")); err != nil {
			t.Error(err)
		}
		if err := writeSummary(filepath.Join(dir, "summary.md"), s.Matrix, p); err != nil {
			t.Error(err)
		}
		if missing := s.Matrix.Missing(s.Languages()); len(missing) != 0 {
			t.Errorf("star lost coverage: %v", missing)
		}
	})
	s.buildRuntime(t)
	for _, pair := range s.Pairings(false) {
		t.Run(pairName(pair[0], pair[1]), func(t *testing.T) { s.pair(t, pair[0], pair[1]) })
	}
	s.PrepareGenerated(t)
	for _, pair := range s.Pairings(false) {
		t.Run("generated/"+pairName(pair[0], pair[1]), func(t *testing.T) { s.runGenerated(t, pair[0], pair[1]) })
	}
}

func TestBuildOutcomesFollowTheTierTable(t *testing.T) {
	for _, stage := range []string{"runtime", "generated"} {
		for _, tier := range []int{1, 2, 3, 4} {
			for _, strict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/tier%d/nightly%v", stage, tier, strict), func(t *testing.T) {
					dir := t.TempDir()
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					cmd := exec.CommandContext(ctx, executable, "-test.run=^TestBuildOutcomeHelper$", "-test.v")
					nightly := ""
					if strict {
						nightly = "1"
					}
					cmd.Env = append(os.Environ(), "NIGHTSEAM_BUILD_OUTCOME_HELPER=suite", "NIGHTSEAM_BUILD_STAGE="+stage,
						"NIGHTSEAM_BUILD_TIER="+strconv.Itoa(tier), "NIGHTSEAM_BUILD_OUTPUT="+dir, "NIGHTSEAM_MATRIX="+nightly)
					output, err := cmd.CombinedOutput()
					blocking := tier <= 2
					if (err != nil) != (blocking || strict) {
						t.Fatalf("star exit: %v, want failure=%v\n%s", err, blocking || strict, output)
					}
					data, err := os.ReadFile(filepath.Join(dir, "matrix.json"))
					if err != nil {
						t.Fatalf("matrix: %v\n%s", err, output)
					}
					var report Report
					if err := json.Unmarshal(data, &report); err != nil {
						t.Fatal(err)
					}
					row := report.Languages["broken"]
					want := "provisional"
					if blocking {
						want = "blocking"
					}
					if row.State != "absent" || row.Verdict != want || !strings.Contains(row.Reasons[stage], "fixture compiler refused") || !strings.Contains(row.Reasons[stage], "exit status 1") {
						t.Fatalf("build absence lost: %s", data)
					}
					if strings.Contains(row.Reasons[stage], "build progress 0\n") {
						t.Fatal("build diagnostic did not retain only its bounded last lines")
					}
					for _, profile := range []string{"core", "generator"} {
						if cell := report.Languages["healthy"].Cells[profile]; cell.Passed != 2 || cell.Skipped != 0 || cell.Failed != 0 {
							t.Fatalf("remaining pairing did not run: healthy/%s=%+v\n%s", profile, cell, output)
						}
						cell := row.Cells[profile]
						if stage == "runtime" || profile == "generator" {
							if cell.Passed != 0 || cell.Skipped != 2 || cell.Failed != 0 {
								t.Fatalf("unavailable scenarios were not skipped: %s=%+v", profile, cell)
							}
						} else if cell.Passed != 2 || cell.Skipped != 0 || cell.Failed != 0 {
							t.Fatalf("generated build failure hid runtime coverage: %+v", cell)
						}
					}
					summary, err := os.ReadFile(filepath.Join(dir, "summary.md"))
					if err != nil || !strings.Contains(string(summary), "absent") || !strings.Contains(string(summary), "fixture compiler refused") || !strings.Contains(string(summary), want) {
						t.Fatalf("summary lost build absence: %v\n%s", err, summary)
					}
					if tier == 4 && !strict {
						// Reuse the output paths, as CI's next run does, but a new
						// process and matrix. A repaired build must replace absence.
						cmd = exec.CommandContext(ctx, executable, "-test.run=^TestBuildOutcomeHelper$")
						cmd.Env = append(os.Environ(), "NIGHTSEAM_BUILD_OUTCOME_HELPER=suite", "NIGHTSEAM_BUILD_STAGE="+stage,
							"NIGHTSEAM_BUILD_TIER=4", "NIGHTSEAM_BUILD_OUTPUT="+dir, "NIGHTSEAM_MATRIX=", "NIGHTSEAM_BUILD_RECOVERED=1")
						if output, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("recovered star: %v\n%s", err, output)
						}
						for _, file := range []string{"matrix.json", "summary.md"} {
							data, err := os.ReadFile(filepath.Join(dir, file))
							if err != nil || strings.Contains(string(data), "fixture compiler refused") || strings.Contains(string(data), "absent") {
								t.Fatalf("repaired run retained stale absence in %s: %v\n%s", file, err, data)
							}
						}
					}
				})
			}
		}
	}
}

func TestBuildDiagnosticIsBoundedAndSafeForSummary(t *testing.T) {
	reason := buildDiagnostic(fmt.Errorf("command %s: exit status 1\n%s</pre><script>bad()</script>", strings.Repeat("arg ", 1000), strings.Repeat("λ", 10000)))
	if len(reason) > 8192 || !strings.Contains(reason, "exit status 1") || !strings.HasSuffix(reason, "</script>") {
		t.Fatalf("diagnostic bounds lost: %d bytes", len(reason))
	}
	m := NewMatrix(tiered())
	m.recordAbsent("fourth", "runtime", reason)
	if summary := m.summary(tiered()); strings.Contains(summary, "<script>") || !strings.Contains(summary, "&lt;script&gt;") {
		t.Fatal("build output was not escaped in the job summary")
	}
}
