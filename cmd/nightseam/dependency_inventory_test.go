package main

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These are consumer roots, not all the packages generated in the checkout.
// The live consumer uses the same generic cell artifact as the scalar one;
// only its supplied value implementation introduces the live component.
var dependencyConsumers = []struct {
	name     string
	families []string
	live     bool
}{
	{"model-only", []string{"data"}, false},
	{"scalar-generic", []string{"cell"}, false},
	{"live-generic", []string{"cell", "values"}, true},
	{"ordinary-operation-without-auth", []string{"echo"}, false},
}

func dependencyFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	files := map[string]string{
		"data/model.json":      `{"nightseam":2,"types":{"Row":{"kind":"record","fields":[{"name":"name","type":"string"}]}}}`,
		"cell/model.json":      `{"nightseam":2,"types":{"Box":{"kind":"record","parameters":[{"name":"U"}],"fields":[{"name":"value","type":"U"}]}}}`,
		"cell/protocol.json":   `{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{"methods":{"exchange":{"request":{"kind":"record","fields":[{"name":"value","type":"T"}]},"result":"T"}}}}`,
		"values/model.json":    `{"nightseam":2}`,
		"values/protocol.json": `{"profile":"nightseam.duplex/1"}`,
		"values/live.json":     `{"types":{"Unary":{"kind":"callable","request":"integer","result":"integer"}}}`,
		"echo/model.json":      `{"nightseam":2}`,
		"echo/protocol.json":   `{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"request":{"kind":"record","fields":[{"name":"value","type":"string"}]},"result":"string"}}}}`,
	}
	for name, body := range files {
		writeFixture(t, directory, "api/contracts/"+name, []byte(body))
	}
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate dependency consumers: %v\n%s\n%s", err, out, errs)
	}
	return directory
}

func dependencyGoRoots(directory string, families []string) []string {
	var roots []string
	for _, family := range families {
		for _, suffix := range []string{"protocol", "client", "binding"} {
			name := "api/go/" + family + "-" + suffix
			if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
				roots = append(roots, name)
			}
		}
	}
	return roots
}

func dependencyTSRoots(directory string, families []string) []string {
	var roots []string
	for _, family := range families {
		for _, suffix := range []string{"client", "binding"} {
			name := "api/ts/" + family + "-" + suffix
			if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
				roots = append(roots, name)
			}
		}
	}
	return roots
}

func checkDependencyComponents(t *testing.T, label string, dependencies []string, live bool) {
	t.Helper()
	slices.Sort(dependencies)
	var components []string
	for _, dependency := range dependencies {
		if strings.HasPrefix(dependency, "@nightseam/") || strings.HasPrefix(dependency, "github.com/Bitspark/nightseam/") {
			components = append(components, dependency)
		}
	}
	t.Logf("%s: %s", label, strings.Join(components, ", "))
	liveFound := false
	for _, dependency := range dependencies {
		component := ""
		if strings.HasPrefix(dependency, "@nightseam/") {
			component = strings.Split(strings.TrimPrefix(dependency, "@nightseam/"), "/")[0]
		} else if strings.HasPrefix(dependency, "github.com/Bitspark/nightseam/") {
			component = strings.Split(strings.TrimPrefix(dependency, "github.com/Bitspark/nightseam/"), "/")[0]
		}
		switch component {
		case "", "runtime", "duplex", "internal":
		case "live":
			liveFound = true
			if !live {
				t.Errorf("%s requires unchosen component %s", label, dependency)
			}
		default:
			t.Errorf("%s requires unchosen component %s", label, dependency)
		}
	}
	if live && !liveFound {
		t.Errorf("%s did not observe the live positive control", label)
	}
}

// The fast tier inventories generated imports and installation requirements.
// A type-only TypeScript import may vanish from JavaScript while its package
// remains a mandatory installation; neither is evidence for the other.
func TestGeneratedDependencyInventory(t *testing.T) {
	directory := dependencyFixture(t)
	root := repositoryRoot(t)
	for _, consumer := range dependencyConsumers {
		t.Run(consumer.name, func(t *testing.T) {
			goImports := map[string]bool{}
			for _, pkg := range dependencyGoRoots(directory, consumer.families) {
				files, err := filepath.Glob(filepath.Join(directory, pkg, "*.go"))
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range files {
					file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
					if err != nil {
						t.Fatal(err)
					}
					for _, spec := range file.Imports {
						path, err := strconv.Unquote(spec.Path.Value)
						if err != nil {
							t.Fatal(err)
						}
						goImports[path] = true
					}
				}
			}
			checkDependencyComponents(t, "generated Go imports", dependencyKeys(goImports), consumer.live)
			packages := map[string]bool{}
			var visit func(string)
			visit = func(dir string) {
				var manifest struct {
					Name         string
					Dependencies map[string]string
				}
				data, err := os.ReadFile(filepath.Join(dir, "package.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &manifest); err != nil {
					t.Fatal(err)
				}
				if packages[manifest.Name] {
					return
				}
				packages[manifest.Name] = true
				for name, version := range manifest.Dependencies {
					if strings.HasPrefix(name, "@nightseam/") {
						visit(filepath.Join(root, strings.TrimPrefix(name, "@nightseam/"), "ts"))
					} else if strings.HasPrefix(version, "file:") {
						visit(filepath.Join(dir, strings.TrimPrefix(version, "file:")))
					} else {
						packages[name] = true
					}
				}
			}
			for _, pkg := range dependencyTSRoots(directory, consumer.families) {
				visit(filepath.Join(directory, pkg))
			}
			checkDependencyComponents(t, "npm installation closure", dependencyKeys(packages), consumer.live)
		})
	}
}

func dependencyKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

// The full tier asks Go for its actual build dependency closure and traverses
// browser ESM after TypeScript erasure. It does not assume tree shaking, execute
// application code, or treat an erased type import as a runtime dependency.
func TestGeneratedDependencyInventoryToolchains(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := dependencyFixture(t)
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"target": "ES2022", "module": "ESNext", "moduleResolution": "Bundler",
			"rootDir": ".", "outDir": "browser", "noCheck": true, "types": []string{},
			"rewriteRelativeImportExtensions": true, "verbatimModuleSyntax": true,
			"paths": fixtureTypeScriptPaths(t, directory),
		},
		"include": []string{"api/ts/**/*.ts", "runtime/ts/src/**/*.ts", "duplex/ts/src/**/*.ts", "tunnel/ts/src/**/*.ts", "live/ts/src/**/*.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.browser.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.browser.json")
	writeFixture(t, directory, "inventory.mjs", []byte(dependencyBrowserInventory))
	for _, consumer := range dependencyConsumers {
		t.Run(consumer.name, func(t *testing.T) {
			var args []string
			args = append(args, "list", "-deps", "-f", "{{.ImportPath}}")
			for _, pkg := range dependencyGoRoots(directory, consumer.families) {
				args = append(args, "./"+pkg)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "go", args...)
			command.Dir = directory
			out, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("Go dependency closure: %v\n%s", err, out)
			}
			checkDependencyComponents(t, "Go build closure", strings.Fields(string(out)), consumer.live)
			args = []string{"--no-warnings", "--experimental-vm-modules", "inventory.mjs"}
			for _, pkg := range dependencyTSRoots(directory, consumer.families) {
				args = append(args, pkg)
			}
			command = exec.CommandContext(ctx, "node", args...)
			command.Dir = directory
			out, err = command.CombinedOutput()
			if err != nil {
				t.Fatalf("browser dependency closure: %v\n%s", err, out)
			}
			var dependencies []string
			if err := json.Unmarshal(out, &dependencies); err != nil {
				t.Fatalf("browser inventory: %v\n%s", err, out)
			}
			checkDependencyComponents(t, "browser ESM closure", dependencies, consumer.live)
		})
	}
}

const dependencyBrowserInventory = `import fs from 'node:fs';
import path from 'node:path';
import {SourceTextModule} from 'node:vm';
const roots = process.argv.slice(2);
const seen = new Set(), packages = new Set();
function entry(dir, subpath = '.') {
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'));
  packages.add(manifest.name);
  let target = manifest.exports;
  if (typeof target === 'object' && Object.keys(target).some(key => key.startsWith('.'))) target = target[subpath];
  while (target && typeof target === 'object') target = target.browser ?? target.import ?? target.default;
  if (typeof target !== 'string') throw new Error('No browser entry: ' + manifest.name + '/' + subpath);
  return path.resolve('browser', path.relative(process.cwd(), dir), target.replace(/\.ts$/, '.js'));
}
function resolve(specifier, source) {
  if (specifier.startsWith('.')) return path.resolve(path.dirname(source), specifier);
  const parts = specifier.split('/');
  const name = parts.slice(0, 2).join('/');
  const subpath = parts.length > 2 ? './' + parts.slice(2).join('/') : '.';
  if (name.startsWith('@nightseam/')) return entry(path.resolve(parts[1], 'ts'), subpath);
  if (name.startsWith('@example/')) return entry(path.resolve('api/ts', parts[1]), subpath);
  throw new Error('Unexpected browser import ' + specifier + ' from ' + source);
}
function visit(source) {
  if (seen.has(source)) return;
  seen.add(source);
  const module = new SourceTextModule(fs.readFileSync(source, 'utf8'), {identifier: source});
  for (const specifier of module.dependencySpecifiers) visit(resolve(specifier, source));
}
for (const root of roots) visit(entry(path.resolve(root)));
process.stdout.write(JSON.stringify([...packages].sort()));
`
