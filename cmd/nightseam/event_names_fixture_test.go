package main

import (
	"path/filepath"
	"testing"
)

// A target override changes the callback field, never the event on the
// wire. Both targets must still allow that callback to be omitted.
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
	for _, component := range []string{"runtime", "duplex", "tunnel"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "tsconfig.json", []byte(`{
		"compilerOptions": {
			"target":"ES2022", "module":"NodeNext", "moduleResolution":"NodeNext",
			"strict":true, "skipLibCheck":true, "noEmit":true, "allowImportingTsExtensions":true,
			"paths": {
				"@nightseam/runtime":["./runtime/ts/src/index.ts"],
				"@nightseam/duplex":["./duplex/ts/src/index.ts"],
				"@nightseam/tunnel":["./tunnel/ts/src/index.ts"]
			}
		},
		"include":["api/ts/**/*.ts","events.ts"]
	}`))
	writeFixture(t, directory, "events.ts", []byte(tsEventNameOverrideFixture))
	writeFixture(t, directory, "events_test.go", []byte(goEventNameOverrideFixture))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "events.ts")
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

const tsEventNameOverrideFixture = `import {Client, type Events} from './api/ts/probe-client/src/index.ts';
import {DuplexPeer, type FrameConnection} from '@nightseam/runtime';
for (const supplied of [false, true]) {
 const peer = new DuplexPeer();
 let receive!: (value: string) => void;
 const delivered = new Promise<string>(resolve => { receive = resolve; });
 const events: Events = supplied ? {textChanged: receive} : {};
 const client = new Client(peer, undefined, events);
 if (!supplied) client.onTextChanged(receive);
 const connection: FrameConnection = {state:'open', buffered:0, send() {}, close() {}, listen(listener) {
  listener.frame?.({kind:'text',data:JSON.stringify({version:1,kind:'event',event:'to_string',data:'first'})});
  return () => {};
 }};
 await peer.attach(connection);
 if (await delivered !== 'first') throw new Error('the override changed the wire event');
 client.close();
}
`

const goEventNameOverrideFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "fmt"
 "testing"
 "time"
 client "example.test/generated/api/go/probe-client"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)
func TestOptionalEvent(t *testing.T) {
 for _, supplied := range []bool{false,true} {
  t.Run(fmt.Sprint(supplied),func(t *testing.T) {
   ctx,cancel := context.WithTimeout(context.Background(),5*time.Second); defer cancel()
   near,far := duplex.Pipe(1<<20); defer far.Abort()
   received := make(chan string,1)
   events := client.Events{}
   if supplied { events.ToString = func(_ context.Context,value string){ received <- value } }
   options := runtime.Options{Prepare:func(peer *runtime.Peer)error {
    err := peer.HandleEvent("to_string",func(_ context.Context,_ *runtime.Peer,raw json.RawMessage){
     var value string; if e := json.Unmarshal(raw,&value); e != nil { received <- e.Error() } else { received <- value }
    })
    if supplied { if err == nil { return fmt.Errorf("typed event was not installed") }; return nil }
    return err
   }}
   if err := far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"to_string\",\"data\":\"first\"}")}); err != nil { t.Fatal(err) }
   c,err := client.Attach(ctx,near,options,nil,events); if err != nil { t.Fatal(err) }; defer c.Close()
   select { case value := <-received: if value != "first" { t.Fatalf("received %q",value) }; case <-ctx.Done(): t.Fatal("lost wire event") }
  })
 }
}
`
