package generate

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func exampleAPI(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	err := json.Unmarshal([]byte(`{
 "schema_version":1,"profile":"nighthall.duplex/1","name":"probe",
 "types":{
  "Base":{"kind":"record","fields":[{"name":"text","type":"string"}]},
  "Payload":{"kind":"record","extends":["Base"],"fields":[{"name":"count","type":"integer"},{"name":"note","type":"string","required":false,"nullable":true}]},
  "OpenRecord":{"kind":"record","open":true,"fields":[{"name":"id","type":"string"},{"name":"note","type":"string","required":false}]},
  "Status":{"kind":"enum","values":["ready","done","context.example"]},
  "Payloads":{"kind":"alias","type":{"array":"Payload"}}
 },
 "methods":[
  {"name":"echo","go_name":"Echo","ts_name":"echo","direction":"client_to_server","request":"Payload","result":"Payload"},
  {"name":"reverse","go_name":"Reverse","ts_name":"reverse","direction":"server_to_client","request":"Payload","result":"Payload"},
  {"name":"no_args","go_name":"NoArgs","ts_name":"noArgs","direction":"client_to_server","result":"string"}
 ],
 "events":[{"name":"changed","go_name":"Changed","ts_name":"changed","direction":"server_to_client","type":"Payload"}],
 "errors":[{"code":"denied","description":"The caller is denied"}]
}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDeterministicGeneration(t *testing.T) {
	api := exampleAPI(t)
	first, err := Generate(api, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(api, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("generation is not deterministic")
	}
	if len(first.Files) != 8 {
		t.Fatalf("got %d files", len(first.Files))
	}
	for name, data := range first.Files {
		if len(data) == 0 {
			t.Errorf("empty output %s", name)
		}
	}
	if _, err := Generate(api, Options{ClientPath: "../outside"}); err == nil {
		t.Fatal("accepted escaping path")
	}
}

func TestGeneratedGoFamilyCompilesAndCommunicates(t *testing.T) {
	root := repositoryRoot(t)
	directory := t.TempDir()
	renderFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "api/ts/ws-runtime"), filepath.Join(directory, "api/ts/ws-runtime"))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(`export async function resolve(specifier,context,next){if(specifier==='@nighthall/ws-runtime')return {url:new URL('./api/ts/ws-runtime/src/index.ts',import.meta.url).href,shortCircuit:true};return next(specifier,context);}`))
	writeFixture(t, directory, "roundtrip.mjs", []byte(`import assert from 'node:assert/strict';import {Client} from './api/ts/probe-client/src/index.ts';const client=await Client.dial(process.argv[2],{}, {reverse(params){return {...params,text:'typescript:'+params.text};}});let observed;client.onChanged(data=>{observed=data;});const result=await client.echo({text:'value',count:7,note:null});assert.equal(result.text,'typescript:value');assert.equal(result.note,null);assert.equal(observed.count,7);await assert.rejects(client.echo({text:'bad',count:9007199254740992}));client.close();`))
	writeFixture(t, directory, "integration_test.go", []byte(goIntegrationFixture))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func writeFixture(t *testing.T, root, name string, data []byte) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func copyFixtureTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".test.ts") || filepath.Base(p) == "interop.ts" {
			return nil
		}
		relative, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFixture(t, destination, relative, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func renderFixture(t *testing.T, directory, root string) {
	t.Helper()
	result, err := Generate(exampleAPI(t), Options{Module: "example.test/generated"})
	if err != nil {
		t.Fatal(err)
	}
	for p, data := range result.Files {
		writeFixture(t, directory, p, data)
	}
	copyFixtureTree(t, filepath.Join(root, "api/go/ws-runtime"), filepath.Join(directory, "api/go/ws-runtime"))
	writeFixture(t, directory, "go.mod", []byte("module example.test/generated\n\ngo 1.25.0\n\nrequire github.com/coder/websocket v1.8.15\n"))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "go.sum", sum)
}
func runFixture(t *testing.T, directory, program string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", program, args, err, output)
	}
}

func TestGeneratedTypeScriptChecksAndValidates(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is not installed")
	}
	root := repositoryRoot(t)
	tsc := filepath.Join(root, "node_modules/typescript/bin/tsc")
	if _, err := os.Stat(tsc); err != nil {
		t.Skip("TypeScript parser is not installed")
	}
	directory := t.TempDir()
	renderFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "api/ts/ws-runtime"), filepath.Join(directory, "api/ts/ws-runtime"))
	// Node refuses to strip source TypeScript inside node_modules. A paths entry
	// gives the compiler the runtime; executable validation imports protocol types.
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": map[string]any{"@nighthall/ws-runtime": []string{"./api/ts/ws-runtime/src/index.ts"}}}, "include": []string{"api/ts/**/*.ts"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	writeFixture(t, directory, "validation.mjs", []byte(`import assert from 'node:assert/strict';import {validateWire} from './api/ts/probe-client/src/types.ts';
validateWire('Payload',{text:'hello',count:4});validateWire('Payload',{text:'hello',count:4,note:null});
assert.throws(()=>validateWire('Payload',{text:'hello',count:9007199254740992}));assert.throws(()=>validateWire('Payload',{text:'hello'}));assert.throws(()=>validateWire('Payload',{text:'hello',count:4,unknown:1}));assert.throws(()=>validateWire('Payload',{text:'hello',count:4,note:undefined}));assert.throws(()=>validateWire({array:'integer'},new Array(1)));assert.throws(()=>validateWire({map:'integer'},new Date()));
validateWire('OpenRecord',{id:'open',extra:{supported:true}});assert.throws(()=>validateWire('Status','unknown'));assert.throws(()=>validateWire('timestamp','2024-02-30T00:00:00Z'));validateWire('timestamp','2024-02-29T00:00:00Z');
`))
	runFixture(t, directory, "node", "validation.mjs")
}

func TestWorkbenchContractRenders(t *testing.T) {
	data, err := os.ReadFile("testdata/workbench-api.json")
	if err != nil {
		t.Fatal(err)
	}
	var api map[string]any
	if err = json.Unmarshal(data, &api); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(api, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 8 {
		t.Fatalf("API writes %d files", len(result.Files))
	}
	if !strings.Contains(string(result.Files["api/ts/workbench-client/src/index.ts"]), "async subscribe(") {
		t.Fatal("missing typed subscription acknowledgement")
	}
}

const goIntegrationFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "strings"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 client "example.test/generated/api/go/probe-client"
 protocol "example.test/generated/api/go/probe-protocol"
 runtime "example.test/generated/api/go/ws-runtime"
)
type serverHandler struct{}
func(serverHandler)Echo(ctx context.Context,remote *binding.Remote,p protocol.Payload)(protocol.Payload,error){if err:=remote.EmitChanged(ctx,p);err!=nil{return p,err};return remote.Reverse(ctx,p)}
func(serverHandler)NoArgs(context.Context,*binding.Remote)(string,error){return "ok",nil}
type clientHandler struct{}
func(clientHandler)Reverse(ctx context.Context,c *client.Client,p protocol.Payload)(protocol.Payload,error){p.Text="reversed:"+p.Text;return p,nil}
func TestRoundTrip(t *testing.T){h,err:=binding.NewHandler(serverHandler{},runtime.ServerOptions{Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}});if err!=nil{t.Fatal(err)};server:=httptest.NewServer(h);defer server.Close();ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();c,err:=client.Dial(ctx,"ws"+strings.TrimPrefix(server.URL,"http"),runtime.DialOptions{},clientHandler{});if err!=nil{t.Fatal(err)};defer c.Close();result,err:=c.Echo(ctx,protocol.Payload{Text:"value",Count:3,Note:runtime.Some(runtime.Null[string]())});if err!=nil{t.Fatal(err)};if result.Text!="reversed:value"||!result.Note.Present||!result.Note.Value.Null{t.Fatalf("unexpected roundtrip %#v",result)}}
func TestGeneratedTypeScript(t *testing.T){if _,err:=exec.LookPath("node");err!=nil{t.Skip("Node is not installed")};h,err:=binding.NewHandler(serverHandler{},runtime.ServerOptions{Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}});if err!=nil{t.Fatal(err)};server:=httptest.NewServer(h);defer server.Close();ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel();command:=exec.CommandContext(ctx,"node","--loader","./runtime-loader.mjs","roundtrip.mjs","ws"+strings.TrimPrefix(server.URL,"http"));if output,err:=command.CombinedOutput();err!=nil{t.Fatalf("Node generated client: %v\n%s",err,output)}}
func TestEmptyRequestAndNullResult(t *testing.T){ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();options:=runtime.ServerOptions{Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}};h,err:=binding.NewHandler(serverHandler{},options);if err!=nil{t.Fatal(err)};server:=httptest.NewServer(h);defer server.Close();c,err:=client.Dial(ctx,"ws"+strings.TrimPrefix(server.URL,"http"),runtime.DialOptions{},clientHandler{});if err!=nil{t.Fatal(err)};defer c.Close();var raw json.RawMessage;if err=c.Peer.Call(ctx,"no_args",42,&raw);err==nil{t.Fatal("accepted scalar request")};options.Options.Handlers=map[string]runtime.Handler{"no_args":func(context.Context,*runtime.Peer,json.RawMessage)(any,error){return nil,nil}};badHandler,err:=runtime.NewHandler(options);if err!=nil{t.Fatal(err)};badServer:=httptest.NewServer(badHandler);defer badServer.Close();badClient,err:=client.Dial(ctx,"ws"+strings.TrimPrefix(badServer.URL,"http"),runtime.DialOptions{},clientHandler{});if err!=nil{t.Fatal(err)};defer badClient.Close();if _,err=badClient.NoArgs(ctx);err==nil{t.Fatal("accepted null string reply")}}
func TestOpenOwnershipAndPrecision(t *testing.T){open:=protocol.OpenRecord{ID:"o",AdditionalFields:map[string]json.RawMessage{"note":json.RawMessage("\"injected\"")}};if _,err:=json.Marshal(open);err==nil{t.Fatal("extension overwrote omitted declared field")};for _,data:=range []string{"{\"text\":\"a\",\"count\":9007199254740991.1}","{\"text\":\"a\",\"count\":1.00000000000000001}","{\"text\":\"a\",\"count\":1e-1000000000}"}{var p protocol.Payload;if err:=json.Unmarshal([]byte(data),&p);err==nil{t.Fatalf("accepted imprecise integer %s",data)}};if err:=protocol.ValidateExpressionRaw("json",[]byte("{\"huge\":1e999}"));err==nil{t.Fatal("accepted nonfinite arbitrary JSON")}}
func TestCodecs(t *testing.T){for _,input:=range []string{"{}","{\"text\":\"a\",\"count\":9007199254740992}","{\"text\":\"a\",\"count\":1,\"extra\":1}","{\"text\":null,\"count\":1}"}{var value protocol.Payload;if err:=json.Unmarshal([]byte(input),&value);err==nil{t.Errorf("accepted %s",input)}};for _,input:=range []string{"{\"text\":\"a\",\"count\":1}","{\"text\":\"a\",\"count\":1,\"note\":null}","{\"text\":\"a\",\"count\":1,\"note\":\"n\"}"}{var value protocol.Payload;if err:=json.Unmarshal([]byte(input),&value);err!=nil{t.Fatal(err)};data,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};var again protocol.Payload;if err=json.Unmarshal(data,&again);err!=nil{t.Fatal(err)}};var open protocol.OpenRecord;if err:=json.Unmarshal([]byte("{\"id\":\"o\",\"extension\":true}"),&open);err!=nil{t.Fatal(err)};if len(open.AdditionalFields)!=1{t.Fatal("lost open fields")};if _,err:=json.Marshal(open);err!=nil{t.Fatal(err)}}
`
