// nightseam renders a checkout's API contracts into generated packages. A
// family is declared in layer files under api/contracts —
// <family>.dto.json for its data, <family>.rpc.json for its operations,
// <family>.sess.json for how a session of it is governed — merged into one
// contract and rendered by every language the tool is composed with, today
// Go and TypeScript. A declaration refers to its own layer or a lower one,
// never a higher one, and the tool refuses one that does.
//
//	nightseam generate [family...]   render every contract in api/contracts, or the named ones
//	nightseam check [family...]      fail if the checked-in output is stale
//	nightseam validate [family...]   report every diagnostic; exit 1 if any
//
// The generated Go packages are rooted at the checkout's module, read from
// its go.mod or given as --module; the TypeScript packages at an npm scope,
// --scope, the module's last element unless given. Both bind to Nightseam's
// runtime, github.com/Bitspark/nightseam/runtime and @nightseam/runtime, the
// peer of the nightseam.duplex/1 profile.
//
// The generator is a kernel and one package per language, composed here and
// nowhere else: languages names them, the kernel renders through the seam
// in internal/legacy/spi. It is developer tooling, never a runtime dependency; a
// consumer runs it as a Go tool, go tool nightseam check. Family names
// complete in the shell: nightseam completion --help.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Bitspark/nightseam/internal/legacy/contract"
	"github.com/Bitspark/nightseam/internal/legacy/kernel"
	"github.com/Bitspark/nightseam/internal/legacy/languages/golang"
	"github.com/Bitspark/nightseam/internal/legacy/languages/typescript"
	"github.com/Bitspark/nightseam/internal/legacy/spi"
)

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// languages is the composition root: the only place a language is named.
// The order is the order families are rendered in. module roots the Go
// packages and scope the TypeScript ones; the runtime both bind to is each
// language's default, Nightseam's own.
func languages(module, scope string) []spi.Language {
	return []spi.Language{
		golang.New(golang.Options{Module: module}),
		typescript.New(typescript.Options{Scope: scope}),
	}
}

// app is the checkout every command works on, and where its generated
// packages are rooted: the Go module and the npm scope.
type app struct{ root, module, scope string }

// languages composes the languages for this checkout, settling the module
// and the scope a command left to their defaults: the module is read from
// the checkout's go.mod, the scope is the module's last element behind an @.
func (a *app) languages() ([]spi.Language, error) {
	if a.module == "" {
		module, err := moduleOf(filepath.Join(a.root, "go.mod"))
		if err != nil {
			return nil, err
		}
		a.module = module
	}
	if a.scope == "" {
		a.scope = "@" + path.Base(a.module)
	}
	return languages(a.module, a.scope), nil
}

// moduleOf reads the module path a go.mod declares.
func moduleOf(file string) (string, error) {
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no go.mod in %s to read the module from; pass --module", filepath.Dir(file))
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("%s declares no module; pass --module", file)
}

func (a *app) contracts() string { return filepath.Join(a.root, "api", "contracts") }

// families names the contracts in api/contracts, in order: a family is
// declared in layer files, <family>.dto.json, <family>.rpc.json and
// <family>.sess.json, and is named once whatever layers it has. A checkout
// with no contracts directory has none.
func (a *app) families() ([]string, error) {
	entries, err := os.ReadDir(a.contracts())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, entry := range entries {
		family, _, ok := layerFile(entry.Name())
		if entry.IsDir() || !ok || seen[family] {
			continue
		}
		seen[family] = true
		names = append(names, family)
	}
	sort.Strings(names)
	return names, nil
}

// layerFile splits a layer file's name, <family>.<layer>.json, into its
// family and its layer.
func layerFile(name string) (family, layer string, ok bool) {
	if !strings.HasSuffix(name, ".json") {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ".json")
	at := strings.LastIndexByte(stem, '.')
	if at <= 0 {
		return "", "", false
	}
	family, layer = stem[:at], stem[at+1:]
	if !slices.Contains(contract.Layers, layer) {
		return "", "", false
	}
	return family, layer, true
}

// chosen resolves a command's arguments to families: every contract when
// none is named, otherwise the named ones, each of which must exist.
func (a *app) chosen(args []string) ([]string, error) {
	known, err := a.families()
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if len(known) == 0 {
			return nil, fmt.Errorf("no contracts in %s", a.contracts())
		}
		return known, nil
	}
	for _, name := range args {
		if !slices.Contains(known, name) {
			if len(known) == 0 {
				return nil, fmt.Errorf("no contract named %q: there are no contracts in %s", name, a.contracts())
			}
			return nil, fmt.Errorf("no contract named %q in %s; there are: %s", name, a.contracts(), strings.Join(known, ", "))
		}
	}
	return args, nil
}

// load reads a family's layer files and merges them into the one contract
// the kernel renders. Each file must name the family it is filed under and
// the layer its name says; what the merge refuses is reported by file.
func (a *app) load(name string) (map[string]any, error) {
	files := map[string]map[string]any{}
	for _, layer := range contract.Layers {
		data, err := os.ReadFile(filepath.Join(a.contracts(), name+"."+layer+".json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var file map[string]any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&file); err != nil {
			return nil, fmt.Errorf("%s.%s contract: %w", name, layer, err)
		}
		if file["name"] != name {
			return nil, fmt.Errorf("%s.%s contract names API %v", name, layer, file["name"])
		}
		files[layer] = file
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no layer files for family %q in %s", name, a.contracts())
	}
	merged, diagnostics := contract.Merge(files)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("%s contract: %s: %s", name, diagnostics[0].Pointer, diagnostics[0].Message)
	}
	return merged, nil
}

// world loads every contract in the checkout, by family: what a family may
// import from, and what a slot may name.
func (a *app) world() (kernel.World, error) {
	names, err := a.families()
	if err != nil {
		return nil, err
	}
	world := kernel.World{}
	for _, name := range names {
		contract, err := a.load(name)
		if err != nil {
			return nil, err
		}
		world[name] = contract
	}
	return world, nil
}

// render generates every named family within the world of all of them and
// merges their files; two families may not render one path.
func (a *app) render(names []string) (map[string][]byte, error) {
	world, err := a.world()
	if err != nil {
		return nil, err
	}
	languages, err := a.languages()
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, name := range names {
		contract := world[name]
		result, err := kernel.GenerateIn(world, contract, languages...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for path, data := range result.Files {
			if previous, exists := files[path]; exists && !bytes.Equal(previous, data) {
				return nil, fmt.Errorf("conflicting generated output %s", path)
			}
			files[path] = data
		}
	}
	return files, nil
}

// changed returns the rendered paths whose checked-in bytes differ, in order.
func (a *app) changed(files map[string][]byte) ([]string, error) {
	var stale []string
	for path, data := range files {
		current, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(path)))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !bytes.Equal(current, data) {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

func newCommand() *cobra.Command {
	a := &app{}
	root := &cobra.Command{
		Use:   "nightseam",
		Short: "Render api/contracts into generated Go and TypeScript packages",
		Long: `nightseam renders a checkout's API contracts into generated packages. A
family is declared in layer files under api/contracts, <family>.dto.json,
<family>.rpc.json and <family>.sess.json, merged into one contract and
rendered by every language the tool is composed with, today Go and
TypeScript. The Go packages are rooted at the checkout's module, the
TypeScript packages at an npm scope; both bind to Nightseam's runtime.
Nothing is written that is already up to date.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&a.root, "root", ".", "repository root")
	root.PersistentFlags().StringVar(&a.module, "module", "", "Go module the generated packages are rooted at (default: the module of <root>/go.mod)")
	root.PersistentFlags().StringVar(&a.scope, "scope", "", "npm scope of the generated TypeScript packages (default: @ and the module's last element)")

	// family completes an argument with the contracts' names.
	family := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		names, err := a.families()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}

	root.AddCommand(
		upgradeCommand(a, family),
		&cobra.Command{
			Use:               "generate [family...]",
			Short:             "Render every contract, or the named ones, writing what changed",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				files, err := a.render(names)
				if err != nil {
					return err
				}
				stale, err := a.changed(files)
				if err != nil {
					return err
				}
				for _, path := range stale {
					destination := filepath.Join(a.root, filepath.FromSlash(path))
					if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(destination, files[path], 0o644); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "generated", path)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:               "check [family...]",
			Short:             "Fail if the checked-in output of every contract, or the named ones, is stale",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				files, err := a.render(names)
				if err != nil {
					return err
				}
				stale, err := a.changed(files)
				if err != nil {
					return err
				}
				for _, path := range stale {
					fmt.Fprintln(cmd.ErrOrStderr(), "stale generated output:", path)
				}
				if len(stale) > 0 {
					return fmt.Errorf("generated APIs are stale; run nightseam generate")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:               "validate [family...]",
			Short:             "Report every diagnostic of every contract, or the named ones; exit 1 if any",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				world, err := a.world()
				if err != nil {
					return err
				}
				languages, err := a.languages()
				if err != nil {
					return err
				}
				problems := 0
				for _, name := range names {
					for _, d := range kernel.ValidateIn(world, world[name], languages...) {
						fmt.Fprintf(cmd.ErrOrStderr(), "%s %s: %s [%s]\n", name, d.Pointer, d.Message, d.Code)
						problems++
					}
				}
				if problems > 0 {
					return fmt.Errorf("%d problems", problems)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d contracts; valid\n", len(names))
				return nil
			},
		},
	)
	return root
}
