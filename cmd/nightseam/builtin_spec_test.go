package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/targets/spec"
)

// The references are the built-in declarations rendered by the same spec
// target as a consumer's family. The regular golden update command rewrites
// them, and the fast test tier rejects documents that no longer match.
func TestBuiltinSpecificationsGolden(t *testing.T) {
	root := repositoryRoot(t)
	world := analysis.World(builtin.Families())
	target := spec.New(spec.Config{Layout: "docs/declaration/builtins/{family}"})
	for _, name := range builtin.Names() {
		t.Run(name, func(t *testing.T) {
			family := analysis.Resolve(world, name)
			if diagnostics := check.Family(family); len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			files, err := target.Render(render.Build(family))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				path := filepath.Join(root, filepath.FromSlash(file.Path))
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, file.Data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, file.Data) {
					t.Errorf("%s is stale; regenerate with go test ./cmd/nightseam -short -run TestBuiltinSpecificationsGolden -update", file.Path)
				}
			}
		})
	}
}
