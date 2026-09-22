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
	loaded, diagnostics := load.Checkout(os.DirFS("../../../cmd/nightseam/testdata/families"), "api/contracts", []string{Name, "typescript"})
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	world := analysis.World{"probe": loaded.Families["probe"], "proof": loaded.Families["proof"]}
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
	command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-parallel", "4", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated proof: %v\n%s", err, output)
	}
}

const proofGoProgram = `package proof_test
import (
 "context"
 "errors"
 "testing"
 "time"
 binding "example.test/proof/api/go/proof-binding"
 protocol "example.test/proof/api/go/proof-protocol"
 probe "example.test/proof/api/go/probe-protocol"
 probebinding "example.test/proof/api/go/probe-binding"

 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type server struct{protocol.ServerMethods[probe.Envelope,probe.Handle,string];remote protocol.Client[probe.Envelope,probe.Handle,string]}
func (s server) Echo(ctx context.Context,p probe.Payload)(probe.Payload,error) {
 if err:=s.remote.Events.Changed(ctx,p); err!=nil { return probe.Payload{},err }; return p,nil
}
func (server) Parts(context.Context,protocol.PartsRequest)(protocol.Result[protocol.Parts,string],error) {
 return protocol.Result[protocol.Parts,string]{Ok:&protocol.ResultOkValue[protocol.Parts,string]{Value:protocol.Parts{Items:[]protocol.Part{{Count:&protocol.PartCountValue{Value:3}}}}}},nil
}
func (server) Relay(_ context.Context,p protocol.Carried[probe.Envelope,probe.Handle,string])(protocol.Option[protocol.Envelope],error) {
 return protocol.Option[protocol.Envelope]{Some:&protocol.OptionSomeValue[protocol.Envelope]{Value:protocol.Envelope(p.Message)}},nil
}
type reverse struct{}
func (reverse) Reverse(_ context.Context,p probe.Payload)(probe.Payload,error) { return p,nil }
type receiver struct{protocol.ClientEvents[probe.Envelope,probe.Handle,string];values chan probe.Payload}
func(r receiver) Changed(_ context.Context,p probe.Payload)error{r.values<-p;return nil}
func model(remote protocol.Client[probe.Envelope,probe.Handle,string])(protocol.Server[probe.Envelope,probe.Handle,string],error){return protocol.Server[probe.Envelope,probe.Handle,string]{Methods:server{remote:remote}},nil}
func TestMixedParametersAndInheritedOperations(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 wire,err:=binding.ToWire[probe.Envelope,probe.Handle,string](model,runtime.AdapterContext{},runtime.JSONAdapter[probe.Envelope](),runtime.JSONAdapter[probe.Handle](),runtime.JSONAdapter[string]());if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 events:=make(chan probe.Payload,1)
 factory,err:=binding.FromWire[probe.Envelope,probe.Handle,string](ctx,wire,runtime.AdapterContext{},runtime.JSONAdapter[probe.Envelope](),runtime.JSONAdapter[probe.Handle](),runtime.JSONAdapter[string]());if err!=nil{t.Fatal(err)}
 c,err:=factory(protocol.Client[probe.Envelope,probe.Handle,string]{Events:receiver{values:events}});if err!=nil{t.Fatal(err)}
 echo,err:=c.Methods.Echo(ctx,probe.Payload{Text:"hello"}); if err!=nil || echo.Text!="hello" { t.Fatalf("echo: %#v %v",echo,err) }
 select {case event:=<-events: if event.Text!="hello" { t.Fatal(event) }; case <-ctx.Done(): t.Fatal(ctx.Err())}
 result,err:=c.Methods.Parts(ctx,protocol.PartsRequest{}); if err!=nil || result.Ok==nil || result.Ok.Value.Items[0].Count.Value!=3 { t.Fatalf("parts: %#v %v",result,err) }
 relayed,err:=c.Methods.Relay(ctx,protocol.Carried[probe.Envelope,probe.Handle,string]{Message:probe.Envelope{Version:1,Kind:"event"},Back:runtime.Null[probe.Handle](),Page:protocol.Page[string]{Items:[]string{"item"}}})
 if err!=nil || relayed.Some==nil || relayed.Some.Value.Kind!="event" { t.Fatalf("relay: %#v %v",relayed,err) }
}
func TestBaseClientRefusesExtendedBinding(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 wire,err:=binding.ToWire[probe.Envelope,probe.Handle,string](model,runtime.AdapterContext{},runtime.JSONAdapter[probe.Envelope](),runtime.JSONAdapter[probe.Handle](),runtime.JSONAdapter[string]());if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 base,err:=probebinding.FromWire(ctx,wire,runtime.AdapterContext{})
 var public *runtime.PublicError
 if base!=nil || !errors.As(err,&public) || public.Code!="contract_mismatch" { t.Fatalf("cross-family interpretation: %v",err) }
 // A refused interpretation leaves the wire available to its own family.
 factory,err:=binding.FromWire[probe.Envelope,probe.Handle,string](ctx,wire,runtime.AdapterContext{},runtime.JSONAdapter[probe.Envelope](),runtime.JSONAdapter[probe.Handle](),runtime.JSONAdapter[string]());if err!=nil{t.Fatal(err)}
 events:=make(chan probe.Payload,1)
 c,err:=factory(protocol.Client[probe.Envelope,probe.Handle,string]{Methods:reverse{},Events:receiver{values:events}});if err!=nil{t.Fatal(err)}
 result,err:=c.Methods.Echo(ctx,probe.Payload{Text:"inherited"}); if err!=nil || result.Text!="inherited" { t.Fatalf("inherited method: %#v %v",result,err) }
 select {case event:=<-events: if event.Text!="inherited" { t.Fatal(event) }; case <-ctx.Done(): t.Fatal(ctx.Err())}
}
`
