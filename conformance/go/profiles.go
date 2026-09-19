package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profiles is conformance/profiles.json: what each language promises and
// how the suite holds it. A profile is a set of scenarios, by layer or by
// a feature they need; a tier is what a language guarantees; docs/tiers.md
// says what each means. The runner places every scenario in exactly one
// profile and refuses one it cannot place, so nothing is unclassified.
type Profiles struct {
	Profiles  map[string]Profile  `json:"profiles"`
	Tiers     map[string]Tier     `json:"tiers"`
	Languages map[string]Language `json:"languages"`
	Planned   map[string]int      `json:"planned"`
}

// Profile is one named set of scenarios: those of its layers, or those
// needing one of its features.
type Profile struct {
	Layers  []string `json:"layers"`
	Needs   []string `json:"needs"`
	Promise string   `json:"promise"`
}

// Tier is what a language of it must hold, and what a failure means.
type Tier struct {
	Requires  []string `json:"requires"`
	Lag       int      `json:"lag"`
	OnFailure string   `json:"onFailure"`
}

// Language is a language's place in the tiers.
type Language struct {
	Tier      int  `json:"tier"`
	Reference bool `json:"reference"`
}

// LoadProfiles reads root/profiles.json.
func LoadProfiles(root string) (*Profiles, error) {
	data, err := os.ReadFile(filepath.Join(root, "profiles.json"))
	if err != nil {
		return nil, err
	}
	var p Profiles
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profiles.json: %w", err)
	}
	for name, profile := range p.Profiles {
		if len(profile.Layers) == 0 && len(profile.Needs) == 0 {
			return nil, fmt.Errorf("profiles.json: profile %s names neither layers nor needs", name)
		}
	}
	return &p, nil
}

// Place names the one profile a scenario belongs to. A profile by feature
// takes precedence over one by layer, so that a trace scenario of the peer
// layer is observability's and not core's; a scenario two feature profiles
// would take, or none, is refused.
func (p *Profiles) Place(s Scenario) (string, error) {
	var byNeed, byLayer []string
	for name, profile := range p.Profiles {
		for _, need := range profile.Needs {
			for _, n := range s.Needs {
				if n == need {
					byNeed = append(byNeed, name)
				}
			}
		}
		for _, layer := range profile.Layers {
			if layer == s.Layer {
				byLayer = append(byLayer, name)
			}
		}
	}
	sort.Strings(byNeed)
	sort.Strings(byLayer)
	byNeed = dedupe(byNeed)
	switch {
	case len(byNeed) == 1:
		return byNeed[0], nil
	case len(byNeed) > 1:
		return "", fmt.Errorf("%s: scenario %q is placed by more than one profile: %s", s.File, s.Name, strings.Join(byNeed, ", "))
	case len(byLayer) == 1:
		return byLayer[0], nil
	case len(byLayer) > 1:
		return "", fmt.Errorf("%s: scenario %q is placed by more than one profile: %s", s.File, s.Name, strings.Join(byLayer, ", "))
	}
	return "", fmt.Errorf("%s: scenario %q is placed by no profile in profiles.json", s.File, s.Name)
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Cell is one language's standing in one profile, on one side of the wire
// against the reference: how many scenarios passed, were skipped, failed.
type Cell struct {
	Passed, Skipped, Failed int
}

// Matrix is the report of one run: a row per language, a column per
// profile, and the language's tier beside it.
type Matrix struct {
	profiles []string
	rows     map[string]map[string]*Cell
	tiers    map[string]int
}

func NewMatrix(p *Profiles) *Matrix {
	m := &Matrix{rows: map[string]map[string]*Cell{}, tiers: map[string]int{}}
	for name := range p.Profiles {
		m.profiles = append(m.profiles, name)
	}
	sort.Strings(m.profiles)
	for language, l := range p.Languages {
		m.tiers[language] = l.Tier
	}
	return m
}

// Record adds one outcome for a language in a profile.
func (m *Matrix) Record(language, profile string, o Outcome) {
	row, ok := m.rows[language]
	if !ok {
		row = map[string]*Cell{}
		m.rows[language] = row
	}
	cell, ok := row[profile]
	if !ok {
		cell = &Cell{}
		row[profile] = cell
	}
	switch {
	case o.Failed != nil:
		cell.Failed++
	case o.Skipped != "":
		cell.Skipped++
	default:
		cell.Passed++
	}
}

// String renders the matrix as a table a reader takes in at a glance.
func (m *Matrix) String() string {
	var languages []string
	for language := range m.rows {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s %-5s", "language", "tier")
	for _, profile := range m.profiles {
		fmt.Fprintf(&b, " %-22s", profile)
	}
	b.WriteString("\n")
	for _, language := range languages {
		tier := "—"
		if t, ok := m.tiers[language]; ok {
			tier = fmt.Sprint(t)
		}
		fmt.Fprintf(&b, "%-12s %-5s", language, tier)
		for _, profile := range m.profiles {
			cell := m.rows[language][profile]
			if cell == nil {
				fmt.Fprintf(&b, " %-22s", "—")
				continue
			}
			fmt.Fprintf(&b, " %-22s", fmt.Sprintf("%d passed %d skipped %d failed", cell.Passed, cell.Skipped, cell.Failed))
		}
		b.WriteString("\n")
	}
	return b.String()
}
