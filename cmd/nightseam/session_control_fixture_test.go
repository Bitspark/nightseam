package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// The relay has already sent control and replay before the generated client
// starts reading. Its typed construction callback must see both in order;
// a second client installs its typed control callback after construction.
func TestGeneratedSessionControl(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	for _, target := range []spi.Target{
		golang.New(golang.Config{Module: module}),
		typescript.New(typescript.Config{Scope: scope}),
	} {
		t.Run(target.Name(), func(t *testing.T) {
			testGeneratedSessionControl(t, root, tsc, target)
		})
	}
}

func testGeneratedSessionControl(t *testing.T, root, tsc string, target spi.Target) {
	t.Helper()
	directory := t.TempDir()
	for _, family := range []string{"probe", "plain"} {
		writeFixture(t, directory, "api/contracts/"+family+"/model.json", []byte(`{"nightseam":2}`))
		writeFixture(t, directory, "api/contracts/"+family+"/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"events":{"changed":{"type":"string"}}}}`))
	}
	writeFixture(t, directory, "api/contracts/probe/session.json", []byte(`{}`))
	k := kernel.New(target)
	world := k.Load(os.DirFS(directory), "api/contracts")
	for _, name := range world.Names {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		writeAll(t, directory, result.Files)
	}
	if target.Name() == golang.Name {
		fixtureModule(t, directory, root)
		writeFixture(t, directory, "session_control_test.go", []byte(goSessionControlFixture))
		runFixture(t, directory, "go", "test", "-count=1", "./...")
		return
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "session"} {
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
				"@nightseam/tunnel":["./tunnel/ts/src/index.ts"],
				"@example/*":["./api/ts/*/src/index.ts"]
			}
		},
		"include":["api/ts/**/*.ts","session-control.ts"]
	}`))
	writeFixture(t, directory, "session-control.ts", []byte(tsSessionControlFixture))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(sessionControlLoader))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "session-control.ts")
}

const sessionControlLoader = runtimeLoader

const goSessionControlFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "fmt"
 "reflect"
 "testing"
 "time"
 plain "example.test/generated/api/go/plain-client"
 client "example.test/generated/api/go/probe-client"
 sessionprotocol "example.test/generated/api/go/session-protocol"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
 session "github.com/Bitspark/nightseam/session/go"
)
func TestTypedSessionControl(t *testing.T) {
 ctx,cancel := context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 log := session.NewMemoryLog(1<<20)
 _,err := log.Append(ctx,session.Frame{Direction:session.Down,Message:json.RawMessage("{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":\"replay\"}")})
 if err != nil { t.Fatal(err) }
 registry,err := session.New(session.Options{}); if err != nil { t.Fatal(err) }
 up,machine := duplex.Pipe(1<<20); defer machine.Abort()
 if err := registry.Bind("conversation",up,session.Governance{Decides:client.Decides,Asks:client.Asks},log); err != nil { t.Fatal(err) }
 first,down := duplex.Pipe(1<<20)
 alice,err := registry.Attach("conversation",down,session.Participant,"alice",0); if err != nil { t.Fatal(err) }; defer alice.Detach()
 firstEvents := make(chan string,8)
 holder := func(p sessionprotocol.Control) string { if p.Holder.Null { return "control:null" }; return "control:"+p.Holder.Value }
 prepared := false
 options := runtime.Options{Prepare:func(peer *runtime.Peer)error {
  if err := peer.HandleEvent("session.control",func(context.Context,*runtime.Peer,json.RawMessage){}); err == nil { return fmt.Errorf("typed control was not installed before Prepare") }
  prepared = true
  return nil
 }}
 a,err := client.Attach(ctx,first,options,nil,client.Events{
  SessionControl:func(_ context.Context,p sessionprotocol.Control){firstEvents<-holder(p)},
  Changed:func(_ context.Context,p string){firstEvents<-p},
 }); if err != nil { t.Fatal(err) }; defer a.Close()
 if !prepared { t.Fatal("caller Prepare was lost") }
 take := func(ch <-chan string,want string) { t.Helper(); select { case got:=<-ch: if got!=want { t.Fatalf("got %q, want %q",got,want) }; case <-ctx.Done(): t.Fatalf("waiting for %q: %v",want,ctx.Err()) } }
 take(firstEvents,"control:null")
 take(firstEvents,"replay")
 if err:=registry.Control("conversation",alice); err!=nil { t.Fatal(err) }
 take(firstEvents,"control:alice")

 second,otherDown := duplex.Pipe(1<<20)
 bob,err := registry.Attach("conversation",otherDown,session.Participant,"bob",0); if err != nil { t.Fatal(err) }; defer bob.Detach()
 secondEvents := make(chan string,8)
 b,err := client.Attach(ctx,second,runtime.Options{},nil,client.Events{Changed:func(_ context.Context,p string){secondEvents<-p}})
 if err != nil { t.Fatal(err) }; defer b.Close()
 // Seeing replay proves that the unhandled initial control was consumed.
 take(secondEvents,"replay")
 if err:=b.OnSessionControl(func(_ context.Context,p sessionprotocol.Control){secondEvents<-holder(p)}); err!=nil { t.Fatal(err) }
 if err:=registry.Control("conversation",bob); err!=nil { t.Fatal(err) }
 take(firstEvents,"control:bob")
 take(secondEvents,"control:bob")
 if err:=registry.Control("conversation",nil); err!=nil { t.Fatal(err) }
 take(firstEvents,"control:null")
 take(secondEvents,"control:null")
 head,err:=log.(session.Header).Head(ctx); if err!=nil || head!=1 { t.Fatalf("control entered the log: head=%d err=%v",head,err) }
 if _,ok:=reflect.TypeOf(plain.Events{}).FieldByName("SessionControl"); ok { t.Fatal("protocol-only family gained a session callback") }
 if _,ok:=reflect.TypeOf((*plain.Client)(nil)).MethodByName("OnSessionControl"); ok { t.Fatal("protocol-only family gained a session registration") }
}
`

const tsSessionControlFixture = `import {Client} from './api/ts/probe-client/src/index.ts';
import {Client as PlainClient, type Events as PlainEvents} from './api/ts/plain-client/src/index.ts';
import {pipe} from '@nightseam/duplex';
import {Registry, memoryLog} from './session/ts/src/index.ts';

function mailbox() {
 const values: string[] = [];
 const waiting: Array<(value: string) => void> = [];
 return {
  push(value: string) { const receive=waiting.shift(); if(receive) receive(value); else values.push(value); },
  async expect(want: string) {
   let timer: ReturnType<typeof setTimeout> | undefined;
   const available = values.length ? Promise.resolve(values.shift()!) : new Promise<string>(resolve=>waiting.push(resolve));
   try {
    const got = await Promise.race([available,new Promise<never>((_,reject)=>{timer=setTimeout(()=>reject(new Error('waiting for '+want)),5000);})]);
    if(got!==want) throw new Error('received '+got+', expected '+want);
   } finally { clearTimeout(timer); }
  }
 };
}
const log=memoryLog(1<<20);
await log.append({sequence:0,direction:'down',origin:'',at:new Date(),truncated:false,message:{version:1,kind:'event',event:'changed',data:'replay'}});
const registry=new Registry();
const [up,machine]=pipe();
registry.bind('conversation',up,{decides:()=>false,asks:()=>false},log);
const [first,down]=pipe();
const alice=registry.attach('conversation',down,'participant','alice',0);
const firstEvents=mailbox();
const a=await Client.attach(first,{},undefined,{
 sessionControl:p=>firstEvents.push('control:'+p.holder),
 changed:p=>firstEvents.push(p),
});
try {
 await firstEvents.expect('control:null');
 await firstEvents.expect('replay');
 registry.control('conversation',alice);
 await firstEvents.expect('control:alice');

 const [second,otherDown]=pipe();
 const bob=registry.attach('conversation',otherDown,'participant','bob',0);
 const secondEvents=mailbox();
 const b=await Client.attach(second,{},undefined,{changed:p=>secondEvents.push(p)});
 try {
  await secondEvents.expect('replay');
  b.onSessionControl(p=>secondEvents.push('control:'+p.holder));
  registry.control('conversation',bob);
  await firstEvents.expect('control:bob');
  await secondEvents.expect('control:bob');
  registry.control('conversation',null);
  await firstEvents.expect('control:null');
  await secondEvents.expect('control:null');
  if(await log.head!()!==1) throw new Error('control entered the log');
 } finally { b.close(); bob.detach(); }
} finally { a.close(); alice.detach(); machine.close(); }

const plainEvents: PlainEvents={};
// @ts-expect-error A protocol-only family has no session construction callback.
plainEvents.sessionControl=()=>{};
if('onSessionControl' in PlainClient.prototype) throw new Error('protocol-only family gained a session registration');
`
