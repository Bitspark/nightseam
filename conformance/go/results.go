package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Results is the per-scenario record of one run, beside the matrix's
// per-profile counts: for each language, one entry per scenario file, the
// rows a foreach expands and both orientations of a pairing collapsed into
// it. The matrix says how many scenarios of a profile a language held; the
// record says which, and why each one that was not held was not. It gates
// nothing: the verdicts read the matrix. TestMain writes it with the matrix,
// and only when the run held the whole suite.
type Results struct {
	entries map[string]map[string]*resultEntry
}

// resultEntry is one scenario file's outcome for one language, gathered
// run by run as the outcomes arrive.
type resultEntry struct {
	name, profile           string
	passed, skipped, failed int
	skips                   map[string]bool
	failures                []resultFailure
}

// resultFailure is where one run of a scenario parted from it: which run
// (the expanded name, a mirror marked), the pairing it ran in, and the step.
type resultFailure struct {
	Run    string `json:"run"`
	Pair   string `json:"pair"`
	Step   int    `json:"step"`
	Op     string `json:"op"`
	On     string `json:"on"`
	Reason string `json:"reason"`
}

// Provenance is what a record came from: the commit the run held, whether
// the tree had changes beside it, the CI run when there was one, and when.
type Provenance struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty,omitempty"`
	URL    string `json:"url,omitempty"`
	Date   string `json:"date"`
}

// ResultsReport is the record as conformance/results.json spells it.
type ResultsReport struct {
	Run       Provenance                                `json:"run"`
	Languages map[string]map[string]scenarioResultEntry `json:"languages"`
}

// scenarioResultEntry is one scenario file's collapsed outcome for one
// language: failed when any run failed, else skipped when any run was
// skipped, else passed; the counts say how many runs each was.
type scenarioResultEntry struct {
	Name     string          `json:"name"`
	Profile  string          `json:"profile"`
	Outcome  string          `json:"outcome"`
	Passed   int             `json:"passed"`
	Skipped  int             `json:"skipped"`
	Failed   int             `json:"failed"`
	Skips    []string        `json:"skips,omitempty"`
	Failures []resultFailure `json:"failures,omitempty"`
}

func NewResults() *Results {
	return &Results{entries: map[string]map[string]*resultEntry{}}
}

// Record adds one run of a scenario, in one pairing, to the language whose
// row it counts in — the same language the matrix counts it in, a skip
// already attributed to the participant that lacked the capability.
func (r *Results) Record(language, profile string, sc Scenario, a, b string, o Outcome) {
	row := r.entries[language]
	if row == nil {
		row = map[string]*resultEntry{}
		r.entries[language] = row
	}
	entry := row[sc.File]
	if entry == nil {
		name := sc.Title
		if name == "" {
			name = sc.Name
		}
		entry = &resultEntry{name: name, profile: profile, skips: map[string]bool{}}
		row[sc.File] = entry
	}
	switch {
	case o.Failed != nil:
		entry.failed++
		entry.failures = append(entry.failures, resultFailure{
			Run:    sc.Name,
			Pair:   pairName(a, b),
			Step:   o.Failed.Step,
			Op:     o.Failed.Op,
			On:     o.Failed.On,
			Reason: bounded(firstLine(o.Failed.Reason), 1024),
		})
	case o.Skipped != "":
		entry.skipped++
		entry.skips[bounded(firstLine(o.Skipped), 1024)] = true
	default:
		entry.passed++
	}
}

// Report is the record with its provenance, sorted wherever order is the
// arrival's and not the meaning's, so that one run's outcomes write the
// same bytes whatever order they came in.
func (r *Results) Report(run Provenance) ResultsReport {
	report := ResultsReport{Run: run, Languages: map[string]map[string]scenarioResultEntry{}}
	for language, row := range r.entries {
		out := map[string]scenarioResultEntry{}
		for file, entry := range row {
			e := scenarioResultEntry{
				Name: entry.name, Profile: entry.profile, Outcome: "passed",
				Passed: entry.passed, Skipped: entry.skipped, Failed: entry.failed,
			}
			switch {
			case entry.failed > 0:
				e.Outcome = "failed"
			case entry.skipped > 0:
				e.Outcome = "skipped"
			}
			for skip := range entry.skips {
				e.Skips = append(e.Skips, skip)
			}
			sort.Strings(e.Skips)
			e.Failures = append([]resultFailure(nil), entry.failures...)
			sort.Slice(e.Failures, func(i, j int) bool {
				x, y := e.Failures[i], e.Failures[j]
				if x.Run != y.Run {
					return x.Run < y.Run
				}
				if x.Pair != y.Pair {
					return x.Pair < y.Pair
				}
				return x.Step < y.Step
			})
			out[file] = e
		}
		report.Languages[language] = out
	}
	return report
}

// Write renders the record as JSON to file.
func (r *Results) Write(run Provenance, file string) error {
	data, err := json.MarshalIndent(r.Report(run), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o644)
}

// runProvenance is the provenance of a run in checkout: the commit git
// names as HEAD, whether the tree differs from it, and the CI run when the
// environment names one. A checkout git cannot read is recorded as such
// rather than refusing the record.
func runProvenance(checkout string) Provenance {
	run := Provenance{Commit: "unknown", Date: time.Now().UTC().Format(time.RFC3339)}
	if out, err := exec.Command("git", "-C", checkout, "rev-parse", "HEAD").Output(); err == nil {
		run.Commit = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", checkout, "status", "--porcelain").Output(); err == nil {
		run.Dirty = strings.TrimSpace(string(out)) != ""
	}
	server, repository, id := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	if server != "" && repository != "" && id != "" {
		run.URL = fmt.Sprintf("%s/%s/actions/runs/%s", server, repository, id)
		if attempt := os.Getenv("GITHUB_RUN_ATTEMPT"); attempt != "" && attempt != "1" {
			run.URL += "/attempts/" + attempt
		}
	}
	return run
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// bounded keeps a reason to limit bytes, cut on a rune boundary, so that
// one testee's runaway message cannot inflate the record.
func bounded(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
