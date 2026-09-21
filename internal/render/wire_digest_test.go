package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestValidatorDescriptorBytesRemainStable(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct{ Name, Declaration, Wire, Digest string }
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("digest table has no cases")
	}
	for _, row := range table.Cases {
		t.Run(row.Name, func(t *testing.T) {
			source := row.Declaration
			if source == "" {
				source = `{"nightseam":2,` + row.Wire[1:]
			}
			world := analysis.World(modeltest.World(map[string]map[string]string{
				"same": {"model.json": source},
			}))
			first, second := Build(analysis.Resolve(world, "same")), Build(analysis.Resolve(world, "same"))
			if first.Wire != row.Wire {
				t.Fatalf("descriptor changed: got %s, want %s", first.Wire, row.Wire)
			}
			descriptorHash := sha256.Sum256([]byte(first.Wire))
			if hex.EncodeToString(descriptorHash[:]) != row.Digest {
				t.Fatal("validator descriptor byte fixture changed")
			}
			if first.Declaration != second.Declaration || first.WireDigest != second.WireDigest {
				t.Fatal("declaration identity is not deterministic")
			}
		})
	}
}

func TestWireDigestIgnoresDocumentationAndDetectsOptionalMembers(t *testing.T) {
	build := func(source string) *Family {
		world := analysis.World(modeltest.World(map[string]map[string]string{"same": {"model.json": source}}))
		return Build(analysis.Resolve(world, "same"))
	}
	first := build(`{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`)
	documented := build(`{
		"types": {"Payload": {"description":"An explanation outside the wire descriptor.","fields":[{"type":"string","name":"text","description":"A comment."}],"kind":"record"}},
		"nightseam": 2
	}`)
	optional := build(`{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"text","type":"string"},{"name":"hint","type":"string","required":false}]}}}`)
	if first.Wire != documented.Wire || first.WireDigest != documented.WireDigest {
		t.Fatal("source formatting or documentation changed the rendered descriptor's digest")
	}
	if first.Wire == optional.Wire || first.WireDigest == optional.WireDigest {
		t.Fatal("an optional member added to the same family's declaration kept its digest")
	}
}
