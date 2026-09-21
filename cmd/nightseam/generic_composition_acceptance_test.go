package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/oracle"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// This shared corpus combines both #363 forms. Its callable aliases retain
// the original Function constructor and ordered arguments; OtherFunction is
// the negative nominal control, despite having the same native signature.
func TestGenericCompositionCorpus(t *testing.T) {
	k, _ := toolKernel(load.Config{}, module, scope, "")
	root := filepath.Join(repositoryRoot(t), "conformance", "corpora", "generic-composition")
	world := k.Load(os.DirFS(root), "api/contracts")
	for _, name := range []string{"functions", "numbers", "texts", "holder", "compose-cell"} {
		t.Run(name, func(t *testing.T) {
			if _, err := k.Render(world, name); err != nil {
				t.Fatalf("required generic composition declaration: %v", err)
			}
		})
	}
}

// GEN-COMPOSE-ROUTES compares generated behavior, including retained live
// values, with an independently constructed declaration specialization.
// The Holder source route substitutes the model before either target resolves
// or renders it. The generic route renders the original model once, supplying
// both provider recipes only in the consumer. Callable aliases are separately
// lowered specialized bodies whose identity retains Function's origin and
// ordered arguments; their execution is compared in both mixed directions.
func TestGenericCompositionIndependentRoutes(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	k, _ := toolKernel(load.Config{}, module, scope, "")
	world := k.Load(os.DirFS(filepath.Join(root, "conformance/corpora/generic-composition")), "api/contracts")
	// The concrete source names live provider records directly, so its types
	// and operations belong in live.json. The generic source stays in protocol:
	// its supplied interpretations remain neutral until consumer construction.
	sourceRoot := t.TempDir()
	copyFixtureTree(t, filepath.Join(root, "conformance/corpora/generic-composition/api/contracts"), filepath.Join(sourceRoot, "api/contracts"))
	protocolPath := "api/contracts/holder/protocol.json"
	data, err := os.ReadFile(filepath.Join(sourceRoot, protocolPath))
	if err != nil {
		t.Fatal(err)
	}
	var protocol map[string]json.RawMessage
	if err := json.Unmarshal(data, &protocol); err != nil {
		t.Fatal(err)
	}
	live := map[string]json.RawMessage{}
	for _, key := range []string{"types", "server", "client"} {
		live[key] = protocol[key]
		delete(protocol, key)
	}
	for file, document := range map[string]map[string]json.RawMessage{protocolPath: protocol, "api/contracts/holder/live.json": live} {
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, sourceRoot, file, data)
	}
	sourceWorld := k.Load(os.DirFS(sourceRoot), "api/contracts")
	for _, provider := range []string{"numbers", "texts"} {
		bound := *sourceWorld
		bound.Families = make(map[string]*model.Family, len(sourceWorld.Families))
		for name, family := range sourceWorld.Families {
			bound.Families[name] = family
		}
		bound.Families["holder"] = oracle.Substitute(sourceWorld.Families["holder"], map[string]string{"S": provider})
		base := "bound/" + provider
		plain := kernel.New(
			golang.New(golang.Config{Module: module, Place: map[string]golang.Layout{"holder": {Protocol: base + "/go/{family}-protocol", Binding: base + "/go/{family}-binding", Client: base + "/go/{family}-client"}}}),
			typescript.New(typescript.Config{Scope: scope, Place: map[string]typescript.Layout{"holder": {Client: base + "/ts/{family}-client", Binding: base + "/ts/{family}-binding"}}}),
		)
		result, err := plain.Render(&bound, "holder")
		if err != nil {
			t.Fatalf("source specialization for %s: %v", provider, err)
		}
		writeAll(t, directory, result.Files)
	}
	for _, name := range []string{"functions", "numbers", "texts", "holder", "compose-cell"} {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(t, directory, result.Files)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	copyProgram(t, "testdata/generic-composition-routes", directory, ".go")
	copyProgram(t, "testdata/generic-composition-routes", directory, ".ts")
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}},
		"include":         []string{"api/ts/**/*.ts", "bound/*/ts/holder-client/**/*.ts", "routes.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "routes.ts")
	runFixture(t, directory, "go", "test", "-p", "2", "-parallel", "2", "-count=1", "-v", "./...")
}
