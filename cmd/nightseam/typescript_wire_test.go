package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A generated bridge consumes and produces the same session factory. Both
// directions are held against a direct model, including reverse calls/events.
func TestGeneratedTypeScriptWireFactories(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/cell/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/cell/protocol.json", []byte(`{
		"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],
		"types":{"Value":{"kind":"record","fields":[{"name":"value","type":"T"}]}},
		"server":{"methods":{"put":{"request":"Value","result":"T"},"get":{"result":"T"}},"events":{"changed":{"type":"Value"}}},
		"client":{"methods":{"mirror":{"request":"Value","result":"T"}},"events":{"noted":{"type":"Value"}}}
	}`))
	writeFixture(t, directory, "api/contracts/values/model.json", []byte(`{"nightseam":2,"types":{"Count":{"kind":"alias","type":"integer"}}}`))
	writeFixture(t, directory, "api/contracts/intrinsics/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/intrinsics/protocol.json", []byte(`{
		"profile":"nightseam.duplex/1",
		"server":{"methods":{"constructor":{"result":"integer"},"to_string":{"result":"string"}}}
	}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)},
		"include":         []string{"api/ts/**/*.ts", "wire.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "wire.ts", []byte(tsWireFactoriesProgram))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "wire.ts")
}

const tsWireFactoriesProgram = `import type { Endpoint } from '@bitspark/bitwire';
import type {WireModelContext} from '@nightseam/runtime';
import {adapterCount} from '@example/values-client';
import * as binding from '@example/cell-binding';
import * as client from '@example/cell-client';
import * as intrinsics from '@example/intrinsics-binding';
import type {Server, Client, ServerMethods, ClientMethods, ServerEvents, ClientEvents, ServerModel, ClientModel} from '@example/cell-client/types';

type Equal<A,B>=(<T>()=>T extends A?1:2) extends (<T>()=>T extends B?1:2)?true:false;
const serverInput:Equal<Parameters<typeof binding.toWire<number>>[0],ServerModel<number>>=true;
const serverOutput:Equal<Awaited<ReturnType<typeof binding.fromWire<number>>>,ServerModel<number>>=true;
const clientInput:Equal<Parameters<typeof client.toWire<number>>[0],ClientModel<number>>=true;
const clientOutput:Equal<Awaited<ReturnType<typeof client.fromWire<number>>>,ClientModel<number>>=true;
const serverMethods:Equal<Server<number>['methods'],ServerMethods<number>>=true;
const serverEvents:Equal<Server<number>['events'],ServerEvents<number>>=true;
const clientMethods:Equal<Client<number>['methods'],ClientMethods<number>>=true;
const clientEvents:Equal<Client<number>['events'],ClientEvents<number>>=true;
const optionalContext:Equal<Parameters<ServerMethods<number>['put']>[1],WireModelContext|undefined>=true;
const optionalEventContext:Equal<Parameters<ServerEvents<number>['noted']>[1],WireModelContext|undefined>=true;
const methodResult:Equal<ReturnType<ServerMethods<number>['put']>,number|Promise<number>>=true;
const eventResult:Equal<ReturnType<ClientEvents<number>['changed']>,void|Promise<void>>=true;
const omittedRequest:Equal<Parameters<ServerMethods<number>['get']>[0],Record<string,never>>=true;

function check(condition:unknown,message:string):asserts condition {if(!condition)throw new Error(message);}
function equal(left:unknown,right:unknown,message:string){check(JSON.stringify(left)===JSON.stringify(right),message+': '+JSON.stringify(left)+' / '+JSON.stringify(right));}
function rejectTwice(run:()=>unknown){let rejected=false;try{run();}catch{rejected=true;}check(rejected,'fromWire factory accepted a second opposite implementation');}
const context={};
const callContext:WireModelContext={signal:AbortSignal.timeout(5000),timeoutMs:5000};
type State={factories:number;reverse:number;events:number;observations:string[]};
const state=():State=>({factories:0,reverse:0,events:0,observations:[]});
async function delivered(test:()=>boolean){const until=Date.now()+5000;while(!test()){check(Date.now()<until,'event did not arrive');await new Promise(resolve=>setTimeout(resolve,1));}}

// Neither implementation knows how it is presented. The same generic model
// and opposite implementation run directly and through the generated bridge.
function serverModel<T>(log:State,initial:T):ServerModel<T>{
 return remote=>{
  log.factories++;
  let held=initial;
  return {
   methods:{
    async put(params,context){log.observations.push('put:'+String(params.value));held=await remote.methods.mirror(params,context);await remote.events.changed({value:held},context);return held;},
    get(_params,_context){log.observations.push('get:'+String(held));return held;},
   },
   events:{noted(params,_context){log.events++;log.observations.push('noted:'+String(params.value));}},
  };
 };
}
function clientImplementation<T>(log:State):Client<T>{
 return {
  methods:{mirror(params,_context){log.reverse++;log.observations.push('mirror:'+String(params.value));return params.value;}},
  events:{changed(params,_context){log.events++;log.observations.push('changed:'+String(params.value));}},
 };
}
async function serverRoute(roundtrip:boolean){
 const log=state(),opposite=clientImplementation<number>(log),factory=serverModel(log,0);
 let model:Server<number>,wire:Endpoint|undefined;
 try{
  if(roundtrip){
   wire=binding.toWire(factory,context,adapterCount());
   check(log.factories===1,'toWire did not construct exactly one server model');
   const restored:ServerModel<number>=await binding.fromWire(wire,context,adapterCount());
   model=restored(opposite);
   rejectTwice(()=>restored(clientImplementation<number>(log)));
  }else model=factory(opposite);
  check(log.factories===1,'binding recreated server model');
  check(await model.methods.put({value:41},callContext)===41,'first put');
  await delivered(()=>log.events===1);
  check(await model.methods.get({},callContext)===41,'state after first put');
  await model.events.noted({value:7},callContext);
  await delivered(()=>log.events===2);
  check(await model.methods.put({value:42})===42,'optional context put');
  await delivered(()=>log.events===3);
  check(await model.methods.get({})===42,'omitted request is empty params');
  check(log.factories===1&&log.reverse===2&&log.events===3,'server factory or callback multiplicity');
  return log;
 }finally{wire?.close();}
}

function clientModel<T>(log:State):ClientModel<T>{
 return remote=>{
  log.factories++;
  return {
   methods:{async mirror(params,context){log.observations.push('mirror:'+String(params.value));const current=await remote.methods.get({},context);await remote.events.noted(params,context);return current;}},
   events:{changed(params,_context){log.events++;log.observations.push('changed:'+String(params.value));}},
  };
 };
}
function serverImplementation<T>(log:State,initial:T):Server<T>{
 let held=initial;
 return {
  methods:{put(params,_context){held=params.value;return held;},get(_params,_context){log.reverse++;log.observations.push('get:'+String(held));return held;}},
  events:{noted(params,_context){held=params.value;log.events++;log.observations.push('noted:'+String(held));}},
 };
}
async function clientRoute(roundtrip:boolean){
 const log=state(),opposite=serverImplementation(log,10),factory=clientModel<number>(log);
 let model:Client<number>,wire:Endpoint|undefined;
 try{
  if(roundtrip){
   wire=client.toWire(factory,context,adapterCount());
   check(log.factories===1,'toWire did not construct exactly one client model');
   const restored:ClientModel<number>=await client.fromWire(wire,context,adapterCount());
   model=restored(opposite);
   rejectTwice(()=>restored(serverImplementation(log,99)));
  }else model=factory(opposite);
  check(await model.methods.mirror({value:20},callContext)===10,'client reverse call observed initial server state');
  await delivered(()=>log.events===1);
  await model.events.changed({value:30},callContext);
  await delivered(()=>log.events===2);
  check(await model.methods.mirror({value:40})===20,'client reverse event noted server state');
  await delivered(()=>log.events===3);
  check(log.factories===1&&log.reverse===2&&log.events===3,'client factory or callback multiplicity');
  return log;
 }finally{wire?.close();}
}

equal(await serverRoute(false),await serverRoute(true),'server direct and wire observations');
equal(await clientRoute(false),await clientRoute(true),'client direct and wire observations');

// Object's inherited functions are absent handlers, while supplied prototype
// implementations remain valid model facets.
function rejectsMissingIntrinsic(methods:object,name:string){
 let rejected=false;
 try{
  const wire=intrinsics.toWire(_remote=>({methods:methods as intrinsics.ServerMethods,events:{}}),{});
  wire.close();
 }catch(error){
  check(error instanceof Error&&error.message==='handler for '+name+' is required','wrong missing handler refusal: '+String(error));
  rejected=true;
 }
 check(rejected,'inherited Object intrinsic was accepted as '+name+' handler');
}
rejectsMissingIntrinsic({toString(){return 'provided';}},'constructor');
rejectsMissingIntrinsic({constructor(){return 41;}},'to_string');
const intrinsicCalls:string[]=[];
const customPrototype:intrinsics.ServerMethods={
 constructor(_params){intrinsicCalls.push('constructor');return 41;},
 toString(_params){intrinsicCalls.push('to_string');return 'provided';},
};
const inheritedMethods=Object.create(Object.create(customPrototype)) as intrinsics.ServerMethods;
check(!Object.hasOwn(inheritedMethods,'constructor')&&!Object.hasOwn(inheritedMethods,'toString'),'prototype fixture supplied own handlers');
const intrinsicWire=intrinsics.toWire(_remote=>({methods:inheritedMethods,events:{}}),{});
try{
 const model=(await intrinsics.fromWire(intrinsicWire,{}))({methods:{},events:{}});
 check(await model.methods.constructor({},callContext)===41,'custom prototype constructor was lost');
 check(await model.methods.toString({},callContext)==='provided','custom prototype toString was lost');
 equal(intrinsicCalls,['constructor','to_string'],'custom prototype handler multiplicity');
}finally{intrinsicWire.close();}
`
