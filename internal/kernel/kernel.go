// Package kernel generates an API family: it parses and checks a contract,
// has every language it is given check the contract too, and only then has
// each render its sources, which it merges into one deterministic set. It
// names no language; the tool composes it with the languages it renders.
// Rendering is pure: it never reads or writes a checkout.
package kernel

import (
	"fmt"
	"path"
	"strings"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/spi"
)

// Result is one family rendered by every language, keyed by path relative
// to the repository root.
type Result struct {
	Files          map[string][]byte
	Recipe         string
	RuntimeVersion string
}

// Validate returns every diagnostic the contract and the languages raise,
// sorted, without rendering. When the schema refuses the contract, the
// schema's diagnostics are the whole answer: nothing else is asked about a
// contract that is not well-formed.
func Validate(input map[string]any, languages ...spi.Language) []contract.Diagnostic {
	_, diagnostics := validate(input, languages)
	return diagnostics
}

func validate(input map[string]any, languages []spi.Language) (contract.API, []contract.Diagnostic) {
	api, diagnostics := contract.Parse(input)
	if len(diagnostics) != 0 {
		return api, diagnostics
	}
	diagnostics = contract.Check(api)
	for _, language := range languages {
		diagnostics = append(diagnostics, language.Check(api)...)
	}
	contract.Sort(diagnostics)
	return api, diagnostics
}

// Generate validates a contract, then renders it with every language, in
// order. A language's file may not escape the repository root, and no two
// languages may render the same path.
func Generate(input map[string]any, languages ...spi.Language) (Result, error) {
	api, diagnostics := validate(input, languages)
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

// safe holds a rendered path inside the repository: relative, slash-
// separated, clean, and with no parent reference.
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
