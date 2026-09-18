package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/kernel"
)

// module and scope root the fixtures' generated packages.
const module = "example.test/generated"
const scope = "@example"

// These tests exercise the tool as composed: every language, through the
// kernel, from the contract to compiled and communicating packages.

const probeContract = `{
 "schema_version":1,"profile":"nightseam.duplex/1","name":"probe",
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
}`

func exampleAPI(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(probeContract), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDeterministicGeneration(t *testing.T) {
	api := exampleAPI(t)
	first, err := kernel.Generate(api, languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	second, err := kernel.Generate(api, languages(module, scope)...)
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
}

func TestGeneratedGoFamilyCompilesAndCommunicates(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go", "node")
	directory := t.TempDir()
	renderFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
	copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
	copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(`export async function resolve(specifier,context,next){const map={'@nightseam/runtime':'./runtime/ts/src/index.ts','@nightseam/duplex':'./duplex/ts/src/index.ts','@nightseam/tunnel':'./tunnel/ts/src/index.ts'};if(map[specifier])return {url:new URL(map[specifier],import.meta.url).href,shortCircuit:true};return next(specifier,context);}`))
	writeFixture(t, directory, "roundtrip.mjs", []byte(`import assert from 'node:assert/strict';import {Client} from './api/ts/probe-client/src/index.ts';const client=await Client.dial(process.argv[2],{}, {reverse(params){return {...params,text:'typescript:'+params.text};}});let observed;client.onChanged(data=>{observed=data;});const result=await client.echo({text:'value',count:7,note:null});assert.equal(result.text,'typescript:value');assert.equal(result.note,null);assert.equal(observed.count,7);await assert.rejects(client.echo({text:'bad',count:9007199254740992}));client.close();`))
	writeFixture(t, directory, "integration_test.go", []byte(goIntegrationFixture))
	writeFixture(t, directory, "tunnel_test.go", []byte(goTunnelFixture))
	writeFixture(t, directory, "tunnel-roundtrip.mjs", []byte(tsTunnelRoundtrip))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

// fixture marks a test that hands generated code to a toolchain — go, node,
// tsc — and returns the path of tsc. The tests that do are the slow tier:
// under -short they are skipped, and the fast tier — the model, every
// target's Check, the golden output — runs alone. Without -short they run,
// and a toolchain that is missing fails the test rather than skipping it,
// since a skip nobody reads is a gate nobody passes: the README names what
// the fixtures need.
func fixture(t *testing.T, root string, programs ...string) (tsc string) {
	t.Helper()
	if testing.Short() {
		t.Skip("a fixture test; skipped under -short")
	}
	for _, program := range programs {
		if program == "tsc" {
			tsc = filepath.Join(root, "node_modules/typescript/bin/tsc")
			if _, err := os.Stat(tsc); err != nil {
				t.Fatalf("the fixture needs the TypeScript compiler at %s; run pnpm install", tsc)
			}
			continue
		}
		if _, err := exec.LookPath(program); err != nil {
			t.Fatalf("the fixture needs %s on the path: %v", program, err)
		}
	}
	return tsc
}

// repositoryRoot is the nearest ancestor of the test's directory that holds
// go.mod, so moving the package does not move the fixtures' source.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
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
			// A package's installed dependencies are the workspace's links,
			// not its sources; the fixture maps the packages it needs itself.
			if d.Name() == "node_modules" {
				return fs.SkipDir
			}
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
	result, err := kernel.Generate(exampleAPI(t), languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	for p, data := range result.Files {
		writeFixture(t, directory, p, data)
	}
	// The generated packages bind to the runtime by its import path; the
	// fixture module resolves Nightseam to this checkout, so the runtime and
	// the seam beneath it are the real ones and only the generated packages
	// are the copy under test.
	writeFixture(t, directory, "go.mod", []byte("module example.test/generated\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/Bitspark/nightseam v0.0.0\n\tgithub.com/coder/websocket v1.8.15\n)\n\nreplace github.com/Bitspark/nightseam => "+filepath.ToSlash(root)+"\n"))
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
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	renderFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
	copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
	copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
	// Node refuses to strip source TypeScript inside node_modules. A paths entry
	// gives the compiler the runtime; executable validation imports protocol types.
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": map[string]any{"@nightseam/runtime": []string{"./runtime/ts/src/index.ts"}, "@nightseam/duplex": []string{"./duplex/ts/src/index.ts"}, "@nightseam/tunnel": []string{"./tunnel/ts/src/index.ts"}}}, "include": []string{"api/ts/**/*.ts", "runtime/ts/**/*.ts", "duplex/ts/**/*.ts", "tunnel/ts/**/*.ts"}}
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
	result, err := kernel.Generate(api, languages(module, scope)...)
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

// run executes the tool against a checkout and returns what it printed.
func run(t *testing.T, root string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	command := newCommand()
	var out, errs bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errs)
	command.SetArgs(append([]string{"--root", root, "--module", module, "--scope", scope}, args...))
	err = command.Execute()
	return out.String(), errs.String(), err
}

// TestCommands drives generate, check and validate over a checkout that
// holds one contract: what is stale is written once, then holds.
func TestCommands(t *testing.T) {
	root := t.TempDir()
	if _, _, err := run(t, root, "generate"); err == nil || !strings.Contains(err.Error(), "no contracts in") {
		t.Fatalf("an empty checkout generated: %v", err)
	}
	writeLayers(t, root, "probe", false)
	out, _, err := run(t, root, "validate")
	if err != nil || !strings.Contains(out, "1 contracts; valid") {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	_, errs, err := run(t, root, "check")
	if err == nil || !strings.Contains(err.Error(), "stale") || !strings.Contains(errs, "stale generated output: api/go/probe-protocol/types_generated.go") {
		t.Fatalf("check passed an ungenerated checkout: %v\n%s", err, errs)
	}
	out, _, err = run(t, root, "generate", "probe")
	if err != nil || strings.Count(out, "generated ") != 8 {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "api", "ts", "probe-client", "src", "index.ts")); err != nil {
		t.Fatal(err)
	}
	if out, errs, err := run(t, root, "check"); err != nil || out != "" || errs != "" {
		t.Fatalf("check after generate: %v\n%s%s", err, out, errs)
	}
	if out, _, err := run(t, root, "generate"); err != nil || out != "" {
		t.Fatalf("generate rewrote what was current: %v\n%s", err, out)
	}
	if _, _, err := run(t, root, "generate", "nope"); err == nil || !strings.Contains(err.Error(), `no contract named "nope"`) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("unknown family: %v", err)
	}
	writeFixture(t, root, "api/contracts/other.rpc.json", []byte(`{"schema_version":1,"profile":"nightseam.duplex/1","name":"probe","layer":"rpc","methods":[],"events":[]}`))
	if _, _, err := run(t, root, "validate", "other"); err == nil || !strings.Contains(err.Error(), "names API") {
		t.Fatalf("a contract named for another family passed: %v", err)
	}
	os.Remove(filepath.Join(root, "api", "contracts", "other.rpc.json"))
	writeLayers(t, root, "other", false)
	rpc := filepath.Join(root, "api", "contracts", "other.rpc.json")
	data, err := os.ReadFile(rpc)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "api/contracts/other.rpc.json", []byte(strings.Replace(string(data), `"go_name":"Echo"`, `"go_name":"Close"`, 1)))
	_, errs, err = run(t, root, "validate", "other")
	if err == nil || !strings.Contains(err.Error(), "problems") || !strings.Contains(errs, "other /methods/0/go_name:") || !strings.Contains(errs, "[reserved_name]") {
		t.Fatalf("validate did not report the diagnostic: %v\n%s", err, errs)
	}
	if _, _, err := run(t, root, "generate"); err == nil || !strings.Contains(err.Error(), "other: invalid API contract") {
		t.Fatalf("generate rendered a refused contract: %v", err)
	}
}

// TestImportDirection holds the seam: the kernel, the contract and the seam
// never import a language; a language imports neither the kernel nor
// another language; only the tool itself composes them.
func TestImportDirection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data, err := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "-json", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	type pkg struct {
		ImportPath string
		Imports    []string
	}
	const nightseam = "github.com/Bitspark/nightseam"
	const tool = nightseam + "/cmd/nightseam"
	internal := nightseam + "/internal/"
	// What each package of the seam may import of the others; the tool
	// itself may import any. No entry names a language.
	allowed := map[string]map[string]bool{
		"contract":             {},
		"spi":                  {"contract": true},
		"kernel":               {"contract": true, "spi": true},
		"languages/golang":     {"contract": true, "spi": true},
		"languages/typescript": {"contract": true, "spi": true},
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	seen := 0
	for {
		var p pkg
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		name := strings.Split(p.ImportPath, " [")[0]
		if (name != tool && !strings.HasPrefix(name, internal)) || strings.HasSuffix(name, ".test") {
			continue
		}
		seen++
		if name == tool {
			continue
		}
		from := strings.TrimPrefix(name, internal)
		rules, ok := allowed[from]
		if !ok {
			t.Errorf("unexpected package %s", name)
			continue
		}
		for _, imported := range p.Imports {
			if target, within := strings.CutPrefix(imported, internal); within && !rules[target] {
				t.Errorf("%s imports %s; only the tool composes what the seam separates", from, target)
			}
		}
	}
	if seen < 6 {
		t.Fatalf("saw %d packages of the tool", seen)
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
 runtime "github.com/Bitspark/nightseam/runtime/go"
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
