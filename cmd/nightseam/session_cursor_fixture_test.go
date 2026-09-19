package main

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

func TestGeneratedSessionCursor(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	for _, target := range []spi.Target{
		golang.New(golang.Config{Module: module}),
		typescript.New(typescript.Config{Scope: scope}),
	} {
		t.Run(target.Name(), func(t *testing.T) {
			testGeneratedSession(t, root, tsc, target, goSessionCursorFixture, tsSessionCursorFixture)
		})
	}
}

const goSessionCursorFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "fmt"
 "reflect"
 "testing"
 "time"
 generic "example.test/generated/api/go/generic-client"
 plain "example.test/generated/api/go/plain-client"
 client "example.test/generated/api/go/probe-client"
 sessionprotocol "example.test/generated/api/go/session-protocol"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
 session "github.com/Bitspark/nightseam/session/go"
)
func TestRetainedCursor(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 log:=session.NewMemoryLog(1<<20)
 frame:=func(text string) json.RawMessage { b,_:=json.Marshal(map[string]any{"version":1,"kind":"event","event":"changed","data":text}); return b }
 for i,direction:=range []session.Direction{session.Down,session.Up,session.Down,session.Up} {
  if _,err:=log.Append(ctx,session.Frame{Direction:direction,Message:frame(fmt.Sprint(i+1))});err!=nil {t.Fatal(err)}
 }
 registry,err:=session.New(session.Options{});if err!=nil {t.Fatal(err)}
 up,machine:=duplex.Pipe(1<<20);defer machine.Abort()
 if err:=registry.Bind("cursor",up,session.Governance{Decides:client.Decides,Asks:client.Asks},log);err!=nil {t.Fatal(err)}
 attach:=func(after int64,events client.Events) (*client.Client,*session.Attachment) {
  t.Helper();conn,down:=duplex.Pipe(1<<20)
  attachment,err:=registry.Attach("cursor",down,session.Participant,"reader",after);if err!=nil {t.Fatal(err)}
  options:=runtime.Options{Prepare:func(peer *runtime.Peer)error {
   if err:=peer.HandleEvent("session.cursor",func(context.Context,*runtime.Peer,json.RawMessage){});err==nil {return fmt.Errorf("cursor tracker not installed before Prepare")};return nil
  }}
  c,err:=client.Attach(ctx,conn,options,nil,events);if err!=nil {t.Fatal(err)}
  t.Cleanup(func(){c.Close();attachment.Detach()});return c,attachment
 }
 take:=func(events <-chan string,want string) {t.Helper();select {case got:=<-events:if got!=want {t.Fatalf("got %q, want %q",got,want)};case <-ctx.Done():t.Fatalf("waiting for %q: %v",want,ctx.Err())}}
 waitSequence:=func(c *client.Client,want int64) {t.Helper();tick:=time.NewTicker(time.Millisecond);defer tick.Stop();for c.Sequence()!=want {select{case <-tick.C:case <-ctx.Done():t.Fatalf("sequence=%d, want %d",c.Sequence(),want)}}}
 var zero client.Client
 if zero.Sequence()!=0 {t.Fatal("initial cursor is not zero")}
 events:=make(chan string,16)
 a,attachment:=attach(0,client.Events{
  Changed:func(_ context.Context,v string){events<-"changed:"+v},
  SessionCursor:func(_ context.Context,v sessionprotocol.Cursor){events<-fmt.Sprintf("cursor:%d",v.Sequence)},
 })
 for _,want:=range []string{"changed:1","cursor:1","changed:3","cursor:3","cursor:4"} {take(events,want)}
 if a.Sequence()!=4 {t.Fatalf("sequence=%d, counted delivered frames or missed replay end",a.Sequence())}
 if err:=a.OnSessionCursor(func(context.Context,sessionprotocol.Cursor){});err==nil {t.Fatal("duplicate cursor callback accepted")}
 a.Close();attachment.Detach()
 resumed:=make(chan string,16)
 b,_:=attach(a.Sequence(),client.Events{Changed:func(_ context.Context,v string){resumed<-"changed:"+v}})
 // Resuming at the head delivers nothing; state is zero until a new cursor.
 if b.Sequence()!=0 {t.Fatal("client invented an initial cursor")}
 if err:=machine.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:frame("live")});err!=nil {t.Fatal(err)}
 take(resumed,"changed:live");waitSequence(b,5)
 if err:=b.OnSessionCursor(nil);err==nil {t.Fatal("nil cursor callback accepted")}
 if err:=b.OnSessionCursor(func(_ context.Context,v sessionprotocol.Cursor){resumed<-fmt.Sprintf("cursor:%d:%d",v.Sequence,b.Sequence())});err!=nil {t.Fatal(err)}
 if err:=machine.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:frame("later")});err!=nil {t.Fatal(err)}
 take(resumed,"changed:later");take(resumed,"cursor:6:6")
 if _,ok:=reflect.TypeOf((*plain.Client)(nil)).MethodByName("Sequence");ok {t.Fatal("protocol-only client gained session state")}
}
type sequenceHandler struct { seen chan *generic.Client[string] }
func (h sequenceHandler) SequenceRead(_ context.Context,c *generic.Client[string]) (int64,error) {h.seen<-c;return c.Sequence(),nil}
func TestGenericReverseHandlerSharesCursor(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 near,far:=duplex.Pipe(1<<20)
 peer,err:=runtime.NewPeer(ctx,far,runtime.ServerRole,runtime.Options{});if err!=nil {t.Fatal(err)};defer peer.Close()
 if err:=peer.Emit(ctx,"session.cursor",sessionprotocol.Cursor{Sequence:29});err!=nil {t.Fatal(err)}
 cursors:=make(chan int64,1);seen:=make(chan *generic.Client[string],1)
 c,err:=generic.Attach[string](ctx,near,runtime.Options{},sequenceHandler{seen},generic.Events[string]{SessionCursor:func(_ context.Context,p sessionprotocol.Cursor){cursors<-p.Sequence}})
 if err!=nil {t.Fatal(err)};defer c.Close()
 select {case got:=<-cursors:if got!=29||c.Sequence()!=29 {t.Fatal("generic client lost queued cursor")};case <-ctx.Done():t.Fatal(ctx.Err())}
 var got int64
 if err:=peer.Call(ctx,"sequence_read",map[string]any{},&got);err!=nil {t.Fatal(err)}
 if got!=29 {t.Fatalf("reverse handler saw cursor %d",got)}
 select {case delivered:=<-seen:if delivered!=c {t.Fatal("reverse handler received a different client")};case <-ctx.Done():t.Fatal(ctx.Err())}
}
`

const tsSessionCursorFixture = `import {Client} from './api/ts/probe-client/src/index.ts';
import {Client as PlainClient} from './api/ts/plain-client/src/index.ts';
import {DuplexPeer} from '@nightseam/runtime';
import {pipe, type FrameConnection} from '@nightseam/duplex';
import {Registry, memoryLog} from './session/ts/src/index.ts';

function mailbox() {
 const values:string[]=[]; const waiting:Array<(value:string)=>void>=[];
 return {
  push(value:string) {const receive=waiting.shift();if(receive)receive(value);else values.push(value);},
  async expect(want:string) {
   let timer:ReturnType<typeof setTimeout>|undefined;
   const next=values.length?Promise.resolve(values.shift()!):new Promise<string>(resolve=>waiting.push(resolve));
   try {const got=await Promise.race([next,new Promise<never>((_,reject)=>{timer=setTimeout(()=>reject(new Error('waiting for '+want)),5000);})]);if(got!==want)throw new Error('got '+got+', want '+want);}finally{clearTimeout(timer);}
  }
 };
}
const tick=()=>new Promise<void>(resolve=>setTimeout(resolve,1));
async function sequence(c:Client,want:number) {const end=Date.now()+5000;while(c.sequence!==want){if(Date.now()>end)throw new Error('sequence='+c.sequence+', want '+want);await tick();}}
const frame=(text:string)=>({version:1 as const,kind:'event' as const,event:'changed',data:text});
const log=memoryLog(1<<20);
for(const [i,direction] of (['down','up','down','up'] as const).entries()) await log.append({sequence:0,direction,origin:'',at:new Date(),truncated:false,message:frame(String(i+1))});
const registry=new Registry();const [up,machine]=pipe();registry.bind('cursor',up,{decides:()=>false,asks:()=>false},log);
const [conn,down]=pipe();const attachment=registry.attach('cursor',down,'participant','reader',0);
const events=mailbox();
const a=await Client.attach(conn,{},undefined,{changed:p=>events.push('changed:'+p),sessionCursor:p=>events.push('cursor:'+p.sequence)});
try {
 for(const want of ['changed:1','cursor:1','changed:3','cursor:3','cursor:4'])await events.expect(want);
 if(a.sequence!==4)throw new Error('counted delivered frames or missed replay end');
 a.close();attachment.detach();
 const [second,secondDown]=pipe();const resumedAttachment=registry.attach('cursor',secondDown,'participant','reader',a.sequence);
 const resumed=mailbox();const b=await Client.attach(second,{},undefined,{changed:p=>resumed.push('changed:'+p)});
 try {
  if(b.sequence!==0)throw new Error('client invented an initial cursor');
  machine.send({kind:'text',data:JSON.stringify(frame('live'))});await resumed.expect('changed:live');await sequence(b,5);
  const stop=b.onSessionCursor(p=>resumed.push('cursor:'+p.sequence+':'+b.sequence));
  machine.send({kind:'text',data:JSON.stringify(frame('later'))});await resumed.expect('changed:later');await resumed.expect('cursor:6:6');
  stop();machine.send({kind:'text',data:JSON.stringify(frame('unsubscribed'))});await resumed.expect('changed:unsubscribed');await sequence(b,7);
 }finally{b.close();resumedAttachment.detach();}
}finally{a.close();attachment.detach();machine.close();}

// A connection may deliver queued frames synchronously from listen().
const immediate=mailbox();const peer=new DuplexPeer();const c=new Client(peer,undefined,{sessionCursor:p=>immediate.push(p.sequence+':'+c.sequence)});
if(c.sequence!==0)throw new Error('initial cursor is not zero');
const queued:FrameConnection={state:'open',buffered:0,send:()=>{},close:()=>{},listen(handlers){handlers.frame?.({kind:'text',data:JSON.stringify({version:1,kind:'event',event:'session.cursor',data:{sequence:29}})});return()=>{};}};
await peer.attach(queued);await immediate.expect('29:29');c.close();
if('sequence' in PlainClient.prototype)throw new Error('protocol-only client gained session state');
function plainSurface(c:PlainClient) {
 // @ts-expect-error Protocol-only clients do not expose sequence state.
 return c.sequence;
}
`
