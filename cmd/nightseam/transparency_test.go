package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// These declarations belong to a consumer fixture, not to a canned testee.
// The same emitted helper is exercised in both languages and both directions.
func TestGeneratedTransparencyHelpers(t *testing.T) {
	testTransparencyFixture(t, "TestHelpers")
}

func TestGeneratedTransparencyPresentations(t *testing.T) {
	testTransparencyFixture(t, "TestPresentations|TestTypeScriptPresentations")
}

func testTransparencyFixture(t *testing.T, tests string) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/data/model.json", []byte(`{"nightseam":2,"types":{"Count":{"kind":"alias","type":"integer"}}}`))
	writeFixture(t, directory, "api/contracts/meter/model.json", []byte(`{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer","min":1}]},"Never":{"kind":"record","fields":[{"name":"next","type":"string","pattern":"a^"}]}}}`))
	writeFixture(t, directory, "api/contracts/meter/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"request":"Input","result":"Input"},"constant":{"request":"Input","result":"integer"},"decline":{"result":"integer","errors":["denied"]},"ping":{"result":"integer"}},"events":{"changed":{"type":"Input"}}},"client":{"methods":{"reverse":{"request":"Input","result":"Input"}},"events":{"noted":{"type":"Input"}}},"errors":{"denied":"declined"}}`))
	writeFixture(t, directory, "api/contracts/cell/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/cell/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"T"}]}},"server":{"methods":{"echo":{"request":"Input","result":"T"}}}}`))
	writeFixture(t, directory, "api/contracts/names/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/names/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"Inputs"},{"name":"Close"},{"name":"Parameters"}],"types":{"Input":{"kind":"record","fields":[{"name":"inputs","type":"Inputs"},{"name":"close","type":"Close"},{"name":"parameters","type":"Parameters"}]}},"server":{"methods":{"echo":{"request":"Input","result":"Input"}}}}`))
	writeFixture(t, directory, "api/contracts/functions/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/functions/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/functions/live.json", []byte(`{"types":{"Unary":{"kind":"callable","request":"integer","result":"integer"}}}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "transparency_test.go", []byte(goTransparencyProgram+goTransparencyPresentations))
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)}, "include": []string{"api/ts/**/*.ts", "transparency.ts", "presentations.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(strings.Replace(runtimeLoader, "const generated=", "const generated=name.endsWith('/test')?'./api/ts/'+name.slice(0,-5)+'/src/test.ts':", 1)))
	writeFixture(t, directory, "transparency.ts", []byte(tsTransparencyProgram))
	writeFixture(t, directory, "presentations.ts", []byte(tsTransparencyPresentations))
	runFixture(t, directory, "go", "vet", "./...")
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "transparency.ts")
	runFixture(t, directory, "go", "test", "-count=1", "-run", tests, ".")
}

const goTransparencyProgram = `package consumer_test
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "reflect"
 "sync"
 "strings"
 "testing"
 "time"
 test "example.test/generated/api/go/meter-binding/familytest"
 clienttest "example.test/generated/api/go/meter-client/familytest"
 celltest "example.test/generated/api/go/cell-binding/familytest"
 protocol "example.test/generated/api/go/meter-protocol"
 cell "example.test/generated/api/go/cell-protocol"
 functiontest "example.test/generated/api/go/functions-binding/familytest"
 datatest "example.test/generated/api/go/data-protocol/familytest"
 functions "example.test/generated/api/go/functions-protocol"
 "github.com/Bitspark/nightseam/duplex/go"
 "github.com/Bitspark/nightseam/runtime/go"
 "github.com/Bitspark/nightseam/tunnel/go"
 "github.com/Bitspark/nightseam/live/go"
)
type methods struct { remote protocol.Client; invalid bool }
func(m methods) Echo(ctx context.Context,p protocol.Input)(protocol.Input,error){if m.invalid{return protocol.Input{Value:-1},nil};return m.remote.Methods.Reverse(ctx,p)}
func(m methods) Constant(context.Context,protocol.Input)(int64,error){return 7,nil}
func(m methods) Decline(context.Context)(int64,error){return 0,&runtime.PublicError{Code:"denied",Message:"declined"}}
func(m methods) Ping(context.Context)(int64,error){return 9,nil}
type events struct{}
func(events) Noted(context.Context,protocol.Input)error{return nil}
type reverse struct{}
func(reverse) Reverse(_ context.Context,p protocol.Input)(protocol.Input,error){return p,nil}
func(reverse) Changed(context.Context,protocol.Input)error{return nil}
func model(remote protocol.Client)(protocol.Server,error){return protocol.Server{Methods:methods{remote:remote},Events:events{}},nil}
func opposite()protocol.Client{return protocol.Client{Methods:reverse{},Events:reverse{}}}
type cellMethods[T any]struct{}
func(cellMethods[T])Echo(_ context.Context,v cell.Input[T])(T,error){return v.Value,nil}
func TestHelpers(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 if err:=test.Smoke(ctx,nil,opposite(),test.Options{});err==nil{t.Fatal("nil model accepted")}
 if err:=test.Smoke(ctx,func(protocol.Client)(protocol.Server,error){return protocol.Server{},nil},opposite(),test.Options{});err==nil{t.Fatal("missing methods accepted")}
 if _,err:=datatest.Example[int64]("Count");err!=nil{t.Fatal(err)}
 for _,presentation:=range []test.Presentation{nil,test.Local,test.Pipe,test.Mounted,test.Forwarded}{if err:=test.Smoke(ctx,model,opposite(),test.Options{Presentation:presentation});err!=nil{t.Fatal(err)}}
 v,err:=test.Example[protocol.Input]("Input");if err!=nil||v.Value<1{t.Fatalf("example %v %v",v,err)}
 if _,err:=test.Example[protocol.Never]("Never");err==nil||!strings.Contains(err.Error(),"unavailable"){t.Fatal("recursive example must report unavailable",err)}
 if _,err:=functiontest.Example[any]("Unary");err==nil||!strings.Contains(err.Error(),"native"){t.Fatal("callable illustration became native",err)}
 if _,err:=test.Example[string]("Input");err==nil{t.Fatal("wrong example instantiation accepted")}
 factory,stop,err:=test.Pair(ctx,model,test.Options{});if err!=nil{t.Fatal(err)}
 access,err:=factory(opposite());if err!=nil{t.Fatal(err)}
 if _,err:=access.Methods.Ping(ctx);err!=nil{t.Fatal(err)}
 stop();stop();if _,err:=access.Methods.Ping(ctx);err==nil{t.Fatal("stop left access usable")}
 if err:=test.Smoke(ctx,model,opposite(),test.Options{Inputs:map[string]any{"constant":protocol.Input{Value:-1}}});err==nil{t.Fatal("invalid input passed smoke")}
 if err:=test.Smoke(ctx,func(r protocol.Client)(protocol.Server,error){s,_:=model(r);s.Methods=methods{remote:r,invalid:true};return s,nil},opposite(),test.Options{});err==nil{t.Fatal("invalid result passed smoke")}
 changed:=func(ctx context.Context,w duplex.Endpoint)(duplex.Endpoint,func(),error){return mutatingWire{w},func(){},nil}
 if err:=test.Smoke(ctx,model,opposite(),test.Options{Presentation:changed});err==nil||!strings.Contains(err.Error(),"constant.request"){t.Fatalf("constant handler concealed changed input: %v",err)}
 err=test.Smoke(ctx,func(protocol.Client)(protocol.Server,error){return protocol.Server{},errors.New("factory failed")},opposite(),test.Options{});if err==nil{t.Fatal("factory failure ignored")}
 ended,abort:=context.WithCancel(ctx);abort()
 var abandoned duplex.Endpoint;detached:=0
 failedPresentation:=func(_ context.Context,w duplex.Endpoint)(duplex.Endpoint,func(),error){abandoned=w;return w,func(){detached++},nil}
 if factory,stop,err:=test.Pair(ended,model,test.Options{Presentation:failedPresentation});err==nil||factory!=nil||stop!=nil{t.Fatal("cancelled preparation returned usable resources",err)}
 if detached!=1{t.Fatalf("failed preparation detached %d times",detached)}
 var discarded int64;if err:=runtime.CallWire(ctx,abandoned,[]string{"ping"},struct{}{},&discarded);err==nil{t.Fatal("failed preparation left its model wire open")}
 if err:=celltest.Smoke(ctx,func(cell.Client[int64])(cell.Server[int64],error){return cell.Server[int64]{Methods:cellMethods[int64]{}},nil},cell.Client[int64]{},celltest.Options{Inputs:map[string]any{"echo":cell.Input[int64]{Value:23}}},runtime.JSONAdapter[int64]());err!=nil{t.Fatal(err)}
 if err:=celltest.Smoke(ctx,func(cell.Client[string])(cell.Server[string],error){return cell.Server[string]{Methods:cellMethods[string]{}},nil},cell.Client[string]{},celltest.Options{},runtime.JSONAdapter[string]());err!=nil{t.Fatal(err)}
 clientModel:=func(protocol.Server)(protocol.Client,error){return opposite(),nil}
 server,_:=model(opposite());if err:=clienttest.Smoke(ctx,clientModel,server,clienttest.Options{});err!=nil{t.Fatal(err)}
 var observedWire duplex.Endpoint
 observer:=test.Options{Presentation:func(ctx context.Context,w duplex.Endpoint)(duplex.Endpoint,func(),error){observedWire=w;return test.Local(ctx,w)}}
 observer.Equal=func(method string,x,y any)error{
  if method=="constant.request"{var value int64;if err:=runtime.CallWire(ctx,observedWire,[]string{"ping"},struct{}{},&value);err!=nil{return err};if value!=9{return errors.New("reentrant observation changed")}}
  if !reflect.DeepEqual(x,y){return errors.New("observation differs")};return nil
 }
 if err:=test.Smoke(ctx,model,opposite(),observer);err!=nil{t.Fatal("reentrant observer",err)}
 liveHelper(t,ctx)
}
func liveHelper(t *testing.T,ctx context.Context){
 a,b:=duplex.Pipe(1<<20)
 left,err:=runtime.NewPeer(ctx,a,runtime.ClientRole,runtime.Options{});if err!=nil{t.Fatal(err)};defer left.Close()
 right,err:=runtime.NewPeer(ctx,b,runtime.ServerRole,runtime.Options{});if err!=nil{t.Fatal(err)};defer right.Close()
 near,err:=live.Over(left,live.Options{});if err!=nil{t.Fatal(err)}
 far,err:=live.Over(right,live.Options{});if err!=nil{t.Fatal(err)}
 owner:=near.Owner().Child();callContext:=live.WithOwner(ctx,owner)
 unary:=functions.Unary(func(_ context.Context,n int64)(int64,error){return n+1,nil})
 factory:=func(cell.Client[functions.Unary])(cell.Server[functions.Unary],error){return cell.Server[functions.Unary]{Methods:cellMethods[functions.Unary]{}},nil}
 options:=celltest.Options{Context:runtime.AdapterContext{ValueEnvironment:live.ValueEnvironment(far)},RemoteContext:runtime.AdapterContext{ValueEnvironment:live.ValueEnvironment(near)},Inputs:map[string]any{"echo":cell.Input[functions.Unary]{Value:unary}}}
 options.Equal=func(method string,expected,actual any)error{
  var x,y functions.Unary
  if strings.HasSuffix(method,".request"){x=expected.(cell.Input[functions.Unary]).Value;y=actual.(cell.Input[functions.Unary]).Value}else{x=expected.(functions.Unary);y=actual.(functions.Unary)}
  first,err:=x(callContext,4);if err!=nil{return err};second,err:=y(callContext,4);if err!=nil{return err};if first!=5||second!=5{return errors.New("callable behavior changed")};return nil
 }
 if err:=celltest.Smoke(callContext,factory,cell.Client[functions.Unary]{},options,functions.AdapterUnary());err!=nil{t.Fatal(err)}
 unobserved:=options;unobserved.Equal=nil;if err:=celltest.Smoke(callContext,factory,cell.Client[functions.Unary]{},unobserved,functions.AdapterUnary());err==nil||!strings.Contains(err.Error(),"requires an Equal observer for live values"){t.Fatal("missing live observer",err)}
 options.Inputs=nil;if err:=celltest.Smoke(callContext,factory,cell.Client[functions.Unary]{},options,functions.AdapterUnary());err==nil||!strings.Contains(err.Error(),"native"){t.Fatal("missing live witness",err)}
 owner.Release();near.Owner().Release();far.Owner().Release()
 until:=time.Now().Add(3*time.Second);for near.Counts()!=(live.Counts{})||far.Counts()!=(live.Counts{}){if time.Now().After(until){t.Fatal("live helper retained caller-owned bindings",near.Counts(),far.Counts())};time.Sleep(time.Millisecond)}
}
type mutatingWire struct{duplex.Endpoint}
func(w mutatingWire)Send(path []string,m duplex.Message)error{if len(path)==1&&path[0]=="constant"&&m.Frame.Kind=="request"{m.Frame.Params=json.RawMessage("{\"value\":77}")};return w.Endpoint.Send(path,m)}
`

const tsTransparencyProgram = `import * as test from '@example/meter-binding/test';
import * as clientTest from '@example/meter-client/test';
import * as cellTest from '@example/cell-binding/test';
import * as namesTest from '@example/names-binding/test';
import * as functionTest from '@example/functions-binding/test';
import * as dataTest from '@example/data-client/test';
import {adapterCount} from '@example/data-client/types';
import {adapterInput,validateWire,type ServerModel,type Client} from '@example/meter-client/types';
import {DuplexError,jsonAdapter} from '@nightseam/runtime';
import {DuplexPeer} from '@nightseam/runtime';
import {callWire} from '@nightseam/runtime';
import type {Endpoint} from '@nightseam/duplex';
import {pipe as framePipe} from '@nightseam/duplex';
import {liveOver,valueEnvironment} from '@nightseam/live';
import {adapterUnary,type Unary} from '@example/functions-client/types';
import type {Input as CellInput} from '@example/cell-client/types';
function check(value:unknown,message:string):asserts value{if(!value)throw new Error(message);}
async function rejects(run:()=>unknown,fragment?:string){try{await run();}catch(error){if(fragment)check(String(error).includes(fragment),String(error));return;}throw new Error('unexpected success');}
const opposite=():Client=>({methods:{reverse:input=>input},events:{changed(){}}});
check(Number.isSafeInteger(dataTest.example('Count',adapterCount())),'data-only example');
const model:ServerModel=remote=>({methods:{echo:(input,context)=>remote.methods.reverse(input,context),constant:()=>7,decline:()=>{throw new DuplexError('denied','declined');},ping:()=>9},events:{noted(){}}});
await rejects(()=>test.smoke((()=>({methods:{},events:{}})) as unknown as ServerModel,opposite(),{}),'required');
for(const presentation of [undefined,test.local,test.pipe,test.mounted,test.forwarded])await test.smoke(model,opposite(),{presentation});
check(test.example('Input',adapterInput()).value>=1,'synthesized constrained value');
await rejects(()=>test.example('Never',jsonAdapter({type:'json',validate:validateWire})),'unavailable');
await rejects(()=>functionTest.example('Unary',jsonAdapter({type:'json',validate:validateWire})),'native');
const paired=await test.pair(model,{});const access=paired.model(opposite());check(await access.methods.ping({})===9,'pair');paired.close();paired.close();await rejects(()=>access.methods.ping({}));
await rejects(()=>test.smoke(model,opposite(),{inputs:{constant:{value:-1}}}));
await rejects(()=>test.smoke(remote=>({...model(remote),methods:{...model(remote).methods,echo:()=>({value:-1})}}),opposite(),{}));
await rejects(()=>test.smoke(model,opposite(),{presentation:wire=>({wire:{...wire,send(path,message){wire.send(path,path[0]==='constant'&&message.frame.kind==='request'?{...message,frame:{...message.frame,params:{value:77}}}:message);},receive:wire.receive.bind(wire),close:wire.close.bind(wire)},close(){}})}),'constant.request');
await rejects(()=>test.pair(()=>{throw new Error('factory failed');},{}),'factory failed');
{
 let abandoned:Endpoint|undefined;let detached=0;
 await rejects(()=>test.pair(model,{callContext:{signal:AbortSignal.abort()},presentation:wire=>{abandoned=wire;return {wire,close(){detached++;}};}}));
 check(abandoned!==undefined&&detached===1,'failed preparation must release its presentation once');
 await rejects(()=>callWire(abandoned!,['ping'],{}));
}
const numbers=jsonAdapter<number>({type:'integer',validate:validateWire});
const strings=jsonAdapter<string>({type:'string',validate:validateWire});
await namesTest.smoke<string,string,string>(()=>({methods:{echo:input=>input},events:{}}),{methods:{},events:{}},{inputs:{echo:{inputs:'first',close:'second',parameters:'third'}}},strings,strings,strings);
await cellTest.smoke<number>(()=>({methods:{echo:input=>input.value},events:{}}),{methods:{},events:{}},{inputs:{echo:{value:23}}},numbers);
await clientTest.smoke(()=>opposite(),model(opposite()),{});
{
 let observedWire:Endpoint;
 await test.smoke(model,opposite(),{presentation:wire=>{observedWire=wire;return test.local(wire);},equal:async(method,x,y)=>{
  if(method==='constant.request')check(await callWire(observedWire,['ping'],{})===9,'reentrant observer');
  check(JSON.stringify(x)===JSON.stringify(y),'observation differs');
 }});
}
{
 const [a,b]=framePipe();const left=new DuplexPeer(),right=new DuplexPeer({role:'server'});
 const near=liveOver(left,{}),far=liveOver(right,{});await Promise.all([left.attach(a),right.attach(b)]);
 const owner=near.owner().child();const options:cellTest.Options={context:{valueEnvironment:valueEnvironment(far)},remoteContext:{valueEnvironment:valueEnvironment(near)},callContext:{valueContext:owner,signal:AbortSignal.timeout(5000)},inputs:{echo:{value:async(n:number)=>n+1}},equal:async(method,x,y)=>{
  const expected=method.endsWith('.request')?(x as CellInput<Unary>).value:x as Unary;
  const actual=method.endsWith('.request')?(y as CellInput<Unary>).value:y as Unary;
  check(await expected(4,{owner})===5&&await actual(4,{owner})===5,'live behavior');
 }};
 try{
  await cellTest.smoke<Unary>(()=>({methods:{echo:input=>input.value},events:{}}),{methods:{},events:{}},options,adapterUnary());
  await rejects(()=>cellTest.smoke<Unary>(()=>({methods:{echo:input=>input.value},events:{}}),{methods:{},events:{}},{...options,equal:undefined},adapterUnary()),'requires an equal observer for live values');
  await rejects(()=>cellTest.smoke<Unary>(()=>({methods:{echo:input=>input.value},events:{}}),{methods:{},events:{}},{...options,inputs:undefined},adapterUnary()),'native');
  owner.release();near.owner().release();far.owner().release();const until=Date.now()+3000;
  while(near.counts().exports||near.counts().imports||far.counts().exports||far.counts().imports){check(Date.now()<until,'live helper retained caller-owned bindings');await new Promise(resolve=>setTimeout(resolve,1));}
 }finally{left.close();right.close();}
}
`

const goTransparencyPresentations = `
func socket(ctx context.Context,wire duplex.Endpoint)(duplex.Endpoint,func(),error){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var off func()
  peer,err:=runtime.Accept(w,r,runtime.ServerOptions{Options:runtime.Options{Prepare:func(p *runtime.Peer)error{var err error;off,err=runtime.ForwardWire(p.Wire(),wire);return err}},Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}})
  if err!=nil{return};defer off();defer peer.Close();<-peer.Done()
 }))
 client,_,err:=runtime.Dial(ctx,"ws"+strings.TrimPrefix(server.URL,"http"),runtime.DialOptions{})
 if err!=nil{server.Close();return nil,nil,err};return client.Wire(),func(){client.Close();server.Close()},nil
}
func channel(ctx context.Context,wire duplex.Endpoint)(duplex.Endpoint,func(),error){
 a,b:=duplex.Pipe(1<<20)
 left,err:=runtime.NewPeer(ctx,a,runtime.ClientRole,runtime.Options{});if err!=nil{return nil,nil,err}
 right,err:=runtime.NewPeer(ctx,b,runtime.ServerRole,runtime.Options{});if err!=nil{left.Close();return nil,nil,err}
 close:=func(){left.Close();right.Close()}
 ct,err:=tunnel.New(left,tunnel.Options{});if err!=nil{close();return nil,nil,err}
 st,err:=tunnel.New(right,tunnel.Options{});if err!=nil{close();return nil,nil,err}
 accepted:=make(chan error,1);var off func()
 go func(){_,err:=st.Accept(ctx,runtime.Options{Prepare:func(p *runtime.Peer)error{var err error;off,err=runtime.ForwardWire(p.Wire(),wire);return err}});accepted<-err}()
 opened,err:=ct.Open(ctx,"meter",protocol.WireDigest(),runtime.Options{});if err!=nil{close();return nil,nil,err}
 if err:=<-accepted;err!=nil{close();return nil,nil,err}
 return opened,func(){off();opened.Close(1000,"");close()},nil
}
func TestPresentations(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 for carrierName,carrier:=range map[string]test.Presentation{"pipe":test.Pipe,"socket":socket,"channel":channel}{
  for viewName,view:=range map[string]test.Presentation{"direct":test.Local,"mounted":test.Mounted,"forwarded":test.Forwarded}{
   t.Run(carrierName+"/"+viewName,func(t *testing.T){
    presentation:=func(ctx context.Context,wire duplex.Endpoint)(duplex.Endpoint,func(),error){host,close,err:=carrier(ctx,wire);if err!=nil{return nil,nil,err};selected,detach,err:=view(ctx,host);if err!=nil{close();return nil,nil,err};return selected,func(){detach();close()},nil}
    if err:=test.Smoke(ctx,model,opposite(),test.Options{Presentation:presentation});err!=nil{t.Fatal(err)}
   })
  }
 }
}
func TestTypeScriptPresentations(t *testing.T){
 // A host relays two actual socket carriers. Its only knowledge is Endpoint;
 // the model and its generated transparency helper live entirely in Node.
 var mutex sync.Mutex;var next duplex.Endpoint
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var off func();var root duplex.Endpoint
  peer,err:=runtime.Accept(w,r,runtime.ServerOptions{Options:runtime.Options{Prepare:func(p *runtime.Peer)error{
   mutex.Lock();defer mutex.Unlock();if next==nil{var err error;root,next,err=runtime.NewWirePair(runtime.Options{});if err!=nil{return err}}else{root=next;next=nil};var err error;off,err=runtime.ForwardWire(p.Wire(),root);return err
  }},Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}})
  if err!=nil{return};defer off();defer root.Close(1000,"");defer peer.Close();<-peer.Done()
 }));defer server.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),25*time.Second);defer cancel()
 cmd:=exec.CommandContext(ctx,"node","--loader","./runtime-loader.mjs","presentations.ts","ws"+strings.TrimPrefix(server.URL,"http"))
 if out,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("TypeScript presentation matrix: %v\n%s",err,out)}
}
`

const tsTransparencyPresentations = `import * as test from '@example/meter-binding/test';
import type {ServerModel,Client} from '@example/meter-client/types';
import {wireDigest} from '@example/meter-client/types';
import {DuplexPeer,DuplexError,forwardWire} from '@nightseam/runtime';
import {pipe as framePipe} from '@nightseam/duplex';
import {Tunnel} from '@nightseam/tunnel';
declare const process:{argv:string[]};
const model:ServerModel=remote=>({methods:{async echo(input,context){await remote.events.changed(input,context);return remote.methods.reverse(input,context);},constant:()=>7,decline:()=>{throw new DuplexError('denied','declined');},ping:()=>9},events:{noted(){}}});
const opposite=():Client=>({methods:{reverse:input=>input},events:{changed(){}}});
const socket:test.Presentation=async wire=>{
 let off=()=>{};const left=new DuplexPeer(),right=new DuplexPeer({prepare:p=>{off=forwardWire(p.wire(),wire);}});
 const close=()=>{off();left.close();right.close();};
 try{await Promise.all([left.connect(process.argv[2]!),right.connect(process.argv[2]!)]);return {wire:left.wire(),close};}catch(error){close();throw error;}
};
const channel:test.Presentation=async wire=>{
 const [a,b]=framePipe();const left=new DuplexPeer(),right=new DuplexPeer({role:'server'});
 let off=()=>{};const close=()=>{off();left.close();right.close();};
 try{const ct=new Tunnel(left),st=new Tunnel(right);await Promise.all([left.attach(a),right.attach(b)]);
  const waiting=st.accept({prepare:p=>{off=forwardWire(p.wire(),wire);}});
  const opened=await ct.open('meter',wireDigest,{});await waiting;return {wire:opened,close};
 }catch(error){close();throw error;}
};
for(const carrier of [test.pipe,socket,channel])for(const view of [test.local,test.mounted,test.forwarded]){
 const presentation:test.Presentation=async wire=>{const host=await carrier(wire);try{const selected=await view(host.wire);return {wire:selected.wire,close(){selected.close();host.close();}};}catch(error){host.close();throw error;}};
 await test.smoke(model,opposite(),{presentation,callContext:{signal:AbortSignal.timeout(5000)}});
}
`
