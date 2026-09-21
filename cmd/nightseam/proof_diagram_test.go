package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// proofSource keeps every experiment on the promoted corpus, including its
// imported probe. Mutations below apply to an in-memory copy of tier files.
func proofSource(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, family := range []string{"probe", "proof"} {
		directory := filepath.Join(familiesRoot, "api/contracts", family)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files["contracts/"+family+"/"+entry.Name()] = &fstest.MapFile{Data: data}
		}
	}
	return files
}

// The left path substitutes JSON type references before loading, independently
// of the generator's semantic substitution and generic rendering machinery.
func bindProofSource(t *testing.T, files fstest.MapFS) {
	t.Helper()
	path := "contracts/proof/protocol.json"
	var source map[string]any
	if err := json.Unmarshal(files[path].Data, &source); err != nil {
		t.Fatal(err)
	}
	delete(source, "parameters")
	var bind func(any) any
	bind = func(value any) any {
		switch v := value.(type) {
		case string:
			switch v {
			case "S.Envelope":
				return "probe.Envelope"
			case "S.Handle":
				return "probe.Handle"
			case "Item":
				return "string"
			}
		case []any:
			for i, x := range v {
				v[i] = bind(x)
			}
		case map[string]any:
			for k, x := range v {
				v[k] = bind(x)
			}
		}
		return value
	}
	data, err := json.Marshal(bind(source))
	if err != nil {
		t.Fatal(err)
	}
	files[path] = &fstest.MapFile{Data: data}
}

func renderProofDiagram(t *testing.T, directory string) {
	t.Helper()
	source := proofSource(t)
	left, _ := toolKernel(load.Config{}, module, scope, "")
	generic := left.Load(source, "contracts")
	bindProofSource(t, source)
	bound := left.Load(source, "contracts")
	for _, name := range []string{"probe", "proof"} {
		result, err := left.Render(bound, name)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(t, directory, result.Files)
	}
	right := kernel.New(
		golang.New(golang.Config{Module: module, Place: map[string]golang.Layout{"proof": {Protocol: "gen/go/{family}-protocol", Binding: "gen/go/{family}-binding", Client: "gen/go/{family}-client"}}}),
		typescript.New(typescript.Config{Scope: scope, Place: map[string]typescript.Layout{"proof": {Client: "gen/ts/{family}-client", Binding: "gen/ts/{family}-binding"}}}),
	)
	result, err := right.Render(generic, "proof")
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, directory, result.Files)
}

func TestProofMixedDiagramCommutes(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	renderProofDiagram(t, directory)
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)}, "include": []string{"api/ts/**/*.ts", "gen/**/*.ts", "diagram.ts"}, "exclude": []string{"gen/ts/*-binding"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "diagram.ts", []byte(tsProofDiagram))
	writeFixture(t, directory, "diagram.mjs", []byte(tsProofDiagramValues))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	// Each route has its own package instance: the generic binding must import
	// its generic converters, never the independently source-bound artifact.
	writeFixture(t, directory, "loader.mjs", []byte(proofDiagramLoader))
	// Reuse the structural comparison already holding the family-only diagram.
	start := strings.Index(goDiagramFixture, "func same(")
	end := strings.Index(goDiagramFixture, "func TestInstantiationIsTheLeftPath(")
	writeFixture(t, directory, "proof_test.go", []byte(goProofDiagram+goDiagramFixture[start:end]))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	checkGenericTypeScriptBindings(t, directory, tsc)
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

const goProofDiagram = `package generated
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "reflect"
 "strings"
 "testing"
 "time"
 left "example.test/generated/api/go/proof-protocol"
 lb "example.test/generated/api/go/proof-binding"
 right "example.test/generated/gen/go/proof-protocol"
 rb "example.test/generated/gen/go/proof-binding"
 probe "example.test/generated/api/go/probe-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
 "github.com/Bitspark/nightseam/duplex/go"
)
type E=probe.Envelope
type H=probe.Handle
type leftServer struct{left.ServerMethods}
type rightServer struct{right.ServerMethods[E,H,string]}
func (leftServer) Relay(_ context.Context,p left.Carried)(left.Option[left.Envelope],error){
 data,err:=json.Marshal(p.Message);if err!=nil{return left.Option[left.Envelope]{},err};var value left.Envelope
 if err=json.Unmarshal(data,&value);err!=nil{return left.Option[left.Envelope]{},err}
 return left.Option[left.Envelope]{Some:&left.OptionSomeValue[left.Envelope]{Value:value}},nil
}
func (rightServer) Relay(_ context.Context,p right.Carried[E,H,string])(right.Option[right.Envelope],error){
 data,err:=json.Marshal(p.Message);if err!=nil{return right.Option[right.Envelope]{},err};var value right.Envelope
 if err=json.Unmarshal(data,&value);err!=nil{return right.Option[right.Envelope]{},err}
 return right.Option[right.Envelope]{Some:&right.OptionSomeValue[right.Envelope]{Value:value}},nil
}
type discardLeftEvents struct{left.ClientEvents}
func(discardLeftEvents)PartAdded(context.Context,left.RichPart)error{return nil}
func(discardLeftEvents)Changed(context.Context,probe.Payload)error{return nil}
type discardRightEvents struct{right.ClientEvents[E,H,string]}
func(discardRightEvents)PartAdded(context.Context,right.RichPart)error{return nil}
func(discardRightEvents)Changed(context.Context,probe.Payload)error{return nil}
var good=[]byte("{\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}},\"back\":null,\"page\":{\"items\":[\"hello\"],\"next\":null}}")
func TestMixedStructureAndCodecs(t *testing.T){
 if !same(reflect.TypeOf(left.Carried{}),reflect.TypeOf(right.Carried[E,H,string]{})){t.Fatal("mixed family/type binding changed the structure")}
 for _,data:=range [][]byte{good,[]byte(strings.Replace(string(good),"[\"hello\"]","[7]",1)),[]byte(strings.Replace(string(good),"\"kind\":\"event\",","",1)),[]byte(strings.Replace(string(good),"\"back\":null","\"back\":{\"channel\":\"wrong\"}",1))}{
  var l left.Carried;var r right.Carried[E,H,string]
  le,re:=json.Unmarshal(data,&l),json.Unmarshal(data,&r)
  if (le==nil)!=(re==nil){t.Fatalf("left %v right %v for %s",le,re,data)}
  if string(data)!=string(good)&&le==nil{t.Fatalf("both paths admitted %s",data)}
  if le==nil{a,_:=json.Marshal(l);b,_:=json.Marshal(r);if string(a)!=string(b){t.Fatalf("left %s right %s",a,b)}}
 }
}
func TestMixedClientsRoundtripBothDeclarations(t *testing.T){
 serve:=func(build func(*runtime.Peer)(duplex.Endpoint,error))*httptest.Server{
  options:=runtime.ServerOptions{Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}}
  options.Options.Prepare=func(peer *runtime.Peer)error{
   model,err:=build(peer);if err!=nil{return err};if _,err=runtime.ForwardWire(peer.Wire(),model);err!=nil{_ = model.Close(duplex.CodeInternalError,"setup failed");return err}
   go func(){<-peer.Done();_ = model.Close(duplex.CodeNormal,"")}();return nil
  }
  handler,err:=runtime.NewHandler(options);if err!=nil{t.Fatal(err)};return httptest.NewServer(handler)
 }
 ls:=serve(func(*runtime.Peer)(duplex.Endpoint,error){return lb.ToWire(func(left.Client)(left.Server,error){return left.Server{Methods:leftServer{},Events:struct{}{}},nil},runtime.AdapterContext{})})
 rs:=serve(func(peer *runtime.Peer)(duplex.Endpoint,error){return rb.ToWire[E,H,string](func(right.Client[E,H,string])(right.Server[E,H,string],error){return right.Server[E,H,string]{Methods:rightServer{},Events:struct{}{}},nil},runtime.AdapterContext{},runtime.JSONAdapter[E](),runtime.JSONAdapter[H](),runtime.JSONAdapter[string]())})
 defer ls.Close();defer rs.Close()
 leftURL,rightURL:="ws"+strings.TrimPrefix(ls.URL,"http"),"ws"+strings.TrimPrefix(rs.URL,"http")
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 var lc func(context.Context)(left.ServerModel,error);var lclose func()
 lpPeer,_,err:=runtime.Dial(ctx,leftURL,runtime.DialOptions{Options:runtime.Options{Prepare:func(peer *runtime.Peer)error{
  var err error;lc,lclose,err=lb.PrepareFromWire(peer.Wire(),runtime.AdapterContext{});return err
 }}});if err!=nil{t.Fatal(err)};defer lpPeer.Close()
 defer lclose();lf,err:=lc(ctx);if err!=nil{t.Fatal(err)};l,err:=lf(left.Client{Methods:struct{}{},Events:discardLeftEvents{}});if err!=nil{t.Fatal(err)}
 var rc func(context.Context)(right.ServerModel[E,H,string],error);var rclose func()
 rpPeer,_,err:=runtime.Dial(ctx,rightURL,runtime.DialOptions{Options:runtime.Options{Prepare:func(peer *runtime.Peer)error{
  var err error;rc,rclose,err=rb.PrepareFromWire[E,H,string](peer.Wire(),runtime.AdapterContext{},runtime.JSONAdapter[E](),runtime.JSONAdapter[H](),runtime.JSONAdapter[string]());return err
 }}});if err!=nil{t.Fatal(err)};defer rpPeer.Close()
 defer rclose();rf,err:=rc(ctx);if err!=nil{t.Fatal(err)};r,err:=rf(right.Client[E,H,string]{Methods:struct{}{},Events:discardRightEvents{}});if err!=nil{t.Fatal(err)}
 var lp left.Carried;var rp right.Carried[E,H,string];_ = json.Unmarshal(good,&lp);_ = json.Unmarshal(good,&rp)
 lv,err:=l.Methods.Relay(ctx,lp);if err!=nil{t.Fatal(err)};rv,err:=r.Methods.Relay(ctx,rp);if err!=nil{t.Fatal(err)}
 a,_:=json.Marshal(lv);b,_:=json.Marshal(rv);if string(a)!=string(b){t.Fatalf("left %s right %s",a,b)}
 command:=exec.CommandContext(ctx,"node","--loader","./loader.mjs","diagram.mjs",leftURL,rightURL)
 if output,err:=command.CombinedOutput();err!=nil{t.Fatalf("TypeScript mixed diagram: %v\n%s",err,output)}
}
func TestMixedDeclarationsRefuseCrossInterpretation(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 plain,err:=lb.ToWire(func(left.Client)(left.Server,error){return left.Server{Methods:leftServer{},Events:struct{}{}},nil},runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer plain.Close(duplex.CodeNormal,"")
 generic,err:=rb.ToWire[E,H,string](func(right.Client[E,H,string])(right.Server[E,H,string],error){return right.Server[E,H,string]{Methods:rightServer{},Events:struct{}{}},nil},runtime.AdapterContext{},runtime.JSONAdapter[E](),runtime.JSONAdapter[H](),runtime.JSONAdapter[string]());if err!=nil{t.Fatal(err)};defer generic.Close(duplex.CodeNormal,"")
 _,leftErr:=lb.FromWire(ctx,generic,runtime.AdapterContext{})
 _,rightErr:=rb.FromWire[E,H,string](ctx,plain,runtime.AdapterContext{},runtime.JSONAdapter[E](),runtime.JSONAdapter[H](),runtime.JSONAdapter[string]())
 for _,err:=range []error{leftErr,rightErr}{var public *runtime.PublicError;if !errors.As(err,&public)||public.Code!="contract_mismatch"{t.Fatalf("cross-interpretation: %v",err)}}
}
`

const tsProofDiagram = `import type * as left from './api/ts/proof-client/src/index.ts';
import type * as right from './gen/ts/proof-client/src/index.ts';
import type * as probe from './api/ts/probe-client/src/index.ts';
type Equals<A,B>=(<T>()=>T extends A?1:2) extends (<T>()=>T extends B?1:2)?true:false;
export const carried:Equals<left.Carried,right.Carried<probe.Family,string>>=true;
export const caller:Equals<left.ServerMethods,right.ServerMethods<probe.Family,string>>=true;
export const handler:Equals<left.ClientMethods,right.ClientMethods<probe.Family,string>>=true;
export const events:Equals<left.ClientEvents,right.ClientEvents<probe.Family,string>>=true;
// @ts-expect-error A type argument does not fill the family parameter.
type WrongFamily=right.Carried<string,string>;
// @ts-expect-error The distinct Item argument stays string.
const wrong: right.Carried<probe.Family,string>={message:{version:1,kind:'event'},back:null,page:{items:[7]}};
`

const tsProofDiagramValues = `import assert from 'node:assert/strict';
import {jsonAdapter,DuplexPeer} from '@nightseam/runtime';
import * as left from './api/ts/proof-client/src/index.ts';
import * as right from './gen/ts/proof-client/src/index.ts';
import * as probe from './api/ts/probe-client/src/index.ts';
import * as leftBinding from './api/ts/proof-binding/src/index.ts';
import * as rightBinding from './gen/ts/proof-binding/src/index.ts';
const slots={S:probe.family,Item:{type:'string',validate:probe.validateWire}};
const good={message:{version:1,kind:'event',event:'changed',data:{}},back:null,page:{items:['hello'],next:null}};
for(const value of [good,{...good,page:{items:[7]}},{...good,message:{version:1}},{...good,back:{channel:'wrong'}}]){
 const errors=[];
 for(const validate of [v=>left.validateWire('Carried',v),v=>right.validateWire('Carried',v,'$',slots)]){
  try{validate(value);errors.push(null);}catch(error){errors.push(error.message);}
 }
 assert.deepEqual(errors[0],errors[1]);
 assert.equal(errors[0]===null,value===good);
}
const plainPeer=new DuplexPeer();const genericPeer=new DuplexPeer();
const reverse={methods:{},events:{changed(){},partAdded(){}}};
const plainPreparation=leftBinding.prepareFromWire(plainPeer.wire(),{});
const genericPreparation=rightBinding.prepareFromWire(genericPeer.wire(),{},probe.family,jsonAdapter(slots.Item));
await plainPeer.connect(process.argv[2]);await genericPeer.connect(process.argv[3]);
try{
 const plain=(await plainPreparation.complete())(reverse).methods;
 const generic=(await genericPreparation.complete())(reverse).methods;
 for(const client of [plain,generic]){
  assert.deepEqual(await client.relay(good),{kind:'some',value:good.message});
  await assert.rejects(client.relay({...good,page:{items:[7]}}));
 }
}finally{plainPeer.close();genericPeer.close();}
for(const [url,prepare] of [[process.argv[3],wire=>leftBinding.prepareFromWire(wire,{})],[process.argv[2],wire=>rightBinding.prepareFromWire(wire,{},probe.family,jsonAdapter(slots.Item))]]){
 const peer=new DuplexPeer();const preparation=prepare(peer.wire());
 try{await peer.connect(url);await assert.rejects(preparation.complete(),error=>error.code==='contract_mismatch');}
 finally{preparation.close();peer.close();}
}
`

const proofDiagramLoader = `import {resolve as base} from './runtime-loader.mjs';
export async function resolve(specifier,context,next){
 if(specifier.startsWith('@example/proof-')&&context.parentURL?.includes('/gen/ts/')){
  const name=specifier.slice('@example/'.length);
  const entry=name.endsWith('/types')?'./gen/ts/'+name.slice(0,-6)+'/src/types.ts':'./gen/ts/'+name+'/src/index.ts';
  return {url:new URL(entry,import.meta.url).href,shortCircuit:true};
 }
 return base(specifier,context,next);
}
`
