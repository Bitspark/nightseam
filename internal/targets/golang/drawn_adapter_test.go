package golang

import (
	"context"
	"fmt"
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

func drawnAdapterWorld() analysis.World {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"source": {
			"model.json": `{"nightseam":2,"types":{
				"Job":{"kind":"record","fields":[{"name":"label","type":"string"}]},
				"Progress":{"kind":"record","fields":[{"name":"count","type":"integer"}]}
			}}`,
			"protocol.json": modeltest.Protocol(``),
		},
		"other": {
			"model.json": `{"nightseam":2,"types":{
				"Job":{"kind":"record","fields":[{"name":"label","type":"string"}]},
				"Progress":{"kind":"record","fields":[{"name":"count","type":"integer"}]}
			}}`,
			"protocol.json": modeltest.Protocol(``),
		},
		"holder": {
			"model.json": `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}],
				"types":{"Holder":{"kind":"record","fields":[
					{"name":"job","type":"S.Job"},
					{"name":"progress","type":"S.Progress"},
					{"name":"jobs","type":{"array":"S.Job"}}
				]}},
				"server":{"methods":{"hold":{"request":"Holder","result":"Holder"},"job":{"request":"S.Job","result":"S.Job"}}}`),
		},
	}))
	for name, family := range builtin.Families() {
		world[name] = family
	}
	return world
}

func TestDrawnOperationAdaptersStayNeutral(t *testing.T) {
	files, err := New(Config{Module: "example.test/drawn"}).Render(render.Build(analysis.Resolve(drawnAdapterWorld(), "holder")))
	if err != nil {
		t.Fatal(err)
	}
	var binding string
	for _, file := range files {
		source := string(file.Data)
		for _, unwanted := range []string{"nightseam/live/go", "source-protocol", "other-protocol"} {
			if strings.Contains(source, unwanted) {
				t.Errorf("%s depends on %s", file.Path, unwanted)
			}
		}
		if strings.HasSuffix(file.Path, "binding_generated.go") {
			binding = source
		}
	}
	for _, want := range []string{
		"func ToWire[SJob runtime.Of[STag], SProgress runtime.Of[STag], STag any]",
		"adapterSJob runtime.ValueAdapter[SJob], adapterSProgress runtime.ValueAdapter[SProgress]",
		"adapterSJob.NeedsContext || adapterSProgress.NeedsContext",
		"c.adapterSJob.Export(ctx, params)",
		"c.adapterSJob.Import(ctx, raw)",
		`"S.Job": adapterSJob.Binding, "S.Progress": adapterSProgress.Binding`,
	} {
		if !strings.Contains(binding, want) {
			t.Errorf("drawn binding is missing %q", want)
		}
	}
}

func TestGeneratedDrawnAdaptersInterpretBeforeConversion(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and exercises generated associated-type adapters")
	}
	world := drawnAdapterWorld()
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
	write("go.mod", []byte(fmt.Sprintf("module example.test/drawn\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root))))
	target := New(Config{Module: "example.test/drawn"})
	for name := range world {
		files, err := target.Render(render.Build(analysis.Resolve(world, name)))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
	}
	write("drawn_test.go", []byte(drawnAdapterGoProgram))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-parallel", "4", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated drawn adapters: %v\n%s", err, output)
	}
}

const drawnAdapterGoProgram = `package drawn_test
import (
 "context"
 "encoding/json"
 "fmt"
 "strings"
 "sync"
 "testing"
 binding "example.test/drawn/api/go/holder-binding"
 protocol "example.test/drawn/api/go/holder-protocol"
 other "example.test/drawn/api/go/other-protocol"
 source "example.test/drawn/api/go/source-protocol"

 "github.com/Bitspark/nightseam/runtime/go"
)
type value = protocol.Holder[source.Job,source.Progress]
type methods struct{}
func(methods) Hold(_ context.Context,v value)(value,error){return v,nil}
func(methods) Job(_ context.Context,v source.Job)(source.Job,error){return v,nil}
func model(protocol.Client[source.Job,source.Progress])(protocol.Server[source.Job,source.Progress],error){return protocol.Server[source.Job,source.Progress]{Methods:methods{}},nil}
func specimen()value{return value{Job:source.Job{Label:"first"},Progress:source.Progress{Count:7},Jobs:[]source.Job{{Label:"second"}}}}
type contextKey struct{}
type environment struct{id string}
func(e environment)Select(ctx context.Context)(context.Context,error){return context.WithValue(ctx,contextKey{},e.id),nil}
func(e environment)Child(ctx context.Context)(context.Context,error){return context.WithValue(ctx,contextKey{},e.id),nil}
func(e environment)Export(ctx context.Context,build func(context.Context)(json.RawMessage,error))(json.RawMessage,error){return build(ctx)}
func(e environment)Import(ctx context.Context,build func(context.Context)error)error{return build(ctx)}
func(e environment)Publish(ctx context.Context,build func(context.Context)(json.RawMessage,error),send func(json.RawMessage)(json.RawMessage,error))(json.RawMessage,error){raw,err:=build(ctx);if err!=nil{return nil,err};return send(raw)}
func TestGENBINDActiveContextAndNestedDraws(t *testing.T){
 var mu sync.Mutex;seen:=map[string]int{}
 record:=func(ctx context.Context)error{mark,_:=ctx.Value(contextKey{}).(string);if mark==""{return fmt.Errorf("missing active conversion context")};mu.Lock();seen[mark]++;mu.Unlock();return nil}
 job,progress:=source.AdapterJob(),source.AdapterProgress();job.NeedsContext=true;progress.NeedsContext=true
 jobExport,jobImport,progressExport,progressImport:=job.Export,job.Import,progress.Export,progress.Import
 job.Export=func(ctx context.Context,v source.Job)(json.RawMessage,error){if err:=record(ctx);err!=nil{return nil,err};return jobExport(ctx,v)}
 job.Import=func(ctx context.Context,raw json.RawMessage)(source.Job,error){if err:=record(ctx);err!=nil{return source.Job{},err};return jobImport(ctx,raw)}
 progress.Export=func(ctx context.Context,v source.Progress)(json.RawMessage,error){if err:=record(ctx);err!=nil{return nil,err};return progressExport(ctx,v)}
 progress.Import=func(ctx context.Context,raw json.RawMessage)(source.Progress,error){if err:=record(ctx);err!=nil{return source.Progress{},err};return progressImport(ctx,raw)}
 adapter:=protocol.AdapterHolder(job,progress)
 if !adapter.NeedsContext{t.Fatal("nested draw lost its context requirement")}
 for _,name:=range []string{"first","second"}{t.Run(name,func(t *testing.T){t.Parallel()
  ctx:=context.Background();server,err:=binding.ToWire(model,runtime.AdapterContext{ValueEnvironment:environment{name+"-server"}},job,progress);if err!=nil{t.Fatal(err)};defer server.Close(duplex.CodeNormal,"")
  factory,err:=binding.FromWire(ctx,server,runtime.AdapterContext{ValueEnvironment:environment{name+"-client"}},job,progress);if err!=nil{t.Fatal(err)}
  access,err:=factory(protocol.Client[source.Job,source.Progress]{});if err!=nil{t.Fatal(err)}
  got,err:=access.Methods.Hold(ctx,specimen());if err!=nil||got.Job.Label!="first"||got.Progress.Count!=7||got.Jobs[0].Label!="second"{t.Fatalf("nested draw: %+v %v",got,err)}
  direct,err:=access.Methods.Job(ctx,source.Job{Label:"direct"});if err!=nil||direct.Label!="direct"{t.Fatalf("direct draw: %+v %v",direct,err)}
  active,_:=environment{name+"-standalone"}.Select(ctx);raw,err:=adapter.Export(active,specimen());if err!=nil{t.Fatal(err)};if _,err=adapter.Import(active,raw);err!=nil{t.Fatal(err)}
  mu.Lock();defer mu.Unlock();for _,suffix:=range []string{"-server","-client","-standalone"}{if seen[name+suffix]==0{t.Errorf("unused context %s",name+suffix)}}
 })}
}
func TestGENBINDMissingRecipesFailBeforeModel(t *testing.T){
 for _,which:=range []string{"export","import"}{t.Run(which,func(t *testing.T){
  job,progress:=source.AdapterJob(),source.AdapterProgress();if which=="export"{job.Export=nil}else{job.Import=nil}
  models:=0;_,err:=binding.ToWire(func(remote protocol.Client[source.Job,source.Progress])(protocol.Server[source.Job,source.Progress],error){models++;return model(remote)},runtime.AdapterContext{},job,progress)
  if err==nil||!strings.Contains(err.Error(),"conversion")||models!=0{t.Fatalf("missing recipe: %v, models=%d",err,models)}
  composed:=protocol.AdapterHolder(job,progress);if _,err=composed.Export(context.Background(),specimen());err==nil{t.Fatal("missing export interpretation accepted")};if _,err=composed.Import(context.Background(),json.RawMessage("{}"));err==nil{t.Fatal("missing import interpretation accepted")}
 })}
}
func TestGENBINDMixedInterpretationFailsBeforeRecipes(t *testing.T){
 for _,wrong:=range []runtime.TypeBinding{{Schema:other.WireSchema(),Type:"Progress"},{Schema:source.WireSchema(),Type:"Job"}}{
  job,progress:=source.AdapterJob(),source.AdapterProgress();progress.Binding=wrong;calls:=0
  job.Export=func(context.Context,source.Job)(json.RawMessage,error){calls++;return nil,fmt.Errorf("recipe reached")}
  job.Import=func(context.Context,json.RawMessage)(source.Job,error){calls++;return source.Job{},fmt.Errorf("recipe reached")}
  models:=0;_,err:=binding.ToWire(func(remote protocol.Client[source.Job,source.Progress])(protocol.Server[source.Job,source.Progress],error){models++;return model(remote)},runtime.AdapterContext{},job,progress)
  if err==nil||models!=0{t.Fatalf("mixed family interpretation: %v, models=%d",err,models)}
  composed:=protocol.AdapterHolder(job,progress);if _,err=composed.Export(context.Background(),specimen());err==nil{t.Fatal("mixed export accepted")};if _,err=composed.Import(context.Background(),json.RawMessage("{}"));err==nil{t.Fatal("mixed import accepted")};if calls!=0{t.Fatalf("incoherent interpretation ran %d recipes",calls)}
 }
}
func TestGENBINDDataNeedsNoEnvironment(t *testing.T){
 job,progress:=source.AdapterJob(),source.AdapterProgress();wire,err:=binding.ToWire(model,runtime.AdapterContext{},job,progress);if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 factory,err:=binding.FromWire(context.Background(),wire,runtime.AdapterContext{},job,progress);if err!=nil{t.Fatal(err)};access,err:=factory(protocol.Client[source.Job,source.Progress]{});if err!=nil{t.Fatal(err)}
 if _,err=access.Methods.Hold(context.Background(),specimen());err!=nil{t.Fatal(err)}
}
`
