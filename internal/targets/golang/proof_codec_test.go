package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/render"
)

func TestProofAndBuiltinGoFamiliesCompileAndCommunicate(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and connects generated packages")
	}
	loaded, diagnostics := load.Checkout(os.DirFS("../../../cmd/nightseam/testdata"), "proof", []string{Name})
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	world := analysis.World(loaded.Families)
	for name, family := range builtin.Families() {
		world[name] = family
	}
	names := make([]string, 0, len(world))
	for name := range world {
		names = append(names, name)
	}
	sort.Strings(names)
	directory := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	write("go.mod", []byte(fmt.Sprintf("module example.test/proof\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root))))
	target := New(Config{Module: "example.test/proof"})
	for _, name := range names {
		files, err := target.Render(render.Build(analysis.Resolve(world, name)))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
	}
	write("proof_test.go", []byte(proofGoProgram))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated proof: %v\n%s", err, output)
	}
}

const proofGoProgram = `package proof_test
import (
 "context"
 "testing"
 "time"
 binding "example.test/proof/api/go/proof-binding"
 client "example.test/proof/api/go/proof-client"
 protocol "example.test/proof/api/go/proof-protocol"
 probe "example.test/proof/api/go/probe-protocol"
 probeclient "example.test/proof/api/go/probe-client"
 duplex "github.com/Bitspark/nightseam/duplex/go"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type server struct{}
func (server) Echo(ctx context.Context,remote *binding.Remote[probe.Envelope,probe.Handle,string],p probe.Payload)(probe.Payload,error) {
 if err:=remote.EmitChanged(ctx,p); err!=nil { return probe.Payload{},err }; return p,nil
}
func (server) Parts(context.Context,*binding.Remote[probe.Envelope,probe.Handle,string],protocol.PartsRequest)(protocol.Result[protocol.Parts,string],error) {
 return protocol.Result[protocol.Parts,string]{Ok:&protocol.ResultOkValue[protocol.Parts,string]{Value:protocol.Parts{Items:[]protocol.Part{{Count:&protocol.PartCountValue{Value:3}}}}}},nil
}
func (server) Relay(_ context.Context,_ *binding.Remote[probe.Envelope,probe.Handle,string],p protocol.Carried[probe.Envelope,probe.Handle,string])(protocol.Option[protocol.Envelope],error) {
 return protocol.Option[protocol.Envelope]{Some:&protocol.OptionSomeValue[protocol.Envelope]{Value:protocol.Envelope(p.Message)}},nil
}
type reverse struct{}
func (reverse) Reverse(_ context.Context,_ *probeclient.Client,p probe.Payload)(probe.Payload,error) { return p,nil }
func TestMixedParametersAndInheritedOperations(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 near,far:=duplex.Pipe(1<<20)
 peer,err:=binding.Serve[probe.Envelope,probe.Handle,string](ctx,far,runtime.Options{},server{}); if err!=nil { t.Fatal(err) }; defer peer.Close()
 events:=make(chan probe.Payload,1)
 c,err:=client.Attach[probe.Envelope,probe.Handle,string](ctx,near,runtime.Options{},nil,client.Events[probe.Envelope,probe.Handle,string]{Changed:func(_ context.Context,p probe.Payload){events<-p}}); if err!=nil { t.Fatal(err) }; defer c.Close()
 echo,err:=c.Echo(ctx,probe.Payload{Text:"hello"}); if err!=nil || echo.Text!="hello" { t.Fatalf("echo: %#v %v",echo,err) }
 select {case event:=<-events: if event.Text!="hello" { t.Fatal(event) }; case <-ctx.Done(): t.Fatal(ctx.Err())}
 result,err:=c.Parts(ctx,protocol.PartsRequest{}); if err!=nil || result.Ok==nil || result.Ok.Value.Items[0].Count.Value!=3 { t.Fatalf("parts: %#v %v",result,err) }
 relayed,err:=c.Relay(ctx,protocol.Carried[probe.Envelope,probe.Handle,string]{Message:probe.Envelope{Version:1,Kind:"event"},Back:runtime.Null[probe.Handle](),Page:protocol.Page[string]{Items:[]string{"item"}}})
 if err!=nil || relayed.Some==nil || relayed.Some.Value.Kind!="event" { t.Fatalf("relay: %#v %v",relayed,err) }
}
func TestBaseClientSpeaksExtendedBinding(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 near,far:=duplex.Pipe(1<<20)
 peer,err:=binding.Serve[probe.Envelope,probe.Handle,string](ctx,far,runtime.Options{},server{}); if err!=nil { t.Fatal(err) }; defer peer.Close()
 c,err:=probeclient.Attach(ctx,near,runtime.Options{},reverse{},probeclient.Events{}); if err!=nil { t.Fatal(err) }; defer c.Close()
 result,err:=c.Echo(ctx,probe.Payload{Text:"base"}); if err!=nil || result.Text!="base" { t.Fatalf("base client: %#v %v",result,err) }
}
`
