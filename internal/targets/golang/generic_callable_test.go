package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func genericCallableWorld() analysis.World {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"function": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json": `{"types":{
				"Function":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"},
				"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"call","type":{"apply":"Function","with":{"A":"T","B":"T"}}}]},
				"ValueBox":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"},{"name":"call","type":{"apply":"Function","with":{"A":"T","B":"T"}}}]},
				"Same":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"},
				"Transform":{"kind":"callable","parameters":[{"name":"T"}],"request":{"apply":"Function","with":{"A":"T","B":"T"}},"result":{"apply":"Function","with":{"A":"T","B":"T"}}},
				"IntFunction":{"kind":"alias","type":{"apply":"Function","with":{"A":"integer","B":"integer"}}},
				"Mapped":{"kind":"alias","parameters":[{"name":"T"}],"type":{"apply":"Function","with":{"A":"T","B":"T"}}},
				"Phantom":{"kind":"callable","parameters":[{"name":"T"}],"request":"integer","result":"integer"},
				"Notify":{"kind":"callable","parameters":[{"name":"T"}],"request":"T"},
				"Create":{"kind":"callable","parameters":[{"name":"T"}],"result":"T"},
				"Invoke":{"kind":"callable","request":{"apply":"Function","with":{"A":"integer","B":"integer"}},"result":"integer"}
			}}`,
		},
		"source": {
			"model.json":    `{"nightseam":2,"types":{"Job":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`,
			"protocol.json": modeltest.Protocol(``),
		},
		"captured": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}]`),
			"live.json":     `{"types":{"Handler":{"kind":"callable","parameters":[{"name":"T"}],"request":"S.Job","result":"T"}}}`,
		},
		"consumer": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"imports":["function"],"parameters":[{"name":"T"}],"server":{"methods":{"exchange":{"request":{"apply":"function.Function","with":{"A":{"array":"T"},"B":"T"}},"result":{"apply":"function.Function","with":{"A":{"array":"T"},"B":"T"}}}}}`),
		},
	}))
	for name, family := range builtin.Families() {
		world[name] = family
	}
	return world
}

func TestGeneratedGenericCallables(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and invokes generated closed callables")
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
	write("go.mod", []byte(fmt.Sprintf("module example.test/callable\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root))))
	world := genericCallableWorld()
	for name := range world {
		files, err := New(Config{Module: "example.test/callable"}).Render(render.Build(analysis.Resolve(world, name)))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
	}
	write("callable_test.go", []byte(genericCallableProgram))
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-parallel", "4", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated generic callables: %v\n%s", err, output)
	}
}

func TestGenericCallableArgumentRecipesDoNotCaptureClient(t *testing.T) {
	files, err := New(Config{Module: "example.test/callable"}).Render(render.Build(analysis.Resolve(genericCallableWorld(), "consumer")))
	if err != nil {
		t.Fatal(err)
	}
	recipes := 0
	for _, file := range files {
		if !strings.HasSuffix(file.Path, ".go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), file.Path, file.Data, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			indexed, ok := literal.Type.(*ast.IndexExpr)
			if !ok {
				return true
			}
			name, ok := indexed.X.(*ast.SelectorExpr)
			if !ok || name.Sel.Name != "ValueAdapter" {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				body, ok := field.Value.(*ast.FuncLit)
				if !ok {
					continue
				}
				recipes++
				ast.Inspect(body.Body, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "c" {
						t.Errorf("%s: neutral recipe captures client through c.%s", file.Path, selector.Sel.Name)
					}
					return true
				})
			}
			return true
		})
	}
	if recipes == 0 {
		t.Fatal("no nested argument recipes were generated")
	}
}

const genericCallableProgram = `package callable_test
import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 protocol "example.test/callable/api/go/function-protocol"
 captured "example.test/callable/api/go/captured-protocol"
 source "example.test/callable/api/go/source-protocol"

 "github.com/Bitspark/nightseam/live/go"
 "github.com/Bitspark/nightseam/runtime/go"
)
type markKey struct{}
func pair(t *testing.T)(*live.Scope,*live.Scope){
 t.Helper();ctx,cancel:=context.WithCancel(t.Context());a,b:=duplex.Pipe(8<<20);var sa,sb *live.Scope
 pa,err:=runtime.NewPeer(ctx,a,runtime.ClientRole,runtime.Options{Prepare:func(p *runtime.Peer)(err error){sa,err=live.Over(p,live.Options{});return err}});if err!=nil{t.Fatal(err)}
 pb,err:=runtime.NewPeer(ctx,b,runtime.ServerRole,runtime.Options{Prepare:func(p *runtime.Peer)(err error){sb,err=live.Over(p,live.Options{});return err}});if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){pa.Close();pb.Close();cancel()});return sa,sb
}
func guarded(_ context.Context,n int64)(int64,error){if n<0{return 0,&runtime.PublicError{Code:"denied",Message:"guarded"}};return n+1,nil}
func TestGENIDApplicationsAndNativeBoundaries(t *testing.T){
 integer,text:=runtime.JSONAdapter[int64](),runtime.JSONAdapter[string]()
 identity,err:=protocol.ContractFunction(integer,text);if err!=nil{t.Fatal(err)}
 if identity.Path!="function/Function<integer,string>"||identity.Digest==""{t.Fatalf("closed identity: %+v",identity)}
 reverse,err:=protocol.ContractFunction(text,integer);if err!=nil{t.Fatal(err)};if reverse==identity{t.Fatal("argument order collapsed")}
 other,err:=protocol.ContractSame(integer,text);if err!=nil{t.Fatal(err)};if other==identity{t.Fatal("distinct callable declarations collapsed")}
 alias,err:=runtime.CallableIdentity(protocol.AdapterIntFunction().Binding);if err!=nil{t.Fatal(err)}
 applied,err:=protocol.ContractFunction(integer,integer);if err!=nil{t.Fatal(err)};if alias!=applied{t.Fatalf("alias identity: %+v != %+v",alias,applied)}
 mapped,err:=runtime.CallableIdentity(protocol.AdapterMapped(integer).Binding);if err!=nil||mapped!=applied{t.Fatalf("generic alias identity: %+v %v",mapped,err)}
 capture,err:=captured.ContractHandler(source.AdapterJob(),integer);if err!=nil{t.Fatal(err)};if capture.Path!="captured/Handler<source,integer>"{t.Fatalf("captured order: %+v",capture)}
 left,_:=pair(t);owner:=left.Owner().Child();defer owner.Release()
 for _,missing:=range []string{"export","import"}{broken:=integer;if missing=="export"{broken.Export=nil}else{broken.Import=nil};if _,err:=protocol.ExportFunction(owner,guarded,broken,integer);err==nil||!strings.Contains(err.Error(),"recipes"){t.Fatalf("missing %s recipe: %v",missing,err)};if owner.Counts()!=(live.Counts{}){t.Fatal("missing recipe acquired a binding")}}
 open:=integer;open.Binding=runtime.TypeBinding{};if _,err:=protocol.ExportFunction(owner,guarded,open,integer);err==nil{t.Fatal("unbound callable argument acquired a binding")};if owner.Counts()!=(live.Counts{}){t.Fatal("unbound application leaked a binding")}
 wrongDraw:=source.AdapterJob();wrongDraw.Binding.Type="Envelope";if _,err:=captured.ExportHandler(owner,func(_ context.Context,job source.Job)(int64,error){return job.Value,nil},wrongDraw,integer);err==nil{t.Fatal("incoherent family member acquired a binding")};if owner.Counts()!=(live.Counts{}){t.Fatal("incoherent family leaked a binding")}
 if _,err:=protocol.ExportFunction[int64,int64](owner,nil,integer,integer);err==nil{t.Fatal("nil callable accepted")}
 raw,err:=protocol.ExportFunction(owner,guarded,integer,integer);if err!=nil{t.Fatal(err)}
 if _,err=protocol.ImportFunction(owner,raw,text,integer);err==nil{t.Fatal("wrong application imported")}
 aliasValue,err:=protocol.ImportIntFunction(owner,raw);if err!=nil{t.Fatal(err)}
 if got,err:=aliasValue(t.Context(),4);err!=nil||got!=5{t.Fatalf("alias specialization: %d %v",got,err)}
}
func TestGENBINDRetainedInvocationUsesCurrentContext(t *testing.T){
 integer:=runtime.JSONAdapter[int64]();encode,decode:=integer.Export,integer.Import;calls:=0
 record:=func(ctx context.Context)error{calls++;if mark,_:=ctx.Value(markKey{}).(string);mark!="active"{return errors.New("retained recipe used supplying context")};if owner,ok:=live.OwnerOf(ctx);!ok||owner==nil{return errors.New("recipe has no active owner")};return nil}
 integer.NeedsContext=true
 integer.Export=func(ctx context.Context,value int64)(json.RawMessage,error){if err:=record(ctx);err!=nil{return nil,err};return encode(ctx,value)}
 integer.Import=func(ctx context.Context,raw json.RawMessage)(int64,error){if err:=record(ctx);err!=nil{return 0,err};return decode(ctx,raw)}
 box:=protocol.AdapterBox(integer);transform:=protocol.AdapterTransform(integer)
 for range 2 {scope,_:=pair(t);owner:=scope.Owner().Child();defer owner.Release();supplying:=live.WithOwner(context.WithValue(t.Context(),markKey{},"supplying"),owner)
  raw,err:=box.Export(supplying,protocol.Box[int64]{Call:guarded});if err!=nil{t.Fatal(err)}
  held,err:=box.Import(supplying,raw);if err!=nil{t.Fatal(err)}
  active:=live.WithOwner(context.WithValue(t.Context(),markKey{},"active"),owner)
  values:=protocol.AdapterValueBox(integer);data,err:=values.Export(active,protocol.ValueBox[int64]{Value:7,Call:guarded});if err!=nil{t.Fatal(err)};if decoded,err:=values.Import(active,data);err!=nil||decoded.Value!=7{t.Fatalf("adapter lost caller context: %+v %v",decoded,err)}
  if got,err:=held.Call(active,9);err!=nil||got!=10{t.Fatalf("retained boxed callable: %d %v",got,err)}
  change:=func(_ context.Context,fn protocol.Function[int64,int64])(protocol.Function[int64,int64],error){return fn,nil}
  raw,err=transform.Export(supplying,change);if err!=nil{t.Fatal(err)};proxy,err:=transform.Import(supplying,raw);if err!=nil{t.Fatal(err)}
  returned,err:=proxy(active,held.Call);if err!=nil{t.Fatal(err)}
  if got,err:=returned(active,11);err!=nil||got!=12{t.Fatalf("nested returned callable: %d %v",got,err)}
  var public *runtime.PublicError;if _,err:=returned(active,-1);!errors.As(err,&public)||public.Code!="denied"{t.Fatalf("roundtrip guard lost: %v",err)}
  if err:=owner.Release();err!=nil{t.Fatal(err)};if scope.Counts()!=(live.Counts{}){t.Fatalf("bindings survive before peer teardown: %+v",scope.Counts())};if _,err:=returned(active,1);err==nil{t.Fatal("released returned callable remained usable")}
 }
 if calls==0{t.Fatal("no invocation recipe ran")}
}
func TestGENBINDGenericCallableCrossesPeer(t *testing.T){
 left,right:=pair(t);sender,receiver:=left.Owner().Child(),right.Owner().Child();defer sender.Release();defer receiver.Release()
 integer:=runtime.JSONAdapter[int64]();call:=protocol.AdapterFunction(integer,integer)
 raw,err:=call.Export(live.WithOwner(t.Context(),sender),guarded);if err!=nil{t.Fatal(err)}
 proxy,err:=call.Import(live.WithOwner(t.Context(),receiver),raw);if err!=nil{t.Fatal(err)}
 if got,err:=proxy(live.WithOwner(t.Context(),receiver),20);err!=nil||got!=21{t.Fatalf("remote generic callable: %d %v",got,err)}
}
`

func TestGenericCallableCompleteInterpretations(t *testing.T) {
	files, err := New(Config{Module: "example.test/callable"}).Render(render.Build(analysis.Resolve(genericCallableWorld(), "function")))
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if strings.HasSuffix(file.Path, "types_generated.go") {
			source = string(file.Data)
		}
	}
	for _, want := range []string{
		"type Function[A, B any] = func(ctx context.Context, params A) (B, error)",
		"func ContractFunction[A, B any](adapterA runtime.ValueAdapter[A], adapterB runtime.ValueAdapter[B]) (runtime.DeclarationIdentity, error)",
		"func ExportFunction[A, B any](owner *live.Owner, v Function[A, B], adapterA runtime.ValueAdapter[A], adapterB runtime.ValueAdapter[B])",
		"func ImportFunction[A, B any](owner *live.Owner, raw json.RawMessage, adapterA runtime.ValueAdapter[A], adapterB runtime.ValueAdapter[B])",
		"runtime.CallableIdentity(binding)",
		"func ContractIntFunction() (runtime.DeclarationIdentity, error)",
		"adapterA.Import(ctx, request)",
		"adapterB.Export(ctx, result)",
		"const ContractInvoke = \"function/Invoke\"",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("generic callable is missing %q", want)
		}
	}
	tree, err := parser.ParseFile(token.NewFileSet(), "types_generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range tree.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || (function.Name.Name != "ExportIntFunction" && function.Name.Name != "ImportIntFunction" && function.Name.Name != "ExportMapped" && function.Name.Name != "ImportMapped") {
			continue
		}
		acquisition := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "Export" || selector.Sel.Name == "Import") {
				acquisition = true
			}
			ast.Inspect(call.Fun, func(node ast.Node) bool {
				if name, ok := node.(*ast.Ident); ok && (name.Name == "ExportFunction" || name.Name == "ImportFunction" || name.Name == "AdapterFunction") {
					t.Errorf("%s delegates to generic callable %s", function.Name.Name, name.Name)
				}
				return true
			})
			return true
		})
		if !acquisition {
			t.Errorf("%s has no independently specialized callable boundary", function.Name.Name)
		}
	}
}
