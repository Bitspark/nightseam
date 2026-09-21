package typescript

import (
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"testing"
)

func TestRecordedDeclarationsAreReserved(t *testing.T) {
	for _, name := range []string{"RecordedEvent", "Recorder", "WireLog", "RecordOptions", "RecordedWire"} {
		diagnostics := check(map[string]string{"model.json": `{"nightseam":2,"types":{"` + name + `":{"kind":"alias","type":"string"}}}`, "protocol.json": modeltest.Protocol(`"server":{"events":{"changed":{"type":"string"}}}`)})
		if !has(diagnostics, "reserved_name", "model.json#/types/"+name) {
			t.Fatalf("recorded declaration %s accepted: %v", name, diagnostics)
		}
	}
}
