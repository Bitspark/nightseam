package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Each provider is bound into the same generated Holder. Node opens two
// simultaneous physical sockets, and both generated model roles are exercised
// in each language before the same peers run acquisition-rollback checks.
func TestLiveFamilyDrawSockets(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := liveFamilyDrawFixture(t)
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/live-family-draws", directory, ".go")
	copyProgram(t, "testdata/live-family-draws", directory, ".ts")
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}},
		"include":         []string{"api/ts/**/*.ts", "sockets.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "go", "test", "-p", "2", "-parallel", "4", "-count=1", "-v", ".")
}
