package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/contract"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/languages/golang"
	"github.com/Bitspark/nightseam/internal/languages/typescript"
	"github.com/Bitspark/nightseam/internal/spi"
)

// carrierContract carries a family it is generic in, S: a frame holding one
// of S's envelopes, an attachment holding a handle to a channel that speaks
// S, a method that returns one of the probe family's envelopes by name, and
// an event of frames.
const carrierContract = `{
 "schema_version":1,"profile":"nightseam.duplex/1","name":"carrier",
 "parameters":[{"name":"S","of":"session"}],
 "types":{
  "Frame":{"kind":"record","fields":[{"name":"sequence","type":"integer"},{"name":"message","type":{"envelope":"S"}}]},
  "Attachment":{"kind":"record","fields":[{"name":"connection","type":{"connection":"S"}},{"name":"last","type":"integer"}]},
  "AttachParams":{"kind":"record","fields":[{"name":"id","type":"string","go_name":"ID"}]},
  "Frames":{"kind":"alias","type":{"array":"Frame"}}
 },
 "methods":[
  {"name":"attach","go_name":"Attach","ts_name":"attach","direction":"client_to_server","request":"AttachParams","result":"Attachment"},
  {"name":"relay","go_name":"Relay","ts_name":"relay","direction":"client_to_server","request":"Frame","result":{"envelope":"probe"}}
 ],
 "events":[{"name":"frame.relayed","go_name":"FrameRelayed","ts_name":"frameRelayed","direction":"server_to_client","type":"Frame"}],
 "errors":[]
}`

// slotWorld is the probe family, declared a session family, and the
// carrier, with the carrier's parameter S bound to probe: the left path.
func slotWorld(t *testing.T) (world kernel.World, probe, substituted map[string]any) {
	t.Helper()
	probe = exampleAPI(t)
	probe["role"] = contract.SessionRole
	var carrier map[string]any
	if err := json.Unmarshal([]byte(carrierContract), &carrier); err != nil {
		t.Fatal(err)
	}
	world = kernel.World{"probe": probe, "carrier": carrier}
	return world, probe, contract.Substitute(carrier, map[string]string{"S": "probe"})
}

// rightLanguages render the carrier as written, generically — the right
// path of the diagram — beside the left path's output, under gen/.
func rightLanguages(module, scope string) []spi.Language {
	return []spi.Language{
		golang.New(golang.Options{Module: module, ProtocolPath: "gen/go/carrier-protocol", BindingPath: "gen/go/carrier-binding", ClientPath: "gen/go/carrier-client"}),
		typescript.New(typescript.Options{Scope: scope, ClientPath: "gen/ts/carrier-client"}),
	}
}

// renderSlotFixture renders probe, the substituted carrier — the left path
// — and the carrier as written — the right path — into a temporary module
// that resolves Nightseam to this checkout.
func renderSlotFixture(t *testing.T, directory, root string) {
	t.Helper()
	world, probe, substituted := slotWorld(t)
	render := func(input map[string]any, languages []spi.Language) {
		result, err := kernel.GenerateIn(world, input, languages...)
		if err != nil {
			t.Fatal(err)
		}
		for p, data := range result.Files {
			writeFixture(t, directory, p, data)
		}
	}
	render(probe, languages(module, scope))
	render(substituted, languages(module, scope))
	render(world["carrier"], rightLanguages(module, scope))
	writeFixture(t, directory, "go.mod", []byte("module example.test/generated\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/Bitspark/nightseam v0.0.0\n\tgithub.com/coder/websocket v1.8.15\n)\n\nreplace github.com/Bitspark/nightseam => "+filepath.ToSlash(root)+"\n"))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "go.sum", sum)
}

// TestSlottedContractRendersGenerically: the carrier as written renders —
// the right path — with its slots of the session role as type parameters, E
// and H in Go and the associated types of F in TypeScript, and its slot of
// a named family as that family's own type, in both languages.
func TestSlottedContractRendersGenerically(t *testing.T) {
	world, _, _ := slotWorld(t)
	if diagnostics := kernel.ValidateIn(world, world["carrier"], languages(module, scope)...); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	result, err := kernel.GenerateIn(world, world["carrier"], languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for pattern, path := range map[string]string{"Message\\s+SEnvelope\\s": "api/go/carrier-protocol/types_generated.go", "Connection\\s+SHandle\\s": "api/go/carrier-protocol/types_generated.go"} {
		if !regexp.MustCompile(pattern).MatchString(files[path]) {
			t.Errorf("%s lacks %s", path, pattern)
		}
	}
	for path, wants := range map[string][]string{
		"api/go/carrier-protocol/types_generated.go":      {"type Frame[SEnvelope any] struct {", "type Attachment[SHandle any] struct {", "Connection", "type Frames[SEnvelope any] = []Frame[SEnvelope]", "type AttachParams struct {", "func (v Frame[SEnvelope]) MarshalJSON()", "func (v *Frame[SEnvelope]) UnmarshalJSON("},
		"api/go/carrier-protocol/validation_generated.go": {`"probe": probeprotocol.ValidateRaw`},
		"api/go/carrier-binding/binding_generated.go":     {"type Remote[SEnvelope, SHandle any] struct", "type Handler[SEnvelope, SHandle any] interface", "remote *Remote[SEnvelope, SHandle], params protocol.AttachParams) (protocol.Attachment[SHandle], error)", "remote *Remote[SEnvelope, SHandle], params protocol.Frame[SEnvelope]) (probeprotocol.Envelope, error)", "func NewHandler[SEnvelope runtime.Of[STag], SHandle runtime.Of[STag], STag any](handler Handler[SEnvelope, SHandle], options runtime.ServerOptions)", "EmitFrameRelayed(ctx context.Context, data protocol.Frame[SEnvelope]) error"},
		"api/go/carrier-client/client_generated.go":       {"type Client[SEnvelope, SHandle any] struct", "type Caller[SEnvelope, SHandle any] interface", "func Dial[SEnvelope runtime.Of[STag], SHandle runtime.Of[STag], STag any](ctx context.Context, url string, options runtime.DialOptions, handler Handler[SEnvelope, SHandle]) (*Client[SEnvelope, SHandle], error)", "OnFrameRelayed(handler func(context.Context, protocol.Frame[SEnvelope])) error"},
		"api/ts/carrier-client/src/types.ts":              {"export interface Frame<S extends AnyFamily = SessionFamily> {", `"message": S["Envelope"];`, "export interface Attachment<S extends AnyFamily = SessionFamily> {", `"connection": S["Handle"];`, "export type Frames<S extends AnyFamily = SessionFamily> = Array<Frame<S>>;", "export interface AttachParams {", "export type SessionFamily = probe.Family;", `export const family = { name: "carrier", validate: validateWire } as const;`},
		"api/ts/carrier-client/src/index.ts":              {"export interface Handler<S extends AnyFamily = SessionFamily> {", "export interface Caller<S extends AnyFamily = SessionFamily> {", "attach(params: Protocol.AttachParams, options?: CallOptions): Promise<Protocol.Attachment<S>>;", "relay(params: Protocol.Frame<S>, options?: CallOptions): Promise<probe.Envelope>;", "export class Client<S extends AnyFamily = SessionFamily> implements Caller<S> {", "static async dial<S extends AnyFamily = SessionFamily>(url: string, s: FamilyBinding<S>, options: PeerOptions = {}, handler?: Handler<S>): Promise<Client<S>>", "onFrameRelayed(handler: (data: Protocol.Frame<S>) => void | Promise<void>): () => void"},
		"api/ts/carrier-client/package.json":              {`"@example/probe-client":"0.0.0"`},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
}

// TestSubstitutedCarrierRendersReferencingProbe: the left path renders, and
// what it renders refers to probe's Envelope and Handle by import, in Go
// and in TypeScript. The exported surface of the carrier's Go protocol
// package is the golden below: the instantiation of the generic rendering
// must reproduce it exactly, which TestDiagramCommutesInGo holds.
func TestSubstitutedCarrierRendersReferencingProbe(t *testing.T) {
	world, _, substituted := slotWorld(t)
	result, err := kernel.GenerateIn(world, substituted, languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for path, wants := range map[string][]string{
		"api/go/carrier-protocol/types_generated.go":      {`probeprotocol "example.test/generated/api/go/probe-protocol"`},
		"api/go/carrier-protocol/validation_generated.go": {`"probe": probeprotocol.ValidateRaw`},
		"api/go/carrier-client/client_generated.go":       {"(probeprotocol.Envelope, error)"},
		"api/ts/carrier-client/src/types.ts":              {`import type * as probe from "@example/probe-client";`, `import { validateWire as validate_probe } from "@example/probe-client";`, `"message": probe.Envelope;`, `"connection": probe.Handle;`, `"probe": validate_probe`},
		"api/ts/carrier-client/src/index.ts":              {"Promise<probe.Envelope>"},
		"api/ts/carrier-client/package.json":              {`"@example/probe-client":"0.0.0"`},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	golden := []string{
		"AttachParams{ID string}",
		"Attachment{Connection probeprotocol.Handle, Last int64}",
		"Envelope{Version int64, Kind string, ID runtime.Optional[string], Method runtime.Optional[string], Params runtime.Optional[any], Result runtime.Optional[any], Error runtime.Optional[any], Event runtime.Optional[string], Data runtime.Optional[any]}",
		"Frames = []Frame",
		"Frame{Sequence int64, Message probeprotocol.Envelope}",
		"Handle{Channel int64}",
		"Tag{}",
	}
	if got := surface(t, files["api/go/carrier-protocol/types_generated.go"]); strings.Join(got, "\n") != strings.Join(golden, "\n") {
		t.Fatalf("the carrier's Go surface is not the golden one:\n%s", strings.Join(got, "\n"))
	}
}

// surface lists the exported types of a generated Go file: each record with
// its fields and their types, each alias with its target, sorted by name.
func surface(t *testing.T, source string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "types_generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || !spec.Name.IsExported() {
			return true
		}
		switch typed := spec.Type.(type) {
		case *ast.StructType:
			var fields []string
			for _, field := range typed.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() {
						fields = append(fields, name.Name+" "+exprString(field.Type))
					}
				}
			}
			out = append(out, spec.Name.Name+"{"+strings.Join(fields, ", ")+"}")
		default:
			out = append(out, spec.Name.Name+" = "+exprString(spec.Type))
		}
		return true
	})
	sort.Strings(out)
	return out
}

func exprString(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return exprString(typed.X) + "." + typed.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(typed.Elt)
	case *ast.MapType:
		return "map[" + exprString(typed.Key) + "]" + exprString(typed.Value)
	case *ast.IndexExpr:
		return exprString(typed.X) + "[" + exprString(typed.Index) + "]"
	case *ast.StarExpr:
		return "*" + exprString(typed.X)
	case *ast.InterfaceType:
		return "any"
	}
	return "?"
}

// TestDiagramCommutesInGo holds the diagram in Go: both paths compile in one
// module; the generic rendering instantiated with probe has the types, the
// interfaces and the method sets of the plain rendering, field for field
// and signature for signature; the plain client speaks with the generic
// server and the generic client with the plain server, events included; the
// instantiation validates what fills a slot through probe's codec, and an
// opaque instantiation passes it through; and the left path delegates a
// slot's validation to probe.
func TestDiagramCommutesInGo(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go", "node")
	directory := t.TempDir()
	renderSlotFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
	copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
	copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
	writeFixture(t, directory, "loader.mjs", []byte(slotLoader))
	writeFixture(t, directory, "roundtrip-generic.mjs", []byte(tsGenericRoundtrip))
	writeFixture(t, directory, "diagram_test.go", []byte(goDiagramFixture))
	writeFixture(t, directory, "delegation_test.go", []byte(`package generated
import ("testing";carrier "example.test/generated/api/go/carrier-protocol")
func TestDelegation(t *testing.T){
 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}}}"));err!=nil{t.Fatal(err)}
 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1}}"));err==nil{t.Fatal("an envelope without a kind passed")}
 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"extra\":true}}"));err==nil{t.Fatal("an unknown envelope field passed")}
 if err:=carrier.ValidateExpressionRaw("probe.Nope",[]byte("{}"));err==nil{t.Fatal("an unknown imported type passed")}
 if err:=carrier.ValidateExpressionRaw("nobody.Envelope",[]byte("{}"));err==nil{t.Fatal("an unknown family passed")}
}`))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

// slotLoader resolves the runtime and probe's client for Node, which does
// not strip types inside node_modules.
const slotLoader = `export async function resolve(specifier,context,next){const map={'@nightseam/runtime':'./runtime/ts/src/index.ts','@nightseam/duplex':'./duplex/ts/src/index.ts','@nightseam/tunnel':'./tunnel/ts/src/index.ts','@example/probe-client':'./api/ts/probe-client/src/index.ts'};if(map[specifier])return {url:new URL(map[specifier],import.meta.url).href,shortCircuit:true};return next(specifier,context);}`

// tsGenericRoundtrip dials a carrier server with the generic TypeScript
// client bound to probe: a relayed frame comes back as its envelope, the
// event arrives typed, and a frame whose envelope probe refuses is refused
// before it is sent.
const tsGenericRoundtrip = `import assert from 'node:assert/strict';
import {Client} from './gen/ts/carrier-client/src/index.ts';
import {family as probe} from './api/ts/probe-client/src/index.ts';
const client = await Client.dial(process.argv[2], probe);
let observed;
client.onFrameRelayed((frame) => { observed = frame; });
const message = {version: 1, kind: 'event', event: 'changed', data: {}};
const result = await client.relay({sequence: 1, message});
assert.deepEqual(result, message);
assert.equal(observed.sequence, 1);
assert.deepEqual(observed.message, message);
await assert.rejects(client.relay({sequence: 2, message: {version: 1}}));
const attachment = await client.attach({id: 'x'});
assert.equal(attachment.connection.channel, 7);
client.close();
`

const goDiagramFixture = `package generated
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
 leftbinding "example.test/generated/api/go/carrier-binding"
 leftclient "example.test/generated/api/go/carrier-client"
 left "example.test/generated/api/go/carrier-protocol"
 probe "example.test/generated/api/go/probe-protocol"
 rightbinding "example.test/generated/gen/go/carrier-binding"
 rightclient "example.test/generated/gen/go/carrier-client"
 right "example.test/generated/gen/go/carrier-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
)
type E = probe.Envelope
type H = probe.Handle
// same holds when two types are one type or the same structure: structs field by field with tags, composites by their parts, functions by their signatures, interfaces by their method sets. An instantiated generic type is the same as the plain type it must equal, however the instantiation is spelled.
func same(a, b reflect.Type) bool {
 if a == b { return true }
 if a.Kind() != b.Kind() { return false }
 switch a.Kind() {
 case reflect.Struct:
  if a.NumField() != b.NumField() { return false }
  for i := 0; i < a.NumField(); i++ {
   fa, fb := a.Field(i), b.Field(i)
   if fa.Name != fb.Name || fa.Tag != fb.Tag || !same(fa.Type, fb.Type) { return false }
  }
  return true
 case reflect.Slice, reflect.Array, reflect.Pointer, reflect.Chan:
  return same(a.Elem(), b.Elem())
 case reflect.Map:
  return same(a.Key(), b.Key()) && same(a.Elem(), b.Elem())
 case reflect.Func:
  if a.NumIn() != b.NumIn() || a.NumOut() != b.NumOut() { return false }
  for i := 0; i < a.NumIn(); i++ { if !same(a.In(i), b.In(i)) { return false } }
  for i := 0; i < a.NumOut(); i++ { if !same(a.Out(i), b.Out(i)) { return false } }
  return true
 case reflect.Interface:
  return sameMethods(a, b)
 }
 return a.Name() == b.Name() && a.PkgPath() == b.PkgPath()
}
// sameMethods holds when two types have the same method set, name for name and signature for signature.
func sameMethods(a, b reflect.Type) bool {
 if a.NumMethod() != b.NumMethod() { return false }
 for i := 0; i < a.NumMethod(); i++ {
  ma, mb := a.Method(i), b.Method(i)
  if ma.Name != mb.Name || !same(ma.Type, mb.Type) { return false }
 }
 return true
}
func TestInstantiationIsTheLeftPath(t *testing.T) {
 for _, pair := range []struct{ name string; left, right reflect.Type }{
  {"Frame", reflect.TypeOf(left.Frame{}), reflect.TypeOf(right.Frame[E]{})},
  {"Attachment", reflect.TypeOf(left.Attachment{}), reflect.TypeOf(right.Attachment[H]{})},
  {"Frames", reflect.TypeOf(left.Frames{}), reflect.TypeOf(right.Frames[E]{})},
  {"AttachParams", reflect.TypeOf(left.AttachParams{}), reflect.TypeOf(right.AttachParams{})},
  {"Envelope", reflect.TypeOf(left.Envelope{}), reflect.TypeOf(right.Envelope{})},
  {"Caller", reflect.TypeOf((*leftclient.Caller)(nil)).Elem(), reflect.TypeOf((*rightclient.Caller[E, H])(nil)).Elem()},
  {"client Handler", reflect.TypeOf((*leftclient.Handler)(nil)).Elem(), reflect.TypeOf((*rightclient.Handler[E, H])(nil)).Elem()},
  {"binding Handler", reflect.TypeOf((*leftbinding.Handler)(nil)).Elem(), reflect.TypeOf((*rightbinding.Handler[E, H])(nil)).Elem()},
  {"Client", reflect.TypeOf((*leftclient.Client)(nil)), reflect.TypeOf((*rightclient.Client[E, H])(nil))},
  {"Remote", reflect.TypeOf((*leftbinding.Remote)(nil)), reflect.TypeOf((*rightbinding.Remote[E, H])(nil))},
 } {
  if !same(pair.left, pair.right) { t.Errorf("%s: %v is not %v", pair.name, pair.right, pair.left) }
  if !sameMethods(pair.left, pair.right) { t.Errorf("%s: the method set of %v is not %v's", pair.name, pair.right, pair.left) }
 }
}
type rightServer struct{}
func (rightServer) Attach(ctx context.Context, remote *rightbinding.Remote[E, H], params right.AttachParams) (right.Attachment[H], error) {
 return right.Attachment[H]{Connection: H{Channel: 7}, Last: 1}, nil
}
func (rightServer) Relay(ctx context.Context, remote *rightbinding.Remote[E, H], frame right.Frame[E]) (E, error) {
 if err := remote.EmitFrameRelayed(ctx, frame); err != nil { return frame.Message, err }
 return frame.Message, nil
}
type leftServer struct{}
func (leftServer) Attach(ctx context.Context, remote *leftbinding.Remote, params left.AttachParams) (left.Attachment, error) {
 return left.Attachment{Connection: H{Channel: 7}, Last: 1}, nil
}
func (leftServer) Relay(ctx context.Context, remote *leftbinding.Remote, frame left.Frame) (E, error) {
 if err := remote.EmitFrameRelayed(ctx, frame); err != nil { return frame.Message, err }
 return frame.Message, nil
}
var options = runtime.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }}
func envelope(t *testing.T) E {
 t.Helper()
 var e E
 if err := json.Unmarshal([]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}}"), &e); err != nil { t.Fatal(err) }
 return e
}
func serve(t *testing.T, h http.Handler) (*httptest.Server, string) {
 t.Helper()
 server := httptest.NewServer(h)
 return server, "ws" + strings.TrimPrefix(server.URL, "http")
}
func TestPlainClientSpeaksWithGenericServer(t *testing.T) {
 h, err := rightbinding.NewHandler(rightServer{}, options)
 if err != nil { t.Fatal(err) }
 server, url := serve(t, h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 c, err := leftclient.Dial(ctx, url, runtime.DialOptions{}, nil)
 if err != nil { t.Fatal(err) }
 defer c.Close()
 relayed := make(chan left.Frame, 1)
 if err := c.OnFrameRelayed(func(_ context.Context, frame left.Frame) { relayed <- frame }); err != nil { t.Fatal(err) }
 message := envelope(t)
 result, err := c.Relay(ctx, left.Frame{Sequence: 1, Message: message})
 if err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(result, message) { t.Fatalf("relayed %#v", result) }
 select {
 case frame := <-relayed:
  if frame.Sequence != 1 || !reflect.DeepEqual(frame.Message, message) { t.Fatalf("event %#v", frame) }
 case <-ctx.Done():
  t.Fatal("no event")
 }
 attachment, err := c.Attach(ctx, left.AttachParams{ID: "x"})
 if err != nil || attachment.Connection.Channel != 7 { t.Fatalf("attach %#v %v", attachment, err) }
}
func TestGenericClientSpeaksWithPlainServer(t *testing.T) {
 h, err := leftbinding.NewHandler(leftServer{}, options)
 if err != nil { t.Fatal(err) }
 server, url := serve(t, h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 c, err := rightclient.Dial[E, H](ctx, url, runtime.DialOptions{}, nil)
 if err != nil { t.Fatal(err) }
 defer c.Close()
 relayed := make(chan right.Frame[E], 1)
 if err := c.OnFrameRelayed(func(_ context.Context, frame right.Frame[E]) { relayed <- frame }); err != nil { t.Fatal(err) }
 message := envelope(t)
 result, err := c.Relay(ctx, right.Frame[E]{Sequence: 2, Message: message})
 if err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(result, message) { t.Fatalf("relayed %#v", result) }
 select {
 case frame := <-relayed:
  if frame.Sequence != 2 || !reflect.DeepEqual(frame.Message, message) { t.Fatalf("event %#v", frame) }
 case <-ctx.Done():
  t.Fatal("no event")
 }
 attachment, err := c.Attach(ctx, right.AttachParams{ID: "x"})
 if err != nil || attachment.Connection.Channel != 7 { t.Fatalf("attach %#v %v", attachment, err) }
 var caller rightclient.Caller[E, H] = c
 if _, err := caller.Attach(ctx, right.AttachParams{ID: "y"}); err != nil { t.Fatal(err) }
}
func TestInstantiationValidatesThroughTheFamily(t *testing.T) {
 var frame right.Frame[E]
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1}}"), &frame); err == nil { t.Fatal("an envelope without a kind passed probe's codec") }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"extra\":true}}"), &frame); err == nil { t.Fatal("an unknown envelope field passed probe's codec") }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}}}"), &frame); err != nil { t.Fatal(err) }
 var opaque right.Frame[runtime.Raw]
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1}}"), &opaque); err != nil { t.Fatalf("the opaque instantiation did not pass a message through: %v", err) }
 if string(opaque.Message) != "{\"version\":1}" { t.Fatalf("passed through %s", opaque.Message) }
 if err := right.ValidateRaw("Frame", []byte("{\"sequence\":1,\"message\":{\"version\":1}}")); err != nil { t.Fatalf("the raw validator did not see the role's slot as JSON: %v", err) }
 if err := right.ValidateRaw("Frame", []byte("{\"sequence\":1}")); err == nil { t.Fatal("a frame without a message passed") }
 if err := right.ValidateExpressionRaw(map[string]any{"envelope": "probe"}, []byte("{\"version\":1}")); err == nil { t.Fatal("the slot of a named family did not delegate to it") }
 if err := right.ValidateExpressionRaw(map[string]any{"envelope": "probe"}, []byte("{\"version\":1,\"kind\":\"event\"}")); err != nil { t.Fatal(err) }
 if err := right.ValidateExpressionRaw(map[string]any{"connection": "nobody"}, []byte("{\"channel\":1}")); err == nil { t.Fatal("a slot of an unknown family passed") }
}
func TestGenericTypeScriptClientSpeaksWithPlainServer(t *testing.T) {
 if _, err := exec.LookPath("node"); err != nil { t.Skip("Node is not installed") }
 h, err := leftbinding.NewHandler(leftServer{}, options)
 if err != nil { t.Fatal(err) }
 server, url := serve(t, h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 command := exec.CommandContext(ctx, "node", "--loader", "./loader.mjs", "roundtrip-generic.mjs", url)
 if output, err := command.CombinedOutput(); err != nil { t.Fatalf("Node generic client: %v\n%s", err, output) }
}
`

// TestDiagramCommutesInTypeScript holds the diagram in TypeScript: both
// paths type-check in one project against probe's client; the generic
// rendering instantiated with probe's Family is, type for type, the plain
// rendering, which tsc holds through Equals; and the generic validator bound
// to probe validates as the plain one delegates.
func TestDiagramCommutesInTypeScript(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	renderSlotFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
	copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
	copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
	writeFixture(t, directory, "gen/ts/diagram.ts", []byte(tsDiagramFixture))
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": map[string]any{"@nightseam/runtime": []string{"./runtime/ts/src/index.ts"}, "@nightseam/duplex": []string{"./duplex/ts/src/index.ts"}, "@nightseam/tunnel": []string{"./tunnel/ts/src/index.ts"}, "@example/probe-client": []string{"./api/ts/probe-client/src/index.ts"}}}, "include": []string{"api/ts/**/*.ts", "runtime/ts/**/*.ts", "duplex/ts/**/*.ts", "tunnel/ts/**/*.ts", "gen/**/*.ts"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	writeFixture(t, directory, "loader.mjs", []byte(slotLoader))
	writeFixture(t, directory, "delegation.mjs", []byte(`import assert from 'node:assert/strict';import {validateWire} from './api/ts/carrier-client/src/types.ts';
validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{}}});
assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1}}));
assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',extra:true}}));
assert.throws(()=>validateWire('probe.Nope',{}));assert.throws(()=>validateWire('nobody.Envelope',{}));
`))
	runFixture(t, directory, "node", "--loader", "./loader.mjs", "delegation.mjs")
	writeFixture(t, directory, "generic.mjs", []byte(`import assert from 'node:assert/strict';
import {validateWire, family as carrier} from './gen/ts/carrier-client/src/types.ts';
import {family as probe} from './api/ts/probe-client/src/index.ts';
const good = {sequence: 1, message: {version: 1, kind: 'event', event: 'changed', data: {}}};
validateWire('Frame', good, '$', {S: probe});
validateWire('Frames', [good], '$', {S: probe});
assert.throws(() => validateWire('Frame', {sequence: 1, message: {version: 1}}, '$', {S: probe}));
assert.throws(() => validateWire('Frame', {sequence: 1, message: {version: 1, kind: 'event', extra: true}}, '$', {S: probe}));
assert.throws(() => validateWire('Frame', good), /binding of the parameter S/);
assert.throws(() => validateWire({envelope: 'probe'}, {version: 1}));
validateWire({envelope: 'probe'}, good.message);
assert.throws(() => validateWire({connection: 'nobody'}, {channel: 1}));
assert.equal(carrier.name, 'carrier');
assert.equal(probe.name, 'probe');
`))
	runFixture(t, directory, "node", "--loader", "./loader.mjs", "generic.mjs")
}

// tsDiagramFixture is the diagram at the type level: an instantiation of the
// generic rendering is identical to the plain rendering, type for type.
const tsDiagramFixture = `import type * as left from "../../api/ts/carrier-client/src/index.ts";
import type * as right from "./carrier-client/src/index.ts";
import type * as probe from "../../api/ts/probe-client/src/index.ts";
type Equals<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
export const frame: Equals<right.Frame<probe.Family>, left.Frame> = true;
export const attachment: Equals<right.Attachment<probe.Family>, left.Attachment> = true;
export const frames: Equals<right.Frames<probe.Family>, left.Frames> = true;
export const params: Equals<right.AttachParams, left.AttachParams> = true;
export const caller: Equals<right.Caller<probe.Family>, left.Caller> = true;
export const handler: Equals<right.Handler<probe.Family>, left.Handler> = true;
export const role: Equals<right.SessionFamily, probe.Family> = true;
export const bound: Equals<right.FamilyBinding<probe.Family>["name"], "probe"> = true;
`

// pairContract is generic in two parameters at once: S at both kinds, T at
// an envelope, and a named family at a third slot. It is what proves the
// parameters do not collapse into one another in either language.
const pairContract = `{
 "schema_version":1,"profile":"nightseam.duplex/1","name":"pair",
 "parameters":[{"name":"S","of":"session"},{"name":"T","of":"session"}],
 "types":{
  "Frame":{"kind":"record","fields":[{"name":"message","type":{"envelope":"S"}},{"name":"back","type":{"connection":"S"}}]},
  "Echo":{"kind":"record","fields":[{"name":"heard","type":{"envelope":"T"}}]},
  "Both":{"kind":"record","fields":[{"name":"frame","type":"Frame"},{"name":"echoes","type":{"array":"Echo"}}]},
  "Named":{"kind":"record","fields":[{"name":"held","type":{"envelope":"probe"}}]}
 },
 "methods":[
  {"name":"relay","go_name":"Relay","ts_name":"relay","direction":"client_to_server","request":{"envelope":"T"},"result":"Both"},
  {"name":"named","go_name":"Named","ts_name":"named","direction":"client_to_server","request":"Named","result":"Named"}
 ],
 "events":[{"name":"echoed","go_name":"Echoed","ts_name":"echoed","direction":"server_to_client","type":"Echo"}],
 "errors":[]
}`

// pairWorld is two session families, probe and codex, and the pair family
// generic in both.
func pairWorld(t *testing.T) kernel.World {
	t.Helper()
	probe := exampleAPI(t)
	probe["role"] = contract.SessionRole
	codex := exampleAPI(t)
	codex["name"] = "codex"
	codex["role"] = contract.SessionRole
	var pair map[string]any
	if err := json.Unmarshal([]byte(pairContract), &pair); err != nil {
		t.Fatal(err)
	}
	return kernel.World{"probe": probe, "codex": codex, "pair": pair}
}

// TestTwoParametersRenderApart: a family generic in two parameters renders
// with a Go type parameter per parameter and drawn type — SEnvelope, SHandle, TEnvelope — each type
// taking only the ones it uses, and with one TypeScript type parameter per
// contract parameter, each defaulting to the session union and each bound
// by its own argument. A slot of a named family is still a plain reference.
func TestTwoParametersRenderApart(t *testing.T) {
	world := pairWorld(t)
	if diagnostics := kernel.ValidateIn(world, world["pair"], languages(module, scope)...); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	result, err := kernel.GenerateIn(world, world["pair"], languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for path, wants := range map[string][]string{
		"api/go/pair-protocol/types_generated.go": {
			"type Frame[SEnvelope, SHandle any] struct {",
			"type Echo[TEnvelope any] struct {",
			"type Both[SEnvelope, SHandle, TEnvelope any] struct {",
			"type Named struct {",
			"Held probeprotocol.Envelope",
		},
		"api/go/pair-binding/binding_generated.go": {
			"type Handler[SEnvelope, SHandle, TEnvelope any] interface",
			"params TEnvelope) (protocol.Both[SEnvelope, SHandle, TEnvelope], error)",
			"params protocol.Named) (protocol.Named, error)",
			"EmitEchoed(ctx context.Context, data protocol.Echo[TEnvelope]) error",
		},
		"api/go/pair-client/client_generated.go": {
			"type Client[SEnvelope, SHandle, TEnvelope any] struct",
			"func Dial[SEnvelope runtime.Of[STag], SHandle runtime.Of[STag], TEnvelope runtime.Of[TTag], STag, TTag any](ctx context.Context, url string, options runtime.DialOptions,",
		},
		"api/ts/pair-client/src/types.ts": {
			"export interface Frame<S extends AnyFamily = SessionFamily> {",
			"export interface Echo<T extends AnyFamily = SessionFamily> {",
			"export interface Both<S extends AnyFamily = SessionFamily, T extends AnyFamily = SessionFamily> {",
			`"message": S["Envelope"];`,
			`"back": S["Handle"];`,
			`"heard": T["Envelope"];`,
			"export interface Named {",
		},
		"api/ts/pair-client/src/index.ts": {
			"export class Client<S extends AnyFamily = SessionFamily, T extends AnyFamily = SessionFamily>",
			"readonly s: FamilyBinding<S>;",
			"readonly t: FamilyBinding<T>;",
			`this.slots = { "S": s, "T": t };`,
			"s: FamilyBinding<S>, t: FamilyBinding<T>,",
		},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	// The session union is both families, and neither parameter is tied to one.
	if got := files["api/ts/pair-client/src/types.ts"]; !strings.Contains(got, "export type SessionFamily = codex.Family | probe.Family;") {
		t.Error("the session union is not both session families")
	}
}

// applyContract refers to the carrier's generic Frame twice, filling the
// carrier's parameter S once with its own parameter B and once with a named
// family: an application says which, where a plain reference could not.
const applyContract = `{
 "schema_version":1,"profile":"nightseam.duplex/1","name":"album",
 "parameters":[{"name":"A","of":"session"},{"name":"B","of":"session"}],
 "imports":["carrier"],
 "types":{
  "Mine":{"kind":"record","fields":[{"name":"held","type":{"envelope":"A"}}]},
  "Borrowed":{"kind":"record","fields":[{"name":"frame","type":{"apply":"carrier.Frame","with":{"S":"B"}}}]},
  "Fixed":{"kind":"record","fields":[{"name":"frame","type":{"apply":"carrier.Frame","with":{"S":"probe"}}}]},
  "Both":{"kind":"record","fields":[{"name":"mine","type":"Mine"},{"name":"borrowed","type":"Borrowed"},{"name":"fixed","type":"Fixed"}]}
 },
 "methods":[{"name":"look","go_name":"Look","ts_name":"look","direction":"client_to_server","request":"Mine","result":"Both"}],
 "events":[],"errors":[]
}`

// TestApplicationFillsAnImportedFamilysParameters: a family may refer to a
// generic type of a family it imports by saying what fills each of that
// type's parameters — one of its own, which keeps it generic there, or a
// named family, which does not. Where a plain reference would be ambiguous
// it is refused instead, and the message says what to write.
func TestApplicationFillsAnImportedFamilysParameters(t *testing.T) {
	probe := exampleAPI(t)
	probe["role"] = contract.SessionRole
	var carrier, album map[string]any
	if err := json.Unmarshal([]byte(carrierContract), &carrier); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(applyContract), &album); err != nil {
		t.Fatal(err)
	}
	world := kernel.World{"probe": probe, "carrier": carrier, "album": album}
	if diagnostics := kernel.ValidateIn(world, album, languages(module, scope)...); len(diagnostics) != 0 {
		t.Fatalf("an application was refused: %+v", diagnostics)
	}
	result, err := kernel.GenerateIn(world, album, languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for path, wants := range map[string][]string{
		"api/go/album-protocol/types_generated.go": {
			"type Mine[AEnvelope any] struct {",
			// B fills the carrier's S, so Borrowed is generic in B alone.
			"type Borrowed[BEnvelope any] struct {",
			"Frame carrierprotocol.Frame[BEnvelope]",
			// probe fills it, so Fixed is generic in nothing.
			"type Fixed struct {",
			"Frame carrierprotocol.Frame[probeprotocol.Envelope]",
			"type Both[AEnvelope, BEnvelope any] struct {",
		},
		"api/ts/album-client/src/types.ts": {
			"export interface Borrowed<B extends AnyFamily = SessionFamily> {",
			`"frame": carrier.Frame<B>;`,
			"export interface Fixed {",
			`"frame": carrier.Frame<probe.Family>;`,
		},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	// A plain reference to the same generic type says nothing about which
	// parameter fills it, and is refused with the application to write.
	plain := map[string]any{}
	if err := json.Unmarshal([]byte(applyContract), &plain); err != nil {
		t.Fatal(err)
	}
	plain["types"].(map[string]any)["Borrowed"].(map[string]any)["fields"].([]any)[0].(map[string]any)["type"] = "carrier.Frame"
	found := ""
	for _, d := range kernel.ValidateIn(kernel.World{"probe": probe, "carrier": carrier, "album": plain}, plain, languages(module, scope)...) {
		if d.Code == "ambiguous_application" {
			found = d.Message
		}
	}
	if !strings.Contains(found, `{"apply": "carrier.Frame", "with": {…}}`) {
		t.Fatalf("a plain reference to a generic imported type was not refused with the application to write: %q", found)
	}
}

// drawnContract draws a type of its parameter beyond the two every family
// carries: S.Payload, a record probe declares. Where the two kinds of slot
// draw a family's Envelope and Handle, "S.T" draws any record or enum T of
// whichever family binds S.
const drawnContract = `{
 "schema_version":1,"profile":"nightseam.duplex/1","name":"holder",
 "parameters":[{"name":"S","of":"session"}],
 "types":{
  "Held":{"kind":"record","fields":[{"name":"payload","type":"S.Payload"},{"name":"message","type":{"envelope":"S"}}]}
 },
 "methods":[{"name":"hold","go_name":"Hold","ts_name":"hold","direction":"client_to_server","request":"Held","result":"S.Payload"}],
 "events":[],"errors":[]
}`

// TestASlotDrawsAnyTypeOfTheBoundFamily: "S.Payload" is a slot of S at
// Payload. Go takes a type parameter for it, SPayload, held to S's tag at
// every entry point like the others; TypeScript draws S["Payload"] and
// bounds S to a family that has it; each family's descriptor lists every
// plain type for this. A world with a session family that does not declare
// the type refuses the contract, naming the family.
func TestASlotDrawsAnyTypeOfTheBoundFamily(t *testing.T) {
	probe := exampleAPI(t)
	probe["role"] = contract.SessionRole
	var holder map[string]any
	if err := json.Unmarshal([]byte(drawnContract), &holder); err != nil {
		t.Fatal(err)
	}
	world := kernel.World{"probe": probe, "holder": holder}
	if diagnostics := kernel.ValidateIn(world, holder, languages(module, scope)...); len(diagnostics) != 0 {
		t.Fatalf("a slot of a drawn type was refused: %+v", diagnostics)
	}
	files := map[string]string{}
	for _, family := range []map[string]any{holder, probe} {
		result, err := kernel.GenerateIn(world, family, languages(module, scope)...)
		if err != nil {
			t.Fatal(err)
		}
		for p, data := range result.Files {
			files[p] = string(data)
		}
	}
	for path, wants := range map[string][]string{
		"api/go/holder-protocol/types_generated.go": {
			"type Held[SEnvelope, SPayload any] struct {",
			"Payload SPayload",
			"Message SEnvelope",
		},
		"api/go/holder-client/client_generated.go": {
			"func Dial[SEnvelope runtime.Of[STag], SPayload runtime.Of[STag], STag any](",
			"Hold(ctx context.Context, params protocol.Held[SEnvelope, SPayload]) (SPayload, error)",
		},
		"api/go/probe-protocol/types_generated.go": {
			"type Tag struct{}",
			"func (Payload) Of() Tag { return Tag{} }",
			"func (Envelope) Of() Tag { return Tag{} }",
		},
		"api/ts/holder-client/src/types.ts": {
			`export interface Held<S extends AnyFamily & { "Payload": unknown } = SessionFamily> {`,
			`"payload": S["Payload"];`,
		},
		"api/ts/probe-client/src/types.ts": {
			"export interface Family { readonly name: \"probe\"; ",
			"Payload: Payload",
		},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	// A session family without the type may not bind S, so the world refuses
	// the contract and says which family falls short.
	bare := exampleAPI(t)
	bare["name"] = "bare"
	bare["role"] = contract.SessionRole
	delete(bare["types"].(map[string]any), "Payload")
	for _, method := range bare["methods"].([]any) {
		m := method.(map[string]any)
		delete(m, "request")
		m["result"] = "string"
	}
	for _, event := range bare["events"].([]any) {
		event.(map[string]any)["type"] = "string"
	}
	found := ""
	for _, d := range kernel.ValidateIn(kernel.World{"probe": probe, "bare": bare, "holder": holder}, holder, languages(module, scope)...) {
		if d.Code == "unresolved_type" && strings.Contains(d.Message, "bare") {
			found = d.Message
		}
	}
	if found == "" {
		t.Fatal("a session family lacking the drawn type did not refuse the contract")
	}
}

// TestMixedInstantiationDoesNotCompile: the tag holds. A generic package
// instantiated with an Envelope of one family and a Handle of another, or
// with a type of no family, is refused by the compiler at every entry
// point, whether the type arguments are spelled or inferred.
func TestMixedInstantiationDoesNotCompile(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go")
	directory := t.TempDir()
	renderSlotFixture(t, directory, root)
	writeFixture(t, directory, "mixed_test.go", []byte(`//go:build mixed

package generated
import (
 "context"
 rightclient "example.test/generated/gen/go/carrier-client"
 probe "example.test/generated/api/go/probe-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
)
var _, _ = rightclient.Dial[probe.Envelope, string](context.Background(), "", runtime.DialOptions{}, nil)
`))
	writeFixture(t, directory, "mixed_families_test.go", []byte(`//go:build families

package generated
import (
 "context"
 rightclient "example.test/generated/gen/go/carrier-client"
 probe "example.test/generated/api/go/probe-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
)
var _, _ = rightclient.Dial[probe.Envelope, runtime.Raw](context.Background(), "", runtime.DialOptions{}, nil)
`))
	// The compiler reports one inference failure per package, so each case
	// is its own build.
	for tag, want := range map[string]string{"mixed": "string) does not satisfy runtime.Of[STag] (missing method Of)", "families": "Raw) does not satisfy runtime.Of[STag] (wrong type for method Of)"} {
		command := exec.Command("go", "vet", "-tags", tag, ".")
		command.Dir = directory
		out, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("a mixed instantiation compiled under -tags %s", tag)
		}
		if !strings.Contains(string(out), want) {
			t.Errorf("the compiler did not refuse as expected, wanting %q:\n%s", want, out)
		}
	}
	// The same package, instantiated coherently, compiles: the fixture's own
	// tests are the proof, and so is the relay's opaque instantiation.
	writeFixture(t, directory, "opaque_test.go", []byte(`package generated
import (
 "context"
 rightclient "example.test/generated/gen/go/carrier-client"
 "github.com/Bitspark/nightseam/runtime/go"
)
var _ = func() { _, _ = rightclient.Dial[runtime.Raw, runtime.Raw](context.Background(), "", runtime.DialOptions{}, nil) }
`))
	command := exec.Command("go", "vet", ".")
	command.Dir = directory
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("the opaque instantiation did not compile:\n%s", out)
	}
}

// TestPublicErrorsAreRendered: the errors a family declares reach both
// languages by name — a constant and IsError in the Go protocol package, an
// errors object and an ErrorCode type in the TypeScript client — so that a
// handler returns one and a caller tells it apart without spelling the code.
func TestPublicErrorsAreRendered(t *testing.T) {
	probe := exampleAPI(t)
	probe["errors"] = []any{
		map[string]any{"code": "denied", "description": "The caller is denied"},
		map[string]any{"code": "not_found", "description": "Nothing of that name"},
		map[string]any{"code": "429-too-many", "description": ""},
	}
	result, err := kernel.Generate(probe, languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for path, wants := range map[string][]string{
		// gofmt aligns a constant block, so the constants are matched without
		// regard to the spaces before their equals signs.
		"api/go/probe-protocol/types_generated.go": {
			"// ErrorDenied: The caller is denied",
			`ErrorDenied = "denied"`,
			`ErrorNotFound = "not_found"`,
			`Error429TooMany = "429-too-many"`,
			"var Errors = []string{ErrorDenied, ErrorNotFound, Error429TooMany}",
			"func IsError(err error, code string) bool",
		},
		"api/ts/probe-client/src/index.ts": {
			`export const errors = { /** The caller is denied */ denied: "denied", /** Nothing of that name */ notFound: "not_found", "429-too-many": "429-too-many" } as const;`,
			"export type ErrorCode = (typeof errors)[keyof typeof errors];",
		},
	} {
		got := regexp.MustCompile(`[ 	]+= `).ReplaceAllString(files[path], " = ")
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
}
