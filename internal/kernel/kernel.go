// Package kernel drives generation: it reads a checkout's families, gives
// each its world, holds it to the model's and every concern's rules, and
// — once every target is composed with it — asks each target to check and
// render what it consumes. It names no target and no tier file; the tiers
// are the loader's table and the concerns are check's.
package kernel

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
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

// Kernel is the pipeline composed with its targets.
type Kernel struct {
	targets []spi.Target
}

// New composes the kernel with the targets it renders with, in the order
// they render.
func New(targets ...spi.Target) *Kernel { return &Kernel{targets: targets} }

// Targets names the targets, for the loader's override files.
func (k *Kernel) Targets() []string {
	names := make([]string, len(k.targets))
	for i, target := range k.targets {
		names[i] = target.Name()
	}
	return names
}

// Load reads the checkout for this kernel's targets.
func (k *Kernel) Load(fsys fs.FS, contracts string) *World { return Load(fsys, contracts, k.Targets()) }

// Validate reports every diagnostic of one family: the loader's, or else
// the model's and the concerns', or else every target's.
func (k *Kernel) Validate(world *World, name string) []diag.Diagnostic {
	if diagnostics := Validate(world, name); len(diagnostics) != 0 {
		return diagnostics
	}
	f := render.Build(Resolve(world, name))
	var diagnostics []diag.Diagnostic
	for _, target := range k.targets {
		if k.consumes(target, f) {
			diagnostics = append(diagnostics, target.Check(f)...)
		}
	}
	diag.Sort(diagnostics)
	return diagnostics
}

// Result is one family rendered by every target: its files by path.
type Result struct {
	Files          map[string][]byte
	Recipe         string
	RuntimeVersion string
}

// Render renders one family with every target that consumes what it
// declares. A family with any diagnostic is refused before any target
// renders; a rendered path outside what its target owns, or rendered
// twice, is refused after.
func (k *Kernel) Render(world *World, name string) (Result, error) {
	if diagnostics := k.Validate(world, name); len(diagnostics) != 0 {
		return Result{}, fmt.Errorf("invalid family: %s", diagnostics[0])
	}
	f := render.Build(Resolve(world, name))
	files := map[string][]byte{}
	for _, target := range k.targets {
		if !k.consumes(target, f) {
			continue
		}
		rendered, err := target.Render(f)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", target.Name(), err)
		}
		for _, file := range rendered {
			if !safe(file.Path) {
				return Result{}, fmt.Errorf("%s: invalid output path %q", target.Name(), file.Path)
			}
			if !owned(file.Path, target.Owns(name)) {
				return Result{}, fmt.Errorf("%s: %s lies outside the directories the target owns for %s: %s", target.Name(), file.Path, name, strings.Join(target.Owns(name), ", "))
			}
			if _, exists := files[file.Path]; exists {
				return Result{}, fmt.Errorf("%s: conflicting generated output %s", target.Name(), file.Path)
			}
			files[file.Path] = file.Data
		}
	}
	return Result{Files: files, Recipe: spi.Recipe, RuntimeVersion: spi.RuntimeVersion}, nil
}

// consumes reports whether a target has anything to render for a family:
// the family has a tier the target consumes.
func (k *Kernel) consumes(target spi.Target, f *render.Family) bool {
	for _, concern := range target.Consumes() {
		for _, file := range f.Files {
			if tier, ok := model.TierOf(file); ok && tier.Name == concern {
				return true
			}
		}
	}
	return false
}

// safe accepts a slash-separated path relative to the checkout that cannot
// escape it.
func safe(p string) bool {
	if p == "" || p == "." || p == ".." || strings.Contains(p, "\\") || strings.Contains(p, ":") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func owned(p string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}
