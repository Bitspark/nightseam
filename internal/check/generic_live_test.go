package check

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestGenericContainersAdmitLiveArguments(t *testing.T) {
	f := world("x", map[string]string{
		"model.json":    `{"nightseam":2,"types":{"Page":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"array":"T"}}]}}}`,
		"protocol.json": modeltest.Protocol(``),
		"live.json":     `{"types":{"Cancel":{"kind":"callable"},"Jobs":{"kind":"alias","type":{"apply":"Page","with":{"T":"Cancel"}}}},"server":{"methods":{"jobs":{"result":"Jobs"}}}}`,
	})
	if got := Family(f); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestGenericCallablesRemainRefused(t *testing.T) {
	f := world("x", map[string]string{
		"model.json":    `{"nightseam":2,"types":{}}`,
		"protocol.json": modeltest.Protocol(``),
		"live.json":     `{"types":{"Handler":{"kind":"callable","parameters":[{"name":"T"}],"request":"T"}}}`,
	})
	if got := codes(Family(f)); !strings.Contains(got, "callable_parameters") {
		t.Fatalf("generic callable was admitted: %s", got)
	}
}
