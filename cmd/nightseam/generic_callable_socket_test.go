package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericCallableGolden(t *testing.T) {
	holdGolden(t, "testdata/golden-generic-callables", renderTool(t, "testdata/generic-callables"))
}

// GEN-CALL-SOCKETS holds the generated Cell model, nested callable conversion,
// retained values and acquisition rollback in both generated language roles.
func TestGenericCallableSockets(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	copyFixtureTree(t, "testdata/generic-callables/api/contracts", filepath.Join(directory, "api/contracts"))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("GEN-CALL-SOCKETS generation: %v\n%s\n%s", err, out, errs)
	}
	for _, name := range []string{"api/go/cell-protocol/types_generated.go", "api/go/cell-binding/binding_generated.go", "api/ts/cell-client/src/types.ts", "api/ts/cell-binding/src/index.ts", "api/ts/cell-client/package.json"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"@nightseam/live", "nightseam/live/go", "functions-protocol", "@example/functions-client"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("neutral Cell imports %s in %s", forbidden, name)
			}
		}
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/generic-callables", directory, ".go")
	copyProgram(t, "testdata/generic-callables", directory, ".ts")
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
	runFixture(t, directory, "go", "test", "-p", "2", "-parallel", "2", "-count=1", "-v", ".")
}
