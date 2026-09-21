package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The source route substitutes declarations before generation. It never uses
// the generic adapter artifact, while both routes run the same Cell<T> model.
func TestTypeScriptValueAdapterConstructionDiagram(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/values/model.json", []byte(`{"nightseam":2,"types":{"Count":{"kind":"alias","type":"integer"},"Page":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"array":"T"}}]}}}`))
	writeFixture(t, directory, "api/contracts/values/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/values/live.json", []byte(`{"types":{"Unary":{"kind":"callable","request":"Count","result":"Count"},"Factory":{"kind":"callable","request":"Unary","result":"Unary"},"Bundle":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"},{"name":"run","type":"Unary"}]}}}`))
	body := func(expression string) string {
		return fmt.Sprintf(`"types":{"Value":{"kind":"record","fields":[{"name":"value","type":%s}]}},"server":{"methods":{"put":{"request":"Value","result":"integer"},"get":{"result":%s}},"events":{"changed":{"type":"Value"}}},"client":{"methods":{"mirror":{"request":"Value","result":%s}},"events":{"noted":{"type":"Value"}}}`, expression, expression, expression)
	}
	writeFixture(t, directory, "api/contracts/cell/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/cell/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],`+body(`"T"`)+`}`))
	var imports, routes, runs strings.Builder
	for _, slot := range []struct {
		name, expression, native, adapter, input, observe string
		live                                              bool
	}{
		{"Scalar", `"values.Count"`, "number", "values.adapterCount()", "42", "return value", false},
		{"Unary", `"values.Unary"`, "values.Unary", "values.adapterUnary()", "unary", "return value(41, {signal, owner})", true},
		{"Factory", `"values.Factory"`, "values.Factory", "values.adapterFactory()", "factory", "const callback = await value(async n => { state.arguments++; return n + 1; }, {signal, owner}); return callback(41, {signal})", true},
		{"Nested", `{"apply":"values.Page","with":{"T":{"apply":"values.Bundle","with":{"T":"values.Unary"}}}}`, "values.Page<values.Bundle<values.Unary>>", "values.adapterPage(values.adapterBundle(values.adapterUnary()))", "nested", "return (await value.items[0]!.value(20, {signal, owner})) + (await value.items[0]!.run(20, {signal, owner}))", true},
	} {
		name := "cell" + strings.ToLower(slot.name)
		path := "api/contracts/" + name + "/"
		writeFixture(t, directory, path+"model.json", []byte(`{"nightseam":2}`))
		if slot.live {
			writeFixture(t, directory, path+"protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
			writeFixture(t, directory, path+"live.json", []byte(`{"imports":["values"],`+body(slot.expression)+`}`))
		} else {
			writeFixture(t, directory, path+"protocol.json", []byte(`{"profile":"nightseam.duplex/1","imports":["values"],`+body(slot.expression)+`}`))
		}
		fmt.Fprintf(&imports, "import * as client%s from '@example/%s-client';\nimport * as binding%s from '@example/%s-binding';\n", slot.name, name, slot.name, name)
		fmt.Fprintf(&routes, `
const caller%s: Equal<genericClient.Caller<%s>, client%s.Caller> = true;
const context%s: Equal<Parameters<genericBinding.Handler<%s>['put']>[2], Parameters<binding%s.Handler['put']>[2]> = true;
async function source%s(state: State): Promise<Boundary<%s>> {
 const model = cell<%s>(state);
 const events = listeners<%s>(state);
 const [a,b] = pipe();
 const [peer,client] = await Promise.all([
  binding%s.serve(b, {}, model, events.server),
  client%s.Client.attach(a, {}, events.reverse, events.client),
 ]);
 return {peer, client, remote: new binding%s.Remote(peer), changed:events.changed, noted:events.noted};
}
`, slot.name, slot.native, slot.name, slot.name, slot.native, slot.name, slot.name, slot.native, slot.native, slot.native, slot.name, slot.name, slot.name)
		fmt.Fprintf(&runs, `
await compare<%s>(%q, %s, source%s, state => {
 const unary: values.Unary = async n => { state.callbacks++; return n+1; };
 const factory: values.Factory = async callback => { state.factories++; return async n => { state.callbacks++; return callback(n, {signal}); }; };
 const nested: values.Page<values.Bundle<values.Unary>> = {items:[{value:unary, run:unary}]};
 return {input: %s, observe: async (value, owner) => { %s; }};
});
`, slot.native, slot.name, slot.adapter, slot.name, slot.input, slot.observe)
	}
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)},
		"include":         []string{"api/ts/**/*.ts", "diagram.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "diagram.ts", []byte(imports.String()+tsAdapterDiagramProgram+routes.String()+runs.String()+tsAdapterDiagramRollback))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "diagram.ts")
}

const tsAdapterDiagramProgram = `import {pipe} from '@nightseam/duplex';
import {DuplexPeer, DuplexError, type CallOptions, type EmitOptions} from '@nightseam/runtime';
import {scopeOf, liveOver, type LiveOwner, type ValueAdapter} from '@nightseam/live';
import * as values from '@example/values-client';
import * as genericClient from '@example/cell-client';
import * as genericBinding from '@example/cell-binding';

type Equal<A,B>=(<T>()=>T extends A?1:2) extends (<T>()=>T extends B?1:2)?true:false;
function check(condition: unknown, message: string): asserts condition { if (!condition) throw new Error(message); }
function equal(a:unknown,b:unknown,message:string) { check(JSON.stringify(a)===JSON.stringify(b),message+': '+JSON.stringify(a)+' / '+JSON.stringify(b)); }
const signal = AbortSignal.timeout(15000);
type Options = CallOptions & {owner?:LiveOwner};
type EventOptions = EmitOptions & {owner?:LiveOwner};
type State = {transitions:string[]; callbacks:number; factories:number; arguments:number};
const initial = ():State => ({transitions:[],callbacks:0,factories:0,arguments:0});

// This stateful model is unchanged for every slot and for both construction routes.
function cell<T>(state:State) {
 let value:T; let count=0;
 return {
  put(params:{value:T}) { value=params.value; count++; state.transitions.push('put:'+count); return count; },
  get() { state.transitions.push('get:'+count); return value; },
 };
}
function pending<T>() {
 let resolve!:(value:T)=>void;
 const value=new Promise<T>((done,reject)=>{resolve=done;signal.addEventListener('abort',()=>reject(signal.reason),{once:true});});
 return {value,resolve};
}
function listeners<T>(state:State) {
 const changed=pending<T>(),noted=pending<T>();
 return {
  changed:changed.value, noted:noted.value,
  client:{changed(params:{value:T}){state.transitions.push('changed');changed.resolve(params.value);}},
  server:{noted(params:{value:T}){state.transitions.push('noted');noted.resolve(params.value);}},
  reverse:{mirror(params:{value:T}){state.transitions.push('mirror');return params.value;}},
 };
}
interface Boundary<T> {
 peer:DuplexPeer;
 client:{peer:DuplexPeer;put(params:{value:T},options?:Options):Promise<number>;get(options?:Options):Promise<T>;emitNoted(params:{value:T},options?:EventOptions):Promise<void>;close():void};
 remote:{mirror(params:{value:T},options?:Options):Promise<T>;emitChanged(params:{value:T},options?:EventOptions):Promise<void>};
 changed:Promise<T>;noted:Promise<T>;
}
async function derived<T>(adapter:ValueAdapter<T>,state:State):Promise<Boundary<T>> {
 const model=cell<T>(state),events=listeners<T>(state),[a,b]=pipe();
 const [peer,client]=await Promise.all([
  genericBinding.serve(b,adapter,{},model,events.server),
  genericClient.Client.attach(a,adapter,{},events.reverse,events.client),
 ]);
 return {peer,client,remote:new genericBinding.Remote(peer,adapter),changed:events.changed,noted:events.noted};
}
type Input<T> = {input:T;observe:(value:T,owner:LiveOwner|undefined)=>Promise<number>};
async function record<T>(make:()=>Promise<Boundary<T>>,live:boolean,state:State,value:Input<T>) {
 const boundary=await make(),{client,remote,peer}=boundary;
 const near=scopeOf(client.peer),far=scopeOf(peer);
 check(Boolean(near)===live&&Boolean(far)===live,'a scalar route opened a live scope or a live route omitted one');
 const nearOwner=near?.owner().child(),farOwner=far?.owner().child();
 const owners=[nearOwner,farOwner],scopes=[near,far].filter(s=>s!==undefined);
 const snapshots:unknown[]=[];
 const snapshot=(name:string,result:unknown)=>snapshots.push({name,result,state:structuredClone(state),counts:scopes.map(scope=>scope.counts())});
 try {
  const first=await client.put({value:value.input},{signal,owner:nearOwner}); check(first===1,'first state transition');snapshot('first',first);
  const second=await client.put({value:value.input},{signal,owner:nearOwner});check(second===2,'second state transition');snapshot('second',second);
  const got=await client.get({signal,owner:nearOwner});check(await value.observe(got,nearOwner)===42,'get behavior');snapshot('get',42);
  const mirrored=await remote.mirror({value:value.input},{signal,owner:farOwner});check(await value.observe(mirrored,farOwner)===42,'reverse behavior');snapshot('mirror',42);
  await remote.emitChanged({value:value.input},{owner:farOwner});check(await value.observe(await boundary.changed,nearOwner)===42,'changed behavior');snapshot('changed',42);
  await client.emitNoted({value:value.input},{owner:nearOwner});check(await value.observe(await boundary.noted,farOwner)===42,'noted behavior');snapshot('noted',42);
  equal(state.transitions,['put:1','put:2','get:2','mirror','changed','noted'],'handler and event multiplicity');
  if(live)check(scopes.every(scope=>scope.counts().exports>0&&scope.counts().imports>0),'boundary never acquired both live directions');
  for(const owner of owners)owner?.release();
  for(const scope of scopes)scope.owner().release();
  const deadline=Date.now()+5000;
  while(scopes.some(scope=>scope.counts().exports||scope.counts().imports)){check(Date.now()<deadline,'explicit release leaked bindings');await new Promise(resolve=>setTimeout(resolve,1));}
  snapshot('released',null);
  return snapshots;
 } finally {client.close();peer.close();}
}
async function compare<T>(name:string,adapter:ValueAdapter<T>,source:(state:State)=>Promise<Boundary<T>>,makeInput:(state:State)=>Input<T>) {
 const genericState=initial(),sourceState=initial();
 const generic=await record(()=>derived(adapter,genericState),adapter.live,genericState,makeInput(genericState));
 const concrete=await record(()=>source(sourceState),adapter.live,sourceState,makeInput(sourceState));
 equal(generic,concrete,name+' independent construction observations');
 const expectedCallbacks=name==='Scalar'?0:name==='Nested'?8:4;
 check(genericState.callbacks===expectedCallbacks,name+' callback count');
 check(genericState.factories===(name==='Factory'?4:0),name+' factory count');
 check(genericState.arguments===(name==='Factory'?4:0),name+' argument callback count');
}
`

const tsAdapterDiagramRollback = `
type Nested = values.Page<values.Bundle<values.Unary>>;
async function rollback(adapter:ValueAdapter<{value:Nested}>) {
 let acquired=0,callbacks=0;
 const a=new DuplexPeer({role:'client',observer:{observe(event){if(event.type==='live.exported')acquired++;}}});
 const b=new DuplexPeer({role:'server'});
 const near=liveOver(a,{maxExports:3}),far=liveOver(b,{maxImports:2});
 const [left,right]=pipe();await Promise.all([a.attach(left),b.attach(right)]);
 const baselineA=near.owner().child(),baselineB=far.owner().child(),sent=near.owner().child(),received=far.owner().child();
 const unary:values.Unary=async n=>{callbacks++;return n+1;};
 const unaryAdapter=values.adapterUnary(),bundle={value:unary,run:unary};
 const snapshots:unknown[]=[];
 const snapshot=(name:string)=>snapshots.push({name,callbacks,counts:[near.counts(),far.counts()]});
 async function waitCounts(exports:number,imports:number) {
  const deadline=Date.now()+5000;
  while(near.counts().exports!==exports||far.counts().imports!==imports){check(Date.now()<deadline,'rollback counts failed to settle');await new Promise(resolve=>setTimeout(resolve,1));}
 }
 function refusal(action:()=>unknown,code:string) {
  let caught:unknown;try{action();}catch(error){caught=error;}
  check(caught instanceof DuplexError&&caught.code===code,'wrong rollback refusal: '+String(caught));
 }
 try {
  const retained=unaryAdapter.import(baselineB,unaryAdapter.export(baselineA,unary));
  const before=acquired;
  refusal(()=>adapter.export(sent,{value:{items:[bundle,bundle]}}),'too_many_exports');
  check(acquired-before===2,'export failure did not exercise partial acquisition');
  equal(sent.counts(),{exports:0,imports:0},'failed export retained owner allocations');
  snapshot('export rollback');
  const wire=adapter.export(sent,{value:{items:[bundle]}});
  refusal(()=>adapter.import(received,wire),'too_many_imports');
  equal(received.counts(),{exports:0,imports:0},'failed import retained owner allocations');
  await waitCounts(2,1);
  check(await retained(41,{signal})===42,'rollback revoked a preexisting callback');
  snapshot('import rollback preserves prior attachment');
  sent.release();baselineB.release();baselineA.release();await waitCounts(0,0);
  const again=near.owner().child(),arrival=far.owner().child();
  const value=adapter.import(arrival,adapter.export(again,{value:{items:[bundle]}}));
  check(await value.value.items[0]!.value(41,{signal})===42,'reused import slot value');
  check(await value.value.items[0]!.run(41,{signal})===42,'reused import slot sibling');
  check(callbacks===3,'rollback callback multiplicity');snapshot('successful reuse');
  again.release();arrival.release();received.release();near.owner().release();far.owner().release();await waitCounts(0,0);snapshot('released');
  return snapshots;
 } finally {a.close();b.close();}
}
const genericRollback=await rollback(genericClient.adapterValue(values.adapterPage(values.adapterBundle(values.adapterUnary()))));
const sourceRollback=await rollback(clientNested.adapterValue());
equal(genericRollback,sourceRollback,'independent atomic conversion routes');
`
