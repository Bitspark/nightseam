package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserverPollingPreservesEvents(t *testing.T) {
	cases := []struct {
		name, step string
		valid      bool
	}{
		{"default drain", `{"on":"a","op":"peer.observed","args":{"on":"p"},"repeat":{"max":40,"until":"match"}}`, false},
		{"explicit drain", `{"on":"a","op":"peer.observed","args":{"on":"p","drain":true},"repeat":{"max":40,"until":"match"}}`, false},
		{"preserved history", `{"on":"a","op":"peer.observed","args":{"on":"p","drain":false},"repeat":{"max":40,"until":"match"}}`, true},
		{"one-shot drain", `{"on":"a","op":"peer.observed","args":{"on":"p"}}`, true},
		{"other operation", `{"on":"a","op":"peer.await_event","args":{"on":"p"},"repeat":{"max":40,"until":"match"}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseStep(json.RawMessage(tc.step))
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "drain: false")) {
				t.Fatalf("observer polling must preserve its evidence, got %v", err)
			}
		})
	}
}

// TestScenariosLoad: every scenario under conformance/scenarios fits the
// schema, lies under its layer, names a distinct scenario, and every foreach
// selects rows. Fast tier: no testee runs.
func TestScenariosLoad(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("no scenarios")
	}
	profiles, err := LoadProfiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Cover(scenarios); err != nil {
		t.Error(err)
	}
	byLayer := map[string]int{}
	for _, s := range scenarios {
		byLayer[s.Layer]++
		for i, step := range s.Steps {
			if step.On != "runner" && !strings.HasPrefix(step.Op, s.Layer+".") && !allowedAcross(s.Layer, step.Op) {
				t.Errorf("%s step %d: %s is not an op of layer %s or one beneath it", s.Name, i, step.Op, s.Layer)
			}
		}
	}
	t.Logf("%d scenarios: %v", len(scenarios), byLayer)
}

// allowedAcross says which ops a scenario of one layer may use from
// another: everything beneath it, and the seam's connections everywhere.
func allowedAcross(layer, op string) bool {
	family, _, _ := strings.Cut(op, ".")
	beneath := map[string][]string{
		"seam":    {"conn"},
		"peer":    {"conn", "peer", "call"},
		"tunnel":  {"conn", "peer", "call", "tunnel"},
		"session": {"conn", "peer", "call", "tunnel", "session", "attachment"},
		// The live layer runs over a peer and not over a tunnel: a binding is
		// an ordinary request of the profile, not a channel of its own.
		"live":      {"conn", "peer", "call", "live"},
		"generated": {"gen", "client", "server"},
	}
	if family == "pair" {
		return true
	}
	for _, f := range beneath[layer] {
		if f == family {
			return true
		}
	}
	return false
}

// TestTablesAreWellFormed: every table under conformance/tables has a
// description and rows, and the frames table's rows are each addressed and
// judged.
func TestTablesAreWellFormed(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "tables"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(root, "tables", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var table struct {
			Description string            `json:"description"`
			Rows        []json.RawMessage `json:"rows"`
			Cases       []json.RawMessage `json:"cases"`
		}
		if err := json.Unmarshal(data, &table); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if table.Description == "" || (len(table.Rows) == 0 && len(table.Cases) == 0) {
			t.Errorf("%s: a table describes itself and holds rows", entry.Name())
		}
	}
	rows, err := tableRows(filepath.Join(root, "tables", "frames.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		name, _ := row["name"].(string)
		to, _ := row["to"].(string)
		frame, _ := row["frame"].(string)
		if _, judged := row["valid"].(bool); name == "" || frame == "" || !judged || (to != "server" && to != "client" && to != "either") {
			t.Errorf("frames.json: row %q is not named, addressed and judged", name)
		}
		if seen[name] {
			t.Errorf("frames.json: two rows named %q", name)
		}
		seen[name] = true
	}
	invalid, err := tableRows(filepath.Join(root, "tables", "frames.json"), map[string]any{"valid": false})
	if err != nil || len(invalid) == 0 || len(invalid) == len(rows) {
		t.Fatalf("where did not select: %d of %d, %v", len(invalid), len(rows), err)
	}
}

func TestRecipesAreFound(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := Recipes(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := recipes["go"]; !ok {
		t.Fatal("the Go testee's recipe is missing; it is the reference")
	}
	for language, recipe := range recipes {
		if recipe.Language != language || recipe.Dir == "" {
			t.Errorf("%s: recipe is not its own", language)
		}
	}
}
