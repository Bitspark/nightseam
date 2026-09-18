// Package conformance is the runner of Nightseam's conformance suite: the
// scenarios under conformance/scenarios, driven over the protocol of
// conformance/DRIVER.md against a testee of each language. The runner
// never speaks the profile; it starts two testees, tells each what to do,
// one request at a time, and holds what came back to the scenario. A
// language joins the suite with a testee.json in its directory under
// conformance/, and is held to Go's testee on either side of the wire.
package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Scenario is one scenario as the runner runs it: a foreach has been
// expanded into one Scenario per row, the row bound under its name.
type Scenario struct {
	Name        string
	Layer       string
	Replaces    string
	Description string
	Needs       []string
	Steps       []Step
	// Row is what a foreach bound, or nil.
	Row   map[string]any
	RowAs string
	File  string
}

// Step is one op of one testee and what is held against its answer.
type Step struct {
	On          string
	Op          string
	Args        map[string]any
	Bind        any
	Expect      any
	HasExpect   bool
	ExpectError map[string]any
	Assert      map[string]any
	Repeat      *Repeat
	Note        string
}

// Repeat says a step is sent again until an answer of one kind, at most
// Max times; the step's expectations then hold against that answer.
type Repeat struct {
	Max   int
	Until string
}

type scenarioFile struct {
	Name        string            `json:"name"`
	Layer       string            `json:"layer"`
	Replaces    string            `json:"replaces"`
	Description string            `json:"description"`
	Needs       []string          `json:"needs"`
	Foreach     *foreachClause    `json:"foreach"`
	Steps       []json.RawMessage `json:"steps"`
}

type foreachClause struct {
	Table string         `json:"table"`
	As    string         `json:"as"`
	Where map[string]any `json:"where"`
}

// Root is the conformance directory of the checkout the runner is in:
// where DRIVER.md, the schema, the tables and the scenarios are.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "conformance", "DRIVER.md")); err == nil {
			return filepath.Join(dir, "conformance"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no conformance directory above %s", dir)
		}
		dir = parent
	}
}

// Load reads every scenario under root/scenarios, holds each file to the
// schema, expands every foreach, and returns them sorted by layer and name.
func Load(root string) ([]Scenario, error) {
	schema, err := loadSchema(filepath.Join(root, "scenario.schema.json"))
	if err != nil {
		return nil, err
	}
	var scenarios []Scenario
	err = filepath.WalkDir(filepath.Join(root, "scenarios"), func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, p)
		expanded, err := parse(root, filepath.ToSlash(relative), data, schema)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
		scenarios = append(scenarios, expanded...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(scenarios, func(i, j int) bool {
		if scenarios[i].Layer != scenarios[j].Layer {
			return layerRank(scenarios[i].Layer) < layerRank(scenarios[j].Layer)
		}
		return scenarios[i].Name < scenarios[j].Name
	})
	seen := map[string]string{}
	for _, s := range scenarios {
		key := s.Layer + "/" + s.Name
		if other, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s: scenario %q is also declared in %s", s.File, key, other)
		}
		seen[key] = s.File
	}
	return scenarios, nil
}

var layers = []string{"seam", "peer", "tunnel", "session", "generated"}

func layerRank(layer string) int {
	for i, known := range layers {
		if known == layer {
			return i
		}
	}
	return len(layers)
}

func loadSchema(file string) (*jsonschema.Schema, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("scenario.schema.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("scenario.schema.json")
}

func parse(root, file string, data []byte, schema *jsonschema.Schema) ([]Scenario, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(document); err != nil {
		return nil, fmt.Errorf("does not fit the scenario schema: %v", err)
	}
	var sf scenarioFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	layer := path.Base(path.Dir(file))
	if sf.Layer != layer {
		return nil, fmt.Errorf("declares layer %s but lies under %s", sf.Layer, layer)
	}
	steps := make([]Step, 0, len(sf.Steps))
	for i, raw := range sf.Steps {
		step, err := parseStep(raw)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		steps = append(steps, step)
	}
	base := Scenario{Name: sf.Name, Layer: sf.Layer, Replaces: sf.Replaces, Description: sf.Description, Needs: sf.Needs, Steps: steps, File: file}
	if sf.Foreach == nil {
		return []Scenario{base}, nil
	}
	rows, err := tableRows(filepath.Join(root, filepath.FromSlash(sf.Foreach.Table)), sf.Foreach.Where)
	if err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}
	var out []Scenario
	for i, row := range rows {
		s := base
		s.Row, s.RowAs = row, sf.Foreach.As
		label := fmt.Sprint(i)
		if name, ok := row["name"].(string); ok {
			label = name
		}
		s.Name = fmt.Sprintf("%s[%s]", base.Name, label)
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("foreach over %s selects no row", sf.Foreach.Table)
	}
	return out, nil
}

func parseStep(raw json.RawMessage) (Step, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return Step{}, err
	}
	decoded, err := decode(raw)
	if err != nil {
		return Step{}, err
	}
	object := decoded.(map[string]any)
	step := Step{On: object["on"].(string), Op: object["op"].(string)}
	if args, ok := object["args"].(map[string]any); ok {
		step.Args = args
	}
	step.Bind = object["bind"]
	if _, ok := members["expect"]; ok {
		step.Expect, step.HasExpect = object["expect"], true
	}
	if e, ok := object["expect_error"].(map[string]any); ok {
		step.ExpectError = e
	}
	if a, ok := object["assert"].(map[string]any); ok {
		step.Assert = a
	}
	if r, ok := object["repeat"].(map[string]any); ok {
		max, _ := r["max"].(json.Number).Int64()
		until, _ := r["until"].(string)
		step.Repeat = &Repeat{Max: int(max), Until: until}
	}
	step.Note, _ = object["note"].(string)
	return step, nil
}

// tableRows reads a table's rows, keeping those whose members equal where.
func tableRows(file string, where map[string]any) ([]map[string]any, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	decoded, err := decode(data)
	if err != nil {
		return nil, err
	}
	table, _ := decoded.(map[string]any)
	rows, _ := table["rows"].([]any)
	if rows == nil {
		return nil, fmt.Errorf("%s holds no rows", file)
	}
	var out []map[string]any
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: a row is not an object", file)
		}
		keep := true
		for key, want := range where {
			if !equal(want, row[key]) {
				keep = false
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	return out, nil
}
