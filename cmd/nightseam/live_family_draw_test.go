package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GEN-BIND-GENERATION: one protocol-only consumer draws two live associated
// types from either provider without importing either concrete implementation.
func liveFamilyDrawFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	copyFixtureTree(t, "testdata/live-family-draws/api/contracts", filepath.Join(directory, "api/contracts"))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("GEN-BIND-GENERATION: %v\n%s\n%s", err, out, errs)
	}
	return directory
}

func TestLiveFamilyDrawGolden(t *testing.T) {
	holdGolden(t, "testdata/golden-live-family-draws", renderTool(t, "testdata/live-family-draws"))
}

// GEN-BIND-CONSTRAINT: a consumer derives independently of its providers;
// unrelated live declarations do not become obligations on the supplied slot.
func TestLiveFamilyDrawIndependentGeneration(t *testing.T) {
	for _, unrelated := range []bool{false, true} {
		directory := t.TempDir()
		copyFixtureTree(t, "testdata/live-family-draws/api/contracts/holder", filepath.Join(directory, "api/contracts/holder"))
		if unrelated {
			writeFixture(t, directory, "api/contracts/functions/model.json", []byte(`{"nightseam":2}`))
			writeFixture(t, directory, "api/contracts/functions/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
			writeFixture(t, directory, "api/contracts/functions/live.json", []byte(`{"types":{"Run":{"kind":"callable","request":"integer","result":"integer"}}}`))
		}
		if out, errs, err := run(t, directory, "generate"); err != nil {
			t.Fatalf("GEN-BIND-CONSTRAINT unrelated=%v: %v\n%s\n%s", unrelated, err, out, errs)
		}
	}
}

func TestLiveFamilyDrawGeneration(t *testing.T) {
	directory := liveFamilyDrawFixture(t)
	for _, relative := range []string{"api/go/holder-protocol/types_generated.go", "api/go/holder-binding/binding_generated.go", "api/go/holder-client/client_generated.go", "api/ts/holder-client/src/types.ts", "api/ts/holder-client/src/index.ts", "api/ts/holder-client/package.json", "api/ts/holder-binding/src/index.ts", "api/ts/holder-binding/package.json"} {
		data, err := os.ReadFile(filepath.Join(directory, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"@nightseam/live", "nightseam/live/go", "first-protocol", "second-protocol", "@example/first-client", "@example/second-client"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("GEN-BIND-GENERATION: %s imports %s", relative, forbidden)
			}
		}
	}
}

func TestLiveFamilyDrawTypeScript(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := liveFamilyDrawFixture(t)
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}},
		"include":         []string{"api/ts/**/*.ts", "family-draw.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "family-draw.ts", []byte(tsLiveFamilyDrawConstruction))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "family-draw.ts")
}

const tsLiveFamilyDrawConstruction = `import assert from 'node:assert/strict';
import * as first from '@example/first-client/types';
import * as second from '@example/second-client/types';
import {toWire, adapterHeld, type Held} from '@example/holder-binding';
import {familyTypeAdapter, type ValueEnvironment, type FamilyBinding} from '@nightseam/runtime';

// GEN-BIND-ASSOCIATION and GEN-BIND-MISSING check construction before effects.
let constructions = 0;
const model = () => { constructions++; return {methods: {exchange(value: Held<first.Family>) {return value;}}, events:{}}; };
const unused = () => { throw new Error('construction touched an operation environment'); };
const environment: ValueEnvironment = {select:unused, child:unused, export:unused, import:unused, publish:unused};
const missing = {...first.family, types:{Job:first.family.types.Job}} as unknown as FamilyBinding<first.Family,'Job'|'Progress'>;
const mixed = {...first.family, types:{...first.family.types, Progress:second.family.types.Progress}};
const wrongMember = {...first.family, types:{...first.family.types, Job:first.family.types.Progress}} as unknown as FamilyBinding<first.Family,'Job'|'Progress'>;
for (const binding of [missing, mixed, wrongMember]) {
  assert.throws(() => toWire(model, {valueEnvironment:environment}, binding), /interpretation/);
  assert.throws(() => adapterHeld<first.Family>(binding), /interpretation/);
}
const broken = {...first.family, types:{...first.family.types, Job:{...first.family.types.Job, import:undefined}}} as unknown as FamilyBinding<first.Family,'Job'|'Progress'>;
assert.throws(() => toWire(model, {valueEnvironment:environment}, broken), /interpretation/);
assert.equal(constructions, 0);
assert.equal(familyTypeAdapter<first.Family, 'Job'>(first.family, 'Job'), first.family.types.Job);
assert.equal(adapterHeld<first.Family>(first.family).needsContext, true);
assert.equal(adapterHeld<second.Family>(second.family).needsContext, true);
const wire = toWire(model, {valueEnvironment:environment}, first.family);
assert.equal(constructions, 1);
wire.close();
`
