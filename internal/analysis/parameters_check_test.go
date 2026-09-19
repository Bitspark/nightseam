package analysis_test

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestImportedApplicationsRequireOnlyTheFamilyParametersTheyUse(t *testing.T) {
	for _, test := range []struct {
		name, with, code string
	}{
		{"missing", `{}`, "unbound_parameter"},
		{"concrete", `{"Item":{"array":{"nullable":"string"}}}`, ""},
		{"unused", `{"Item":"string","Other":"integer"}`, "unresolved_parameter"},
	} {
		t.Run(test.name, func(t *testing.T) {
			world := analysis.World(modeltest.World(map[string]map[string]string{
				"boxes":    {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"},{"name":"Other"}],"types":{"Inner":{"kind":"record","fields":[{"name":"value","type":"Item"}]},"Box":{"kind":"record","fields":[{"name":"child","type":{"kind":"record","fields":[{"name":"inner","type":"Inner"}]}}]},"OtherBox":{"kind":"alias","type":"Other"}}`)},
				"consumer": {"model.json": `{"nightseam":2,"imports":["boxes"]}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"read":{"result":{"apply":"boxes.Box","with":` + test.with + `}}}}`)},
			}))
			if diagnostics := check.Family(analysis.Resolve(world, "boxes")); len(diagnostics) != 0 {
				t.Fatalf("generic declaration is invalid: %v", diagnostics)
			}
			diagnostics := check.Family(analysis.Resolve(world, "consumer"))
			if test.code == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("concrete application was refused: %v", diagnostics)
				}
			} else if len(diagnostics) != 1 || diagnostics[0].Code != test.code {
				t.Fatalf("diagnostics = %v, want %s", diagnostics, test.code)
			}
		})
	}
}
