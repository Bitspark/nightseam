package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The commuting diagram: binding a family's parameters to families in the
// declaration and rendering it plain — the left path, oracle.Substitute —
// must give what rendering it generically and instantiating gives — the
// right path. testdata/families holds the carrier, generic in one protocol
// family, and probe, which fills it.

// TestDiagramCommutesInGo holds the diagram in Go: both paths compile in one
// module; the generic rendering instantiated with probe has the types, the
// interfaces and the method sets of the plain rendering, field for field
// and signature for signature; the plain client speaks with the generic
// server and the generic client with the plain server, events included; the
// instantiation validates what fills a slot through probe's codec, and an
// opaque instantiation passes it through; and the left path delegates a
// slot's validation to probe.
func TestDiagramCommutesInGo(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			root := repositoryRoot(t)
			fixture(t, root, "go", "node")
			directory := t.TempDir()
			g.slots(t, directory)
			fixtureModule(t, directory, root)
			copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
			copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
			copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
			copyFixtureTree(t, filepath.Join(root, "live/ts"), filepath.Join(directory, "live/ts"))
			writeFixture(t, directory, "loader.mjs", []byte(slotLoader))
			writeFixture(t, directory, "roundtrip-generic.mjs", []byte(tsGenericRoundtrip))
			writeFixture(t, directory, "diagram_test.go", []byte(goDiagramFixture))
			writeFixture(t, directory, "delegation_test.go", []byte(`package generated
		import ("testing";carrier "example.test/generated/api/go/carrier-protocol")
		func TestDelegation(t *testing.T){
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}}}"));err!=nil{t.Fatal(err)}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1}}"));err==nil{t.Fatal("an envelope without a kind passed")}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"extra\":true}}"));err==nil{t.Fatal("an unknown envelope field passed")}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"traceparent\":\"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\",\"tracestate\":\"vendor=1\"}}"));err!=nil{t.Fatalf("an envelope carrying the trace context did not pass the slot: %v",err)}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"traceparent\":1}}"));err==nil{t.Fatal("a non-string traceparent passed the slot")}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"request\",\"id\":\"c:1\",\"method\":\"read\",\"params\":{},\"meta\":{\"tenant\":\"acme\"}}}"));err!=nil{t.Fatalf("an envelope carrying meta did not pass the slot: %v",err)}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"meta\":{}}}"));err!=nil{t.Fatalf("an empty carriage did not pass the slot: %v",err)}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"meta\":{\"attempt\":2}}}"));err==nil{t.Fatal("a non-string meta value passed the slot")}
		 if err:=carrier.ValidateRaw("Frame",[]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"meta\":\"acme\"}}"));err==nil{t.Fatal("a meta that is no object passed the slot")}
		 if err:=carrier.ValidateExpressionRaw("probe.Nope",[]byte("{}"));err==nil{t.Fatal("an unknown imported type passed")}
		 if err:=carrier.ValidateExpressionRaw("nobody.Envelope",[]byte("{}"));err==nil{t.Fatal("an unknown family passed")}
		}`))
			runFixture(t, directory, "go", "test", "-count=1", "./...")
		})
	}
}

// The two renderings intentionally have the same package name. Resolve the
// generic binding's protocol import to its own artifact, so the socket proof
// cannot accidentally exercise the substituted validator for both routes.
const slotLoader = `export async function resolve(specifier, context, next) {
 const map = {'@nightseam/runtime':'./runtime/ts/src/index.ts','@nightseam/duplex':'./duplex/ts/src/index.ts','@nightseam/tunnel':'./tunnel/ts/src/index.ts','@nightseam/live':'./live/ts/src/index.ts'};
 const name = specifier.slice('@example/'.length);
 const root = specifier === '@example/carrier-client/types' && context.parentURL?.includes('/gen/ts/') ? './gen/ts/' : './api/ts/';
 const generated = name.endsWith('/types') ? root + name.slice(0, -6) + '/src/types.ts' : root + name + '/src/index.ts';
 const entry = map[specifier] ?? (specifier.startsWith('@example/') ? generated : undefined);
 if (entry) return {url: new URL(entry, import.meta.url).href, shortCircuit: true};
 return next(specifier, context);
}`

// tsGenericRoundtrip dials a carrier server with the generic TypeScript
// client bound to probe: a relayed frame comes back as its envelope, the
// event arrives typed, and a frame whose envelope probe refuses is refused
// before it is sent.
const tsGenericRoundtrip = `import assert from 'node:assert/strict';
import {fromWire} from './gen/ts/carrier-binding/src/index.ts';
import {family as probe} from './api/ts/probe-client/src/index.ts';
import {DuplexPeer} from '@nightseam/runtime';
const peer = new DuplexPeer();
let observed;
const bind = await fromWire(peer.wire(), {}, probe);
const client = bind({methods: {}, events: {frameRelayed(frame) { observed = frame; }}});
await peer.connect(process.argv[2]);
const message = {version: 1, kind: 'event', event: 'changed', data: {}};
const result = await client.methods.relay({sequence: 1, message});
assert.deepEqual(result, message);
const deadline = Date.now() + 5000;
while (!observed) { assert.ok(Date.now() < deadline, 'frameRelayed did not arrive'); await new Promise(resolve => setTimeout(resolve, 1)); }
assert.equal(observed.sequence, 1);
assert.deepEqual(observed.message, message);
await assert.rejects(client.methods.relay({sequence: 2, message: {version: 1}}));
const attachment = await client.methods.attach({id: 'x'});
assert.equal(attachment.connection.channel, 7);
peer.close();
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
 left "example.test/generated/api/go/carrier-protocol"
 probe "example.test/generated/api/go/probe-protocol"
 rightbinding "example.test/generated/gen/go/carrier-binding"
 right "example.test/generated/gen/go/carrier-protocol"
 "github.com/Bitspark/nightseam/runtime/go"
 "github.com/Bitspark/nightseam/duplex/go"
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
  {"ServerMethods", reflect.TypeOf((*left.ServerMethods)(nil)).Elem(), reflect.TypeOf((*right.ServerMethods[E, H])(nil)).Elem()},
  {"ClientMethods", reflect.TypeOf((*left.ClientMethods)(nil)).Elem(), reflect.TypeOf((*right.ClientMethods[E, H])(nil)).Elem()},
  {"ServerEvents", reflect.TypeOf((*left.ServerEvents)(nil)).Elem(), reflect.TypeOf((*right.ServerEvents[E, H])(nil)).Elem()},
  {"ClientEvents", reflect.TypeOf((*left.ClientEvents)(nil)).Elem(), reflect.TypeOf((*right.ClientEvents[E, H])(nil)).Elem()},
  {"Server", reflect.TypeOf(left.Server{}), reflect.TypeOf(right.Server[E, H]{})},
  {"Client", reflect.TypeOf(left.Client{}), reflect.TypeOf(right.Client[E, H]{})},
  {"ServerModel", reflect.TypeOf((*left.ServerModel)(nil)).Elem(), reflect.TypeOf((*right.ServerModel[E, H])(nil)).Elem()},
  {"ClientModel", reflect.TypeOf((*left.ClientModel)(nil)).Elem(), reflect.TypeOf((*right.ClientModel[E, H])(nil)).Elem()},
 } {
  if !same(pair.left, pair.right) { t.Errorf("%s: %v is not %v", pair.name, pair.right, pair.left) }
  if !sameMethods(pair.left, pair.right) { t.Errorf("%s: the method set of %v is not %v's", pair.name, pair.right, pair.left) }
 }
}
type rightServer struct{ remote right.Client[E, H] }
func (rightServer) Attach(ctx context.Context, params right.AttachParams) (right.Attachment[H], error) {
 return right.Attachment[H]{Connection: H{Channel: 7}, Last: 1}, nil
}
func (s rightServer) Relay(ctx context.Context, frame right.Frame[E]) (E, error) {
 if err := s.remote.Events.FrameRelayed(ctx, frame); err != nil { return frame.Message, err }
 return frame.Message, nil
}
type leftServer struct{ remote left.Client }
func (leftServer) Attach(ctx context.Context, params left.AttachParams) (left.Attachment, error) {
 return left.Attachment{Connection: H{Channel: 7}, Last: 1}, nil
}
func (s leftServer) Relay(ctx context.Context, frame left.Frame) (E, error) {
 if err := s.remote.Events.FrameRelayed(ctx, frame); err != nil { return frame.Message, err }
 return frame.Message, nil
}
var options = runtime.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }}
func rightModel(remote right.Client[E,H]) (right.Server[E,H],error) { return right.Server[E,H]{Methods:rightServer{remote},Events:struct{}{}},nil }
func leftModel(remote left.Client) (left.Server,error) { return left.Server{Methods:leftServer{remote},Events:struct{}{}},nil }
type leftEvents struct{ relayed chan left.Frame }
func (e leftEvents) FrameRelayed(_ context.Context, frame left.Frame) error {e.relayed<-frame;return nil}
type rightEvents struct{ relayed chan right.Frame[E] }
func (e rightEvents) FrameRelayed(_ context.Context, frame right.Frame[E]) error {e.relayed<-frame;return nil}
func wireHandler(t *testing.T, model func()(duplex.Wire,error)) http.Handler {
 t.Helper()
 settings:=options
 settings.Options.Prepare=func(peer *runtime.Peer)error{
  wire,err:=model();if err!=nil{return err}
  if _,err=runtime.ForwardWire(peer.Wire(),wire);err!=nil{_ = wire.Close(duplex.CodeInternalError,"forward failed");return err}
  go func(){<-peer.Done();_ = wire.Close(duplex.CodeNormal,"")}()
  return nil
 }
 handler,err:=runtime.NewHandler(settings);if err!=nil{t.Fatal(err)};return handler
}
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
 h := wireHandler(t,func()(duplex.Wire,error){return rightbinding.ToWire(rightModel,runtime.AdapterContext{})})
 server, url := serve(t, h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 relayed := make(chan left.Frame, 1)
 var c left.Server
 peer, _, err := runtime.Dial(ctx,url,runtime.DialOptions{Options:runtime.Options{Prepare:func(peer *runtime.Peer)error{
  bind,err:=leftbinding.FromWire(ctx,peer.Wire(),runtime.AdapterContext{});if err!=nil{return err}
  c,err=bind(left.Client{Methods:struct{}{},Events:leftEvents{relayed}});return err
 }}})
 if err != nil { t.Fatal(err) }
 defer peer.Close()
 message := envelope(t)
 result, err := c.Methods.Relay(ctx, left.Frame{Sequence: 1, Message: message})
 if err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(result, message) { t.Fatalf("relayed %#v", result) }
 select {
 case frame := <-relayed:
  if frame.Sequence != 1 || !reflect.DeepEqual(frame.Message, message) { t.Fatalf("event %#v", frame) }
 case <-ctx.Done():
  t.Fatal("no event")
 }
 attachment, err := c.Methods.Attach(ctx, left.AttachParams{ID: "x"})
 if err != nil || attachment.Connection.Channel != 7 { t.Fatalf("attach %#v %v", attachment, err) }
}
func TestGenericClientSpeaksWithPlainServer(t *testing.T) {
 h := wireHandler(t,func()(duplex.Wire,error){return leftbinding.ToWire(leftModel,runtime.AdapterContext{})})
 server, url := serve(t, h)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 relayed := make(chan right.Frame[E], 1)
 var c right.Server[E,H]
 peer, _, err := runtime.Dial(ctx,url,runtime.DialOptions{Options:runtime.Options{Prepare:func(peer *runtime.Peer)error{
  bind,err:=rightbinding.FromWire[E,H](ctx,peer.Wire(),runtime.AdapterContext{});if err!=nil{return err}
  c,err=bind(right.Client[E,H]{Methods:struct{}{},Events:rightEvents{relayed}});return err
 }}})
 if err != nil { t.Fatal(err) }
 defer peer.Close()
 message := envelope(t)
 result, err := c.Methods.Relay(ctx, right.Frame[E]{Sequence: 2, Message: message})
 if err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(result, message) { t.Fatalf("relayed %#v", result) }
 select {
 case frame := <-relayed:
  if frame.Sequence != 2 || !reflect.DeepEqual(frame.Message, message) { t.Fatalf("event %#v", frame) }
 case <-ctx.Done():
  t.Fatal("no event")
 }
 attachment, err := c.Methods.Attach(ctx, right.AttachParams{ID: "x"})
 if err != nil || attachment.Connection.Channel != 7 { t.Fatalf("attach %#v %v", attachment, err) }
 var caller right.ServerMethods[E, H] = c.Methods
 if _, err := caller.Attach(ctx, right.AttachParams{ID: "y"}); err != nil { t.Fatal(err) }
}
func TestInstantiationValidatesThroughTheFamily(t *testing.T) {
 var frame right.Frame[E]
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1}}"), &frame); err == nil { t.Fatal("an envelope without a kind passed probe's codec") }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"extra\":true}}"), &frame); err == nil { t.Fatal("an unknown envelope field passed probe's codec") }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{}}}"), &frame); err != nil { t.Fatal(err) }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"traceparent\":\"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\",\"tracestate\":\"vendor=1\"}}"), &frame); err != nil { t.Fatalf("an envelope carrying the trace context did not pass probe's codec: %v", err) }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"traceparent\":1}}"), &frame); err == nil { t.Fatal("a non-string traceparent passed probe's codec") }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"request\",\"id\":\"c:1\",\"method\":\"read\",\"params\":{},\"meta\":{\"tenant\":\"acme\"}}}"), &frame); err != nil { t.Fatalf("an envelope carrying meta did not pass probe's codec: %v", err) }
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{},\"meta\":{\"attempt\":2}}}"), &frame); err == nil { t.Fatal("a non-string meta value passed probe's codec") }
 var opaque right.Frame[runtime.Raw]
 if err := json.Unmarshal([]byte("{\"sequence\":1,\"message\":{\"version\":1}}"), &opaque); err != nil { t.Fatalf("the opaque instantiation did not pass a message through: %v", err) }
 if string(opaque.Message) != "{\"version\":1}" { t.Fatalf("passed through %s", opaque.Message) }
 if err := right.ValidateRaw("Frame", []byte("{\"sequence\":1,\"message\":{\"version\":1}}")); err != nil { t.Fatalf("the raw validator did not see the role's slot as JSON: %v", err) }
 if err := right.ValidateRaw("Frame", []byte("{\"sequence\":1}")); err == nil { t.Fatal("a frame without a message passed") }
 if err := right.ValidateExpressionRaw("probe.Envelope", []byte("{\"version\":1}")); err == nil { t.Fatal("a reference to a named family's envelope did not delegate to it") }
 if err := right.ValidateExpressionRaw("probe.Envelope", []byte("{\"version\":1,\"kind\":\"event\"}")); err != nil { t.Fatal(err) }
 if err := right.ValidateExpressionRaw("nobody.Handle", []byte("{\"channel\":1}")); err == nil { t.Fatal("a reference to an unknown family passed") }
}
func TestGenericTypeScriptClientSpeaksWithPlainServer(t *testing.T) {
 if _, err := exec.LookPath("node"); err != nil { t.Skip("Node is not installed") }
 h := wireHandler(t,func()(duplex.Wire,error){return leftbinding.ToWire(leftModel,runtime.AdapterContext{})})
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
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			root := repositoryRoot(t)
			tsc := fixture(t, root, "node", "tsc")
			directory := t.TempDir()
			g.slots(t, directory)
			fixtureModule(t, directory, root)
			copyFixtureTree(t, filepath.Join(root, "runtime/ts"), filepath.Join(directory, "runtime/ts"))
			copyFixtureTree(t, filepath.Join(root, "duplex/ts"), filepath.Join(directory, "duplex/ts"))
			copyFixtureTree(t, filepath.Join(root, "tunnel/ts"), filepath.Join(directory, "tunnel/ts"))
			copyFixtureTree(t, filepath.Join(root, "live/ts"), filepath.Join(directory, "live/ts"))
			writeFixture(t, directory, "gen/ts/diagram.ts", []byte(tsDiagramFixture))
			config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)}, "include": []string{"api/ts/**/*.ts", "runtime/ts/**/*.ts", "duplex/ts/**/*.ts", "tunnel/ts/**/*.ts", "live/ts/**/*.ts", "gen/**/*.ts"}, "exclude": []string{"gen/ts/*-binding"}}
			data, _ := json.Marshal(config)
			writeFixture(t, directory, "tsconfig.json", data)
			writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
			runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
			checkGenericTypeScriptBindings(t, directory, tsc)
			writeFixture(t, directory, "loader.mjs", []byte(slotLoader))
			writeFixture(t, directory, "delegation.mjs", []byte(`import assert from 'node:assert/strict';import {validateWire} from './api/ts/carrier-client/src/types.ts';
		validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{}}});
		assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1}}));
		assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',extra:true}}));
		validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{},traceparent:'00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01',tracestate:'vendor=1'}});
		assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{},traceparent:1}}));
		validateWire('Frame',{sequence:1,message:{version:1,kind:'request',id:'c:1',method:'read',params:{},meta:{tenant:'acme'}}});
		validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{},meta:{}}});
		assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{},meta:{attempt:2}}}));
		assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{},meta:'acme'}}));
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
		validateWire('Frame', {sequence: 1, message: {version: 1, kind: 'event', event: 'changed', data: {}, traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'vendor=1'}}, '$', {S: probe});
		assert.throws(() => validateWire('Frame', {sequence: 1, message: {version: 1, kind: 'event', event: 'changed', data: {}, traceparent: 1}}, '$', {S: probe}));
		validateWire('Frame', {sequence: 1, message: {version: 1, kind: 'request', id: 'c:1', method: 'read', params: {}, meta: {tenant: 'acme'}}}, '$', {S: probe});
		assert.throws(() => validateWire('Frame', {sequence: 1, message: {version: 1, kind: 'event', event: 'changed', data: {}, meta: {attempt: 2}}}, '$', {S: probe}));
		assert.throws(() => validateWire('Frame', good), /binding of the parameter S/);
		assert.throws(() => validateWire('probe.Envelope', {version: 1}));
		validateWire('probe.Envelope', good.message);
		assert.throws(() => validateWire('nobody.Handle', {channel: 1}));
		assert.equal(carrier.name, 'carrier');
		assert.equal(probe.name, 'probe');
		`))
			runFixture(t, directory, "node", "--loader", "./loader.mjs", "generic.mjs")
		})
	}
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
export const serverMethods: Equals<right.ServerMethods<probe.Family>, left.ServerMethods> = true;
export const clientMethods: Equals<right.ClientMethods<probe.Family>, left.ClientMethods> = true;
export const serverEvents: Equals<right.ServerEvents<probe.Family>, left.ServerEvents> = true;
export const clientEvents: Equals<right.ClientEvents<probe.Family>, left.ClientEvents> = true;
export const server: Equals<right.Server<probe.Family>, left.Server> = true;
export const client: Equals<right.Client<probe.Family>, left.Client> = true;
export const serverModel: Equals<right.ServerModel<probe.Family>, left.ServerModel> = true;
export const clientModel: Equals<right.ClientModel<probe.Family>, left.ClientModel> = true;
export const bound: Equals<right.FamilyBinding<probe.Family>["name"], "probe"> = true;
`

// pairContract is generic in two parameters at once: S at both kinds, T at
// an envelope, and a named family at a third slot. It is what proves the
// parameters do not collapse into one another in either language.
func TestMixedInstantiationDoesNotCompile(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			root := repositoryRoot(t)
			fixture(t, root, "go")
			directory := t.TempDir()
			g.slots(t, directory)
			fixtureModule(t, directory, root)
			writeFixture(t, directory, "mixed_test.go", []byte(`//go:build mixed

		package generated
		import (
		 "context"
		 rightclient "example.test/generated/gen/go/carrier-client"
		 probe "example.test/generated/api/go/probe-protocol"
		 "github.com/Bitspark/nightseam/runtime/go"
		)
		var _, _ = rightclient.FromWire[probe.Envelope, string](context.Background(), nil, runtime.AdapterContext{})
		`))
			writeFixture(t, directory, "mixed_families_test.go", []byte(`//go:build families

		package generated
		import (
		 "context"
		 rightclient "example.test/generated/gen/go/carrier-client"
		 probe "example.test/generated/api/go/probe-protocol"
		 "github.com/Bitspark/nightseam/runtime/go"
		)
		var _, _ = rightclient.FromWire[probe.Envelope, runtime.Raw](context.Background(), nil, runtime.AdapterContext{})
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
		var _ = func() { _, _ = rightclient.FromWire[runtime.Raw, runtime.Raw](context.Background(), nil, runtime.AdapterContext{}) }
		`))
			command := exec.Command("go", "vet", ".")
			command.Dir = directory
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("the opaque instantiation did not compile:\n%s", out)
			}
		})
	}
}
