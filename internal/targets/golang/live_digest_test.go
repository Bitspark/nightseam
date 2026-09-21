package golang

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
	files, err := New(Config{Module: "example.test/m"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, file := range files {
		source.Write(file.Data)
	}
	for _, want := range []string{
		"owner.Export(ContractCallback, WireDigest(),",
		"owner.Import(reference, ContractCallback, WireDigest())",
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
	files, err := New(Config{Module: "example.test/m"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "binding_generated.go") && !strings.HasSuffix(file.Path, "client_generated.go") {
			continue
		}
		source := string(file.Data)
		if got := strings.Count(source, `public.Code == "contract_mismatch"`); got != 3 {
			t.Errorf("%s preserves %d validation/import/decode refusals, want 3", file.Path, got)
		}
		if got := strings.Count(source, `Code: "invalid_params"`); got != 3 {
			t.Errorf("%s keeps %d ordinary invalid-parameter paths, want 3", file.Path, got)
		}
	}
}
