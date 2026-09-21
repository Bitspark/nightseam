package typescript

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestCallableHelpersUseTheFamilyWireDigest(t *testing.T) {
	f := family(map[string]string{
		"model.json":    `{"nightseam":2}`,
		"protocol.json": modeltest.Protocol(""),
		"live.json":     `{"types":{"Callback":{"kind":"callable","request":"integer","result":"integer"}}}`,
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, file := range files {
		source.Write(file.Data)
	}
	for _, want := range []string{
		"owner.export(contractCallback, wireDigest,",
		"owner.import(owner.scope.decode(raw), contractCallback, wireDigest)",
	} {
		if !strings.Contains(source.String(), want) {
			t.Errorf("callable boundary does not contain %q", want)
		}
	}
}

func TestIncomingValidationPreservesContractMismatch(t *testing.T) {
	f := family(map[string]string{
		"model.json":    `{"nightseam":2}`,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"accept":{"request":"Callback","result":"integer"},"data":{"request":"integer","result":"integer"}}},"client":{"methods":{"reverse":{"request":"Callback","result":"integer"},"reverse_data":{"request":"integer","result":"integer"}}}`),
		"live.json":     `{"types":{"Callback":{"kind":"callable","request":"integer","result":"integer"}}}`,
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/src/index.ts") {
			continue
		}
		source := string(file.Data)
		if got := strings.Count(source, "if (error instanceof DuplexError && error.code === 'contract_mismatch') throw error;"); got != 2 {
			t.Errorf("%s preserves %d incoming refusals, want 2", file.Path, got)
		}
		if got := strings.Count(source, "throw new DuplexError('invalid_params', String(error));"); got != 2 {
			t.Errorf("%s keeps %d ordinary invalid-parameter paths, want 2", file.Path, got)
		}
	}
}
