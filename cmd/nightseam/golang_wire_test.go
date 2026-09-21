package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedGoWireModelFactories(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/probe/model.json", []byte(`{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`))
	writeFixture(t, directory, "api/contracts/probe/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"methods":{"step":{"request":"Input","result":"integer"}},"events":{"changed":{"type":"Input"}}},"client":{"methods":{"mirror":{"request":"Input","result":"integer"}},"events":{"noted":{"type":"Input"}}}}`))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	for _, file := range []string{"binding/binding", "client/client"} {
		parts := strings.Split(file, "/")
		source, err := os.ReadFile(filepath.Join(directory, "api/go/probe-"+parts[0], parts[1]+"_generated.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range []string{"func Dial", "func Attach", "func Open", "func Serve", "func NewHandler", "type Remote", "type Client", ".ScopeOf(", ".NewPeer("} {
			if strings.Contains(string(source), old) {
				t.Errorf("generated %s retains %s", file, old)
			}
		}
		if !strings.Contains(string(source), "func ToWire") || !strings.Contains(string(source), "func FromWire") {
			t.Errorf("generated %s lacks direct Wire adapters", file)
		}
	}
	if t.Failed() {
		return
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "wire_test.go", []byte(goWireFactoryProgram))
	runFixture(t, directory, "go", "test", "-count=1", ".")
}

const goWireFactoryProgram = `package wire_test
import (
 "context"
 "sync/atomic"
 "testing"
 "time"
 binding "example.test/generated/api/go/probe-binding"
 client "example.test/generated/api/go/probe-client"
 protocol "example.test/generated/api/go/probe-protocol"
 "github.com/Bitspark/nightseam/duplex/go"
 "github.com/Bitspark/nightseam/runtime/go"
)
type implementation struct { remote protocol.Client; state *atomic.Int64; steps chan int64 }
func (s implementation) Step(ctx context.Context, p protocol.Input)(int64,error){n,err:=s.remote.Methods.Mirror(ctx,p);if err!=nil{return 0,err};s.state.Add(n);return s.state.Load(),nil}
type incoming struct{ values chan int64 }
func (s incoming) Noted(_ context.Context,p protocol.Input)error{s.values<-p.Value;return nil}
type reverse struct{ calls *atomic.Int64 }
func (r reverse) Mirror(_ context.Context,p protocol.Input)(int64,error){r.calls.Add(1);return p.Value+1,nil}
type events struct{ values chan int64 }
func(e events) Changed(_ context.Context,p protocol.Input)error{e.values<-p.Value;return nil}
func TestFactories(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 var factories,calls,state atomic.Int64;steps:=make(chan int64,4);changed:=make(chan int64,4)
 var remote protocol.Client
 var model protocol.ServerModel=func(opposite protocol.Client)(protocol.Server,error){factories.Add(1);remote=opposite;return protocol.Server{Methods:implementation{remote:opposite,state:&state},Events:incoming{steps}},nil}
 wire,err:=binding.ToWire(model,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 selected:=duplex.At(duplex.Mount(map[string]duplex.Wire{"nested":wire}),[]string{"nested"})
 var roundtrip protocol.ServerModel
 roundtrip,err=binding.FromWire(ctx,selected,runtime.AdapterContext{});if err!=nil{t.Fatal(err)}
 rewired,err:=binding.ToWire(roundtrip,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer rewired.Close(duplex.CodeNormal,"")
 rebuilt,err:=binding.FromWire(ctx,rewired,runtime.AdapterContext{});if err!=nil{t.Fatal(err)}
 access,err:=rebuilt(protocol.Client{Methods:reverse{&calls},Events:events{changed}});if err!=nil{t.Fatal(err)}
 if _,err=roundtrip(protocol.Client{});err==nil{t.Fatal("factory rebound")}
 n,err:=access.Methods.Step(ctx,protocol.Input{Value:3});if err!=nil||n!=4||calls.Load()!=1||factories.Load()!=1{t.Fatalf("call %d %v factories=%d reverse=%d",n,err,factories.Load(),calls.Load())}
 if err=access.Events.Noted(ctx,protocol.Input{Value:7});err!=nil{t.Fatal(err)}
 if err=remote.Events.Changed(ctx,protocol.Input{Value:9});err!=nil{t.Fatal(err)}
 for _,c:=range []struct{ch chan int64;want int64}{{steps,7},{changed,9}}{select{case n:=<-c.ch:if n!=c.want{t.Fatal(n)};case <-ctx.Done():t.Fatal(ctx.Err())}}
 // The client-side pair closes over exactly the same client model type too.
 clientWire,err:=client.ToWire(func(protocol.Server)(protocol.Client,error){return protocol.Client{Methods:reverse{&calls},Events:events{changed}},nil},runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer clientWire.Close(duplex.CodeNormal,"")
 clientFactory,err:=client.FromWire(ctx,clientWire,runtime.AdapterContext{});if err!=nil{t.Fatal(err)}
 clientAccess,err:=clientFactory(protocol.Server{Methods:implementation{remote:remote,state:&state},Events:incoming{steps}});if err!=nil{t.Fatal(err)}
 if n,err:=clientAccess.Methods.Mirror(ctx,protocol.Input{Value:8});err!=nil||n!=9{t.Fatalf("client model %d %v",n,err)}
}
`
