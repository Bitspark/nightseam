package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/load"
)

// This shared corpus combines both #363 forms. Its callable aliases retain
// the original Function constructor and ordered arguments; OtherFunction is
// the negative nominal control, despite having the same native signature.
func TestGenericCompositionCorpus(t *testing.T) {
	k, _ := toolKernel(load.Config{}, module, scope, "")
	root := filepath.Join(repositoryRoot(t), "conformance", "corpora", "generic-composition")
	world := k.Load(os.DirFS(root), "api/contracts")
	for _, name := range []string{"functions", "numbers", "texts", "holder", "compose-cell"} {
		t.Run(name, func(t *testing.T) {
			if _, err := k.Render(world, name); err != nil {
				t.Fatalf("required generic composition declaration: %v", err)
			}
		})
	}
}
