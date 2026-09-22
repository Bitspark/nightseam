package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A consumer owns this fixed synthetic policy. The generated bridges only
// carry context and values; metadata and routing names confer no authority.
func TestGeneratedWireConsumerGuards(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/guarded/model.json", []byte(`{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`))
	writeFixture(t, directory, "api/contracts/guarded/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"methods":{"step":{"request":"Input","result":"integer"}},"events":{"changed":{"type":"Input"}}},"client":{"methods":{"mirror":{"request":"Input","result":"integer"}},"events":{"noted":{"type":"Input"}}}}`))
	writeFixture(t, directory, "api/contracts/guarded/live.json", []byte(`{"types":{"Callback":{"kind":"callable","request":"integer","result":"integer"},"Supply":{"kind":"record","fields":[{"name":"callback","type":"Callback"}]}},"server":{"methods":{"wrap":{"request":"Supply","result":"Callback"},"pass":{"request":"Supply","result":"Callback"}}}}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	t.Run("go", func(t *testing.T) {
		fixtureModule(t, directory, root)
		writeFixture(t, directory, "guard_test.go", []byte(goGeneratedGuardProgram))
		runFixture(t, directory, "go", "test", "-count=1", ".")
	})
	t.Run("typescript", func(t *testing.T) {
		for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
			copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
		}
		config, err := json.Marshal(map[string]any{
			"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory), "typeRoots": []string{filepath.Join(root, "node_modules/@types")}, "types": []string{"node"}},
			"include":         []string{"api/ts/**/*.ts", "guard.ts"},
		})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, "tsconfig.json", config)
		writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
		writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
		writeFixture(t, directory, "guard.ts", []byte(tsGeneratedGuardProgram))
		runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
		runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "guard.ts")
	})
}

const goGeneratedGuardProgram = `package guard_test
import (
 bitwire "github.com/Bitspark/bitwire/wire/go"
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
 binding "example.test/generated/api/go/guarded-binding"
 protocol "example.test/generated/api/go/guarded-protocol"

 "github.com/Bitspark/nightseam/live/go"
 "github.com/Bitspark/nightseam/runtime/go"
 "github.com/Bitspark/nightseam/tunnel/go"
)
type trustedKey struct{}
type principal struct{ name string; allowed bool }
type fixed struct{ principal *principal }
func(p fixed)Extract(ctx context.Context,trace runtime.Trace)context.Context{return context.WithValue(runtime.DefaultPropagator.Extract(ctx,trace),trustedKey{},p.principal)}
func(fixed)Inject(ctx context.Context)runtime.Trace{return runtime.DefaultPropagator.Inject(ctx)}
func bound(p *principal)context.Context{return context.WithValue(context.Background(),trustedKey{},p)}
func guard(ctx context.Context,want *principal)error{if ctx.Value(trustedKey{})!=want || !want.allowed{return &runtime.PublicError{Code:"denied",Message:"consumer guard denied"}};return nil}
type effects struct{ordinary,reverse,returned,supplied,passed,noted,changed atomic.Int64; events chan bool}
type server struct{remote protocol.Client; identity *principal; state *effects}
func(s server)Step(ctx context.Context,p protocol.Input)(int64,error){if err:=guard(ctx,s.identity);err!=nil{return 0,err};s.state.ordinary.Add(1);n,err:=s.remote.Methods.Mirror(ctx,p);if err!=nil{return 0,err};if err:=s.remote.Events.Changed(ctx,p);err!=nil{return 0,err};return n,nil}
func(s server)Wrap(ctx context.Context,p protocol.Supply)(protocol.Callback,error){if err:=guard(ctx,s.identity);err!=nil{return nil,err};captured:=ctx;return func(ctx context.Context,n int64)(int64,error){if err:=guard(captured,s.identity);err!=nil{return 0,err};s.state.returned.Add(1);return p.Callback(ctx,n)},nil}
func(s server)Pass(ctx context.Context,p protocol.Supply)(protocol.Callback,error){if err:=guard(ctx,s.identity);err!=nil{return nil,err};s.state.passed.Add(1);return p.Callback,nil}
func(s server)Noted(ctx context.Context,_ protocol.Input)error{err:=guard(ctx,s.identity);if err==nil{s.state.noted.Add(1)};s.state.events<-err==nil;return nil}
type opposite struct{identity *principal;state *effects}
func(c opposite)Mirror(ctx context.Context,p protocol.Input)(int64,error){if err:=guard(ctx,c.identity);err!=nil{return 0,err};if len(runtime.MetaFrom(ctx))!=0{return 0,errors.New("received metadata became reverse credentials")};c.state.reverse.Add(1);return p.Value,nil}
func(c opposite)Changed(ctx context.Context,_ protocol.Input)error{err:=guard(ctx,c.identity);if len(runtime.MetaFrom(ctx))!=0{err=errors.New("received metadata became event credentials")};if err==nil{c.state.changed.Add(1)};c.state.events<-err==nil;return nil}
func options(identity *principal, scope **live.Scope)runtime.Options{return runtime.Options{Propagator:fixed{identity},Prepare:func(p *runtime.Peer)(err error){*scope,err=live.Over(p,live.Options{});return}}}
func pipePeers(t *testing.T,a,b runtime.Options)(*runtime.Peer,*runtime.Peer){t.Helper();left,right:=duplex.Pipe(1<<20);pa,err:=runtime.NewPeer(context.Background(),left,runtime.ClientRole,a);if err!=nil{t.Fatal(err)};pb,err:=runtime.NewPeer(context.Background(),right,runtime.ServerRole,b);if err!=nil{t.Fatal(err)};t.Cleanup(func(){_=pa.Close();_=pb.Close()});return pa,pb}
func carriers(t *testing.T,mode string,caller,callee *principal)(bitwire.Endpoint,bitwire.Endpoint,*live.Scope,*live.Scope){
 t.Helper();var a,b *live.Scope
 if mode=="socket"{
  ready:=make(chan *runtime.Peer,1)
  h,err:=runtime.NewHandler(runtime.ServerOptions{Options:options(callee,&b),Authenticate:func(r *http.Request)(context.Context,error){return bound(callee),nil},CheckOrigin:func(*http.Request)bool{return true},OnConnect:func(p *runtime.Peer){ready<-p}});if err!=nil{t.Fatal(err)}
  host:=httptest.NewServer(h);t.Cleanup(host.Close)
  pa,_,err:=runtime.Dial(context.Background(),"ws"+strings.TrimPrefix(host.URL,"http"),runtime.DialOptions{Options:options(caller,&a)});if err!=nil{t.Fatal(err)};pb:=<-ready;t.Cleanup(func(){_=pa.Close();_=pb.Close()});return pa.Wire(),pb.Wire(),a,b
 }
 if mode=="tunnel"{
  // The outer accepted principal never supplies the inner application's grant.
  outer:=&principal{"outer allowed",true};var ta,tb *tunnel.Tunnel
  pa,pb:=pipePeers(t,runtime.Options{Propagator:fixed{outer},Prepare:func(p *runtime.Peer)(err error){ta,err=tunnel.New(p,tunnel.Options{});return}},runtime.Options{Propagator:fixed{outer},Prepare:func(p *runtime.Peer)(err error){tb,err=tunnel.New(p,tunnel.Options{});return}});_ =pa;_ =pb
  left,err:=ta.Open(context.Background(),"guarded",protocol.WireDigest(),options(caller,&a));if err!=nil{t.Fatal(err)};right,err:=tb.Accept(context.Background(),options(callee,&b));if err!=nil{t.Fatal(err)};return left,right,a,b
 }
 if mode=="local"{pa,pb:=pipePeers(t,options(caller,&a),runtime.Options{});return pa.Wire(),pb.Wire(),a,a}
 pa,pb:=pipePeers(t,options(caller,&a),options(callee,&b));return pa.Wire(),pb.Wire(),a,b
}
func checkCode(t *testing.T,err error){t.Helper();var p *runtime.PublicError;if !errors.As(err,&p)||p.Code!="denied"{t.Fatalf("want denied: %v",err)}}
func event(t *testing.T,ctx context.Context,ch <-chan bool,want bool){t.Helper();select{case v:=<-ch:if v!=want{t.Fatalf("event guard=%t want=%t",v,want)};case<-ctx.Done():t.Fatal(ctx.Err())}}
func TestGeneratedGuards(t *testing.T){for _,mode:=range []string{"local","socket","tunnel"}{for _,policy:=range []string{"deny","allow","reverse-deny"}{if mode=="local"&&policy=="reverse-deny"{continue};allowed,reverseAllowed:=policy!="deny",policy!="reverse-deny";t.Run(mode+"/"+policy,func(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();state:=&effects{events:make(chan bool,8)}
 callerIdentity:=&principal{"caller",reverseAllowed};serverIdentity:=&principal{"explicit downstream",allowed}
 if mode=="local"{callerIdentity=serverIdentity}
 outgoing,incoming,sa,sb:=carriers(t,mode,callerIdentity,serverIdentity)
 if mode=="local"{sb=sa}
 modelOptions:=runtime.Options{Propagator:fixed{serverIdentity}}
 if mode!="local"{modelOptions.Propagator=fixed{&principal{"untrusted local fallback",false}}}
 var serverRemote protocol.Client
 wire,err:=binding.ToWire(func(remote protocol.Client)(protocol.Server,error){serverRemote=remote;s:=server{remote,serverIdentity,state};return protocol.Server{Methods:s,Events:s},nil},runtime.AdapterContext{ValueEnvironment:live.ValueEnvironment(sb),Options:modelOptions});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 if mode=="local"{outgoing=wire}else{off,err:=runtime.ForwardWire(incoming,wire);if err!=nil{t.Fatal(err)};defer off()}
 // All model code above is independent of this host-only presentation.
 inner:=duplex.Mount(map[string]bitwire.Endpoint{"protected":outgoing});defer inner.Close(duplex.CodeNormal,"")
 root:=duplex.Mount(map[string]bitwire.Endpoint{"nested":inner});defer root.Close(duplex.CodeNormal,"")
 dispatcher,err:=runtime.NewDispatcher(root);if err!=nil{t.Fatal(err)};defer dispatcher.Close(duplex.CodeNormal,"");selected:=dispatcher.Select([]string{"nested","protected"})
 factory,err:=binding.FromWire(ctx,selected,runtime.AdapterContext{ValueEnvironment:live.ValueEnvironment(sa)});if err!=nil{t.Fatal(err)};opposite:=opposite{callerIdentity,state};access,err:=factory(protocol.Client{Methods:opposite,Events:opposite});if err!=nil{t.Fatal(err)}
 forged:=runtime.WithMeta(ctx,map[string]string{"verified":"true","principal":"admin","credential":"forged"})
 n,err:=access.Methods.Step(forged,protocol.Input{Value:7})
 if !allowed||!reverseAllowed{checkCode(t,err)}else{if err!=nil||n!=7{t.Fatalf("step: %d %v",n,err)};event(t,ctx,state.events,true)}
 if allowed&&!reverseAllowed{if err:=serverRemote.Events.Changed(ctx,protocol.Input{Value:8});err!=nil{t.Fatal(err)};event(t,ctx,state.events,false)}
 if err:=access.Events.Noted(forged,protocol.Input{Value:9});err!=nil{t.Fatal(err)};event(t,ctx,state.events,allowed)
 sourceContext:=bound(callerIdentity)
 supplied:=protocol.Callback(func(_ context.Context,n int64)(int64,error){if err:=guard(sourceContext,callerIdentity);err!=nil{return 0,err};state.supplied.Add(1);return n+1,nil})
 wrapped,err:=access.Methods.Wrap(forged,protocol.Supply{Callback:supplied})
 if !allowed{checkCode(t,err);if state.ordinary.Load()+state.reverse.Load()+state.returned.Load()+state.supplied.Load()+state.noted.Load()+state.changed.Load()!=0{t.Fatal("denied effect escaped")}}else{
  if err!=nil{t.Fatal(err)};n,err:=wrapped(ctx,10);if !reverseAllowed{checkCode(t,err)}else if err!=nil||n!=11{t.Fatalf("returned callback: %d %v",n,err)}
  self,err:=access.Methods.Pass(ctx,protocol.Supply{Callback:supplied});if err!=nil{t.Fatal(err)};n,err=self(ctx,20);if !reverseAllowed{checkCode(t,err)}else if err!=nil||n!=21{t.Fatalf("returned self-reference: %d %v",n,err)}
  deniedContext:=bound(&principal{"denied supplied binding",false});denied:=protocol.Callback(func(_ context.Context,n int64)(int64,error){if err:=guard(deniedContext,callerIdentity);err!=nil{return 0,err};state.supplied.Add(1);return n,nil})
  blocked,err:=access.Methods.Pass(ctx,protocol.Supply{Callback:denied});if err!=nil{t.Fatal(err)};_,err=blocked(ctx,30);checkCode(t,err)
  var reverseCount int64;if reverseAllowed{reverseCount=1};if state.ordinary.Load()!=1||state.reverse.Load()!=reverseCount||state.returned.Load()!=1||state.supplied.Load()!=2*reverseCount||state.passed.Load()!=2||state.noted.Load()!=1||state.changed.Load()!=reverseCount{t.Fatalf("effect counts: ordinary=%d reverse=%d returned=%d supplied=%d pass=%d noted=%d changed=%d",state.ordinary.Load(),state.reverse.Load(),state.returned.Load(),state.supplied.Load(),state.passed.Load(),state.noted.Load(),state.changed.Load())}
 }
 // Snapshot every participating scope before release and teardown. Local
 // self-reference holds exports only; transported callbacks hold both ledgers.
 beforeA,beforeB:=sa.Counts(),sb.Counts();t.Logf("%s allowed=%t retained=%+v/%+v",mode,allowed,beforeA,beforeB)
 // Each supplied value and returned value gets its own binding: generated
 // conversion preserves the wrapper instead of assuming native identity.
 wantA,wantB:=live.Counts{Exports:1},live.Counts{Imports:1};if allowed{wantA,wantB=live.Counts{Exports:3,Imports:3},live.Counts{Exports:3,Imports:3}};if mode=="local"{wantA=live.Counts{Exports:1};if allowed{wantA.Exports=6};wantB=wantA};if beforeA!=wantA||beforeB!=wantB{t.Fatalf("retained ownership: %+v/%+v want %+v/%+v",beforeA,beforeB,wantA,wantB)}
 _=sa.Owner().Release();_=sb.Owner().Release()
 if sa.Counts()!=(live.Counts{})||sb.Counts()!=(live.Counts{}){t.Fatalf("before teardown: %+v %+v",sa.Counts(),sb.Counts())}
})}}}
`

const tsGeneratedGuardProgram = `import {createServer,createConnection,type Socket} from 'node:net';
import { mount, pipe, type FrameConnection, type ConnectionHandlers } from '@nightseam/duplex';
import type { Endpoint } from '@bitspark/bitwire';
import {DuplexPeer,DuplexError,defaultPropagator,forwardWire,createDispatcher,type PeerOptions,type Propagator,type WireModelContext} from '@nightseam/runtime';
import {liveOver,valueEnvironment,type LiveScope} from '@nightseam/live';
import {Tunnel} from '@nightseam/tunnel';
import * as binding from '@example/guarded-binding';
import type {ServerModel,Client,Callback} from '@example/guarded-client/types';

const verified:unique symbol=Symbol('consumer verified context');
type Principal={readonly name:string;readonly allowed:boolean};
type Context=WireModelContext&{readonly [verified]?:Principal};
const principal=(name:string,allowed:boolean):Principal=>Object.freeze({name,allowed});
function fixed(identity:Principal):Propagator{return{extract(context,trace){defaultPropagator.extract(context,trace);Object.defineProperty(context,verified,{value:identity});},inject:defaultPropagator.inject};}
function bound(identity:Principal):Context{return Object.defineProperty({},verified,{value:identity});}
function guard(context:Context|undefined,want:Principal){if(context?.[verified]!==want||!want.allowed)throw new DuplexError('denied','consumer guard denied');}
function check(condition:unknown,message:string):asserts condition{if(!condition)throw new Error(message);}
function equal(a:unknown,b:unknown,message:string){check(JSON.stringify(a)===JSON.stringify(b),message+': '+JSON.stringify(a)+' / '+JSON.stringify(b));}
async function denied(call:()=>Promise<unknown>){let error:unknown;try{await call();}catch(e){error=e;}check(error instanceof DuplexError&&error.code==='denied','guard did not deny: '+String(error));}
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(yes=>{resolve=yes;});return{promise,resolve};}
async function wait(test:()=>boolean){const until=Date.now()+5000;while(!test()){check(Date.now()<until,'delivery deadline');await new Promise(resolve=>setTimeout(resolve,1));}}
type Effects={ordinary:number;reverse:number;returned:number;supplied:number;passed:number;noted:number;changed:number;events:boolean[]};
const effects=():Effects=>({ordinary:0,reverse:0,returned:0,supplied:0,passed:0,noted:0,changed:0,events:[]});
function model(identity:Principal,state:Effects):ServerModel{return remote=>({methods:{
 async step(params,context){guard(context,identity);state.ordinary++;const result=await remote.methods.mirror(params,context);await remote.events.changed(params,context);return result;},
 wrap(params,context){guard(context,identity);const captured=context;const callback:Callback=async(value,options)=>{guard(captured,identity);state.returned++;return params.callback(value,options);};return callback;},
 pass(params,context){guard(context,identity);state.passed++;return params.callback;},
},events:{noted(_params,context){let allowed=false;try{guard(context,identity);state.noted++;allowed=true;}catch{/* The consumer drops denied one-way events. */}finally{state.events.push(allowed);}}}});}
function opposite(identity:Principal,state:Effects):Client{return{methods:{mirror(params,context){guard(context,identity);check(!context?.meta||Object.keys(context.meta).length===0,'received metadata became reverse credentials');state.reverse++;return params.value;}},events:{changed(_params,context){let allowed=false;try{guard(context,identity);check(!context?.meta||Object.keys(context.meta).length===0,'received metadata became event credentials');state.changed++;allowed=true;}catch{/* No protected effect for a denied event. */}finally{state.events.push(allowed);}}}};}
function textSocket(socket:Socket):FrameConnection{
 let state:'open'|'closed'='open',pending='';const listeners=new Set<ConnectionHandlers>();socket.setEncoding('utf8');
 socket.on('data',(chunk:string)=>{pending+=chunk;let end:number;while((end=pending.indexOf('\n'))>=0){const data=pending.slice(0,end);pending=pending.slice(end+1);for(const listener of listeners)listener.frame?.({kind:'text',data});}});
 socket.on('error',()=>{for(const listener of listeners)listener.error?.();});socket.on('close',()=>{state='closed';for(const listener of listeners)listener.close?.(1000,'');});
 return{get state(){return state;},get buffered(){return socket.writableLength;},send(frame){check(state==='open'&&frame.kind==='text','text socket not writable');socket.write(frame.data+'\n');},close(){state='closed';socket.destroy();},listen(listener){listeners.add(listener);return()=>{listeners.delete(listener);};}};
}
type Carrier={outgoing:Endpoint;incoming:Endpoint;a:LiveScope;b:LiveScope;close:()=>void};
async function carriers(mode:string,caller:Principal,callee:Principal):Promise<Carrier>{
 let a!:LiveScope,b!:LiveScope;const aOptions:PeerOptions={propagator:fixed(caller),prepare:peer=>{a=liveOver(peer);}},bOptions:PeerOptions={propagator:fixed(callee),prepare:peer=>{b=liveOver(peer);}};
 if(mode==='tunnel'){
  const outer=principal('outer allowed',true);const [left,right]=pipe();const pa=new DuplexPeer({propagator:fixed(outer)}),pb=new DuplexPeer({role:'server',propagator:fixed(outer)});const ta=new Tunnel(pa),tb=new Tunnel(pb);await Promise.all([pa.attach(left),pb.attach(right)]);
  const outgoing=await ta.open('guarded',binding.wireDigest,aOptions),incoming=await tb.accept(bOptions);return{outgoing,incoming,a,b,close(){pa.close();pb.close();}};
 }
 let connections:[FrameConnection,FrameConnection],closeHost=()=>{};
 if(mode==='socket'){
  const accepted=deferred<Socket>();const host=createServer(connection=>accepted.resolve(connection));await new Promise<void>(resolve=>host.listen(0,'127.0.0.1',resolve));const address=host.address();check(address&&typeof address!=='string','socket address');const client=createConnection({host:'127.0.0.1',port:address.port});await new Promise<void>((resolve,reject)=>{client.once('connect',resolve);client.once('error',reject);});const remote=await accepted.promise;connections=[textSocket(client),textSocket(remote)];closeHost=()=>{client.destroy();remote.destroy();host.close();};
 }else connections=pipe();
 const pa=new DuplexPeer(aOptions),pb=new DuplexPeer({... (mode==='local'?{}:bOptions),role:'server'});await Promise.all([pa.attach(connections[0]),pb.attach(connections[1])]);if(mode==='local')b=a;return{outgoing:pa.wire(),incoming:pb.wire(),a,b,close(){pa.close();pb.close();closeHost();}};
}
async function run(mode:string,allowed:boolean,reverseAllowed=true){
 const state=effects(),serverIdentity=principal('explicit downstream',allowed),callerIdentity=mode==='local'?serverIdentity:principal('caller',reverseAllowed);
 const carrier=await carriers(mode,callerIdentity,serverIdentity);const sa=carrier.a,sb=mode==='local'?sa:carrier.b;
 const modelOptions:PeerOptions={propagator:fixed(mode==='local'?serverIdentity:principal('untrusted local fallback',false))};
 let serverRemote!:Client;const construct=model(serverIdentity,state);const wire=binding.toWire(remote=>{serverRemote=remote;return construct(remote);},{options:modelOptions,valueEnvironment:valueEnvironment(sb)});
 let detach=()=>{},releaseView=()=>{};
 try{
  let outgoing:Endpoint=wire;if(mode!=='local'){detach=forwardWire(carrier.incoming,wire);outgoing=carrier.outgoing;}
  // The model and guards above never inspect the chosen carrier or path.
  const inner=mount(new Map([['protected',outgoing]]));
  const root=mount(new Map([['nested',inner]]));
  releaseView=()=>{root.close();inner.close();};
  const dispatcher=createDispatcher(root),selected=dispatcher.select(['nested','protected']);
  releaseView=()=>{dispatcher.close();root.close();inner.close();};
  const factory=await binding.fromWire(selected,{valueEnvironment:valueEnvironment(sa)}),access=factory(opposite(callerIdentity,state));
  const forged:WireModelContext&{valueContext:unknown}={signal:AbortSignal.timeout(8000),valueContext:sa.owner(),outgoingMeta:{verified:'true',principal:'admin',credential:'forged'}};
  if(allowed&&reverseAllowed){equal(await access.methods.step({value:7},forged),7,'ordinary result');await wait(()=>state.events.length===1);check(state.events.shift()===true,'reverse event denied');}else await denied(()=>Promise.resolve(access.methods.step({value:7},forged)));
  if(allowed&&!reverseAllowed){await serverRemote.events.changed({value:8});await wait(()=>state.events.length===1);check(state.events.shift()===false,'reverse event guard bypassed');}
  await access.events.noted({value:9},forged);await wait(()=>state.events.length===1);equal(state.events.shift(),allowed,'event guard');
  const sourceContext=bound(callerIdentity);const supplied:Callback=async(value)=>{guard(sourceContext,callerIdentity);state.supplied++;return value+1;};
  if(!allowed){await denied(()=>Promise.resolve(access.methods.wrap({callback:supplied},forged)));equal(state,{...effects(),events:[]},'denied effects');}
  else{
   const wrapped=await access.methods.wrap({callback:supplied},forged);if(reverseAllowed)equal(await wrapped(10),11,'returned callback');else await denied(()=>wrapped(10));
   const self=await access.methods.pass({callback:supplied});if(reverseAllowed)equal(await self(20),21,'returned local/self reference');else await denied(()=>self(20));
   const deniedContext=bound(principal('denied supplied binding',false));const blocked:Callback=async(value)=>{guard(deniedContext,callerIdentity);state.supplied++;return value;};
   const deniedSelf=await access.methods.pass({callback:blocked});await denied(()=>deniedSelf(30));
   equal(state,{ordinary:1,reverse:reverseAllowed?1:0,returned:1,supplied:reverseAllowed?2:0,passed:2,noted:1,changed:reverseAllowed?1:0,events:[]},'effect counts');
  }
  // Three supplied and three returned functions remain separate bindings;
  // no native-identity optimization unwraps the consumer's guard.
  const beforeA=sa.counts(),beforeB=sb.counts();const wantA=mode==='local'?{exports:allowed?6:1,imports:0}:{exports:allowed?3:1,imports:allowed?3:0};const wantB=mode==='local'?wantA:{exports:allowed?3:0,imports:allowed?3:1};equal(beforeA,wantA,'caller retained ownership');equal(beforeB,wantB,'server retained ownership');
  sa.owner().release();sb.owner().release();equal(sa.counts(),{exports:0,imports:0},'caller counts before teardown');equal(sb.counts(),{exports:0,imports:0},'server counts before teardown');
 }finally{releaseView();detach();wire.close();carrier.close();}
}
for(const mode of ['local','socket','tunnel'])for(const allowed of [false,true])await run(mode,allowed);
for(const mode of ['socket','tunnel'])await run(mode,true,false);
`
