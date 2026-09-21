package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

var updateDeclarations = flag.Bool("update-declarations", false, "rewrite the canonical declaration byte fixtures")

type declarationFixture struct {
	Name              string                              `json:"name"`
	Family            string                              `json:"family,omitempty"`
	Expression        string                              `json:"expression,omitempty"`
	WireExpression    json.RawMessage                     `json:"wireExpression,omitempty"`
	Declaration       string                              `json:"declaration"`
	Digest            string                              `json:"digest"`
	SameAs            string                              `json:"sameAs,omitempty"`
	DifferentFrom     string                              `json:"differentFrom,omitempty"`
	Sources           map[string]map[string]string        `json:"sources,omitempty"`
	Wire              string                              `json:"wire,omitempty"`
	FamilyDeclaration string                              `json:"familyDeclaration,omitempty"`
	Families          map[string]declarationSchemaFixture `json:"families,omitempty"`
	Application       *struct {
		Template  string   `json:"template"`
		Arguments []string `json:"arguments"`
	} `json:"application,omitempty"`
	Composite map[string]any `json:"composite,omitempty"`
}

type declarationSchemaFixture struct {
	Wire        string   `json:"wire"`
	Declaration string   `json:"declaration"`
	Digest      string   `json:"digest"`
	Imports     []string `json:"imports"`
}

func TestCanonicalDeclarationCoverage(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/declaration-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Description string               `json:"description"`
		Cases       []declarationFixture `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) < 20 {
		t.Fatal("declaration coverage table is incomplete")
	}
	seen := map[string]string{}
	graphs := map[string]string{}
	for i := range table.Cases {
		row := &table.Cases[i]
		t.Run(row.Name, func(t *testing.T) {
			var got string
			if row.Composite != nil {
				var bind func(any) any
				bind = func(value any) any {
					switch x := value.(type) {
					case map[string]any:
						if name, ok := x["case"].(string); ok {
							var graph map[string]any
							if err := json.Unmarshal([]byte(graphs[name]), &graph); err != nil {
								t.Fatal(err)
							}
							return map[string]any{"graph": graph}
						}
						out := map[string]any{}
						for key, child := range x {
							out[key] = bind(child)
						}
						return out
					case []any:
						out := []any{}
						for _, child := range x {
							out = append(out, bind(child))
						}
						return out
					default:
						return value
					}
				}
				got = newDeclarationGraph().bytes(bind(row.Composite))
			} else if row.Application != nil {
				args := []string{}
				for _, name := range row.Application.Arguments {
					args = append(args, graphs[name])
				}
				got, err = CanonicalApplication(graphs[row.Application.Template], args)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				world := analysis.World(modeltest.World(row.Sources))
				// modeltest deliberately only decodes the lower tiers; this suite
				// also exercises the live operations of a complete declaration.
				for name, files := range row.Sources {
					if source := files["live.json"]; source != "" {
						world[name].Live, err = model.DecodeLive("live.json", []byte(source))
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				family := analysis.Resolve(world, row.Family)
				families := map[string]declarationSchemaFixture{}
				for name := range world {
					f := analysis.Resolve(world, name)
					declaration := CanonicalDeclaration(f)
					sum := sha256.Sum256([]byte(declaration))
					families[name] = declarationSchemaFixture{Wire: wire(f), Declaration: declaration, Digest: hex.EncodeToString(sum[:]), Imports: append([]string{}, f.Imports...)}
				}
				if *updateDeclarations {
					row.Wire = families[row.Family].Wire
					row.FamilyDeclaration = families[row.Family].Declaration
					row.Families = families
				}
				want, _ := json.Marshal(row.Families)
				actual, _ := json.Marshal(families)
				if string(want) != string(actual) || row.Wire != families[row.Family].Wire || row.FamilyDeclaration != families[row.Family].Declaration {
					t.Fatal("shared validator/declaration source artifacts are stale")
				}
				if row.Expression == "" {
					got = CanonicalDeclaration(family)
				} else {
					expr, err := model.Decode([]byte(row.Expression))
					if err != nil {
						t.Fatal(err)
					}
					wireExpression, _ := json.Marshal(expr)
					if *updateDeclarations {
						row.WireExpression = wireExpression
					}
					var compact bytes.Buffer
					if err := json.Compact(&compact, row.WireExpression); err != nil {
						t.Fatal(err)
					}
					if compact.String() != string(wireExpression) {
						t.Fatal("shared wire expression is stale")
					}
					got = CanonicalExpression(family, expr)
				}
			}
			if *updateDeclarations {
				row.Declaration = got
			}
			if got != row.Declaration {
				t.Fatalf("canonical bytes differ\ngot  %s\nwant %s", got, row.Declaration)
			}
			sum := sha256.Sum256([]byte(got))
			digest := hex.EncodeToString(sum[:])
			if *updateDeclarations {
				row.Digest = digest
			}
			if digest != row.Digest {
				t.Fatalf("digest = %s, want %s", digest, row.Digest)
			}
			if row.SameAs != "" && (seen[row.SameAs] == "" || digest != seen[row.SameAs]) {
				t.Errorf("must equal %s", row.SameAs)
			}
			if row.DifferentFrom != "" && (seen[row.DifferentFrom] == "" || digest == seen[row.DifferentFrom]) {
				t.Errorf("must differ from %s", row.DifferentFrom)
			}
			seen[row.Name] = digest
			graphs[row.Name] = got
		})
	}
	if *updateDeclarations && !t.Failed() {
		data, err := json.MarshalIndent(table, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("../../conformance/tables/declaration-digests.json", append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFamilyDigestIdentifiesDeclarationInsteadOfValidatorPresentation(t *testing.T) {
	build := func(result string) *Family {
		world := analysis.World(modeltest.World(map[string]map[string]string{"same": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"result":"` + result + `"}}}}`,
		}}))
		return Build(analysis.Resolve(world, "same"))
	}
	a, b := build("string"), build("integer")
	if a.Wire != b.Wire {
		t.Fatal("the regression must preserve the validator descriptor")
	}
	if a.Declaration == b.Declaration || a.WireDigest == b.WireDigest {
		t.Fatal("operation revision kept declaration identity")
	}
	sum := sha256.Sum256([]byte(a.Declaration))
	if a.WireDigest != hex.EncodeToString(sum[:]) {
		t.Fatal("family digest does not hash declaration bytes")
	}
}

func TestCanonicalApplicationKeepsTransitiveCallableCaptures(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{"same": {
		"model.json":    `{"nightseam":2}`,
		"protocol.json": `{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}]}`,
		"live.json":     `{"types":{"Callback":{"kind":"callable","result":"T"},"Holder":{"kind":"record","fields":[{"name":"callback","type":"Callback"}]}}}`,
	}}))
	family := analysis.Resolve(world, "same")
	first := CanonicalExpression(family, model.Apply{Name: "Holder", With: map[string]model.Filler{"T": {Type: model.Primitive("integer")}}})
	second := CanonicalExpression(family, model.Apply{Name: "Holder", With: map[string]model.Filler{"T": {Type: model.Primitive("string")}}})
	if first == second {
		t.Fatal("a transitive callable family capture disappeared from the application")
	}
}
