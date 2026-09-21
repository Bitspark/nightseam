package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedRecordedEvents(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	source := filepath.Join(root, "cmd/nightseam/testdata/record-follow")
	copyFixtureTree(t, filepath.Join(source, "contracts"), filepath.Join(directory, "api/contracts"))
	table, err := os.ReadFile(filepath.Join(root, "conformance/tables/recorded-wire.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "recorded-wire.json", table)
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	t.Run("go", func(t *testing.T) {
		fixtureModule(t, directory, root)
		program, err := os.ReadFile(filepath.Join(source, "record_test.go.txt"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "record_test.go", program)
		liveProgram, err := os.ReadFile(filepath.Join(source, "live_record_test.go.txt"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "live_record_test.go", liveProgram)
		setupProgram, err := os.ReadFile(filepath.Join(source, "setup_record_test.go.txt"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "setup_record_test.go", setupProgram)
		runFixture(t, directory, "go", "test", "-count=1", ".")
	})
	t.Run("typescript", func(t *testing.T) {
		for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
			copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
		}
		config, err := json.Marshal(map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}}, "include": []string{"api/ts/**/*.ts", "record.ts"}})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "tsconfig.json", config)
		writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
		writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
		program, err := os.ReadFile(filepath.Join(source, "record.ts"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "record.ts", program)
		liveProgram, err := os.ReadFile(filepath.Join(source, "live_record.ts"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "live_record.ts", liveProgram)
		setupProgram, err := os.ReadFile(filepath.Join(source, "setup_record.ts"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "setup_record.ts", setupProgram)
		runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
		runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "record.ts")
	})
}
