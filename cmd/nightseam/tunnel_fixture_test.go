package main

// The generated model adapters receive prepared channel Wires directly. The
// tunnel owns their one peer, while a handle is resolved by the consumer.
const goTunnelFixture = `package generated_test
import (
	duplex "github.com/Bitspark/nightseam/duplex/go"
 bitwire "github.com/Bitspark/bitwire/wire/go"
 "context"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "strings"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 protocol "example.test/generated/api/go/probe-protocol"

 runtime "github.com/Bitspark/nightseam/runtime/go"
 tunnel "github.com/Bitspark/nightseam/tunnel/go"
)
type tunnelServer struct{remote protocol.Client}
func(s tunnelServer)Echo(ctx context.Context,p protocol.Payload)(protocol.Payload,error){
 if err:=s.remote.Events.Changed(ctx,p);err!=nil{return p,err};return s.remote.Methods.Reverse(ctx,p)
}
func(tunnelServer)NoArgs(context.Context)(string,error){return "ok",nil}
type tunnelReverse struct{}
func(tunnelReverse)Reverse(_ context.Context,p protocol.Payload)(protocol.Payload,error){p.Text="reversed:"+p.Text;return p,nil}
type tunnelEvents struct{}
func(tunnelEvents)Changed(context.Context,protocol.Payload)error{return nil}
func prepareTunnelProbe(peer *runtime.Peer)error{
 model,err:=binding.ToWire(func(remote protocol.Client)(protocol.Server,error){return protocol.Server{Methods:tunnelServer{remote},Events:struct{}{}},nil},runtime.AdapterContext{})
 if err!=nil{return err}
 if _,err=runtime.ForwardWire(peer.Wire(),model);err!=nil{_ = model.Close(duplex.CodeInternalError,"setup failed");return err}
 go func(){<-peer.Done();_ = model.Close(duplex.CodeNormal,"")}();return nil
}
func interpretTunnelProbe(ctx context.Context,wire bitwire.Endpoint)(protocol.ServerMethods,error){
 factory,err:=binding.FromWire(ctx,wire,runtime.AdapterContext{});if err!=nil{return nil,err}
 model,err:=factory(protocol.Client{Methods:tunnelReverse{},Events:tunnelEvents{}});if err!=nil{return nil,err};return model.Methods,nil
}
func tunnels(t *testing.T, ctx context.Context) (*tunnel.Tunnel, *tunnel.Tunnel) {
 t.Helper();a,b:=duplex.Pipe(1<<20)
 pa,err:=runtime.NewPeer(ctx,a,runtime.ClientRole,runtime.Options{});if err!=nil{t.Fatal(err)}
 pb,err:=runtime.NewPeer(ctx,b,runtime.ServerRole,runtime.Options{});if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){pa.Close();pb.Close()})
 ct,err:=tunnel.New(pa,tunnel.Options{});if err!=nil{t.Fatal(err)}
 st,err:=tunnel.New(pb,tunnel.Options{});if err!=nil{t.Fatal(err)};return ct,st
}
func TestModelsOverAChannel(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 ct,st:=tunnels(t,ctx);served:=make(chan error,1)
 go func(){_,err:=st.Accept(ctx,runtime.Options{Prepare:prepareTunnelProbe});served<-err}()
 channel,err:=ct.Open(ctx,"probe","",runtime.Options{});if err!=nil{t.Fatal(err)}
 defer channel.Close(duplex.CodeNormal,"")
 if err:=<-served;err!=nil{t.Fatal(err)}
 model,err:=interpretTunnelProbe(ctx,channel);if err!=nil{t.Fatal(err)}
 result,err:=model.Echo(ctx,protocol.Payload{Text:"value",Count:3});if err!=nil{t.Fatal(err)}
 if result.Text!="reversed:value"{t.Fatalf("echoed %#v",result)}
}
func TestChannelResolutionInterpretsAHandle(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 ct,st:=tunnels(t,ctx)
 opened,err:=st.Open(ctx,"probe","",runtime.Options{Prepare:prepareTunnelProbe});if err!=nil{t.Fatal(err)}
 handle:=protocol.Handle{Channel:opened.ID}
 channel,ok,err:=ct.Channel(handle.Channel,runtime.Options{});if err!=nil||!ok{t.Fatalf("channel %v: %v",ok,err)}
 defer channel.Close(duplex.CodeNormal,"")
 model,err:=interpretTunnelProbe(ctx,channel);if err!=nil{t.Fatal(err)}
 if reply,err:=model.NoArgs(ctx);err!=nil||reply!="ok"{t.Fatalf("no_args %q, %v",reply,err)}
 if _,ok,err:=ct.Channel(999,runtime.Options{});err!=nil||ok{t.Fatalf("a handle to no channel resolved: %v, %v",ok,err)}
}
func TestGeneratedTypeScriptOverATunnel(t *testing.T){
 failures:=make(chan error,4)
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var tn *tunnel.Tunnel
  peer,err:=runtime.Accept(w,r,runtime.ServerOptions{Options:runtime.Options{Prepare:func(peer *runtime.Peer)(err error){tn,err=tunnel.New(peer,tunnel.Options{});return err}},Authenticate:func(r *http.Request)(context.Context,error){return r.Context(),nil},CheckOrigin:func(*http.Request)bool{return true}});if err!=nil{return}
  ctx:=peer.Context()
  if _,err:=tn.Accept(ctx,runtime.Options{Prepare:prepareTunnelProbe});err!=nil{failures<-err;return}
  outbound,err:=tn.Open(ctx,"probe","",runtime.Options{Prepare:prepareTunnelProbe});if err!=nil{failures<-err;return}
  if err:=peer.Emit(ctx,"opened",map[string]int64{"channel":outbound.ID});err!=nil{failures<-err;return}
  <-peer.Done()
 })
 server:=httptest.NewServer(handler);defer server.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 command:=exec.CommandContext(ctx,"node","--loader","./runtime-loader.mjs","tunnel-roundtrip.mjs","ws"+strings.TrimPrefix(server.URL,"http"))
 if output,err:=command.CombinedOutput();err!=nil{t.Fatalf("Node generated model over a tunnel: %v\n%s",err,output)}
 select{case err:=<-failures:t.Fatal(err);default:}
}
`

const tsTunnelRoundtrip = `import assert from 'node:assert/strict';
import {DuplexPeer} from '@nightseam/runtime';
import {Tunnel} from '@nightseam/tunnel';
import {fromWire} from './api/ts/probe-binding/src/index.ts';
const outer=new DuplexPeer();
const opened=new Promise(resolve=>{outer.onEvent('opened',resolve);});
const tunnel=new Tunnel(outer);
await outer.connect(process.argv[2]);
const reverse={methods:{reverse(params){return {...params,text:'typescript:'+params.text};}},events:{changed(){}}};
const channel=await tunnel.open('probe','',{});
const attached=(await fromWire(channel,{}))(reverse).methods;
const echoed=await attached.echo({text:'value',count:7,note:null});
assert.equal(echoed.text,'typescript:value');
const handle=await opened;
const resolvedChannel=await tunnel.channel(handle.channel,{});
assert.ok(resolvedChannel);
const resolved=(await fromWire(resolvedChannel,{}))(reverse).methods;
assert.equal(await resolved.noArgs({}),'ok');
assert.equal(await tunnel.channel(999,{}),undefined);
channel.close();resolvedChannel.close();outer.close();
`
