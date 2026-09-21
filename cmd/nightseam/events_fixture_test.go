package main

// Events wait before construction starts; registration after the host starts
// reading cannot satisfy this fixture by winning a race.
const goEventsFixture = `package generated_test
import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 protocol "example.test/generated/api/go/probe-protocol"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type initialEvents struct{receive func(protocol.Payload)}
func(e initialEvents)Changed(_ context.Context,p protocol.Payload)error{e.receive(p);return nil}
func TestEventsBeforeReading(t *testing.T) {
 for _, mode := range []string{"attach", "dial", "channel"} {
  t.Run(mode, func(t *testing.T) {
   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel()
   received := make(chan protocol.Payload, 1)
   prepared := false
   options := runtime.Options{Prepare: func(p *runtime.Peer) error {
    bind,err:=binding.FromWire(ctx,p.Wire(),runtime.AdapterContext{});if err!=nil{return err}
    if _,err=bind(protocol.Client{Methods:clientHandler{},Events:initialEvents{func(p protocol.Payload){received<-p}}});err!=nil{return err}
    // The generated callback is installed before the rest of host preparation.
    if _,err=p.Wire().Receive([]string{"changed"},duplex.Receiver{Message:func([]string,duplex.Message){}});err==nil{return errors.New("typed event was not installed before host preparation")}
    prepared = true
    return nil
   }}
   var peer *runtime.Peer
   var err error
   if mode == "dial" {
    handler, e := runtime.NewHandler(runtime.ServerOptions{Authenticate: func(r *http.Request)(context.Context,error){return r.Context(),nil}, CheckOrigin:func(*http.Request)bool{return true}, OnConnect:func(p *runtime.Peer){ _ = runtime.EmitWire(ctx,p.Wire(),[]string{"changed"},protocol.Payload{Text:"first",Count:1}) }})
    if e != nil { t.Fatal(e) }
    server := httptest.NewServer(handler); defer server.Close()
    peer,_,err = runtime.Dial(ctx,"ws"+strings.TrimPrefix(server.URL,"http"),runtime.DialOptions{Options:options})
   } else {
    var near, far duplex.Conn
    if mode == "channel" {
     ct, st := tunnels(t, ctx)
     channel, e := st.OpenConnection(ctx,"probe"); if e != nil { t.Fatal(e) }; far = channel
     accepted, e := ct.AcceptConnection(ctx); if e != nil { t.Fatal(e) }; near = accepted
    } else { near,far=duplex.Pipe(1<<20);defer far.Abort() }
    if e:=far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"7:changed\",\"data\":{\"text\":\"first\",\"count\":1}}")});e!=nil{t.Fatal(e)}
    peer,err=runtime.NewPeer(ctx,near,runtime.ClientRole,options)
   }
   if err != nil { t.Fatal(err) }; defer peer.Close()
   if !prepared { t.Fatal("caller Prepare was lost") }
   select { case p := <-received: if p.Text != "first" { t.Fatalf("received %+v",p) }; case <-ctx.Done(): t.Fatal("first event was lost") }
  })
 }
}
func TestEventPreparationErrors(t *testing.T) {
 ctx := context.Background()
 for _, duplicate := range []bool{false,true} {
  near,far := duplex.Pipe(1<<20); defer far.Abort()
  sentinel := errors.New("prepare failed")
  options := runtime.Options{Prepare:func(p *runtime.Peer)error{
   if duplicate { if _,err:=p.Wire().Receive([]string{"changed"},duplex.Receiver{Message:func([]string,duplex.Message){}});err!=nil{return err} }
   bind,err:=binding.FromWire(ctx,p.Wire(),runtime.AdapterContext{});if err!=nil{return err}
   if _,err=bind(protocol.Client{Methods:clientHandler{},Events:initialEvents{func(protocol.Payload){}}});err!=nil{return err}
   return sentinel
  }}
  peer,err := runtime.NewPeer(ctx,near,runtime.ClientRole,options)
  if peer != nil || err == nil { t.Fatalf("preparation returned %v, %v",peer,err) }
  if !duplicate && !errors.Is(err,sentinel) { t.Fatalf("lost Prepare error: %v",err) }
  if duplicate && errors.Is(err,sentinel) { t.Fatal("duplicate registration was accepted") }
 }
}
`

const tsEventsFixture = `import assert from 'node:assert/strict';
import {fromWire} from './api/ts/probe-binding/src/index.ts';
import {DuplexPeer} from './runtime/ts/src/index.ts';
import {at,mount,encodePath} from '@nightseam/duplex';
for (const mounted of [false,true]) {
 let seen;
 const connection = {state: 'open', send() {}, close() {}, abort() {}, listen(listener) {
  listener.frame({kind:'text',data:JSON.stringify({version:1,kind:'event',event:encodePath(['changed']),data:{text:'first',count:1}})});
  return () => {};
 }};
 const peer = new DuplexPeer();
 const wire=mounted?at(mount(new Map([['view',peer.wire()]])),['view']):peer.wire();
 const bind=await fromWire(wire,{});
 bind({methods:{reverse:p=>p},events:{changed:data=>{seen=data;}}});
 await peer.attach(connection);
 await new Promise(resolve => setImmediate(resolve));
 assert.deepEqual(seen,{text:'first',count:1},'host preparation lost the initial event');
 peer.close();
}
`
