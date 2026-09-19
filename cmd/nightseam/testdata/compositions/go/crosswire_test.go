package composition_test

// The composition across the wire: a Go worker behind a real WebSocket and a
// Node client of it, supplying a sink the Go side calls back and resolving a
// job the Go side served. It is one program in each language over one socket,
// which is what parity means here — not two implementations that agree in
// prose.

import (
	"os/exec"
	"testing"
)

func TestAcrossTheWire(t *testing.T) {
	ctx := testContext(t)
	if _, err := exec.LookPath("node"); err != nil {
		// The fixture that laid this file down already checked for Node; a
		// missing one is a failure and not a skip, since a skip nobody reads
		// is a gate nobody passes.
		t.Fatalf("the composition needs node on the path: %v", err)
	}
	workers := serveWorkers(t)
	command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "compositions.ts", workers.url())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the Node client of the Go worker failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
