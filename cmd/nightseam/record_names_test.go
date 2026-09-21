package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGeneratedRecordedEventParameterNames(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/names/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/names/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"Target"},{"name":"Log"},{"name":"Setup"},{"name":"Events"}],"types":{"Payload":{"kind":"record","fields":[{"name":"target","type":"Target"},{"name":"log","type":"Log"},{"name":"setup","type":"Setup"},{"name":"events","type":"Events"}]}},"server":{"events":{"changed":{"type":"Payload"}}}}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	runFixture(t, directory, "go", "test", "./...")
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)}, "include": []string{"api/ts/**/*.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
}
