package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// GEN-BIND-CONSTRAINT: object obligations survive an alias and fail in both
// generated roles before a model factory or operation environment is used.
func TestDrawRequestConstraintBeforeModel(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	copyFixtureTree(t, "testdata/live-family-draws/api/contracts/holder", filepath.Join(directory, "api/contracts/holder"))
	protocolPath := filepath.Join(directory, "api/contracts/holder/protocol.json")
	data, err := os.ReadFile(protocolPath)
	if err != nil {
		t.Fatal(err)
	}
	var protocol map[string]any
	if err = json.Unmarshal(data, &protocol); err != nil {
		t.Fatal(err)
	}
	protocol["types"].(map[string]any)["Request"] = map[string]any{"kind": "alias", "type": "S.Job"}
	protocol["server"].(map[string]any)["methods"].(map[string]any)["direct"] = map[string]any{"request": "Request", "result": "S.Job"}
	data, err = json.Marshal(protocol)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "api/contracts/holder/protocol.json", data)
	writeFixture(t, directory, "api/contracts/choices/model.json", []byte(`{"nightseam":2,"types":{"Job":{"kind":"enum","values":["one"]},"Progress":{"kind":"record","fields":[]}}}`))
	writeFixture(t, directory, "api/contracts/choices/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/choices/live.json", []byte(`{}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "constraint_test.go", []byte(`package constraint_test
import (
	duplex "github.com/Bitspark/nightseam/duplex/go"
 "context"
 "strings"
 "testing"
 choices "example.test/generated/api/go/choices-protocol"
 binding "example.test/generated/api/go/holder-binding"
 client "example.test/generated/api/go/holder-client"
 holder "example.test/generated/api/go/holder-protocol"
 "github.com/Bitspark/nightseam/runtime/go"

)
func TestBeforeModel(t *testing.T){
 calls:=0
 _,err:=binding.ToWire(func(holder.Client[choices.Job,choices.Progress])(holder.Server[choices.Job,choices.Progress],error){calls++;return holder.Server[choices.Job,choices.Progress]{},nil},runtime.AdapterContext{},choices.AdapterJob(),choices.AdapterProgress())
 if err==nil||!strings.Contains(err.Error(),"object")||calls!=0{t.Fatalf("ToWire effects=%d error=%v",calls,err)}
 wire,peer,err:=runtime.NewWirePair(runtime.Options{});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"");defer peer.Close(duplex.CodeNormal,"")
 _,err=client.FromWire(context.Background(),wire,runtime.AdapterContext{},choices.AdapterJob(),choices.AdapterProgress())
 if err==nil||!strings.Contains(err.Error(),"object"){t.Fatalf("FromWire: %v",err)}
}`))
	runFixture(t, directory, "go", "test", "-p", "2", "-count=1", ".")
	for _, component := range []string{"runtime", "duplex", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "constraint.ts", []byte(`import assert from 'node:assert/strict';
import * as choices from '@example/choices-client/types';
import {toWire} from '@example/holder-binding';
import {fromWire} from '@example/holder-client';
import type { Endpoint } from '@bitspark/bitwire';
let calls=0;
assert.throws(()=>toWire<choices.Family>(()=>{calls++;return {methods:{exchange(value){return value;},direct(value){return value;}},events:{}};},{},choices.family),/object/);
await assert.rejects(fromWire<choices.Family>({} as Endpoint,{},choices.family),/object/);
assert.equal(calls,0);
`))
	config, err := json.Marshal(map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}}, "include": []string{"api/ts/**/*.ts", "constraint.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "constraint.ts")
}
