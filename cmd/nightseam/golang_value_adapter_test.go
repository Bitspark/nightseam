package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedGoValueAdapters(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go")
	directory := t.TempDir()
	for _, family := range []string{"boxes", "combinator"} {
		copyFixtureTree(t, filepath.Join(root, "cmd/nightseam/testdata/families/api/contracts", family), filepath.Join(directory, "api/contracts", family))
	}
	writeFixture(t, directory, "api/contracts/cell/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/cell/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"types":{"PutParams":{"kind":"record","fields":[{"name":"value","type":"T"}]}},"server":{"methods":{"put":{"request":"PutParams","result":"integer"},"get":{"result":"T"}},"events":{"changed":{"type":"PutParams"}}},"client":{"methods":{"mirror":{"request":"PutParams","result":"T"}},"events":{"noted":{"type":"PutParams"}}}}`))
	for _, slot := range goBoundAdapterSlots {
		family := "cell" + strings.ToLower(slot.name)
		path := "api/contracts/" + family + "/"
		writeFixture(t, directory, path+"model.json", []byte(`{"nightseam":2}`))
		body := fmt.Sprintf(`"types":{"PutParams":{"kind":"record","fields":[{"name":"value","type":%s}]}},"server":{"methods":{"put":{"request":"PutParams","result":"integer"},"get":{"result":%s}}}`, slot.expression, slot.expression)
		if slot.live {
			writeFixture(t, directory, path+"protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
			writeFixture(t, directory, path+"live.json", []byte(`{"imports":["boxes","combinator"],`+body+`}`))
		} else {
			writeFixture(t, directory, path+"protocol.json", []byte(`{"profile":"nightseam.duplex/1",`+body+`}`))
		}
		program := strings.NewReplacer("FAMILY", family, "NAME", slot.name, "TYPE", slot.goType, "ADAPTER", slot.adapter, "VALUE", slot.value, "OBSERVE", slot.observe, "LIVE", fmt.Sprint(slot.live)).Replace(goBoundAdapterProgram)
		writeFixture(t, directory, family+"_test.go", []byte(program))
	}
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "adapter_test.go", []byte(goValueAdapterProgram))
	runFixture(t, directory, "go", "test", "-p", "2", "-parallel", "4", "-count=1", ".")
}

const goValueAdapterProgram = `package adapters_test
import (
 "context"
 "encoding/json"
 "errors"
 "sync/atomic"
 "testing"
 "time"
 boxes "example.test/generated/api/go/boxes-protocol"
 combinator "example.test/generated/api/go/combinator-protocol"
 client "example.test/generated/api/go/cell-client"
 binding "example.test/generated/api/go/cell-binding"
 cellprotocol "example.test/generated/api/go/cell-protocol"
 "github.com/Bitspark/nightseam/duplex/go"
 "github.com/Bitspark/nightseam/live/go"
 "github.com/Bitspark/nightseam/runtime/go"
)
type cell[T any] struct { value T; puts int64; remote *binding.Remote[T] }
func (c *cell[T]) Put(_ context.Context, remote *binding.Remote[T], params cellprotocol.PutParams[T]) (int64,error) { c.value=params.Value; c.remote=remote; c.puts++; return c.puts,nil }
func (c *cell[T]) Get(_ context.Context, _ *binding.Remote[T]) (T,error) { return c.value,nil }
type reverse[T any] struct{}
func (reverse[T]) Mirror(_ context.Context,_ *client.Client[T],params cellprotocol.PutParams[T])(T,error){return params.Value,nil}
func connect[T any](t *testing.T, adapter live.ValueAdapter[T]) (*client.Client[T],*cell[T],*live.Scope,*live.Scope) {
 t.Helper(); a,b:=duplex.Pipe(1<<20); implementation:=new(cell[T]); var sa,sb *live.Scope
 options:=func(scope **live.Scope) runtime.Options { return runtime.Options{Prepare:func(p *runtime.Peer)(err error){ if adapter.Live { *scope,err=live.Over(p,live.Options{}) }; return }} }
 server,err:=binding.Serve(context.Background(),b,options(&sb),implementation,adapter); if err!=nil {t.Fatal(err)}
 caller,err:=client.Attach(context.Background(),a,options(&sa),reverse[T]{},client.Events[T]{},adapter); if err!=nil {t.Fatal(err)}
 t.Cleanup(func(){_ = caller.Close(); _ = server.Close()}); return caller,implementation,sa,sb
}
func zero(t *testing.T, scopes ...*live.Scope) { t.Helper(); deadline:=time.Now().Add(3*time.Second); for _,s:=range scopes { for s.Counts()!=(live.Counts{}) {if time.Now().After(deadline){t.Fatalf("leak: %+v",s.Counts())};time.Sleep(time.Millisecond)} } }
func round[T any](t *testing.T,adapter live.ValueAdapter[T],value T,observe func(T)) {
 t.Helper(); caller,state,a,b:=connect(t,adapter); ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel()
 n,err:=caller.Put(ctx,cellprotocol.PutParams[T]{Value:value});if err!=nil||n!=1||state.puts!=1 {t.Fatalf("put: %d, %v; state %d",n,err,state.puts)}
 got,err:=caller.Get(ctx);if err!=nil {t.Fatal(err)}; observe(got)
 if adapter.Live {if a.Counts().Exports==0||b.Counts().Imports==0 {t.Fatal("slot did not export/import")}; _=a.Owner().Release();_=b.Owner().Release();zero(t,a,b)} else if a!=nil||b!=nil {t.Fatal("scalar requires live scope")}
}
func TestOneCellAcrossSlots(t *testing.T) {
 round(t,live.JSONAdapter[string](),"held",func(s string){if s!="held"{t.Fatal(s)}})
 var calls atomic.Int64
 unary:=combinator.Unary(func(_ context.Context,n int64)(int64,error){calls.Add(1);return n+3,nil})
 observeUnary:=func(f combinator.Unary){n,err:=f(context.Background(),4);if err!=nil||n!=7 {t.Fatalf("unary: %d %v",n,err)}}
 round(t,combinator.AdapterUnary(),unary,observeUnary)
 factory:=combinator.Factory(func(ctx context.Context,f combinator.Unary)(combinator.Unary,error){calls.Add(1);return func(ctx context.Context,n int64)(int64,error){return f(ctx,n)},nil})
 round(t,combinator.AdapterFactory(),factory,func(f combinator.Factory){g,err:=f(context.Background(),unary);if err!=nil{t.Fatal(err)};observeUnary(g)})
 bundle:=combinator.Bundle[combinator.Unary]{Metadata:combinator.BundleMetadata[combinator.Unary]{Seed:unary},Run:unary}
 adapter:=boxes.AdapterPage(combinator.AdapterBundle(combinator.AdapterUnary()))
 round(t,adapter,boxes.Page[combinator.Bundle[combinator.Unary]]{Items:[]combinator.Bundle[combinator.Unary]{bundle}},func(v boxes.Page[combinator.Bundle[combinator.Unary]]){observeUnary(v.Items[0].Run);observeUnary(v.Items[0].Metadata.Seed)})
 if calls.Load()!=5 {t.Fatalf("callback count %d",calls.Load())}
}
func TestAdapterComposesWithCurrentOwnerAndRollsBack(t *testing.T) {
 a,b:=duplex.Pipe(1<<20); var sa,sb *live.Scope
 pa,err:=runtime.NewPeer(context.Background(),a,runtime.ClientRole,runtime.Options{Prepare:func(p *runtime.Peer)(err error){sa,err=live.Over(p,live.Options{});return}});if err!=nil{t.Fatal(err)};defer pa.Close()
 pb,err:=runtime.NewPeer(context.Background(),b,runtime.ServerRole,runtime.Options{Prepare:func(p *runtime.Peer)(err error){sb,err=live.Over(p,live.Options{MaxImports:1});return}});if err!=nil{t.Fatal(err)};defer pb.Close()
 scalar:=boxes.AdapterPage(live.JSONAdapter[string]()); dead:=sa.Owner().Child();_=dead.Release()
 for _,owner:=range []*live.Owner{nil,dead} {raw,err:=scalar.Export(owner,boxes.Page[string]{Items:[]string{"x"}});if err!=nil{t.Fatal(err)};v,err:=scalar.Import(owner,raw);if err!=nil||v.Items[0]!="x"{t.Fatalf("scalar: %v",err)}}
 calls:=0;unary:=func(_ context.Context,n int64)(int64,error){calls++;return n+1,nil};adapter:=boxes.AdapterPage(combinator.AdapterUnary())
 owner:=sa.Owner().Child();raw,err:=adapter.Export(owner,boxes.Page[combinator.Unary]{Items:[]combinator.Unary{unary,unary}});if err!=nil{t.Fatal(err)}
 if owner.Counts().Exports!=2 {t.Fatal("wrong export owner")}; receiver:=sb.Owner().Child(); if _,err=adapter.Import(receiver,raw);err==nil {t.Fatal("limit accepted")}; if receiver.Counts()!=(live.Counts{})||sb.Counts().Imports!=0 {t.Fatal("partial import leaked")}
 _=owner.Release();zero(t,sa,sb)
 owner=sa.Owner().Child();bad:=boxes.Page[combinator.Unary]{Items:[]combinator.Unary{unary,nil}};if _,err=adapter.Export(owner,bad);err==nil||owner.Counts()!=(live.Counts{}) {t.Fatal("partial export leaked")}
 raw,err=adapter.Export(owner,boxes.Page[combinator.Unary]{Items:[]combinator.Unary{unary}});if err!=nil{t.Fatal(err)};got,err:=adapter.Import(receiver,raw);if err!=nil{t.Fatal(err)};if n,err:=got.Items[0](context.Background(),2);err!=nil||n!=3||calls!=1{t.Fatalf("callback: %d %v %d",n,err,calls)}
 _=owner.Release();if _,err=adapter.Export(owner,boxes.Page[combinator.Unary]{Items:[]combinator.Unary{unary}});err==nil{t.Fatal("released owner reused")};_=receiver.Release();zero(t,sa,sb)
}
func TestGenericPublicationRefusalDoesNotAcquire(t *testing.T) {
 adapter:=boxes.AdapterPage(combinator.AdapterUnary());caller,state,a,b:=connect(t,adapter)
 fn:=func(_ context.Context,n int64)(int64,error){return n,nil};owner:=a.Owner().Child();ctx,cancel:=context.WithCancel(live.WithOwner(context.Background(),owner));cancel()
 _,err:=caller.Put(ctx,cellprotocol.PutParams[boxes.Page[combinator.Unary]]{Value:boxes.Page[combinator.Unary]{Items:[]combinator.Unary{fn}}})
 var proof *runtime.UnpublishedError;if !errors.As(err,&proof) {t.Fatalf("missing unpublished refusal: %v",err)}
 if state.puts!=0 || owner.Counts()!=(live.Counts{}) || a.Counts()!=(live.Counts{}) || b.Counts()!=(live.Counts{}) {t.Fatalf("unpublished request changed state: %d %+v %+v",state.puts,a.Counts(),b.Counts())}
 good:=live.WithOwner(context.Background(),owner);if _,err:=caller.Put(good,cellprotocol.PutParams[boxes.Page[combinator.Unary]]{Value:boxes.Page[combinator.Unary]{Items:[]combinator.Unary{fn}}});err!=nil{t.Fatal(err)}
 if owner.Counts().Exports!=1||a.Owner().Counts().Exports!=0 {t.Fatal("constructor captured a permanent owner")}
 _=owner.Release();_=b.Owner().Release();zero(t,a,b)
}
func TestComposedBindingRejectsInvalidScalarInsideLiveValue(t *testing.T) {
 adapter:=boxes.AdapterPage(live.JSONAdapter[string]());if _,err:=adapter.Import(nil,json.RawMessage("{\"items\":[42]}"));err==nil{t.Fatal("nested scalar constraint lost")}
}
func TestSlotAdaptersReachReverseCallsAndEvents(t *testing.T) {
 caller,state,a,b:=connect(t,combinator.AdapterUnary());ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();var calls atomic.Int64
 fn:=func(_ context.Context,n int64)(int64,error){calls.Add(1);return n+1,nil}
 if _,err:=caller.Put(ctx,cellprotocol.PutParams[combinator.Unary]{Value:fn});err!=nil{t.Fatal(err)}
 got,err:=state.remote.Mirror(ctx,cellprotocol.PutParams[combinator.Unary]{Value:fn});if err!=nil{t.Fatal(err)};if n,err:=got(ctx,4);err!=nil||n!=5{t.Fatalf("reverse: %d %v",n,err)}
 changed:=make(chan combinator.Unary,1);noted:=make(chan combinator.Unary,1)
 if err:=caller.OnChanged(func(_ context.Context,p cellprotocol.PutParams[combinator.Unary]){changed<-p.Value});err!=nil{t.Fatal(err)}
 if err:=state.remote.OnNoted(func(_ context.Context,p cellprotocol.PutParams[combinator.Unary]){noted<-p.Value});err!=nil{t.Fatal(err)}
 if err:=state.remote.EmitChanged(ctx,cellprotocol.PutParams[combinator.Unary]{Value:fn});err!=nil{t.Fatal(err)}
 if err:=caller.EmitNoted(ctx,cellprotocol.PutParams[combinator.Unary]{Value:fn});err!=nil{t.Fatal(err)}
 for _,event:=range []chan combinator.Unary{changed,noted}{select{case fn:=<-event:if n,err:=fn(ctx,5);err!=nil||n!=6{t.Fatalf("event: %d %v",n,err)};case <-ctx.Done():t.Fatal(ctx.Err())}}
 if calls.Load()!=3{t.Fatalf("callback count %d",calls.Load())};_=a.Owner().Release();_=b.Owner().Release();zero(t,a,b)
}
`

var goBoundAdapterSlots = []struct {
	name, expression, goType, adapter, value, observe string
	live                                              bool
}{
	{"String", `"string"`, "string", "live.JSONAdapter[string]()", `"held"`, `return value`, false},
	{"Unary", `"combinator.Unary"`, "combinator.Unary", "combinator.AdapterUnary()", "unary", `n,err:=value(ctx,4);if err!=nil{t.Fatal(err)};return fmt.Sprint(n)`, true},
	{"Factory", `"combinator.Factory"`, "combinator.Factory", "combinator.AdapterFactory()", "factory", `fn,err:=value(ctx,unary);if err!=nil{t.Fatal(err)};n,err:=fn(ctx,4);if err!=nil{t.Fatal(err)};return fmt.Sprint(n)`, true},
	{"Nested", `{"apply":"boxes.Page","with":{"T":{"apply":"combinator.Bundle","with":{"T":"combinator.Unary"}}}}`, "boxes.Page[combinator.Bundle[combinator.Unary]]", "boxes.AdapterPage(combinator.AdapterBundle(combinator.AdapterUnary()))", "nested", `a,err:=value.Items[0].Run(ctx,4);if err!=nil{t.Fatal(err)};b,err:=value.Items[0].Metadata.Seed(ctx,5);if err!=nil{t.Fatal(err)};return fmt.Sprint(a, "/", b)`, true},
}

// Each fixture is generated from a fully substituted declaration, separately
// from Cell<T>. Its binding and its implementation never call the generic path.
const goBoundAdapterProgram = `package adapters_test
import (
 "context"
 "fmt"
 "sync/atomic"
 "testing"
 "time"
 boxes "example.test/generated/api/go/boxes-protocol"
 combinator "example.test/generated/api/go/combinator-protocol"
 concretebinding "example.test/generated/api/go/FAMILY-binding"
 concreteclient "example.test/generated/api/go/FAMILY-client"
 concreteprotocol "example.test/generated/api/go/FAMILY-protocol"
 cellprotocol "example.test/generated/api/go/cell-protocol"
 "github.com/Bitspark/nightseam/duplex/go"
 "github.com/Bitspark/nightseam/live/go"
 "github.com/Bitspark/nightseam/runtime/go"
)
type sourceCellNAME struct { value TYPE; puts int64 }
func (c *sourceCellNAME) Put(_ context.Context,_ *concretebinding.Remote,params concreteprotocol.PutParams)(int64,error){c.value=params.Value;c.puts++;return c.puts,nil}
func (c *sourceCellNAME) Get(_ context.Context,_ *concretebinding.Remote)(TYPE,error){return c.value,nil}
func TestIndependentConstructionNAME(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 var calls atomic.Int64
 unary:=combinator.Unary(func(_ context.Context,n int64)(int64,error){calls.Add(1);return n+3,nil})
 factory:=combinator.Factory(func(ctx context.Context,fn combinator.Unary)(combinator.Unary,error){calls.Add(1);return func(ctx context.Context,n int64)(int64,error){return fn(ctx,n)},nil})
 nested:=boxes.Page[combinator.Bundle[combinator.Unary]]{Items:[]combinator.Bundle[combinator.Unary]{{Metadata:combinator.BundleMetadata[combinator.Unary]{Seed:unary},Run:unary}}}
 _,_=factory,nested
 observe:=func(value TYPE) string { OBSERVE }
 generic,genericState,ga,gb:=connect(t,ADAPTER)
 n,err:=generic.Put(ctx,cellprotocol.PutParams[TYPE]{Value:VALUE});if err!=nil{t.Fatal(err)};gv,err:=generic.Get(ctx);if err!=nil{t.Fatal(err)};gObservation:=fmt.Sprint(n,"/",genericState.puts,"/",observe(gv));gCalls:=calls.Load()
 a,b:=duplex.Pipe(1<<20);source:=new(sourceCellNAME);var sa,sb *live.Scope
 options:=func(scope **live.Scope)runtime.Options{return runtime.Options{Prepare:func(p *runtime.Peer)(err error){if LIVE{*scope,err=live.Over(p,live.Options{})};return}}}
 server,err:=concretebinding.Serve(ctx,b,options(&sb),source);if err!=nil{t.Fatal(err)};defer server.Close()
 plain,err:=concreteclient.Attach(ctx,a,options(&sa),nil,concreteclient.Events{});if err!=nil{t.Fatal(err)};defer plain.Close()
 n,err=plain.Put(ctx,concreteprotocol.PutParams{Value:VALUE});if err!=nil{t.Fatal(err)};pv,err:=plain.Get(ctx);if err!=nil{t.Fatal(err)};pObservation:=fmt.Sprint(n,"/",source.puts,"/",observe(pv));pCalls:=calls.Load()-gCalls
 if gObservation!=pObservation||gCalls!=pCalls{t.Fatalf("paths differ: generic %s/%d, source %s/%d",gObservation,gCalls,pObservation,pCalls)}
 if LIVE {if ga.Counts()!=sa.Counts()||gb.Counts()!=sb.Counts(){t.Fatalf("counts differ: %+v/%+v %+v/%+v",ga.Counts(),gb.Counts(),sa.Counts(),sb.Counts())};_=ga.Owner().Release();_=gb.Owner().Release();_=sa.Owner().Release();_=sb.Owner().Release();zero(t,ga,gb,sa,sb)}
}
`
