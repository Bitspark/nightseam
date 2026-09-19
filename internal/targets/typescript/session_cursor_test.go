package typescript

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestSequenceIsReservedOnlyOnSessionClients(t *testing.T) {
	files := map[string]string{
		"model.json":    `{"nightseam":2}`,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"sequence":{"result":"integer"}}}`),
	}
	if diagnostics := check(files); len(diagnostics) != 0 {
		t.Fatalf("protocol-only sequence method refused: %v", diagnostics)
	}
	files["session.json"] = `{}`
	if diagnostics := check(files); !has(diagnostics, "reserved_name", "protocol.json#/server/methods/sequence") {
		t.Fatalf("session cursor getter collision accepted: %v", diagnostics)
	}
}
