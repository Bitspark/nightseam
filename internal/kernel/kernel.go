// Package kernel drives generation: it reads a checkout's families, gives
// each its world, holds it to the model's and every concern's rules, and
// — once every target is composed with it — asks each target to check and
// render what it consumes. It names no target and no tier file; the tiers
// are the loader's table and the concerns are check's.
package kernel

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/builtin"
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
	var diagnostics []diag.Diagnostic
	for _, f := range renderingFamilies(render.Build(Resolve(world, name))) {
		for _, target := range k.targets {
			if k.consumes(target, f) {
				diagnostics = append(diagnostics, target.Check(f)...)
			}
		}
	}
	diag.Sort(diagnostics)
	return diagnostics
}

// Result is one family and its built-in dependencies rendered by every
// target: their files by path.
type Result struct {
	Files map[string][]byte
}

// Render renders one family and its built-in dependencies with every target
// that consumes what they declare. A family with any diagnostic is refused before any target
// renders; a rendered path outside what its target owns, or rendered
// twice, is refused after.
func (k *Kernel) Render(world *World, name string) (Result, error) {
	if diagnostics := k.Validate(world, name); len(diagnostics) != 0 {
		return Result{}, fmt.Errorf("invalid family: %s", diagnostics[0])
	}
	files := map[string][]byte{}
	for _, f := range renderingFamilies(render.Build(Resolve(world, name))) {
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
				if !owned(file.Path, target.Owns(f.Name)) {
					return Result{}, fmt.Errorf("%s: %s lies outside the directories the target owns for %s: %s", target.Name(), file.Path, f.Name, strings.Join(target.Owns(f.Name), ", "))
				}
				if _, exists := files[file.Path]; exists {
					return Result{}, fmt.Errorf("%s: conflicting generated output %s", target.Name(), file.Path)
				}
				files[file.Path] = file.Data
			}
		}
	}
	return Result{Files: files}, nil
}

// A built-in is a generated dependency with the same target contract as a
// checkout family. Render it once beside the family that needs it; carried
// types remain in that family's own package and create no dependency.
func renderingFamilies(root *render.Family) []*render.Family {
	var families []*render.Family
	seen := map[string]bool{}
	var visit func(*render.Family)
	visit = func(f *render.Family) {
		if seen[f.Name] {
			return
		}
		seen[f.Name] = true
		families = append(families, f)
		for _, name := range f.References {
			if source := f.ReferencedFamily(name); source != nil && strings.HasPrefix(source.Source, builtin.Prefix) {
				visit(source)
			}
		}
	}
	visit(root)
	return families
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

// Stale finds what is left over under the targets' roots of a checkout:
// a file a target would own, by its layout, that no rendering of the
// chosen families produced — a file of a family that was rendered and is
// no longer, or of a family that no longer exists. A file of a family
// that exists and was not chosen this time is not stale, only unrendered;
// a file a target says is nobody's is left alone. Paths are sorted.
func (k *Kernel) Stale(fsys fs.FS, world *World, chosen []string, rendered map[string][]byte) ([]string, error) {
	chosenSet := map[string]bool{}
	for _, name := range chosen {
		chosenSet[name] = true
	}
	implicit := map[string]bool{}
	for name, family := range world.Families {
		for _, tier := range model.Tiers {
			if tier.Builtin == "" || tier.Carries || !family.Has(tier.File) {
				continue
			}
			implicit[tier.Builtin] = true
			if chosenSet[name] {
				chosenSet[tier.Builtin] = true
			}
		}
	}
	var stale []string
	seen := map[string]bool{}
	for _, target := range k.targets {
		for _, root := range target.Roots() {
			err := fs.WalkDir(fsys, root, func(p string, entry fs.DirEntry, err error) error {
				if err != nil {
					if p == root {
						return fs.SkipDir
					}
					return err
				}
				if entry.IsDir() {
					if entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".") {
						return fs.SkipDir
					}
					return nil
				}
				family, owned := target.Family(p)
				if !owned || seen[p] {
					return nil
				}
				if _, current := rendered[p]; current {
					return nil
				}
				_, exists := world.Families[family]
				exists = exists || implicit[family]
				if !exists || chosenSet[family] {
					seen[p] = true
					stale = append(stale, p)
				}
				return nil
			})
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
	}
	sort.Strings(stale)
	return stale, nil
}

// Scaffold writes the hand-written side of a family's slots — what each
// target that can scaffold declares for the consumer to implement — into
// dir. A file that exists is never rewritten: it is the consumer's.
func (k *Kernel) Scaffold(world *World, name, dir string) ([]spi.File, error) {
	if diagnostics := k.Validate(world, name); len(diagnostics) != 0 {
		return nil, fmt.Errorf("invalid family: %s", diagnostics[0])
	}
	f := render.Build(Resolve(world, name))
	var files []spi.File
	for _, target := range k.targets {
		scaffolder, ok := target.(spi.Scaffolder)
		if !ok || !k.consumes(target, f) {
			continue
		}
		rendered, err := scaffolder.Scaffold(f, dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", target.Name(), err)
		}
		for _, file := range rendered {
			if !safe(file.Path) || !strings.HasPrefix(file.Path, dir+"/") {
				return nil, fmt.Errorf("%s: scaffold %q lies outside %s", target.Name(), file.Path, dir)
			}
			files = append(files, file)
		}
	}
	return files, nil
}
