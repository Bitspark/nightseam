// Package kernel drives generation: it reads a checkout's families, gives
// each its world, holds it to the model's and every concern's rules, and
// — once every target is composed with it — asks each target to check and
// render what it consumes. It names no target and no tier file; the tiers
// are the loader's table and the concerns are check's.
package kernel

import (
	"io/fs"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/load"
)

// World is every family of a checkout as loaded, with what the loader had
// to say about each.
type World struct {
	Families analysis.World
	Names    []string
	Problems map[string][]diag.Diagnostic // the loader's diagnostics by family; "" for the checkout's own
}

// Load reads the checkout under a filesystem. The targets name the
// override files a family may carry.
func Load(fsys fs.FS, contracts string, targets []string) *World {
	loaded, problems := load.Checkout(fsys, contracts, targets)
	world := &World{Families: analysis.World{}, Names: loaded.Names, Problems: map[string][]diag.Diagnostic{}}
	for name, family := range loaded.Families {
		world.Families[name] = family
	}
	for _, problem := range problems {
		world.Problems[problem.Family] = append(world.Problems[problem.Family], problem)
	}
	return world
}

// Validate reports every diagnostic of one family: the loader's, or else
// the model's and every concern's the family has, sorted. A family the
// loader could not read at all is reported as it was.
func Validate(world *World, name string) []diag.Diagnostic {
	if problems := world.Problems[name]; len(problems) != 0 {
		return problems
	}
	f := analysis.Resolve(world.Families, name)
	if f == nil {
		return []diag.Diagnostic{{Family: name, Code: "unknown_family", Message: "No family of that name is in the checkout."}}
	}
	return check.Family(f)
}

// Resolve gives a family of the world its facts, for a target's check or a
// rendering; it assumes Validate found nothing.
func Resolve(world *World, name string) *analysis.Family {
	return analysis.Resolve(world.Families, name)
}
