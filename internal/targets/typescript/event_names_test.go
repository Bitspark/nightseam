package typescript

import (
	"fmt"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestEventFieldsRejectObjectMembers(t *testing.T) {
	for _, name := range []string{
		"constructor", "__defineGetter__", "__defineSetter__", "hasOwnProperty",
		"__lookupGetter__", "__lookupSetter__", "isPrototypeOf", "propertyIsEnumerable",
		"toString", "valueOf", "__proto__", "toLocaleString",
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := check(map[string]string{
				"model.json":      fixtureModel,
				"protocol.json":   fixtureProtocol(""),
				"typescript.json": fmt.Sprintf(`{"names":{"changed":%q}}`, name),
			})
			if !has(diagnostics, "reserved_name", "typescript.json#/names/changed") {
				t.Fatalf("an Events field inherited from Object was accepted: %v", diagnostics)
			}
		})
	}
}

func TestEventFieldCollisionCanBeOverridden(t *testing.T) {
	files := map[string]string{
		"model.json": fixtureModel,
		"protocol.json": modeltest.Protocol(`"server":{"events":{"to_string":{"type":"Result"}}},
			"client":{"events":{"has_own_property":{"type":"Result"}}}`),
	}
	if diagnostics := check(files); !has(diagnostics, "reserved_name", "protocol.json#/server/events/to_string") {
		t.Fatalf("a conventional Events field inherited from Object was accepted: %v", diagnostics)
	}
	files["typescript.json"] = `{"names":{"to_string":"textChanged"}}`
	if diagnostics := check(files); len(diagnostics) != 0 {
		t.Fatalf("a safe event override or an unrelated event emitter was refused: %v", diagnostics)
	}
}
