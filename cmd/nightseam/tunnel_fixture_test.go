package main

// The generated code over a tunnel, in both languages: a client attached
// to a channel it opened, a server serving the channel it accepted, and a
// handle resolved to the channel the server opened.

const goTunnelFixture = `package generated_test
import (
 "context"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "strings"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 client "example.test/generated/api/go/probe-client"
 protocol "example.test/generated/api/go/probe-protocol"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
 tunnel "github.com/Bitspark/nightseam/tunnel/go"
)
// tunnels is two outer peers over a pipe, with a tunnel each.
func tunnels(t *testing.T, ctx context.Context) (*tunnel.Tunnel, *tunnel.Tunnel) {
 t.Helper()
 a, b := duplex.Pipe(1 << 20)
 pa, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{})
 if err != nil { t.Fatal(err) }
 pb, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{})
 if err != nil { t.Fatal(err) }
 t.Cleanup(func() { pa.Close(); pb.Close() })
 ct, err := tunnel.New(pa, tunnel.Options{})
 if err != nil { t.Fatal(err) }
 st, err := tunnel.New(pb, tunnel.Options{})
 if err != nil { t.Fatal(err) }
 return ct, st
}
func TestAttachAndServeOverAChannel(t *testing.T) {
 ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
 defer cancel()
 ct, st := tunnels(t, ctx)
 served := make(chan error, 1)
 go func() {
  channel, err := st.Accept(ctx)
  if err != nil { served <- err; return }
  peer, err := binding.Serve(ctx, channel, runtime.Options{}, serverHandler{})
  if err != nil { served <- err; return }
  served <- nil
  <-peer.Done()
 }()
 channel, err := ct.Open(ctx, "probe")
 if err != nil { t.Fatal(err) }
 if err := <-served; err != nil { t.Fatal(err) }
 c, err := client.Attach(ctx, channel, runtime.Options{}, clientHandler{}, client.Events{})
 if err != nil { t.Fatal(err) }
 defer c.Close()
 result, err := c.Echo(ctx, protocol.Payload{Text: "value", Count: 3})
 if err != nil { t.Fatal(err) }
 if result.Text != "reversed:value" { t.Fatalf("echoed %#v", result) }
}
func TestOpenResolvesAHandle(t *testing.T) {
 ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
 defer cancel()
 ct, st := tunnels(t, ctx)
 opened := make(chan int64, 1)
 go func() {
  channel, err := st.Open(ctx, "probe")
  if err != nil { t.Error(err); opened <- 0; return }
  if _, err := binding.Serve(ctx, channel, runtime.Options{}, serverHandler{}); err != nil { t.Error(err); opened <- 0; return }
  opened <- channel.ID
 }()
 id := <-opened
 if id == 0 { t.Fatal("the server opened no channel") }
 c, err := client.Open(ctx, ct, protocol.Handle{Channel: id}, runtime.Options{}, clientHandler{}, client.Events{})
 if err != nil { t.Fatal(err) }
 defer c.Close()
 if reply, err := c.NoArgs(ctx); err != nil || reply != "ok" { t.Fatalf("no_args %q, %v", reply, err) }
 if _, err := client.Open(ctx, ct, protocol.Handle{Channel: 999}, runtime.Options{}, clientHandler{}, client.Events{}); err == nil { t.Fatal("a handle to no channel opened") }
}
func TestGeneratedTypeScriptOverATunnel(t *testing.T) {
 if _, err := exec.LookPath("node"); err != nil { t.Skip("Node is not installed") }
 failures := make(chan error, 4)
 handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  peer, err := runtime.Accept(w, r, runtime.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }})
  if err != nil { return }
  ctx := peer.Context()
  tn, err := tunnel.New(peer, tunnel.Options{})
  if err != nil { failures <- err; return }
  inbound, err := tn.Accept(ctx)
  if err != nil { failures <- err; return }
  if _, err := binding.Serve(ctx, inbound, runtime.Options{}, serverHandler{}); err != nil { failures <- err; return }
  outbound, err := tn.Open(ctx, "probe")
  if err != nil { failures <- err; return }
  if _, err := binding.Serve(ctx, outbound, runtime.Options{}, serverHandler{}); err != nil { failures <- err; return }
  if err := peer.Emit(ctx, "opened", map[string]int64{"channel": outbound.ID}); err != nil { failures <- err; return }
  <-peer.Done()
 })
 server := httptest.NewServer(handler)
 defer server.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "tunnel-roundtrip.mjs", "ws"+strings.TrimPrefix(server.URL, "http"))
 if output, err := command.CombinedOutput(); err != nil { t.Fatalf("Node generated client over a tunnel: %v\n%s", err, output) }
 select { case err := <-failures: t.Fatal(err); default: }
}
`

const tsTunnelRoundtrip = `import assert from 'node:assert/strict';
import { DuplexPeer } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';
import { Client } from './api/ts/probe-client/src/index.ts';
const outer = new DuplexPeer();
const opened = new Promise(resolve => { outer.onEvent('opened', resolve); });
await outer.connect(process.argv[2]);
const tunnel = new Tunnel(outer);
const handler = { reverse(params) { return { ...params, text: 'typescript:' + params.text }; } };
const channel = await tunnel.open('probe');
const attached = await Client.attach(channel, {}, handler, {});
const echoed = await attached.echo({ text: 'value', count: 7, note: null });
assert.equal(echoed.text, 'typescript:value');
const handle = await opened;
const resolved = await Client.open(tunnel, handle, {}, handler, {});
assert.equal(await resolved.noArgs(), 'ok');
await assert.rejects(Client.open(tunnel, { channel: 999 }, {}, handler, {}));
attached.close();
resolved.close();
outer.close();
`
