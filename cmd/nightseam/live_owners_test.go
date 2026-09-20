package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// The socket scenario covers ordinary generated methods. This companion
// consumer holds the public generic conversion callbacks to the same owner
// transaction, including an import failure inside an applied live argument.
func TestGeneratedLiveOwners(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	for _, family := range []string{"worker", "boxes", "combinator"} {
		copyFixtureTree(t, filepath.Join(root, "cmd/nightseam/testdata/families/api/contracts", family), filepath.Join(directory, "api/contracts", family))
	}
	copyFixtureTree(t, filepath.Join(root, "cmd/nightseam/testdata/live-owners/api/contracts/owners"), filepath.Join(directory, "api/contracts/owners"))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/live-owners", directory, ".go")
	copyProgram(t, "testdata/live-owners", directory, ".ts")
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config := map[string]any{"compilerOptions": map[string]any{
		"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true,
		"skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "types": []string{"node"},
		"typeRoots": []string{filepath.ToSlash(filepath.Join(root, "node_modules/@types"))},
		"paths":     map[string]any{"@example/*": []string{"./api/ts/*/src/index.ts"}, "@nightseam/runtime": []string{"./runtime/ts/src/index.ts"}, "@nightseam/duplex": []string{"./duplex/ts/src/index.ts"}, "@nightseam/tunnel": []string{"./tunnel/ts/src/index.ts"}, "@nightseam/live": []string{"./live/ts/src/index.ts"}},
	}, "include": []string{"owners.ts", "api/ts/**/*.ts"}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", data)
	t.Run("go", func(t *testing.T) { runFixture(t, directory, "go", "test", "-count=1", ".") })
	t.Run("typescript", func(t *testing.T) {
		runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
		runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "owners.ts")
	})
}
