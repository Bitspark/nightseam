package conformance

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPublishedBitwireConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("the published contract drivers run Go and TypeScript")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "scripts/bitwire-conformance.mjs")
	command.Dir = filepath.Join("..", "..")
	output, err := runBuildCommand(ctx, command)
	if err != nil {
		t.Fatalf("published Bitwire conformance: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestDeclaredBitwireConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("the declared composition drivers run Go and TypeScript")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "scripts/declared-conformance.mjs")
	command.Dir = filepath.Join("..", "..")
	output, err := runBuildCommand(ctx, command)
	if err != nil {
		t.Fatalf("declared Bitwire conformance: %v\n%s", err, output)
	}
	t.Log(string(output))
}
