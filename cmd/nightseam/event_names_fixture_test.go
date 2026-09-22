package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A target override changes the callback field, never the event on the
// wire. Receivers are prepared before the host reads; the event facet is bound
// after the identity exchange completes.
func TestGeneratedEventNameOverride(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/probe/model.json", []byte(`{"nightseam":2,"types":{}}`))
	writeFixture(t, directory, "api/contracts/probe/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"events":{"to_string":{"type":"string"}}}}`))
	writeFixture(t, directory, "api/contracts/probe/typescript.json", []byte(`{"names":{"to_string":"textChanged"}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)},
		"include":         []string{"api/ts/**/*.ts", "events.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "events.ts", []byte(tsEventNameOverrideFixture))
	writeFixture(t, directory, "events_test.go", []byte(goEventNameOverrideFixture))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "events.ts")
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

const tsEventNameOverrideFixture = `import {prepareFromWire, type ClientEvents} from './api/ts/probe-binding/src/index.ts';
import {DuplexPeer, createDispatcher, type FrameConnection} from '@nightseam/runtime';
import {mount,encodePath,pipe} from '@nightseam/duplex';
for (const mounted of [false,true]) {
 const peer = new DuplexPeer();
 let receive!: (value: string) => void;
 const delivered = new Promise<string>(resolve => { receive = resolve; });
 const events: ClientEvents = {textChanged: receive};
 const root=mounted?mount(new Map([['nested',peer.wire()]])):undefined;
 const dispatcher=root?createDispatcher(root):undefined;
 const wire=dispatcher?dispatcher.select(['nested']):peer.wire();
 const prepared=prepareFromWire(wire,{});
 const [near,far]=pipe();
 const remote=new DuplexPeer({role:'server'});
 await remote.attach(far);
 const connection: FrameConnection = {get state(){return near.state;},get buffered(){return near.buffered;},send(frame){near.send(frame);},close(code,reason){near.close(code,reason);},listen(listener) {
  listener.frame?.({kind:'text',data:JSON.stringify({version:1,kind:'event',event:encodePath(['to_string']),data:'first'})});
  return near.listen(listener);
 }};
 await peer.attach(connection);
 const bind=await prepared.complete();
 bind({methods:{},events});
 if (await delivered !== 'first') throw new Error('the override changed the wire event');
 prepared.close();
 dispatcher?.close();root?.close();
 peer.close();
 remote.close();
}
`

const goEventNameOverrideFixture = `package generated_test
import (
	duplex "github.com/Bitspark/nightseam/duplex/go"
 bitwire "github.com/Bitspark/bitwire/wire/go"
 "context"
 "fmt"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 protocol "example.test/generated/api/go/probe-protocol"

 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type events struct{received chan string}
func(e events)ToString(_ context.Context,value string)error{e.received<-value;return nil}
func TestEventWireName(t *testing.T) {
 for _, mounted := range []bool{false,true} {
  t.Run(fmt.Sprint(mounted),func(t *testing.T) {
   ctx,cancel := context.WithTimeout(context.Background(),5*time.Second); defer cancel()
   near,far := duplex.Pipe(1<<20); defer far.Abort()
   received := make(chan string,1)
   var complete func(context.Context)(protocol.ServerModel,error)
   var cleanup func()
   options := runtime.Options{Prepare:func(peer *runtime.Peer)error {
    wire:=peer.Wire();if mounted{root:=duplex.Mount(map[string]bitwire.Endpoint{"nested":wire});t.Cleanup(func(){root.Close(duplex.CodeNormal,"")});dispatcher,err:=runtime.NewDispatcher(root);if err!=nil{return err};t.Cleanup(func(){dispatcher.Close(duplex.CodeNormal,"")});wire=dispatcher.Select([]string{"nested"})}
    var err error
    complete,cleanup,err=binding.PrepareFromWire(wire,runtime.AdapterContext{});if err!=nil{return err}
    if _,err=wire.Receive(bitwire.Receiver{Message:func([]string,bitwire.Message){}});err==nil{return fmt.Errorf("typed event attachment was not installed")}
    return nil
   }}
   if err := far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"9:to_string\",\"data\":\"first\"}")}); err != nil { t.Fatal(err) }
   // The raw remote answers method_not_found while the first event waits.
   remote,err:=runtime.NewPeer(ctx,far,runtime.ServerRole,runtime.Options{});if err!=nil{t.Fatal(err)};defer remote.Close()
   peer,err := runtime.NewPeer(ctx,near,runtime.ClientRole,options); if err != nil { t.Fatal(err) }; defer peer.Close();defer cleanup()
   bind,err:=complete(ctx);if err!=nil{t.Fatal(err)}
   if _,err=bind(protocol.Client{Methods:struct{}{},Events:events{received}});err!=nil{t.Fatal(err)}
   select { case value := <-received: if value != "first" { t.Fatalf("received %q",value) }; case <-ctx.Done(): t.Fatal("lost wire event") }
  })
 }
}
`
