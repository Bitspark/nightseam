package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

func TestProofInheritanceKeepsGovernanceOnItsTierAndSide(t *testing.T) {
	source := proofSource(t)
	source["contracts/probe/session.json"] = &fstest.MapFile{Data: []byte(`{"decides":["echo"],"asks":["reverse"],"conversation":{"event":"changed","path":"text"}}`)}
	k, _ := toolKernel(load.Config{}, module, scope, "")
	world := k.Load(source, "contracts")
	facts := render.Build(analysis.Resolve(analysis.World(world.Families), "proof"))
	if facts.Session != nil {
		t.Fatal("extending a side silently acquired a session tier")
	}
	source["contracts/proof/session.json"] = &fstest.MapFile{Data: []byte(`{}`)}
	world = k.Load(source, "contracts")
	if _, err := k.Render(world, "proof"); err != nil {
		t.Fatal(err)
	}
	facts = render.Build(analysis.Resolve(analysis.World(world.Families), "proof"))
	if facts.Session == nil || len(facts.Session.Decides) != 1 || facts.Session.Decides[0] != "echo" || len(facts.Session.Asks) != 0 || facts.Session.Conversation == nil || facts.Session.Conversation.Event != "changed" || facts.Session.Conversation.Path != "text" {
		t.Fatalf("governance did not follow only the extended server side: %#v", facts.Session)
	}
}

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
		typescript.New(typescript.Config{Scope: scope, Place: map[string]string{"proof": "gen/ts/{family}-client"}}),
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
	for _, component := range []string{"runtime", "duplex", "tunnel"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": map[string]any{"@example/*": []string{"./api/ts/*/src/index.ts"}, "@nightseam/runtime": []string{"./runtime/ts/src/index.ts"}, "@nightseam/duplex": []string{"./duplex/ts/src/index.ts"}, "@nightseam/tunnel": []string{"./tunnel/ts/src/index.ts"}}}, "include": []string{"api/ts/**/*.ts", "gen/**/*.ts", "diagram.ts"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "diagram.ts", []byte(tsProofDiagram))
	writeFixture(t, directory, "diagram.mjs", []byte(tsProofDiagramValues))
	writeFixture(t, directory, "loader.mjs", []byte(runtimeLoader))
	// Reuse the structural comparison already holding the family-only diagram.
	start := strings.Index(goDiagramFixture, "func same(")
	end := strings.Index(goDiagramFixture, "func TestInstantiationIsTheLeftPath(")
	writeFixture(t, directory, "proof_test.go", []byte(goProofDiagram+goDiagramFixture[start:end]))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

const goProofDiagram = `package generated
import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "reflect"
 "strings"
 "testing"
 "time"
 left "example.test/generated/api/go/proof-protocol"
 lc "example.test/generated/api/go/proof-client"
 lb "example.test/generated/api/go/proof-binding"
 right "example.test/generated/gen/go/proof-protocol"
 rc "example.test/generated/gen/go/proof-client"
 rb "example.test/generated/gen/go/proof-binding"
 probe "example.test/generated/api/go/probe-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
)
type E=probe.Envelope
type H=probe.Handle
type leftServer struct{lb.Handler}
type rightServer struct{rb.Handler[E,H,string]}
func (leftServer) Relay(_ context.Context,_ *lb.Remote,p left.Carried)(left.Option[left.Envelope],error){
 data,err:=json.Marshal(p.Message);if err!=nil{return left.Option[left.Envelope]{},err};var value left.Envelope
 if err=json.Unmarshal(data,&value);err!=nil{return left.Option[left.Envelope]{},err}
 return left.Option[left.Envelope]{Some:&left.OptionSomeValue[left.Envelope]{Value:value}},nil
}
func (rightServer) Relay(_ context.Context,_ *rb.Remote[E,H,string],p right.Carried[E,H,string])(right.Option[right.Envelope],error){
 data,err:=json.Marshal(p.Message);if err!=nil{return right.Option[right.Envelope]{},err};var value right.Envelope
 if err=json.Unmarshal(data,&value);err!=nil{return right.Option[right.Envelope]{},err}
 return right.Option[right.Envelope]{Some:&right.OptionSomeValue[right.Envelope]{Value:value}},nil
}
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
func TestMixedClientsCrossBothPaths(t *testing.T){
 options:=runtime.ServerOptions{Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}}
 leftHandler,err:=lb.NewHandler(leftServer{},options);if err!=nil{t.Fatal(err)}
 rightHandler,err:=rb.NewHandler[E,H,string](rightServer{},options);if err!=nil{t.Fatal(err)}
 ls,rs:=httptest.NewServer(leftHandler),httptest.NewServer(rightHandler);defer ls.Close();defer rs.Close()
 leftURL,rightURL:="ws"+strings.TrimPrefix(ls.URL,"http"),"ws"+strings.TrimPrefix(rs.URL,"http")
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 l,err:=lc.Dial(ctx,rightURL,runtime.DialOptions{},nil,lc.Events{});if err!=nil{t.Fatal(err)};defer l.Close()
 r,err:=rc.Dial[E,H,string](ctx,leftURL,runtime.DialOptions{},nil,rc.Events[E,H,string]{});if err!=nil{t.Fatal(err)};defer r.Close()
 var lp left.Carried;var rp right.Carried[E,H,string];_ = json.Unmarshal(good,&lp);_ = json.Unmarshal(good,&rp)
 lv,err:=l.Relay(ctx,lp);if err!=nil{t.Fatal(err)};rv,err:=r.Relay(ctx,rp);if err!=nil{t.Fatal(err)}
 a,_:=json.Marshal(lv);b,_:=json.Marshal(rv);if string(a)!=string(b){t.Fatalf("left %s right %s",a,b)}
 command:=exec.CommandContext(ctx,"node","--loader","./loader.mjs","diagram.mjs",leftURL,rightURL)
 if output,err:=command.CombinedOutput();err!=nil{t.Fatalf("TypeScript mixed diagram: %v\n%s",err,output)}
}
`

const tsProofDiagram = `import type * as left from './api/ts/proof-client/src/index.ts';
import type * as right from './gen/ts/proof-client/src/index.ts';
import type * as probe from './api/ts/probe-client/src/index.ts';
type Equals<A,B>=(<T>()=>T extends A?1:2) extends (<T>()=>T extends B?1:2)?true:false;
export const carried:Equals<left.Carried,right.Carried<probe.Family,string>>=true;
export const caller:Equals<left.Caller,right.Caller<probe.Family,string>>=true;
export const handler:Equals<left.Handler,right.Handler<probe.Family,string>>=true;
export const events:Equals<left.Events,right.Events<probe.Family,string>>=true;
// @ts-expect-error A type argument does not fill the family parameter.
type WrongFamily=right.Carried<string,string>;
// @ts-expect-error The distinct Item argument stays string.
const wrong: right.Carried<probe.Family,string>={message:{version:1,kind:'event'},back:null,page:{items:[7]}};
`

const tsProofDiagramValues = `import assert from 'node:assert/strict';
import * as left from './api/ts/proof-client/src/index.ts';
import * as right from './gen/ts/proof-client/src/index.ts';
import * as probe from './api/ts/probe-client/src/index.ts';
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
const plain=await left.Client.dial(process.argv[3],{},undefined,{});
const generic=await right.Client.dial(process.argv[2],probe.family,slots.Item,{},undefined,{});
try{
 for(const client of [plain,generic]){
  assert.deepEqual(await client.relay(good),{kind:'some',value:good.message});
  await assert.rejects(client.relay({...good,page:{items:[7]}}));
 }
}finally{plain.close();generic.close();}
`
