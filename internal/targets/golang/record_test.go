package golang

import (
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"testing"
)

func TestRecordedEventNamesRespectTheirEntryPackage(t *testing.T) {
	cases := []struct{ protocol, at, code string }{
		{`"server":{"events":{"event":{"type":"string"}}}`, "protocol.json#/server/events/event", "reserved_name"},
		{`"parameters":[{"name":"RecordedChanged"}],"server":{"events":{"changed":{"type":"RecordedChanged"}}}`, "protocol.json#/parameters/0/name", "generated_name_collision"},
	}
	for _, test := range cases {
		diagnostics := check(map[string]string{"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(test.protocol)})
		if !has(diagnostics, test.code, test.at) {
			t.Fatalf("expected recorded-name collision at %s: %v", test.at, diagnostics)
		}
	}
	diagnostics := check(map[string]string{"model.json": `{"nightseam":2,"types":{"RecordedChanged":{"kind":"alias","type":"string"}}}`, "protocol.json": modeltest.Protocol(`"server":{"events":{"changed":{"type":"RecordedChanged"}}}`)})
	if len(diagnostics) != 0 {
		t.Fatalf("a separate protocol package type collided: %v", diagnostics)
	}
}
