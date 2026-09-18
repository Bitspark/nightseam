package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Bitspark/nightseam/internal/upgrade"
)

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
func upgradeCommand(a *app, family func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "upgrade [family...]",
		Short:             "Rewrite every family, or the named ones, from layer files into the directory form",
		Long:              "upgrade rewrites a family declared in layer files — <family>.dto.json, .rpc.json, .sess.json — into api/contracts/<family>/ with model.json, protocol.json, session.json and, where a hand-spelled name differs from the convention, go.json and typescript.json; then it removes the layer files. With --dry-run it prints what it would write and writes nothing.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: family,
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := a.chosen(args)
			if err != nil {
				return err
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
