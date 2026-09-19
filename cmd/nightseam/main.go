// nightseam renders a checkout's families into generated packages. A
// family is declared in tier files under api/contracts/<family>/ —
// model.json for its types, protocol.json for its two sides, session.json
// for how a session of it is governed, and an override file per target
// where a name differs from the convention — and rendered by every target
// the tool is composed with, today Go and TypeScript. A declaration refers
// to its own tier or a lower one, never a higher one, and the tool refuses
// one that does.
//
//	nightseam generate [family...]   render every family, or the named ones, writing what changed
//	nightseam check [family...]      fail if the checked-in output is stale
//	nightseam validate [family...]   report every diagnostic; exit 1 if any
//	nightseam init <family>          write the handlers a consumer implements, once, into a directory of its own
//	nightseam version                print the version of the tool that is running
//
// The generated Go packages are rooted at the checkout's module, read from
// its go.mod or given as --module; the TypeScript packages at an npm
// scope, --scope, the module's last element unless given. Both bind to
// Nightseam's runtime, github.com/Bitspark/nightseam/runtime/go and
// @nightseam/runtime, the peer of the nightseam.duplex/1 profile.
//
// The generator is a kernel and one package per target, composed in
// internal/compose and nowhere else: it names them, the kernel renders
// through the seam in internal/spi, and the conformance suite composes
// the same targets through the same package, so that what the suite
// renders is what this tool renders. It is developer tooling, never a
// runtime dependency; a consumer runs it as a Go tool, go tool nightseam
// check. Family names complete in the shell: nightseam completion --help.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Bitspark/nightseam/internal/kernel"
)

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// app is the checkout every command works on, and where its generated
// packages are rooted: the Go module and the npm scope.
type app struct{ root, module, scope, sibling string }

// settle fixes the module and the scope a command left to their defaults:
// the module is read from the checkout's go.mod, the scope is the module's
// last element behind an @.
func (a *app) settle() error {
	if a.module == "" {
		module, err := moduleOf(filepath.Join(a.root, "go.mod"))
		if err != nil {
			return err
		}
		a.module = module
	}
	if a.scope == "" {
		a.scope = "@" + path.Base(a.module)
	}
	return nil
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

// contracts is where the families are declared, relative to the checkout.
const contracts = "api/contracts"

func (a *app) contracts() string { return filepath.Join(a.root, filepath.FromSlash(contracts)) }

// load reads every family of the checkout; the targets' names say which
// override files a family may carry, and nothing of the checkout's module
// is needed for that.
func (a *app) load() *kernel.World { return kernel.Load(os.DirFS(a.root), contracts, targetNames) }

// world loads the checkout and composes the kernel with its config; what
// composing had to say is among the checkout's own diagnostics.
func (a *app) world() (*kernel.Kernel, *kernel.World, error) {
	if err := a.settle(); err != nil {
		return nil, nil, err
	}
	world := a.load()
	k, diagnostics := toolKernel(world.Config, a.module, a.scope, a.sibling)
	kernel.Configure(world, diagnostics)
	return k, world, nil
}

// ready is world for a command that renders: a checkout with a diagnostic
// of its own — its config, its contracts directory — is refused before any
// family is, since what the targets are composed with is in question.
func (a *app) ready() (*kernel.Kernel, *kernel.World, error) {
	k, world, err := a.world()
	if err != nil {
		return nil, nil, err
	}
	if diagnostics := kernel.Checkout(world); len(diagnostics) != 0 {
		return nil, nil, fmt.Errorf("invalid checkout: %s", diagnostics[0])
	}
	return k, world, nil
}

// families names the families of the checkout, in order: each directory
// under api/contracts, and each family the loader had something to say
// about, so that one it could not read is still named and refused.
func (a *app) families() ([]string, error) {
	return familiesOf(a.load()), nil
}

func familiesOf(world *kernel.World) []string {
	seen := map[string]bool{}
	var names []string
	for _, name := range world.Names {
		seen[name] = true
		names = append(names, name)
	}
	for name := range world.Problems {
		if name != "" && !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// chosen resolves a command's arguments to families: every family when
// none is named, otherwise the named ones, each of which must exist.
func (a *app) chosen(args []string) ([]string, error) {
	known, err := a.families()
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if len(known) == 0 {
			return nil, fmt.Errorf("no families in %s", a.contracts())
		}
		return known, nil
	}
	for _, name := range args {
		if !slices.Contains(known, name) {
			if len(known) == 0 {
				return nil, fmt.Errorf("no family named %q: there are no families in %s", name, a.contracts())
			}
			return nil, fmt.Errorf("no family named %q in %s; there are: %s", name, a.contracts(), strings.Join(known, ", "))
		}
	}
	return args, nil
}

// render generates every named family within the world of all of them and
// merges their files; two families may not render one path.
func (a *app) render(names []string) (map[string][]byte, error) {
	files, _, err := a.renderAndStale(names)
	return files, err
}

// renderAndStale renders the named families, then the checkout as a whole
// where a target renders it, and finds what is left over under the
// targets' roots: a file of one of them that nothing rendered, or of a
// family that no longer exists.
func (a *app) renderAndStale(names []string) (map[string][]byte, []string, error) {
	k, world, err := a.ready()
	if err != nil {
		return nil, nil, err
	}
	files := map[string][]byte{}
	merge := func(result kernel.Result) error {
		for path, data := range result.Files {
			if previous, exists := files[path]; exists && !bytes.Equal(previous, data) {
				return fmt.Errorf("conflicting generated output %s", path)
			}
			files[path] = data
		}
		return nil
	}
	for _, name := range names {
		result, err := k.Render(world, name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := merge(result); err != nil {
			return nil, nil, err
		}
	}
	result, err := k.RenderCheckout(world)
	if err != nil {
		return nil, nil, err
	}
	if err := merge(result); err != nil {
		return nil, nil, err
	}
	stale, err := k.Stale(os.DirFS(a.root), world, names, files)
	if err != nil {
		return nil, nil, err
	}
	return files, stale, nil
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
		Short: "Render api/contracts into generated packages and specifications",
		Long: `nightseam renders a checkout's families into generated packages. A
family is declared in tier files under api/contracts/<family>/ — model.json,
protocol.json, session.json, and an override file per target — and rendered
by every target the tool is composed with, today Go, TypeScript and the
family's own specification as Markdown. The Go packages are rooted at the
checkout's module, the TypeScript packages at an npm scope; both bind to
Nightseam's runtime. Nothing is written that is already up to date.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// --version and the version command are one answer in one spelling.
		Version: version(),
	}
	root.SetVersionTemplate("nightseam {{.Version}}\n")
	root.PersistentFlags().StringVar(&a.root, "root", ".", "repository root")
	root.PersistentFlags().StringVar(&a.module, "module", "", "Go module the generated packages are rooted at (default: the module of <root>/go.mod)")
	root.PersistentFlags().StringVar(&a.scope, "scope", "", "npm scope of the generated TypeScript packages (default: @ and the module's last element)")
	root.PersistentFlags().StringVar(&a.sibling, "ts-sibling", "file", "generated TypeScript sibling dependencies: file, workspace or version")

	// family completes an argument with the families' names.
	family := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		names, err := a.families()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}

	root.AddCommand(
		initCommand(a, family),
		versionCommand(),
		&cobra.Command{
			Use:               "generate [family...]",
			Short:             "Render every family, or the named ones, writing what changed",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				files, leftover, err := a.renderAndStale(names)
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
				// What a target owns and nothing renders any more is removed,
				// and a directory that is left empty with it.
				for _, path := range leftover {
					destination := filepath.Join(a.root, filepath.FromSlash(path))
					if err := os.Remove(destination); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "removed", path)
					for dir := filepath.Dir(destination); dir != a.root; dir = filepath.Dir(dir) {
						if os.Remove(dir) != nil {
							break
						}
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:               "check [family...]",
			Short:             "Fail if the checked-in output of every family, or the named ones, is stale",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				files, leftover, err := a.renderAndStale(names)
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
				for _, path := range leftover {
					fmt.Fprintln(cmd.ErrOrStderr(), "generated output nothing renders:", path)
				}
				if len(stale)+len(leftover) > 0 {
					return fmt.Errorf("generated APIs are stale; run nightseam generate")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:               "validate [family...]",
			Short:             "Report every diagnostic of every family, or the named ones; exit 1 if any",
			Args:              cobra.ArbitraryArgs,
			ValidArgsFunction: family,
			RunE: func(cmd *cobra.Command, args []string) error {
				names, err := a.chosen(args)
				if err != nil {
					return err
				}
				k, world, err := a.world()
				if err != nil {
					return err
				}
				problems := 0
				for _, d := range kernel.Checkout(world) {
					fmt.Fprintln(cmd.ErrOrStderr(), d)
					problems++
				}
				for _, name := range names {
					for _, d := range k.Validate(world, name) {
						fmt.Fprintln(cmd.ErrOrStderr(), d)
						problems++
					}
				}
				if problems > 0 {
					return fmt.Errorf("%d problems", problems)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d families; valid\n", len(names))
				return nil
			},
		},
	)
	return root
}

// initCommand scaffolds the hand-written side of a family's slots:
// nightseam init <family> [--dir <dir>].
func initCommand(a *app, family func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:               "init <family>",
		Short:             "Write the handlers a consumer implements for a family, once, into a directory of its own",
		Long:              "init writes the hand-written side of a family's slots — the Go server's Handler, the TypeScript client's handler of what the server sends — as stubs into a directory the consumer owns, api/impl/<family> unless --dir says otherwise. A file that exists is left as it is: init writes once and never rewrites.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: family,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := a.chosen(args); err != nil {
				return err
			}
			k, world, err := a.ready()
			if err != nil {
				return err
			}
			target := strings.ReplaceAll(dir, "{family}", args[0])
			target = strings.TrimSuffix(filepath.ToSlash(target), "/")
			files, err := k.Scaffold(world, args[0], target)
			if err != nil {
				return err
			}
			for _, file := range files {
				destination := filepath.Join(a.root, filepath.FromSlash(file.Path))
				if _, err := os.Stat(destination); err == nil {
					fmt.Fprintln(cmd.OutOrStdout(), "kept", file.Path)
					continue
				}
				if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(destination, file.Data, 0o644); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "wrote", file.Path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "api/impl/{family}", "where to write the stubs, a pattern over the family's name")
	return cmd
}

// modulePath is the tool's own module, and the one whose version answers
// "what rendered this?". A consumer adds the tool with go get -tool and
// builds it inside its own module, so the build info's main module is that
// consumer's rather than this one, and the version to report is then the
// one the consumer's requirement resolved to.
const modulePath = "github.com/Bitspark/nightseam"

// versionCommand reports the version of the tool that is running, which is
// the question a reviewer looking at a golden diff asks and the one a check
// that disagrees across two machines raises. The generated files do not
// carry it: a version in their header would rewrite every file of every
// consumer on every release, and would make check fail on a version that
// renders the same bytes, which is the failure this command exists to
// explain rather than one to add. docs/declaration/generator.md states that split.
func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the tool that is running",
		Long:  "version prints the module version of the running tool — what go get -tool resolved, or (devel) from a checkout, a build of a tree being exactly what that answer should say. The generated files name no version; this command is where it is asked for.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "nightseam "+version())
			return nil
		},
	}
}

// version reads the tool's own version out of the running binary.
func version() string { return versionOf(debug.ReadBuildInfo()) }

// versionOf takes what debug.ReadBuildInfo returns and finds the tool's
// module in it: the main module where the binary is the tool itself, and
// the required one where a consumer built it inside its own module. A
// checkout, a replace directive pointing at one and a binary built without
// module information all report (devel), which is the useful answer —
// rendered by a tree rather than by a release — and is what the empty
// version means in each of the three.
func versionOf(info *debug.BuildInfo, ok bool) string {
	const devel = "(devel)"
	if !ok {
		return devel
	}
	if info.Main.Path == modulePath {
		return settled(info.Main.Version, devel)
	}
	for _, dependency := range info.Deps {
		if dependency.Path != modulePath {
			continue
		}
		// A replace is what a consumer points at a checkout of the tool
		// with, and what it points at is what rendered: the requirement it
		// replaced never ran.
		if dependency.Replace != nil {
			return settled(dependency.Replace.Version, devel)
		}
		return settled(dependency.Version, devel)
	}
	// A binary that carries build information naming this module nowhere was
	// not built from it by the go command — vendored into another tree, or
	// stripped — and has no version of its own to report.
	return "(unknown)"
}

func settled(version, devel string) string {
	if version == "" {
		return devel
	}
	return version
}
