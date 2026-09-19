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
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// module and scope root the fixtures' generated packages.
const module = "example.test/generated"
const scope = "@example"

// These tests exercise the tool as composed: every target, through the
// kernel, from the tier files to compiled and communicating packages.

func TestDeterministicGeneration(t *testing.T) {
	first, second := renderTool(t, familiesRoot), renderTool(t, familiesRoot)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("generation is not deterministic")
	}
	if len(first) == 0 {
		t.Fatal("nothing rendered")
	}
	for name, data := range first {
		if len(data) == 0 {
			t.Errorf("empty output %s", name)
		}
	}
}

func TestGeneratedGoFamilyCompilesAndCommunicates(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			root := repositoryRoot(t)
			fixture(t, root, "go", "node")
			directory := t.TempDir()
			g.probe(t, directory)
			fixtureModule(t, directory, root)
			copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
			copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
			copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
			writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
			writeFixture(t, directory, "roundtrip.mjs", []byte(tsRoundtrip))
			writeFixture(t, directory, "integration_test.go", []byte(goIntegrationFixture))
			writeFixture(t, directory, "tunnel_test.go", []byte(goTunnelFixture))
			writeFixture(t, directory, "tunnel-roundtrip.mjs", []byte(tsTunnelRoundtrip))
			runFixture(t, directory, "go", "test", "-count=1", "./...")
		})
	}
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
		// A package's own test support is not what the fixture compiles: it
		// checks the generated code against the packages it binds to, and a
		// suite or a type-level check is reached by nothing it builds. They
		// are left behind because they import node's built-in modules, for
		// which the fixture installs no types — the fixture has no
		// node_modules at all, the packages reaching it through paths.
		if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".test.ts") ||
			strings.HasSuffix(p, ".check.ts") || filepath.Base(p) == "conformance.ts" {
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

// fixtureModule makes the fixture directory a Go module. The generated
// packages bind to the runtime by its import path; the
// fixture module resolves Nightseam to this checkout, so the runtime and
// the seam beneath it are the real ones and only the generated packages
// are the copy under test. It declares the language version the checkout's
// own go.mod declares, read rather than written out here, since a module
// that requires one declaring a newer version than itself is refused before
// it builds — which is what a raised directive did to every fixture that
// compiles generated code.
func fixtureModule(t *testing.T, directory, root string) {
	t.Helper()
	writeFixture(t, directory, "go.mod", []byte("module example.test/generated\n\ngo "+goDirective(t, root)+"\n\nrequire (\n\tgithub.com/Bitspark/nightseam v0.0.0\n\tgithub.com/coder/websocket v1.8.15\n)\n\nreplace github.com/Bitspark/nightseam => "+filepath.ToSlash(root)+"\n"))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "go.sum", sum)
}

// goDirective is the language version the checkout's go.mod declares.
func goDirective(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return strings.TrimSpace(version)
		}
	}
	t.Fatalf("no go directive in the go.mod of %s", root)
	return ""
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
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			root := repositoryRoot(t)
			tsc := fixture(t, root, "node", "tsc")
			directory := t.TempDir()
			g.probe(t, directory)
			fixtureModule(t, directory, root)
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
			// The validator is the runtime's, which Node finds through the
			// same loader the round trip uses.
			writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
			runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "validation.mjs")
		})
	}
}

// tsRoundtrip is the generated TypeScript client against the generated Go
// server: the call, the reverse call and the event, with an observer that
// holds what the client's own install labelled each name with. The dial
// options already label the carrier's relay, which probe's install merges
// beside rather than replacing: one peer carrying two families labels each
// name with its own, and a name nobody labelled has no family.
const tsRoundtrip = `import assert from 'node:assert/strict';
import { Client } from './api/ts/probe-client/src/index.ts';
const labels = new Map();
const observer = { observe(event) {
  if (event.type === 'request.started') labels.set('started ' + event.method, event.family);
  if (event.type === 'request.ended') labels.set('ended ' + event.method, event.family);
  if (event.type === 'event.emitted') labels.set('emitted ' + event.name, event.family);
  if (event.type === 'event.delivered') labels.set('delivered ' + event.name, event.family);
} };
const client = await Client.dial(process.argv[2], { observer, families: { relay: 'carrier' } }, { reverse(params) { return { ...params, text: 'typescript:' + params.text }; } });
let observed;
client.onChanged(data => { observed = data; });
const result = await client.echo({ text: 'value', count: 7, note: null });
assert.equal(result.text, 'typescript:value');
assert.equal(result.note, null);
assert.equal(observed.count, 7);
await assert.rejects(client.echo({ text: 'bad', count: 9007199254740992 }));
await assert.rejects(client.peer.call('relay', {}), 'the probe server served a method of another family');
for (const [name, family] of [['started echo', 'probe'], ['ended echo', 'probe'], ['delivered changed', 'probe'], ['started reverse', 'probe'], ['ended reverse', 'probe'], ['started relay', 'carrier'], ['ended relay', 'carrier']]) {
  assert.equal(labels.get(name), family, name + ' is labelled ' + labels.get(name) + ', not ' + family);
}
client.close();
`

// runtimeLoader resolves the runtime packages to their sources for Node,
// which does not strip types inside node_modules.
const runtimeLoader = `export async function resolve(specifier,context,next){const map={'@nightseam/runtime':'./runtime/ts/src/index.ts','@nightseam/duplex':'./duplex/ts/src/index.ts','@nightseam/tunnel':'./tunnel/ts/src/index.ts'};if(map[specifier])return {url:new URL(map[specifier],import.meta.url).href,shortCircuit:true};return next(specifier,context);}`

func TestWorkbenchContractRenders(t *testing.T) {
	files := renderTool(t, familiesRoot)
	index, ok := files["api/ts/workbench-client/src/index.ts"]
	if !ok {
		t.Fatal("the workbench client is not rendered")
	}
	// The workbench's wire names differ from its language names, which its
	// override files hold.
	for _, want := range []string{"async subscribe(", "async createProject(", `"projects.create"`} {
		if !strings.Contains(string(index), want) {
			t.Errorf("the workbench client lacks %s", want)
		}
	}
	if !strings.Contains(string(files["api/go/workbench-client/client_generated.go"]), "func (c *Client) CreateProject(") {
		t.Error("the Go client does not spell the override")
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
	if _, _, err := run(t, root, "generate"); err == nil || !strings.Contains(err.Error(), "no families in") {
		t.Fatalf("an empty checkout generated: %v", err)
	}
	writeFamily(t, root, "probe")
	out, _, err := run(t, root, "validate")
	if err != nil || !strings.Contains(out, "1 families; valid") {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	_, errs, err := run(t, root, "check")
	if err == nil || !strings.Contains(err.Error(), "stale") || !strings.Contains(errs, "stale generated output: api/go/probe-protocol/types_generated.go") {
		t.Fatalf("check passed an ungenerated checkout: %v\n%s", err, errs)
	}
	out, _, err = run(t, root, "generate", "probe")
	if err != nil || strings.Count(out, "generated ") != 9 {
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
	if _, _, err := run(t, root, "generate", "nope"); err == nil || !strings.Contains(err.Error(), `no family named "nope"`) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("unknown family: %v", err)
	}
	writeFamily(t, root, "codex")
	writeFixture(t, root, "api/contracts/codex/go.json", []byte(`{"names": {"echo": "Close"}}`))
	_, errs, err = run(t, root, "validate", "codex")
	if err == nil || !strings.Contains(err.Error(), "problems") || !strings.Contains(errs, "codex/go.json#/names/echo:") || !strings.Contains(errs, "[reserved_name]") {
		t.Fatalf("validate did not report the diagnostic: %v\n%s", err, errs)
	}
	if _, _, err := run(t, root, "generate"); err == nil || !strings.Contains(err.Error(), "codex: invalid family") {
		t.Fatalf("generate rendered a refused family: %v", err)
	}
}

// TestImportDirection holds the seam: the kernel, the contract and the seam
// never import a language; a language imports neither the kernel nor
// another language; and a target is named in internal/compose alone — the
// tool and the conformance suite both reach the targets through it, so
// that what the suite renders is what the tool renders.
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
	const suite = nightseam + "/conformance/go"
	internal := nightseam + "/internal/"
	root := internal + "compose"
	// What each package of the seam may import of the others; the tool
	// itself may import any. Only the composition root names a target.
	allowed := map[string]map[string]bool{
		"diag":               {},
		"naming":             {},
		"model":              {"diag": true},
		"model/builtin":      {"model": true},
		"model/modeltest":    {"model": true},
		"load":               {"diag": true, "model": true, "model/builtin": true},
		"analysis":           {"diag": true, "model": true, "model/builtin": true, "naming": true},
		"check":              {"diag": true, "model": true, "analysis": true},
		"render":             {"diag": true, "model": true, "analysis": true},
		"emit":               {"diag": true},
		"spi":                {"diag": true, "render": true},
		"targets/golang":     {"diag": true, "model": true, "naming": true, "render": true, "spi": true, "emit": true},
		"targets/typescript": {"diag": true, "model": true, "naming": true, "render": true, "spi": true, "emit": true},
		"targets/spec":       {"diag": true, "model": true, "render": true, "spi": true, "emit": true},
		"kernel":             {"diag": true, "model": true, "load": true, "analysis": true, "check": true, "render": true, "spi": true},
		"compose":            {"kernel": true, "spi": true, "targets/golang": true, "targets/typescript": true, "targets/spec": true},
		"oracle":             {"model": true},
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
	if seen < 12 {
		t.Fatalf("saw %d packages of the tool", seen)
	}
	// The composition root is one place, and both its callers reach the
	// targets only through it: the tool renders a checkout and the
	// conformance suite renders the corpus's probe family for a language's
	// generated testee, and a suite composing targets of its own would go
	// on validating output the tool no longer produces. Read without their
	// tests, which may name a target — what each reserves, what each
	// renders for the corpus — as the subject of a test rather than as a
	// second composition.
	data, err = exec.CommandContext(ctx, "go", "list", "-json", tool, suite).Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	for callers := 0; ; callers++ {
		var p pkg
		if err := decoder.Decode(&p); err == io.EOF {
			if callers != 2 {
				t.Fatalf("read %d of the composition root's callers", callers)
			}
			break
		} else if err != nil {
			t.Fatal(err)
		}
		composes := false
		for _, imported := range p.Imports {
			if strings.HasPrefix(imported, internal+"targets/") {
				t.Errorf("%s imports %s; a target is named in %s and nowhere else", p.ImportPath, imported, root)
			}
			composes = composes || imported == root
		}
		if !composes {
			t.Errorf("%s does not import %s, where the targets it renders with are composed", p.ImportPath, root)
		}
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
 "sync"
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

// labels is an observer that keeps the family of every request and event
// event it is told about, which is all this fixture asks of one.
type labels struct{
 mu sync.Mutex
 seen map[string]string
}
func newLabels() *labels { return &labels{seen: map[string]string{}} }
func (l *labels) Observe(event runtime.ObserverEvent) {
 l.mu.Lock()
 defer l.mu.Unlock()
 switch e := event.(type) {
 case runtime.RequestStarted: l.seen["started "+e.Method] = e.Family
 case runtime.RequestEnded: l.seen["ended "+e.Method] = e.Family
 case runtime.EventEmitted: l.seen["emitted "+e.Name] = e.Family
 case runtime.EventDelivered: l.seen["delivered "+e.Name] = e.Family
 }
}
func (l *labels) hold(t *testing.T, side string, want map[string]string) {
 t.Helper()
 l.mu.Lock()
 defer l.mu.Unlock()
 for key, family := range want {
  if got, seen := l.seen[key]; got != family || !seen { t.Errorf("%s: %s is labelled %q, not %q", side, key, got, family) }
 }
}
// TestInstallLabelsEveryNameWithItsFamily: the generated install labels
// every method and event of the family on the options each peer is made
// with, so that an observer says which family a name belongs to without
// parsing it — on the side that sends a name as well as on the side that
// serves it. The server's options already label the carrier's relay, which
// probe's install merges beside rather than replacing: one peer carrying
// two families labels each name with its own, and a name nobody labelled
// has no family rather than a guessed one.
func TestInstallLabelsEveryNameWithItsFamilyOverTheWire(t *testing.T) {
 consumer, machine := newLabels(), newLabels()
 options := runtime.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }}
 options.Options = runtime.Options{Observer: machine, Families: map[string]string{"relay": "carrier"}}
 h, err := binding.NewHandler(serverHandler{}, options)
 if err != nil { t.Fatal(err) }
 server := httptest.NewServer(h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 c, err := client.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{Options: runtime.Options{Observer: consumer}}, clientHandler{})
 if err != nil { t.Fatal(err) }
 defer c.Close()
 changed := make(chan struct{}, 1)
 if err := c.OnChanged(func(context.Context, protocol.Payload) { select { case changed <- struct{}{}: default: } }); err != nil { t.Fatal(err) }
 if _, err := c.Echo(ctx, protocol.Payload{Text: "value", Count: 1}); err != nil { t.Fatal(err) }
 select { case <-changed: case <-ctx.Done(): t.Fatal("the event never arrived") }
 var raw json.RawMessage
 if err := c.Peer.Call(ctx, "relay", map[string]any{}, &raw); err == nil { t.Fatal("the probe server served a method of another family") }
 consumer.hold(t, "the consumer", map[string]string{"started echo": "probe", "ended echo": "probe", "delivered changed": "probe", "started reverse": "probe", "ended reverse": "probe", "started relay": ""})
 machine.hold(t, "the machine", map[string]string{"started echo": "probe", "ended echo": "probe", "emitted changed": "probe", "started reverse": "probe", "ended reverse": "probe", "started relay": "carrier", "ended relay": "carrier"})
}
`

// TestVersionIsReportedByTheTool holds what #31 asked for: the tool says
// which version of itself is running, in one spelling from both the
// command and the flag, and needs no checkout to say it — the question is
// asked by someone whose check disagreed across two machines, who is not
// necessarily standing in a repository when they ask.
func TestVersionIsReportedByTheTool(t *testing.T) {
	// A test binary is built from this module's own tree, so the answer here
	// is the one a checkout gives, which is the useful one: rendered by a
	// tree rather than by a release.
	const want = "nightseam (devel)\n"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		command := newCommand()
		var out, errs bytes.Buffer
		command.SetOut(&out)
		command.SetErr(&errs)
		// No --root: the command reads no checkout and settles no module.
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errs.String())
		}
		if out.String() != want {
			t.Errorf("%v printed %q, want %q", args, out.String(), want)
		}
	}
	// It reads no go.mod either: an empty directory is a checkout the other
	// commands refuse and this one answers in.
	if out, _, err := run(t, t.TempDir(), "version"); err != nil || out != want {
		t.Fatalf("version in an empty directory: %v %q", err, out)
	}
}

// TestVersionOfBuildInfo holds the shapes of build information the tool is
// run under to the version each should report. Only the first two are
// reachable from a test; the one a consumer sees — the tool built inside
// the consumer's own module by go get -tool, where this module is a
// requirement rather than the main one — is the case the command exists
// for, and is held here because nothing else can reach it.
func TestVersionOfBuildInfo(t *testing.T) {
	tool := func(version string, replace *debug.Module) *debug.Module {
		return &debug.Module{Path: modulePath, Version: version, Replace: replace}
	}
	consumer := debug.Module{Path: "example.test/consumer", Version: "(devel)"}
	for _, c := range []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"a checkout of this module", &debug.BuildInfo{Main: debug.Module{Path: modulePath}}, true, "(devel)"},
		{"a released binary of this module", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.2.0"}}, true, "v0.2.0"},
		{"go get -tool in a consumer's module", &debug.BuildInfo{
			Main: consumer,
			Deps: []*debug.Module{{Path: "github.com/spf13/cobra", Version: "v1.10.1"}, tool("v0.2.0", nil)},
		}, true, "v0.2.0"},
		{"a consumer replacing the tool with a checkout", &debug.BuildInfo{
			Main: consumer,
			Deps: []*debug.Module{tool("v0.2.0", &debug.Module{Path: modulePath})},
		}, true, "(devel)"},
		{"a consumer replacing the tool with another release", &debug.BuildInfo{
			Main: consumer,
			Deps: []*debug.Module{tool("v0.2.0", tool("v0.1.9", nil))},
		}, true, "v0.1.9"},
		{"a binary carrying no build information", nil, false, "(devel)"},
		{"a binary naming this module nowhere", &debug.BuildInfo{Main: consumer}, true, "(unknown)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := versionOf(c.info, c.ok); got != c.want {
				t.Errorf("versionOf = %q, want %q", got, c.want)
			}
		})
	}
}

// TestGeneratedFilesNameNoVersion holds the other half of #31's decision:
// the tool reports the version and its output does not carry it. A version
// in the header would rewrite every generated file of every consumer on
// every release, and would fail a check over bytes that are otherwise
// identical — the failure the command explains rather than one to add.
// docs/declaration/generator.md states the split, and this holds it.
func TestGeneratedFilesNameNoVersion(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	files, err := (&app{root: root, module: module, scope: scope}).render([]string{"probe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("nothing rendered")
	}
	for path, data := range files {
		header, _, _ := strings.Cut(string(data), "\n")
		if strings.Contains(header, "nightseam v") || strings.Contains(header, "(devel)") {
			t.Errorf("%s names a version in its header: %s", path, header)
		}
	}
}
