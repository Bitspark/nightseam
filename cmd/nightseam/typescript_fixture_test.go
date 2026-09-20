package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureTypeScriptPaths resolves the source exports that generated manifests
// actually advertise. Exact entries avoid overlapping package and /types
// wildcards, and do not infer package names from their output directories.
// Later roots take precedence so a diagram can compile its generic binding
// against its own client, alongside a separately compiled bound rendering.
func fixtureTypeScriptPaths(t *testing.T, directory string, roots ...string) map[string][]string {
	t.Helper()
	paths := map[string][]string{}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		paths["@nightseam/"+component] = []string{"./" + component + "/ts/src/index.ts"}
	}
	if len(roots) == 0 {
		roots = []string{"api/ts"}
	}
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(directory, root), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() != "package.json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var manifest struct {
				Name    string          `json:"name"`
				Exports json.RawMessage `json:"exports"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return err
			}
			if manifest.Name == "" {
				t.Fatalf("generated package has no name: %s", path)
			}
			var exports map[string]string
			var main string
			if err := json.Unmarshal(manifest.Exports, &main); err == nil {
				exports = map[string]string{".": main}
			} else if err := json.Unmarshal(manifest.Exports, &exports); err != nil {
				return err
			}
			for subpath, target := range exports {
				if subpath != "." && !strings.HasPrefix(subpath, "./") {
					t.Fatalf("unexpected generated export %q in %s", subpath, path)
				}
				if !strings.HasPrefix(target, "./") || strings.ContainsAny(subpath+target, "*") {
					t.Fatalf("generated export is not an exact source path: %s %q -> %q", path, subpath, target)
				}
				file := filepath.Join(filepath.Dir(path), filepath.FromSlash(target))
				if _, err := os.Stat(file); err != nil {
					return err
				}
				relative, err := filepath.Rel(directory, file)
				if err != nil {
					return err
				}
				name := manifest.Name
				if subpath != "." {
					name += strings.TrimPrefix(subpath, ".")
				}
				paths[name] = []string{"./" + filepath.ToSlash(relative)}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("resolve generated TypeScript exports under %s: %v", root, err)
		}
	}
	return paths
}

// Two renderings of a family intentionally have the same npm name in the
// diagram. A single paths entry cannot represent both bindings' /types import;
// each binding is compiled against the manifest of its corresponding client.
func checkGenericTypeScriptBindings(t *testing.T, directory, tsc string) {
	t.Helper()
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory, "api/ts", "gen/ts")},
		"include":         []string{"gen/ts/**/*.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.generic.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.generic.json")
}

func TestTypeScriptFixtureResolvesExactPackageExports(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	// The package name and both exported files differ from the default layout.
	// A second rendering shares the name but has a different protocol type.
	for _, item := range []struct{ root, value string }{{"custom/plain", "string"}, {"custom/generic", "number"}} {
		writeFixture(t, directory, item.root+"/placed/package.json", []byte(`{"name":"@example/contract-client","type":"module","exports":{".":"./code/api.ts","./types":"./protocol/value.ts"}}`))
		writeFixture(t, directory, item.root+"/placed/code/api.ts", []byte(`export const name = 'contract';`))
		writeFixture(t, directory, item.root+"/placed/protocol/value.ts", []byte(`export type Value = `+item.value+`;`))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "consumer.ts", []byte(`import {name} from '@example/contract-client'; import type {Value} from '@example/contract-client/types'; const value: Value = 7; export {name, value};`))
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "noEmit": true, "paths": fixtureTypeScriptPaths(t, directory, "custom/plain", "custom/generic")},
		"include":         []string{"consumer.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
}
