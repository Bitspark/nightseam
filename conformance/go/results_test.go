package conformance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// fixtureRuns is one scenario file as the runner runs it: a foreach of three
// rows, each row mirrored, the way Load and run expand a file.
func fixtureRuns() []Scenario {
	base := Scenario{Title: "a request serial that does not increase ends the connection", Layer: "peer", File: "scenarios/peer/serials.json", Mirror: true}
	var runs []Scenario
	for _, row := range []string{"lower", "equal", "gap"} {
		s := base
		s.Name = base.Title + "[" + row + "]"
		runs = append(runs, s, s.Mirrored())
	}
	return runs
}

var fixedRun = Provenance{Commit: "0123456789abcdef0123456789abcdef01234567", URL: "https://example.test/runs/1", Date: "2026-09-23T00:00:00Z"}

// TestResultsCollapseAFileIntoOneOutcome: the rows a foreach expands and
// both orientations collapse into one entry per file, which reads failed
// when any run failed, skipped when any was skipped and none failed, and
// passed only when every run passed, with the counts beside it.
func TestResultsCollapseAFileIntoOneOutcome(t *testing.T) {
	runs := fixtureRuns()
	failed := Outcome{Failed: &Failure{Step: 7, Op: "peer.await_close", On: "b", Reason: "expected close 4011\nand more"}}

	r := NewResults()
	for i, run := range runs {
		o := Outcome{}
		if i == 2 {
			o = failed
		}
		r.Record("rust", "core", run, "go", "rust", o)
	}
	e := r.Report(fixedRun).Languages["rust"]["scenarios/peer/serials.json"]
	if e.Outcome != "failed" || e.Failed != 1 || e.Passed != 5 || e.Skipped != 0 {
		t.Fatalf("one failing row of three, mirrored: %+v", e)
	}
	if e.Name != "a request serial that does not increase ends the connection" || e.Profile != "core" {
		t.Fatalf("the entry does not name its scenario or profile: %+v", e)
	}
	if len(e.Failures) != 1 || e.Failures[0].Run != runs[2].Name || e.Failures[0].Pair != "go-with-rust" || e.Failures[0].Op != "peer.await_close" || e.Failures[0].Reason != "expected close 4011" {
		t.Fatalf("the failure does not say where: %+v", e.Failures)
	}

	r = NewResults()
	for i, run := range runs {
		o := Outcome{}
		if i%2 == 1 {
			o = Outcome{Skipped: "the rust testee, on side a, lacks listen"}
		}
		r.Record("rust", "core", run, "go", "rust", o)
	}
	e = r.Report(fixedRun).Languages["rust"]["scenarios/peer/serials.json"]
	if e.Outcome != "skipped" || e.Passed != 3 || e.Skipped != 3 || len(e.Skips) != 1 || e.Skips[0] != "the rust testee, on side a, lacks listen" {
		t.Fatalf("passing in one orientation and skipped in the other: %+v", e)
	}

	r = NewResults()
	for _, run := range runs {
		r.Record("rust", "core", run, "go", "rust", Outcome{})
	}
	if e := r.Report(fixedRun).Languages["rust"]["scenarios/peer/serials.json"]; e.Outcome != "passed" || e.Passed != 6 {
		t.Fatalf("every run passed: %+v", e)
	}
}

// TestResultsFollowTheSkippedParticipant: a skip the runner attributes to
// the partner that lacked the capability lands in the partner's record, as
// it lands in the partner's matrix row.
func TestResultsFollowTheSkippedParticipant(t *testing.T) {
	p := tiered()
	sc := Scenario{Name: "fixture", Title: "fixture", Layer: "peer", File: "scenarios/peer/fixture.json"}
	s := &Suite{Profiles: p, Matrix: NewMatrix(p), Results: NewResults(), Placed: map[string]string{sc.Key(): "core"}}
	s.reportOutcome(&verdictReporter{}, sc, "go", "fourth", "fourth", Outcome{Skipped: "the go testee, on side a, lacks observer", SkippedBy: "go"})
	report := s.Results.Report(fixedRun)
	if _, found := report.Languages["fourth"]; found {
		t.Fatalf("the partner's missing capability was recorded against the held language: %+v", report.Languages)
	}
	if e := report.Languages["go"]["scenarios/peer/fixture.json"]; e.Outcome != "skipped" || e.Skipped != 1 {
		t.Fatalf("the skip did not land on its participant: %+v", report.Languages)
	}
}

// TestResultsAreWrittenTheSameTwice: the record of one run is byte for byte
// the same whatever order its outcomes arrived in.
func TestResultsAreWrittenTheSameTwice(t *testing.T) {
	runs := fixtureRuns()
	outcomes := []Outcome{
		{},
		{Skipped: "the rust testee, on side b, lacks tunnel"},
		{Failed: &Failure{Step: 1, Op: "conn.send", On: "a", Reason: "refused"}},
		{Failed: &Failure{Step: 3, Op: "conn.receive", On: "b", Reason: "closed"}},
		{Skipped: "the rust testee, on side a, lacks tunnel"},
		{},
	}
	write := func(order []int) []byte {
		r := NewResults()
		for _, i := range order {
			r.Record("rust", "core", runs[i], "go", "rust", outcomes[i])
			r.Record("python", "core", runs[i], "python", "go", outcomes[len(outcomes)-1-i])
		}
		file := filepath.Join(t.TempDir(), "results.json")
		if err := r.Write(fixedRun, file); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first, second := write([]int{0, 1, 2, 3, 4, 5}), write([]int{5, 3, 1, 4, 2, 0})
	if !bytes.Equal(first, second) {
		t.Fatalf("the record differs by the order outcomes came in:\n%s\n%s", first, second)
	}
}

// TestAWholeRunWritesItsRecordAndAFilteredOneWritesNone: the record is
// written with the matrix, and a run that left a language's profile unheld
// writes neither, so no file on disk is part of a run read as the whole.
func TestAWholeRunWritesItsRecordAndAFilteredOneWritesNone(t *testing.T) {
	p := tiered()
	languages := []string{"go", "second"}
	whole, filtered := NewMatrix(p), NewMatrix(p)
	r := NewResults()
	for _, language := range languages {
		for profile := range p.Profiles {
			whole.Record(language, profile, Outcome{})
			if language == "go" {
				filtered.Record(language, profile, Outcome{})
			}
		}
		r.Record(language, "core", Scenario{Title: "fixture", Name: "fixture", Layer: "peer", File: "scenarios/peer/fixture.json"}, "go", language, Outcome{})
	}
	dir := t.TempDir()
	if written, err := writeRun(filtered, r, p, languages, dir, fixedRun); err != nil || written {
		t.Fatalf("a filtered run wrote: %v %v", written, err)
	}
	for _, name := range []string{"matrix.json", "results.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("a filtered run left %s: %v", name, err)
		}
	}
	if written, err := writeRun(whole, r, p, languages, dir, fixedRun); err != nil || !written {
		t.Fatalf("a whole run did not write: %v %v", written, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report ResultsReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Run != fixedRun || len(report.Languages) != 2 {
		t.Fatalf("the record lost its provenance or a language: %s", data)
	}
}

// TestTheRecordFitsItsSchema: what Write writes validates against
// conformance/results.schema.json, a failure, a skip and a pass among it.
func TestTheRecordFitsItsSchema(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchemaFile(filepath.Join(root, "results.schema.json"), "results.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	r := NewResults()
	runs := fixtureRuns()
	r.Record("rust", "core", runs[0], "go", "rust", Outcome{})
	r.Record("rust", "core", runs[1], "rust", "go", Outcome{Skipped: "the rust testee, on side a, lacks listen"})
	r.Record("rust", "core", runs[2], "go", "rust", Outcome{Failed: &Failure{Step: 2, Op: "conn.send", On: "b", Reason: strings.Repeat("é", 800)}})
	r.Record("go", "core", runs[0], "go", "go", Outcome{})
	file := filepath.Join(t.TempDir(), "results.json")
	if err := r.Write(fixedRun, file); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("the record does not fit its schema: %v\n%s", err, data)
	}
	var report ResultsReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if reason := report.Languages["rust"]["scenarios/peer/serials.json"].Failures[0].Reason; len(reason) > 1024+len("…") || !strings.HasSuffix(reason, "…") {
		t.Fatalf("a runaway reason was not bounded on a rune: %d bytes", len(reason))
	}
}

// TestProvenanceNamesTheCommitAndTheRun: the provenance of a run in this
// checkout names a commit git knows, and the CI run the environment names.
func TestProvenanceNamesTheCommitAndTheRun(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "Bitspark/nightseam")
	t.Setenv("GITHUB_RUN_ID", "42")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	run := runProvenance(filepath.Dir(root))
	if len(run.Commit) != 40 {
		t.Fatalf("no commit: %+v", run)
	}
	if run.URL != "https://github.com/Bitspark/nightseam/actions/runs/42/attempts/2" {
		t.Fatalf("the run is not named: %q", run.URL)
	}
	if run.Date == "" {
		t.Fatal("no date")
	}
}
