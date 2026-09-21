package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// GEN-COMPOSE-FAILURES complements the carrier product with paired generated
// consumers that observe an allocation before a failed nested walk. The guard
// is a consumer-owned, mutable synthetic policy, as in #355; it does not claim
// #356's exhaustive binding or authentication/expiry validation.
func TestGenericCompositionFailures(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	copyFixtureTree(t, filepath.Join(root, "conformance/corpora/generic-composition/api/contracts"), filepath.Join(directory, "api/contracts"))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("GEN-COMPOSE-FAILURES generation: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/generic-composition-failures", directory, ".go")
	copyProgram(t, "testdata/generic-composition-failures", directory, ".ts")
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}},
		"include":         []string{"api/ts/**/*.ts", "failures.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "go", "test", "-count=1", "-v", ".")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "failures.ts")
}
