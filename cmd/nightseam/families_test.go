package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestFamiliesCompile: every family under testdata/families renders into
// packages that compile, in both languages, in one module — the generic
// ones, the one whose protocol has no operation, the one with an entity,
// a ref and constraints — and so do the handlers init scaffolds for each.
// What the goldens hold byte for byte, the compilers hold as code.
func TestFamiliesCompile(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeAll(t, directory, renderTool(t, familiesRoot))
	k, world, err := (&app{root: familiesRoot, module: module, scope: scope}).world()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range world.Names {
		stubs, err := k.Scaffold(world, name, "api/impl/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, stub := range stubs {
			writeFixture(t, directory, stub.Path, stub.Data)
		}
	}
	fixtureModule(t, directory, root)
	runFixture(t, directory, "go", "vet", "./...")
	copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
	copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
	copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
	copyFixtureTree(t, filepath.Join(root, "live/ts"), filepath.Join(directory, "live/ts"))
	paths := fixtureTypeScriptPaths(t, directory)
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths}, "include": []string{"api/ts/**/*.ts", "api/impl/**/*.ts", "runtime/ts/**/*.ts", "duplex/ts/**/*.ts", "tunnel/ts/**/*.ts", "live/ts/**/*.ts"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
}
