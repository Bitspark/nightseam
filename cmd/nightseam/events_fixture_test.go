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
   var complete func(context.Context)(protocol.ServerModel,error)
   var cleanup func()
   options := runtime.Options{Prepare: func(p *runtime.Peer) error {
    var err error
    complete,cleanup,err=binding.PrepareFromWire(p.Wire(),runtime.AdapterContext{});if err!=nil{return err}
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
     channel, e := st.OpenConnection(ctx,"probe", ""); if e != nil { t.Fatal(e) }; far = channel
     accepted, e := ct.AcceptConnection(ctx); if e != nil { t.Fatal(e) }; near = accepted
    } else { near,far=duplex.Pipe(1<<20);defer far.Abort() }
    if e:=far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"7:changed\",\"data\":{\"text\":\"first\",\"count\":1}}")});e!=nil{t.Fatal(e)}
    // A raw peer has no identity handler and answers method_not_found.
    remote,e:=runtime.NewPeer(ctx,far,runtime.ServerRole,runtime.Options{});if e!=nil{t.Fatal(e)};defer remote.Close()
    peer,err=runtime.NewPeer(ctx,near,runtime.ClientRole,options)
   }
   if err != nil { t.Fatal(err) }; defer peer.Close()
   if !prepared { t.Fatal("caller Prepare was lost") };defer cleanup()
   bind,err:=complete(ctx);if err!=nil{t.Fatal(err)}
   if _,err=bind(protocol.Client{Methods:clientHandler{},Events:initialEvents{func(p protocol.Payload){received<-p}}});err!=nil{t.Fatal(err)}
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
   _,cleanup,err:=binding.PrepareFromWire(p.Wire(),runtime.AdapterContext{});if err!=nil{return err};defer cleanup()
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
import {prepareFromWire} from './api/ts/probe-binding/src/index.ts';
import {DuplexPeer} from './runtime/ts/src/index.ts';
import {at,mount,encodePath,pipe} from '@nightseam/duplex';
for (const mounted of [false,true]) {
 let receive;
 const received=new Promise(resolve=>{receive=resolve;});
 const [near,far]=pipe();
 const remote=new DuplexPeer({role:'server'});
 await remote.attach(far);
 const connection = {get state(){return near.state;},get buffered(){return near.buffered;},send(frame){near.send(frame);},close(code,reason){near.close(code,reason);},listen(listener) {
  listener.frame({kind:'text',data:JSON.stringify({version:1,kind:'event',event:encodePath(['changed']),data:{text:'first',count:1}})});
  return near.listen(listener);
 }};
 const peer = new DuplexPeer();
 const wire=mounted?at(mount(new Map([['view',peer.wire()]])),['view']):peer.wire();
 const prepared=prepareFromWire(wire,{});
 await peer.attach(connection);
 const bind=await prepared.complete();
 bind({methods:{reverse:p=>p},events:{changed:data=>{receive(data);}}});
 assert.deepEqual(await received,{text:'first',count:1},'host preparation lost the initial event');
 prepared.close();
 peer.close();
 remote.close();
}
`
