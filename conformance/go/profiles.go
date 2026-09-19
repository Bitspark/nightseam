package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Profiles is conformance/profiles.json: what each language promises and
// how the suite holds it. A profile is a set of scenarios, by layer or by
// a feature they need; a tier is what a language guarantees; docs/languages/tiers.md
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

// LoadProfiles reads root/profiles.json, holds it to profiles.schema.json
// and to itself: a tier requires only profiles that exist, and a language
// with a tier has a testee under conformance/<lang>.
func LoadProfiles(root string) (*Profiles, error) {
	data, err := os.ReadFile(filepath.Join(root, "profiles.json"))
	if err != nil {
		return nil, err
	}
	schema, err := loadSchemaFile(filepath.Join(root, "profiles.schema.json"), "profiles.schema.json")
	if err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("profiles.json: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return nil, fmt.Errorf("profiles.json does not fit profiles.schema.json: %v", err)
	}
	var p Profiles
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profiles.json: %w", err)
	}
	for name, tier := range p.Tiers {
		for _, required := range tier.Requires {
			if _, ok := p.Profiles[required]; !ok {
				return nil, fmt.Errorf("profiles.json: tier %s requires %s, which no profile is named", name, required)
			}
		}
	}
	for language, l := range p.Languages {
		if _, ok := p.Tiers[fmt.Sprint(l.Tier)]; !ok {
			return nil, fmt.Errorf("profiles.json: %s is of tier %d, which the tiers do not name", language, l.Tier)
		}
	}
	recipes, err := Recipes(root)
	if err != nil {
		return nil, err
	}
	for language, l := range p.Languages {
		if _, ok := recipes[language]; !ok {
			return nil, fmt.Errorf("profiles.json: %s is named at tier %d and no testee.json under conformance/ declares that language", language, l.Tier)
		}
	}
	return &p, nil
}

// Cover holds every profile to having at least one scenario: a profile
// nothing exercises promises nothing.
func (p *Profiles) Cover(scenarios []Scenario) error {
	covered := map[string]bool{}
	for _, s := range scenarios {
		profile, err := p.Place(s)
		if err != nil {
			return err
		}
		covered[profile] = true
	}
	var empty []string
	for name := range p.Profiles {
		if !covered[name] {
			empty = append(empty, name)
		}
	}
	sort.Strings(empty)
	if len(empty) > 0 {
		return fmt.Errorf("profiles.json names profiles no scenario falls in: %s", strings.Join(empty, ", "))
	}
	return nil
}

// Requires is what a testee of a language must answer hello with, given
// its tier: every layer and feature of every profile the tier requires. A
// language of no tier — one with a testee and no entry yet — is held to
// nothing, which is how a language enters the matrix.
func (p *Profiles) Requires(language string) (layers, features []string) {
	l, ok := p.Languages[language]
	if !ok {
		return nil, nil
	}
	tier, ok := p.Tiers[fmt.Sprint(l.Tier)]
	if !ok {
		return nil, nil
	}
	seenL, seenF := map[string]bool{}, map[string]bool{}
	for _, name := range tier.Requires {
		profile := p.Profiles[name]
		for _, layer := range profile.Layers {
			if !seenL[layer] {
				seenL[layer] = true
				layers = append(layers, layer)
			}
		}
		for _, feature := range profile.Needs {
			if !seenF[feature] {
				seenF[feature] = true
				features = append(features, feature)
			}
		}
	}
	sort.Strings(layers)
	sort.Strings(features)
	return layers, features
}

// HoldToTier reports what a testee's hello lacks of what its tier requires:
// nothing for a testee that carries it all, or of a language of no tier.
// The generated layer is a second testee's, held when that one starts.
func (p *Profiles) HoldToTier(language string, h Hello) error {
	layers, features := p.Requires(language)
	var missing []string
	for _, need := range append(layers, features...) {
		if need == "generated" {
			continue
		}
		if !h.Has(need) {
			missing = append(missing, need)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("the %s testee is of tier %d and answers hello without %s, which that tier requires; a skip is what a lower tier is for", language, p.Languages[language].Tier, strings.Join(missing, ", "))
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

// profileCell is one language's standing in one profile, on one side of the wire
// against the reference: how many scenarios passed, were skipped, failed.
type profileCell struct {
	Passed  int `json:"passed"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// Matrix is the report of one run: a row per language, a column per
// profile, and the language's tier beside it.
type Matrix struct {
	profiles []string
	rows     map[string]map[string]*profileCell
	tiers    map[string]int
}

func NewMatrix(p *Profiles) *Matrix {
	m := &Matrix{rows: map[string]map[string]*profileCell{}, tiers: map[string]int{}}
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
		row = map[string]*profileCell{}
		m.rows[language] = row
	}
	cell, ok := row[profile]
	if !ok {
		cell = &profileCell{}
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

// Verdict is what the tier table says of a language's row: ok when every
// cell of a required profile passed or was skipped; else what the tier's
// onFailure says — blocking, which stops a release; provisional, which
// marks the language in the notes; blocking-next, a tier 2 language's
// second failing release. A language of no tier is never blocking.
func (m *Matrix) Verdict(p *Profiles, language string) string {
	l, ok := p.Languages[language]
	if !ok {
		return "provisional"
	}
	tier, ok := p.Tiers[fmt.Sprint(l.Tier)]
	if !ok {
		return "provisional"
	}
	row := m.rows[language]
	for _, profile := range tier.Requires {
		if cell := row[profile]; cell != nil && cell.Failed > 0 {
			switch tier.OnFailure {
			case "stop":
				return "blocking"
			case "provisional":
				return "provisional"
			}
			return tier.OnFailure
		}
	}
	return "ok"
}

// Report is the matrix as conformance/matrix.json spells it: the rows, the
// profiles, and a verdict per language.
type Report struct {
	Profiles  []string                  `json:"profiles"`
	Languages map[string]languageReport `json:"languages"`
}

// languageReport is one language's row.
type languageReport struct {
	Tier    int                    `json:"tier,omitempty"`
	Verdict string                 `json:"verdict"`
	Cells   map[string]profileCell `json:"cells"`
}

// Write renders the matrix as JSON to file, rows and profiles sorted, so
// that two runs of the same suite write the same bytes.
func (m *Matrix) Write(p *Profiles, file string) error {
	report := Report{Profiles: m.profiles, Languages: map[string]languageReport{}}
	for language, row := range m.rows {
		cells := map[string]profileCell{}
		for profile, cell := range row {
			cells[profile] = *cell
		}
		report.Languages[language] = languageReport{Tier: m.tiers[language], Verdict: m.Verdict(p, language), Cells: cells}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o644)
}

// Missing names every language and profile cell the run left empty, as
// "language/profile", sorted: the whole suite leaves none.
func (m *Matrix) Missing(languages []string) []string {
	var out []string
	for _, language := range languages {
		for _, profile := range m.profiles {
			if m.rows[language] == nil || m.rows[language][profile] == nil {
				out = append(out, language+"/"+profile)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Blocking names the languages whose verdict stops a release.
func (m *Matrix) Blocking(p *Profiles) []string {
	var out []string
	for language := range m.rows {
		if m.Verdict(p, language) == "blocking" {
			out = append(out, language)
		}
	}
	sort.Strings(out)
	return out
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
