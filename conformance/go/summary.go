package conformance

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// summary reports this run's actual cells, including every nonblocking red
// cell. It does not infer success from a checked-in matrix or hide optional
// failures behind an otherwise green language verdict.
func (m *Matrix) summary(p *Profiles) string {
	var languages []string
	for language := range m.rows {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	var b strings.Builder
	b.WriteString("## Conformance matrix\n\nCounts are passed / skipped / failed.\n\n| Language | Tier | Verdict |")
	for _, profile := range m.profiles {
		fmt.Fprintf(&b, " %s |", profile)
	}
	b.WriteString("\n|---|---|---|")
	for range m.profiles {
		b.WriteString("---|")
	}
	b.WriteByte('\n')
	var notes []string
	for _, language := range languages {
		verdict := m.Verdict(p, language)
		tierNumber := m.tiers[language]
		tier := p.Tiers[fmt.Sprint(tierNumber)]
		fmt.Fprintf(&b, "| %s | %d | %s |", language, tierNumber, verdict)
		for _, profile := range m.profiles {
			cell := m.rows[language][profile]
			if cell == nil {
				b.WriteString(" — |")
				continue
			}
			fmt.Fprintf(&b, " %d / %d / %d |", cell.Passed, cell.Skipped, cell.Failed)
			required := slices.Contains(tier.Requires, profile)
			if cell.Failed == 0 && (!required || cell.Skipped == 0) {
				continue
			}
			disposition := "informational"
			if required {
				disposition = verdict
			} else if tierNumber == 2 {
				disposition = "nonblocking; due by the next minor release"
			}
			notes = append(notes, fmt.Sprintf("- `%s/%s`: %d failed, %d skipped (%s).", language, profile, cell.Failed, cell.Skipped, disposition))
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n### Provisional and other red cells\n\n")
	if len(notes) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString(strings.Join(notes, "\n") + "\n")
	}
	if blocking := m.Blocking(p); len(blocking) > 0 {
		fmt.Fprintf(&b, "\nBlocking languages: %s.\n", strings.Join(blocking, ", "))
	} else {
		b.WriteString("\nNo blocking language verdicts. Nightly still fails on every red cell.\n")
	}
	return b.String()
}

func writeSummary(file string, m *Matrix, p *Profiles) error {
	return os.WriteFile(file, []byte(m.summary(p)), 0o644)
}
