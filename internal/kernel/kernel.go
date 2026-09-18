// Package kernel drives generation: it parses and checks a contract, asks
// every language composed with it to check and render, and merges what they
// render into one result. It never names a language.
//
// A contract is validated within a world: the raw contracts of every family
// rendered together, by name. A family it imports is parsed from the world and
// handed to it as Imported; every name in the world is handed to it as
// Families, which a slot may name. A world of none is a family alone.
package kernel

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/contract"
	"github.com/Bitspark/nightseam/internal/spi"
)

// World is every family's raw contract, by family name.
type World map[string]map[string]any

// Result is one family rendered by every language: its files by path, and
// the recipe and runtime version they were rendered for.
type Result struct {
	Files          map[string][]byte
	Recipe         string
	RuntimeVersion string
}

// Validate reports every diagnostic of a contract alone: the schema's, or
// the contract's own and every language's, sorted.
func Validate(input map[string]any, languages ...spi.Language) []contract.Diagnostic {
	return ValidateIn(nil, input, languages...)
}

// ValidateIn reports every diagnostic of a contract within a world.
func ValidateIn(world World, input map[string]any, languages ...spi.Language) []contract.Diagnostic {
	_, diagnostics := validate(world, input, languages)
	return diagnostics
}

func validate(world World, input map[string]any, languages []spi.Language) (contract.API, []contract.Diagnostic) {
	api, diagnostics := contract.Parse(input)
	if len(diagnostics) != 0 {
		return api, diagnostics
	}
	diagnostics = append(diagnostics, resolve(&api, world)...)
	diagnostics = append(diagnostics, contract.Check(api)...)
	for _, language := range languages {
		diagnostics = append(diagnostics, language.Check(api)...)
	}
	contract.Sort(diagnostics)
	return api, diagnostics
}

// resolve hands a contract the world it is rendered in: the families it
// imports, parsed, and the names of all of them. An import that names a
// family the world has but cannot parse is reported here; one it does not
// have is reported by Check.
func resolve(api *contract.API, world World) []contract.Diagnostic {
	var diagnostics []contract.Diagnostic
	api.Imported = map[string]contract.API{}
	for name, raw := range world {
		api.Families = append(api.Families, name)
		if raw["role"] == contract.SessionRole {
			api.Sessions = append(api.Sessions, name)
		}
	}
	sort.Strings(api.Families)
	sort.Strings(api.Sessions)
	// A slot of a parameter's type is held to every family that may bind the
	// parameter; a member that does not parse is left out here and reported
	// by whoever renders it.
	api.Members = map[string]contract.API{}
	for _, name := range api.Sessions {
		if name == api.Name {
			continue
		}
		if member, problems := contract.Parse(world[name]); len(problems) == 0 {
			api.Members[name] = member
		}
	}
	for i, name := range api.Imports {
		raw, ok := world[name]
		if !ok || name == api.Name {
			continue
		}
		imported, problems := contract.Parse(raw)
		if len(problems) != 0 {
			diagnostics = append(diagnostics, contract.Diagnostic{Code: "unresolved_import", Pointer: fmt.Sprintf("/imports/%d", i), Message: "Imported family " + name + " does not parse: " + problems[0].Message})
			continue
		}
		api.Imported[name] = imported
	}
	return diagnostics
}

// Generate renders a contract alone with every language.
func Generate(input map[string]any, languages ...spi.Language) (Result, error) {
	return GenerateIn(nil, input, languages...)
}

// GenerateIn renders a contract within a world with every language. A
// contract with any diagnostic is refused before any language renders.
func GenerateIn(world World, input map[string]any, languages ...spi.Language) (Result, error) {
	api, diagnostics := validate(world, input, languages)
	if len(diagnostics) != 0 {
		return Result{}, fmt.Errorf("invalid API contract: %s: %s", diagnostics[0].Pointer, diagnostics[0].Message)
	}
	files := map[string][]byte{}
	for _, language := range languages {
		rendered, err := language.Render(api)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", language.Name(), err)
		}
		for _, file := range rendered {
			if !safe(file.Path) {
				return Result{}, fmt.Errorf("%s: invalid output path %q", language.Name(), file.Path)
			}
			if _, exists := files[file.Path]; exists {
				return Result{}, fmt.Errorf("%s: conflicting generated output %s", language.Name(), file.Path)
			}
			files[file.Path] = file.Data
		}
	}
	return Result{Files: files, Recipe: spi.Recipe, RuntimeVersion: spi.RuntimeVersion}, nil
}

// safe accepts a slash-separated path relative to the repository root that
// cannot escape it.
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
