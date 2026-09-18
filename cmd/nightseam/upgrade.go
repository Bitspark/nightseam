package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Bitspark/nightseam/internal/upgrade"
)

// layerFamilies names the families declared in layer files of the previous
// language, <family>.<dto|rpc|sess>.json, in order.
func (a *app) layerFamilies() ([]string, error) {
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
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".json")
		family, layer, ok := strings.Cut(stem, ".")
		if !ok || seen[family] {
			continue
		}
		for _, known := range upgrade.Layers {
			if known.Layer == layer {
				seen[family] = true
				names = append(names, family)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// layerSources reads a family's layer files, by layer.
func (a *app) layerSources(name string) (map[string][]byte, error) {
	sources := map[string][]byte{}
	for _, layer := range upgrade.Layers {
		data, err := os.ReadFile(filepath.Join(a.contracts(), name+"."+layer.Layer+".json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sources[layer.Layer] = data
	}
	return sources, nil
}

// upgradeCommand rewrites families from layer files into the directory
// form: nightseam upgrade [family...] [--dry-run].
func upgradeCommand(a *app) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "upgrade [family...]",
		Short: "Rewrite every family, or the named ones, from layer files into the directory form",
		Long:  "upgrade rewrites a family declared in layer files — <family>.dto.json, .rpc.json, .sess.json — into api/contracts/<family>/ with model.json, protocol.json, session.json and, where a hand-spelled name differs from the convention, go.json and typescript.json; then it removes the layer files. With --dry-run it prints what it would write and writes nothing.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			known, err := a.layerFamilies()
			if err != nil {
				return err
			}
			names := args
			if len(names) == 0 {
				names = known
			}
			if len(names) == 0 {
				return fmt.Errorf("no layer files in %s: nothing to upgrade", a.contracts())
			}
			for _, name := range names {
				dir := filepath.Join(a.contracts(), name)
				if info, err := os.Stat(dir); err == nil && info.IsDir() {
					return fmt.Errorf("%s is already in the directory form", name)
				}
				sources, err := a.layerSources(name)
				if err != nil {
					return err
				}
				files, err := upgrade.Family(sources)
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				paths := make([]string, 0, len(files))
				for file := range files {
					paths = append(paths, file)
				}
				sort.Strings(paths)
				if dryRun {
					for _, file := range paths {
						fmt.Fprintf(cmd.OutOrStdout(), "--- %s/%s\n%s", name, file, files[file])
					}
					continue
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				for _, file := range paths {
					if err := os.WriteFile(filepath.Join(dir, file), files[file], 0o644); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "wrote api/contracts/%s/%s\n", name, file)
				}
				for layer := range sources {
					if err := os.Remove(filepath.Join(a.contracts(), name+"."+layer+".json")); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be written and write nothing")
	return cmd
}
