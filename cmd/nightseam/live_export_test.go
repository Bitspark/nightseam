package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGeneratedLiveExportRollback(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/box/model.json", []byte(`{"nightseam":2,"types":{"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]}}}`))
	writeFixture(t, directory, "api/contracts/rollback/model.json", []byte(`{"nightseam":2,"types":{"Flag":{"kind":"enum","values":["ok"]}}}`))
	writeFixture(t, directory, "api/contracts/rollback/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/rollback/live.json", []byte(`{"imports":["box"],"types":{
		"Call":{"kind":"callable","result":"integer"},
		"Use":{"kind":"callable","request":{"array":"Call"},"result":"integer"},
		"Make":{"kind":"callable","result":"Pair"},
		"Check":{"kind":"callable","request":"Checked"},
		"Checked":{"kind":"record","fields":[{"name":"first","type":"Call"},{"name":"last","type":"Flag"}]},
		"Bound":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"first","type":"T"},{"name":"last","type":"Call"}]},
		"Pair":{"kind":"record","fields":[{"name":"first","type":"Call"},{"name":"second","type":"Call"}]},
		"Failure":{"kind":"record","fields":[{"name":"first","type":"Call"},{"name":"last","type":"json"}]},
		"Generic":{"kind":"alias","type":{"array":{"apply":"box.Box","with":{"T":"Call"}}}},
		"Choice":{"kind":"union","tag":"kind","variants":{"pair":"Pair","none":{"empty":true}}}
	}}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/live-export-rollback", directory, ".go")
	copyProgram(t, "testdata/live-export-rollback", directory, ".ts")
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config := map[string]any{"compilerOptions": map[string]any{
		"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true,
		"skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "types": []string{"node"},
		"typeRoots": []string{filepath.ToSlash(filepath.Join(root, "node_modules/@types"))},
		"paths":     fixtureTypeScriptPaths(t, directory),
	}, "include": []string{"rollback.ts", "api/ts/**/*.ts"}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", data)
	t.Run("go", func(t *testing.T) { runFixture(t, directory, "go", "test", "-count=1", ".") })
	t.Run("typescript", func(t *testing.T) {
		runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
		runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "rollback.ts")
	})
}
