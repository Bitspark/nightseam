package typescript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/analysis"
	checks "github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func genericCallableWorld() analysis.World {
	fixtures := map[string]map[string]string{
		"boxes":    {"model.json": `{"nightseam":2,"types":{"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]}}}`},
		"provider": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(``)},
		"ordinary": {"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"label","type":"string"}]}}}`},
		"factory": {
			"model.json":    `{"nightseam":2,"types":{"Job":{"kind":"record","fields":[]}}}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}]`),
			"live.json":     `{"types":{"Handler":{"kind":"callable","request":"S.Envelope","result":"S.Envelope"}}}`,
		},
		"exposure": {
			"model.json":    `{"nightseam":2,"types":{"Job":{"kind":"record","fields":[]}}}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json":     `{"imports":["factory","provider","ordinary"],"types":{"Uses":{"kind":"record","fields":[{"name":"run","type":{"apply":"factory.Handler","with":{"S":"provider"}}},{"name":"tag","type":"ordinary.Payload"}]}}}`,
		},
		"worker": {
			"model.json":    `{"nightseam":2,"types":{"Empty":{"kind":"record","fields":[]}}}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json": `{"imports":["boxes"],"types":{
				"Function":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"},
				"Other":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"},
				"Factory":{"kind":"callable","parameters":[{"name":"T"}],"request":"T","result":{"apply":"Function","with":{"A":"T","B":"T"}}},
				"Cell":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"get","type":{"apply":"Function","with":{"A":"Empty","B":"T"}}},{"name":"replace","type":{"apply":"Function","with":{"A":"T","B":"T"}}}]},
				"Job":{"kind":"record","fields":[{"name":"run","type":{"apply":"Function","with":{"A":"integer","B":"integer"}}}]},
				"Closed":{"kind":"alias","type":{"apply":"Function","with":{"A":"integer","B":"integer"}}}
			},"server":{"methods":{"cell":{"result":{"apply":"Cell","with":{"T":{"apply":"Function","with":{"A":"integer","B":"integer"}}}}},"box":{"request":{"apply":"boxes.Box","with":{"T":{"apply":"Function","with":{"A":"integer","B":"integer"}}}},"result":{"apply":"boxes.Box","with":{"T":{"apply":"Function","with":{"A":"integer","B":"integer"}}}}}}}}`,
		},
		"specialized": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json":     `{"imports":["worker"],"types":{"AppliedClosed":{"kind":"alias","type":{"apply":"worker.Function","with":{"A":"integer","B":"integer"}}},"Job":{"kind":"record","fields":[{"name":"run","type":"AppliedClosed"}]}}}`,
		},
		"scoped": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"live"}]`),
			"live.json":     `{"types":{"Captured":{"kind":"callable","parameters":[{"name":"A"}],"request":"S.Job","result":"A"}}}`,
		},
	}
	world := analysis.World(modeltest.World(fixtures))
	for name, files := range fixtures {
		if source, ok := files["live.json"]; ok {
			var err error
			world[name].Live, err = model.DecodeLive(model.LiveFile, json.RawMessage(source))
			if err != nil {
				panic(err)
			}
		}
	}
	for name, family := range builtin.Families() {
		world[name] = family
	}
	return world
}

func TestConcreteCallableFamilyArgumentsImportTheirValues(t *testing.T) {
	world := genericCallableWorld()
	for _, name := range []string{"exposure", "factory"} {
		files, err := New(Config{Scope: "@example"}).Render(render.Build(analysis.Resolve(world, name)))
		if err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for _, file := range files {
			output.Write(file.Data)
		}
		got := output.String()
		if name == "exposure" && !strings.Contains(got, `import * as live_provider from "@example/provider-client"`) {
			t.Fatal("concrete callable family argument has no value import")
		}
		if strings.Contains(got, "import * as live_ordinary") || name == "factory" && strings.Contains(got, "provider-client") {
			t.Fatal("value imports escaped the concrete callable application")
		}
	}
}

func TestGenericCallableHelpersRetainCompleteRecipes(t *testing.T) {
	files, err := New(Config{Scope: "@example"}).Render(render.Build(analysis.Resolve(genericCallableWorld(), "worker")))
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for _, file := range files {
		output.Write(file.Data)
	}
	for _, want := range []string{
		"export type Function<A = unknown, B = unknown>",
		"export function contractFunction<A = unknown, B = unknown>(slot_A: ValueAdapter<A>, slot_B: ValueAdapter<B>): DeclarationIdentity",
		"export function exportFunction<A = unknown, B = unknown>(owner: LiveOwner, value: Function<A, B>, slot_A: ValueAdapter<A>, slot_B: ValueAdapter<B>)",
		"slot_A.import(owner, request)", "slot_B.export(owner, (result) as B)",
		"export function exportCellUnchecked<T = unknown>(owner: LiveOwner, value: Cell<T>, slot_T: ValueAdapter<T>)",
		"export function contractClosed(): DeclarationIdentity", "export function exportClosed(owner: LiveOwner, value: Closed): unknown",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	_, specialized, ok := strings.Cut(output.String(), "export function exportClosed(")
	if !ok {
		t.Fatal("missing specialized body")
	}
	specialized, _, _ = strings.Cut(specialized, "export function importClosed(")
	if strings.Contains(specialized, "exportFunction(") || !strings.Contains(specialized, "owner.export(identity.path") {
		t.Fatal("specialized alias delegated its callable body to the generic constructor")
	}
}

func TestGeneratedTypeScriptGenericCallables(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and invokes generated TypeScript callables")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := map[string][]string{"@bitspark/bitwire": {filepath.ToSlash(filepath.Join(root, "node_modules/@bitspark/bitwire/dist/index.d.ts"))}}
	entries := map[string]string{}
	for _, component := range []string{"duplex", "runtime", "live"} {
		path := filepath.ToSlash(filepath.Join(root, component, "ts/src/index.ts"))
		paths["@nightseam/"+component] = []string{path}
		entries["@nightseam/"+component] = path
	}
	target := New(Config{Scope: "@example"})
	world := genericCallableWorld()
	for name := range world {
		resolved := analysis.Resolve(world, name)
		if diagnostics := checks.Family(resolved); len(diagnostics) != 0 {
			t.Fatalf("%s: %v", name, diagnostics)
		}
		files, err := target.Render(render.Build(resolved))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
		for _, suffix := range []string{"-client", "-binding"} {
			base := "@example/" + name + suffix
			path := filepath.ToSlash(filepath.Join(directory, "api/ts", name+suffix, "src/index.ts"))
			paths[base], entries[base] = []string{path}, path
		}
		path := filepath.ToSlash(filepath.Join(directory, "api/ts", name+"-client", "src/types.ts"))
		paths["@example/"+name+"-client/types"], entries["@example/"+name+"-client/types"] = []string{path}, path
	}
	config, err := json.Marshal(map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths, "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}}, "include": []string{"api/ts/**/*.ts", "callable.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	write("tsconfig.json", config)
	write("package.json", []byte(`{"type":"module"}`))
	mapping, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	write("loader.mjs", []byte(fmt.Sprintf("import {pathToFileURL} from 'node:url'; const entries=%s; export async function resolve(specifier, context, next){if(entries[specifier])return {url:pathToFileURL(entries[specifier]).href,shortCircuit:true};return next(specifier,context);}", mapping)))
	write("callable.ts", []byte(genericCallableProgram))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for _, args := range [][]string{{filepath.Join(root, "node_modules/typescript/bin/tsc"), "--project", "tsconfig.json"}, {"--loader", "./loader.mjs", "callable.ts"}} {
		command := exec.CommandContext(ctx, "node", args...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("generated generic callables: %v\n%s", err, output)
		}
	}
}

const genericCallableProgram = `import assert from 'node:assert/strict';
import {pipe} from '@nightseam/duplex';
import {DuplexPeer,DuplexError,jsonAdapter,type ValueAdapter} from '@nightseam/runtime';
import {LiveOwner,liveOver,valueEnvironment} from '@nightseam/live';
import * as worker from '@example/worker-client/types';
import * as scoped from '@example/scoped-client/types';
import * as specialized from '@example/specialized-client/types';
import * as exposure from '@example/exposure-client/types';
import {toWire,fromWire} from '@example/worker-binding';
const number=jsonAdapter<number>({type:'integer',validate:worker.validateWire});
const text=jsonAdapter<string>({type:'string',validate:worker.validateWire});
assert.notDeepEqual(worker.contractFunction(number,text),worker.contractFunction(text,number));
assert.notDeepEqual(worker.contractFunction(number,number),worker.contractOther(number,number));
assert.deepEqual(worker.contractClosed(),worker.contractFunction(number,number));
assert.deepEqual(specialized.contractAppliedClosed(),worker.contractFunction(number,number));
assert.throws(()=>worker.adapterFunction({...number,import:undefined} as unknown as ValueAdapter<number>,number),/complete/);
const interpretation=worker.adapterFunction(number,number);
await Promise.all([0,1].map(async index=>{
 const [a,b]=pipe();const pa=new DuplexPeer({role:'client'}),pb=new DuplexPeer({role:'server'});const sa=liveOver(pa),sb=liveOver(pb);await Promise.all([pa.attach(a),pb.attach(b)]);
 try{
  const ownerA=sa.owner(),ownerB=sb.owner();
  const concrete=exposure.adapterUses();const interpreted=concrete.import(ownerB,concrete.export(ownerA,{run:async value=>value,tag:{label:'data-only provider'}}));assert.equal(typeof interpreted.run,'function');
  const raw=interpretation.export(ownerA,async value=>value+index+1);const fn=interpretation.import(ownerB,raw);assert.equal(await fn(4),5+index);
  const closed=worker.importClosed(ownerB,raw);assert.equal(await closed(4),5+index);const external=specialized.importAppliedClosed(ownerB,raw);assert.equal(await external(4),5+index);const general=worker.importFunction(ownerB,specialized.exportAppliedClosed(ownerA,async value=>value*2),number,number);assert.equal(await general(4),8);
  assert.throws(()=>worker.importFunction(ownerB,raw,text,text),(error:unknown)=>error instanceof DuplexError&&error.code==='contract_mismatch');
  const factory=worker.adapterFactory(interpretation);const made=factory.import(ownerB,factory.export(ownerA,async value=>async()=>value));const retained=await made(fn);const recovered=await retained(fn);assert.equal(await recovered(9),10+index);
  const captured=scoped.adapterCaptured<worker.Family,number>(worker.family,number);const capturedFn=captured.import(ownerB,captured.export(ownerA,async job=>job.run(6)));assert.equal(await capturedFn({run:fn}),7+index);
  let current:worker.Function<number,number>=async value=>value+1;
  const cellAdapter=worker.adapterCell(interpretation);const cell=cellAdapter.import(ownerB,cellAdapter.export(ownerA,{get:async()=>current,replace:async value=>{const previous=current;current=value;return previous;}}));
  const old=await cell.get({});assert.equal(await old(5),6);await cell.replace(async value=>value+10);const changed=await cell.get({});assert.equal(await changed(5),15);assert.equal(await old(5),6);
  const beforeFailure=sa.counts();assert.throws(()=>cellAdapter.export(ownerA,{get:async()=>current,replace:undefined as unknown as worker.Cell<worker.Function<number,number>>['replace']}),/function/);assert.deepEqual(sa.counts(),beforeFailure);assert.equal(await fn(4),5+index);
  const wire=toWire(()=>({methods:{cell:()=>({get:async()=>current,replace:async value=>{const previous=current;current=value;return previous;}}),box:value=>value},events:{}}),{valueEnvironment:valueEnvironment(sa)});
  const remote=(await fromWire(wire,{valueEnvironment:valueEnvironment(sa)}))({methods:{},events:{}});const localCell=await remote.methods.cell({});const late=await localCell.get({});assert.equal(await late(1),11);wire.close();
  const before=sa.counts();const broken=worker.adapterFunction(interpretation,number);const bad=broken.import(ownerB,broken.export(ownerA,async value=>{await value(1);return 3;}));assert.equal(await bad(async value=>value+2),3);assert.ok(sa.counts().exports>=before.exports);
 }finally{pa.close();pb.close();}
}));
`
