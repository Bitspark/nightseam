package main

// The event is waiting before construction starts, so a handler installed
// after Attach returns cannot satisfy this fixture by winning a race.
const goEventsFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
 client "example.test/generated/api/go/probe-client"
 protocol "example.test/generated/api/go/probe-protocol"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)
func TestEventsBeforeReading(t *testing.T) {
 for _, mode := range []string{"attach", "dial", "open"} {
  t.Run(mode, func(t *testing.T) {
   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel()
   received := make(chan protocol.Payload, 1)
   prepared := false
   events := client.Events{Changed: func(_ context.Context, p protocol.Payload) { received <- p }}
   options := runtime.Options{Prepare: func(p *runtime.Peer) error {
    // The generated callback must already be installed when the caller's hook runs.
    if err := p.HandleEvent("changed", func(context.Context, *runtime.Peer, json.RawMessage) {}); err == nil { return errors.New("typed event was not installed before Prepare") }
    prepared = true
    return nil
   }}
   var c *client.Client
   var err error
   if mode == "dial" {
    handler, e := runtime.NewHandler(runtime.ServerOptions{Authenticate: func(r *http.Request)(context.Context,error){return r.Context(),nil}, CheckOrigin:func(*http.Request)bool{return true}, OnConnect:func(p *runtime.Peer){ _ = p.Emit(ctx,"changed",protocol.Payload{Text:"first",Count:1}) }})
    if e != nil { t.Fatal(e) }
    server := httptest.NewServer(handler); defer server.Close()
    c, err = client.Dial(ctx,"ws"+strings.TrimPrefix(server.URL,"http"),runtime.DialOptions{Options:options},clientHandler{},events)
   } else {
    var near, far duplex.Conn
    if mode == "open" {
     ct, st := tunnels(t, ctx)
     channel, e := st.Open(ctx, "probe", ""); if e != nil { t.Fatal(e) }; far = channel
     accepted, e := ct.Accept(ctx); if e != nil { t.Fatal(e) }; near = accepted
     if e = far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{\"text\":\"first\",\"count\":1}}")}); e != nil { t.Fatal(e) }
     c, err = client.Open(ctx,ct,protocol.Handle{Channel:accepted.ID},options,clientHandler{},events)
    } else {
     near, far = duplex.Pipe(1<<20); defer far.Abort()
     if e := far.Send(ctx,duplex.Frame{Kind:duplex.Text,Data:[]byte("{\"version\":1,\"kind\":\"event\",\"event\":\"changed\",\"data\":{\"text\":\"first\",\"count\":1}}")}); e != nil { t.Fatal(e) }
     c, err = client.Attach(ctx,near,options,clientHandler{},events)
    }
   }
   if err != nil { t.Fatal(err) }; defer c.Close()
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
  options := runtime.Options{Prepare:func(*runtime.Peer)error{return sentinel}}
  if duplicate { options.Events = map[string]runtime.EventHandler{"changed":func(context.Context,*runtime.Peer,json.RawMessage){}} }
  c,err := client.Attach(ctx,near,options,clientHandler{},client.Events{Changed:func(context.Context,protocol.Payload){}})
  if c != nil || err == nil { t.Fatalf("preparation returned %v, %v",c,err) }
  if !duplicate && !errors.Is(err,sentinel) { t.Fatalf("lost Prepare error: %v",err) }
 }
}
`

const tsEventsFixture = `import assert from 'node:assert/strict';
import {Client} from './api/ts/probe-client/src/index.ts';
import {DuplexPeer} from './runtime/ts/src/index.ts';
const handler = {reverse: p => p};
for (const mode of ['attach', 'open', 'constructor']) {
 let seen;
 const events = {changed: data => { seen = data; }};
 const connection = {state: 'open', send() {}, close() {}, abort() {}, listen(listener) {
  listener.frame({kind:'text',data:JSON.stringify({version:1,kind:'event',event:'changed',data:{text:'first',count:1}})});
  return () => {};
 }};
 let client;
 if (mode === 'constructor') { const peer = new DuplexPeer(); client = new Client(peer,handler,events); await peer.attach(connection); }
 else if (mode === 'open') client = await Client.open({channel: () => connection},{channel:1},{},handler,events);
 else client = await Client.attach(connection,{},handler,events);
 await new Promise(resolve => setImmediate(resolve));
 assert.deepEqual(seen,{text:'first',count:1},mode+' lost the initial event');
 client.close();
}
`
