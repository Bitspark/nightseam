package load

import (
	"fmt"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/builtin"
)

func TestFamilyRefusesBuiltinNamesAtDeclaration(t *testing.T) {
	for _, name := range builtin.Names() {
		t.Run(name, func(t *testing.T) {
			dir := path.Join("declarations", name)
			fsys := fstest.MapFS{dir + "/model.json": {Data: []byte(probeModel)}}
			family, problems := Family(fsys, dir, name, nil)
			if family == nil || family.Types["Payload"] == nil {
				t.Fatal("the refused family must still be available to list")
			}
			if len(problems) != 1 || problems[0].Code != "builtin_name" || problems[0].Family != name || problems[0].At() != (diag.Location{File: model.ModelFile}) {
				t.Fatalf("want builtin_name at the family's model file, got %v", problems)
			}
			for _, text := range []string{name, builtin.Locate(name, model.ModelFile), dir, "rename", "imports", "qualified references"} {
				if !strings.Contains(problems[0].Message, text) {
					t.Errorf("diagnostic does not explain %q: %s", text, problems[0])
				}
			}
		})
	}
}

// Imports retain their source tier and index, even when the combined list
// sorts differently or the same name is imported from more than one tier.
func TestBuiltinImportsLocateEachDeclarationAndCheckoutCollision(t *testing.T) {
	for _, name := range builtin.Names() {
		for _, collision := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collision=%t", name, collision), func(t *testing.T) {
				fsys := fstest.MapFS{
					"declarations/consumer/model.json":    {Data: []byte(fmt.Sprintf(`{"nightseam":2,"imports":["zebra","alpha",%q]}`, name))},
					"declarations/consumer/protocol.json": {Data: []byte(fmt.Sprintf(`{"profile":"nightseam.duplex/1","imports":[%q]}`, name))},
				}
				dir := path.Join("declarations", name)
				if collision {
					fsys[dir+"/model.json"] = &fstest.MapFile{Data: []byte(probeModel)}
				}
				_, problems := Checkout(fsys, "declarations", nil)
				want := map[diag.Location]bool{
					{File: model.ModelFile, Pointer: "/imports/2"}:    true,
					{File: model.ProtocolFile, Pointer: "/imports/0"}: true,
				}
				for _, problem := range problems {
					if problem.Family != "consumer" {
						continue
					}
					if problem.Code != "implicit_import" || !want[problem.At()] {
						t.Fatalf("unexpected importer diagnostic: %s", problem)
					}
					delete(want, problem.At())
					if collision {
						for _, text := range []string{dir + "/model.json", "built-in", "rename", "imports", "qualified references"} {
							if !strings.Contains(problem.Message, text) {
								t.Errorf("importer diagnostic does not explain %q: %s", text, problem)
							}
						}
					} else if strings.Contains(problem.Message, dir) {
						t.Errorf("diagnostic names a checkout family that does not exist: %s", problem)
					}
				}
				if len(want) != 0 {
					t.Fatalf("missing importer diagnostics at %v; got %v", want, problems)
				}
			})
		}
	}
}
