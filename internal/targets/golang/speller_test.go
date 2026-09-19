package golang

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/spi"
)

// A signature can use a family type parameter directly, without wrapping
// it in a named record. The scaffold and document must retain that scope.
func TestSpellerDirectFamilyParameterHandler(t *testing.T) {
	f := family(map[string]string{
		"model.json":    `{"nightseam":2,"types":{}}`,
		"protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"echo":{"request":"T","result":"T"}}}`),
	})
	target := New(Config{Module: "example.test/m"})
	invocation := target.(spi.Speller).Invoke(f, "server", "echo")
	if !strings.Contains(invocation.Handle, "params T) (T, error)") {
		t.Fatalf("direct parameter lost in handler: %s", invocation.Handle)
	}
}
