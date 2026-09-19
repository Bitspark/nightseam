package kernel

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/model/builtin"
)

func TestBuiltinFamilyNameRequiresCheckoutRename(t *testing.T) {
	for _, name := range builtin.Names() {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"api/contracts/" + name + "/model.json": {Data: []byte(`{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`)},
			}
			world := Load(fsys, "api/contracts", nil)
			if problems := Validate(world, name); len(problems) != 1 || problems[0].Code != "builtin_name" {
				t.Fatalf("standalone checkout family must be refused at its declaration: %v", problems)
			}
			fsys["api/contracts/consumer/model.json"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(`{"nightseam":2,"imports":[%q],"types":{"Held":{"kind":"alias","type":%q}}}`, name, name+".Payload"))}
			world = Load(fsys, "api/contracts", nil)
			if problems := Validate(world, "consumer"); len(problems) != 1 || problems[0].Code != "implicit_import" {
				t.Fatalf("the importer must also explain the collision: %v", problems)
			}

			// Follow the diagnostic: rename the directory and every reference.
			fsys["api/contracts/agent/model.json"] = fsys["api/contracts/"+name+"/model.json"]
			delete(fsys, "api/contracts/"+name+"/model.json")
			fsys["api/contracts/consumer/model.json"].Data = []byte(`{"nightseam":2,"imports":["agent"],"types":{"Held":{"kind":"alias","type":"agent.Payload"}}}`)
			fsys["api/contracts/consumer/protocol.json"] = &fstest.MapFile{Data: []byte(`{"profile":"nightseam.duplex/1","types":{"Channel":{"kind":"alias","type":"duplex.Handle"}}}`)}
			world = Load(fsys, "api/contracts", nil)
			for _, family := range world.Names {
				if problems := Validate(world, family); len(problems) != 0 {
					t.Errorf("renamed checkout and implicit built-in must validate: %v", problems)
				}
			}
		})
	}
}
