package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func TestGeneratedIdentityPreparation(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and connects generated packages")
	}
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"server":{"methods":{"run":{"request":"integer","result":"integer"}},"events":{"changed":{"type":"integer"}}},"client":{"methods":{"back":{"request":"integer","result":"integer"}}}`),
		},
		"slot": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"server":{"methods":{"echo":{"request":"T","result":"T"}}}`),
		},
	}))
	for name, family := range builtin.Families() {
		world[name] = family
	}
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
	write("go.mod", []byte(fmt.Sprintf("module example.test/identity\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root))))
	target := New(Config{Module: "example.test/identity"})
	for name := range world {
		files, err := target.Render(render.Build(analysis.Resolve(world, name)))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
	}
	write("identity_test.go", []byte(identityGoProgram))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-parallel", "4", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated identity: %v\n%s", err, output)
	}
}

const identityGoProgram = `package identity_test
import (
 duplex "github.com/Bitspark/nightseam/duplex/go"
 bitwire "github.com/Bitspark/bitwire/wire/go"
 "context"
 "encoding/json"
 "errors"
 "strings"
 "sync"
 "sync/atomic"
 "testing"
 "time"
 binding "example.test/identity/api/go/x-binding"
 client "example.test/identity/api/go/x-client"
 protocol "example.test/identity/api/go/x-protocol"
 slotbinding "example.test/identity/api/go/slot-binding"
 slot "example.test/identity/api/go/slot-protocol"

 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type forward struct{calls *atomic.Int64}
func (f forward) Run(_ context.Context,n int64)(int64,error){f.calls.Add(1);return n+1,nil}
type backward struct{}
func (backward) Back(_ context.Context,n int64)(int64,error){return n+2,nil}
type events struct{values chan int64}
func (e events) Changed(_ context.Context,n int64)error{e.values<-n;return nil}
type trackedWire struct {
 bitwire.Endpoint
 eventEntered chan struct{}
 receiversDetached chan struct{}
 eventOnce sync.Once
 registrations atomic.Int64
}
func(w *trackedWire)Receive(r bitwire.Receiver)(func(),error){
 w.registrations.Add(1)
 if w.eventEntered!=nil {
  original:=r.Message
  r.Message=func(path []string,m bitwire.Message){if len(path)==1&&path[0]=="changed"{w.eventOnce.Do(func(){close(w.eventEntered)})};original(path,m)}
 }
 off,err:=w.Endpoint.Receive(r);if err!=nil{return nil,err}
 var once sync.Once
 return func(){off();once.Do(func(){
  if w.receiversDetached!=nil{close(w.receiversDetached)}
 })},nil
}
func testContext(t *testing.T)context.Context{t.Helper();ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);t.Cleanup(cancel);return ctx}
func wait(t *testing.T,ctx context.Context,ch <-chan struct{}){t.Helper();select{case <-ch:case <-ctx.Done():t.Fatal(ctx.Err())}}
type testEndpoint struct{bitwire.Endpoint; registry runtime.HandlerRegistry}
func pair(t *testing.T)(*testEndpoint,*testEndpoint){
 t.Helper();left,right,err:=runtime.NewWirePair(runtime.Options{});if err!=nil{t.Fatal(err)};t.Cleanup(func(){left.Close(duplex.CodeNormal,"")})
 selectRoot:=func(endpoint bitwire.Endpoint)*testEndpoint{dispatcher,err:=runtime.NewDispatcher(endpoint);if err!=nil{t.Fatal(err)};t.Cleanup(func(){dispatcher.Close(duplex.CodeNormal,"")});return &testEndpoint{Endpoint:dispatcher.Select(nil),registry:dispatcher}}
 return selectRoot(left),selectRoot(right)
}
func identity(t *testing.T)runtime.DeclarationIdentity{t.Helper();digest,err:=protocol.WireSchema().DeclarationDigest();if err!=nil{t.Fatal(err)};return runtime.DeclarationIdentity{Path:"x",Digest:digest}}
func serveIdentity(t *testing.T,w *testEndpoint,id runtime.DeclarationIdentity){t.Helper();handler,err:=runtime.IdentityHandler(id);if err!=nil{t.Fatal(err)};off,err:=runtime.HandleWire(w.registry,[]string{runtime.IdentityMethod},func(ctx context.Context,raw json.RawMessage)(any,error){return handler(ctx,nil,raw)});if err!=nil{t.Fatal(err)};t.Cleanup(off)}
func keepCarrier(t *testing.T,left,right *testEndpoint)func(){
 t.Helper();off,err:=runtime.HandleWire(left.registry,[]string{"unrelated"},func(context.Context,json.RawMessage)(any,error){return "alive",nil});if err!=nil{t.Fatal(err)};t.Cleanup(off)
 return func(){var result string;if err:=runtime.CallWire(testContext(t),right,[]string{"unrelated"},nil,&result);err!=nil||result!="alive"{t.Fatalf("shared carrier: %q %v",result,err)}}
}
func assertDetached(t *testing.T,w bitwire.Endpoint){t.Helper();dispatcher,err:=runtime.NewDispatcher(w);if err!=nil{t.Fatalf("interpretation attachment retained: %v",err)};defer dispatcher.Close(duplex.CodeNormal,"");for _,name:=range []string{runtime.IdentityMethod,"back","changed"}{off,err:=runtime.HandleWire(dispatcher,[]string{name},func(context.Context,json.RawMessage)(any,error){return nil,nil});if err!=nil{t.Fatalf("%s receiver retained: %v",name,err)};off()}}
func TestEarlyEventAndLocalIdentityProgress(t *testing.T){
 ctx:=testContext(t);var calls atomic.Int64;var remote protocol.Client
 wire,err:=binding.ToWire(func(value protocol.Client)(protocol.Server,error){remote=value;return protocol.Server{Methods:forward{&calls}},nil},runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 tracked:=&trackedWire{Endpoint:wire,eventEntered:make(chan struct{})}
 complete,cleanup,err:=binding.PrepareFromWire(tracked,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer cleanup()
 if err=remote.Events.Changed(ctx,7);err!=nil{t.Fatal(err)};wait(t,ctx,tracked.eventEntered)
 model,err:=complete(ctx);if err!=nil{t.Fatalf("identity while early event is held: %v",err)}
 values:=make(chan int64,1);access,err:=model(protocol.Client{Methods:backward{},Events:events{values}});if err!=nil{t.Fatal(err)}
 select{case value:=<-values:if value!=7{t.Fatal(value)};case <-ctx.Done():t.Fatal(ctx.Err())}
 if value,err:=access.Methods.Run(ctx,4);err!=nil||value!=5||calls.Load()!=1{t.Fatalf("application: %d %v calls=%d",value,err,calls.Load())}
 if value,err:=remote.Methods.Back(ctx,4);err!=nil||value!=6{t.Fatalf("reverse: %d %v",value,err)}
 if _,err=complete(ctx);err==nil{t.Fatal("completion repeated")}
 if _,err=model(protocol.Client{Methods:backward{}});err==nil{t.Fatal("factory rebound")}
 if tracked.registrations.Load()!=1{t.Fatalf("model interpretation attached %d receivers",tracked.registrations.Load())};cleanup();assertDetached(t,wire)
}
func TestBothInterpretationsAdvertiseBeforeCheck(t *testing.T){
 ctx:=testContext(t);left,right:=pair(t)
 finishServer,closeServer,err:=binding.PrepareFromWire(left,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer closeServer()
 finishClient,closeClient,err:=client.PrepareFromWire(right,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer closeClient()
 serverModel,err:=finishServer(ctx);if err!=nil{t.Fatal(err)}
 clientModel,err:=finishClient(ctx);if err!=nil{t.Fatal(err)}
 serverAccess,err:=serverModel(protocol.Client{Methods:backward{}});if err!=nil{t.Fatal(err)}
 var calls atomic.Int64;clientAccess,err:=clientModel(protocol.Server{Methods:forward{&calls}});if err!=nil{t.Fatal(err)}
 if n,err:=serverAccess.Methods.Run(ctx,2);err!=nil||n!=3{t.Fatalf("server: %d %v",n,err)}
 if n,err:=clientAccess.Methods.Back(ctx,2);err!=nil||n!=4{t.Fatalf("client: %d %v",n,err)}
}
func TestRefusalAndAbsentIdentity(t *testing.T){
 for _,mode:=range []string{"mismatch","absent"}{t.Run(mode,func(t *testing.T){
  ctx:=testContext(t);left,right:=pair(t);checkCarrier:=keepCarrier(t,left,right);var calls atomic.Int64
  off,err:=runtime.HandleWire(right.registry,[]string{"run"},func(context.Context,json.RawMessage)(any,error){calls.Add(1);return int64(3),nil});if err!=nil{t.Fatal(err)};defer off()
  if mode=="mismatch"{id:=identity(t);id.Digest=strings.Repeat("0",64);serveIdentity(t,right,id)}
  complete,cleanup,err:=binding.PrepareFromWire(left,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer cleanup()
  model,err:=complete(ctx)
  if mode=="mismatch"{var public *runtime.PublicError;if !errors.As(err,&public)||public.Code!="contract_mismatch"||public.Message!="the declaration identity for x differs"{t.Fatalf("mismatch: %v",err)};if model!=nil||calls.Load()!=0{t.Fatal("mismatch exposed application")};assertDetached(t,left)
  }else{if err!=nil{t.Fatal(err)};access,err:=model(protocol.Client{Methods:backward{}});if err!=nil{t.Fatal(err)};if _,err=access.Methods.Run(ctx,2);err!=nil||calls.Load()!=1{t.Fatalf("absent identity: %v calls=%d",err,calls.Load())}}
  checkCarrier()
 })}
}
func TestCancellationAndNeverCompletedCleanup(t *testing.T){
 t.Run("cancel",func(t *testing.T){
  ctx:=testContext(t);left,right:=pair(t);checkCarrier:=keepCarrier(t,left,right);entered:=make(chan struct{})
  off,err:=runtime.HandleWire(right.registry,[]string{runtime.IdentityMethod},func(ctx context.Context,_ json.RawMessage)(any,error){close(entered);<-ctx.Done();return nil,ctx.Err()});if err!=nil{t.Fatal(err)};defer off()
  complete,cleanup,err:=binding.PrepareFromWire(left,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer cleanup()
  checking,cancel:=context.WithCancel(ctx);defer cancel();done:=make(chan error,1);go func(){_,err:=complete(checking);done<-err}();wait(t,ctx,entered);cancel()
  select{case err:=<-done:if !errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)};case <-ctx.Done():t.Fatal(ctx.Err())}
  assertDetached(t,left);checkCarrier()
 })
 t.Run("never completed",func(t *testing.T){
  ctx:=testContext(t);left,right:=pair(t);checkCarrier:=keepCarrier(t,left,right);tracked:=&trackedWire{Endpoint:left,receiversDetached:make(chan struct{})}
  _,cleanup,err:=binding.PrepareFromWire(tracked,runtime.AdapterContext{Options:runtime.Options{RequestTimeout:30*time.Millisecond}});if err!=nil{t.Fatal(err)};defer cleanup()
  wait(t,ctx,tracked.receiversDetached);assertDetached(t,left);checkCarrier()
 })
 t.Run("missing methods",func(t *testing.T){
  ctx:=testContext(t);left,right:=pair(t);checkCarrier:=keepCarrier(t,left,right);serveIdentity(t,right,identity(t))
  complete,cleanup,err:=binding.PrepareFromWire(left,runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer cleanup();model,err:=complete(ctx);if err!=nil{t.Fatal(err)}
  if _,err=model(protocol.Client{});err==nil{t.Fatal("missing methods accepted")};assertDetached(t,left);checkCarrier()
 })
}
func TestMissingBindingFailsBeforeRegistrationOrModel(t *testing.T){
 left,_:=pair(t);tracked:=&trackedWire{Endpoint:left};var calls atomic.Int64
 if _,_,err:=slotbinding.PrepareFromWire[string](tracked,runtime.AdapterContext{},runtime.ValueAdapter[string]{});err==nil{t.Fatal("missing binding accepted")}
 if tracked.registrations.Load()!=0{t.Fatal("registered before validating binding")}
 if _,err:=slotbinding.ToWire(func(slot.Client[string])(slot.Server[string],error){calls.Add(1);return slot.Server[string]{},nil},runtime.AdapterContext{},runtime.ValueAdapter[string]{});err==nil{t.Fatal("missing ToWire binding accepted")}
 if calls.Load()!=0{t.Fatal("model invoked before validating binding")}
}
`
