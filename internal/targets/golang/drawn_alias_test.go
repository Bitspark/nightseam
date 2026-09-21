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
	checks "github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func drawnAliasWorld() analysis.World {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"source": {
			"model.json":    `{"nightseam":2,"types":{"Job":{"kind":"record","fields":[{"name":"label","type":"string"}]}}}`,
			"protocol.json": modeltest.Protocol(``),
			"live.json":     `{"nightseam":2}`,
		},
		"holder": {
			"model.json": `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"live"}],"types":{
				"JobAlias":{"kind":"alias","type":"S.Job"},
				"JobChain":{"kind":"alias","type":"JobAlias"},
				"Jobs":{"kind":"alias","type":{"array":"JobChain"}},
				"Identity":{"kind":"alias","parameters":[{"name":"T"}],"type":"T"},
				"Holder":{"kind":"record","fields":[{"name":"job","type":"JobChain"},{"name":"jobs","type":"Jobs"},{"name":"byName","type":{"map":"JobAlias"}}]},
				"Choice":{"kind":"union","tag":"kind","variants":{"job":"JobChain","empty":{"empty":true}}}
			},"server":{"methods":{"hold":{"request":"Holder","result":"Holder"},"job":{"request":"S.Job","result":"JobChain"}}}`),
		},
		"consumer": {
			"model.json": `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"imports":["holder","source"],"parameters":[{"name":"R","of":"live"}],"types":{
				"RemoteHolder":{"kind":"record","fields":[{"name":"job","type":"holder.JobChain"},{"name":"mapped","type":{"apply":"holder.Identity","with":{"T":{"array":"R.Job"}}}},{"name":"fixed","type":{"apply":"holder.JobAlias","with":{"S":"source"}}}]}
			}`),
		},
	}))
	for name, family := range builtin.Families() {
		world[name] = family
	}
	return world
}

func TestDrawnAliasesUseTheirNativeSlot(t *testing.T) {
	files, err := New(Config{Module: "example.test/drawnalias"}).Render(render.Build(analysis.Resolve(drawnAliasWorld(), "holder")))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		for _, forbidden := range []string{"type JobAlias[", "type JobChain[", "type Identity[", "protocol.JobAlias", "protocol.JobChain", "ValueAdapter[JobAlias[", "ValueAdapter[JobChain["} {
			if strings.Contains(string(file.Data), forbidden) {
				t.Errorf("%s retains an unrepresentable alias: %s", file.Path, forbidden)
			}
		}
	}
}

func TestGeneratedDrawnAliasesComposeWithoutNominalWrappers(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and exercises generated aliases to native slots")
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
	write("go.mod", []byte(fmt.Sprintf("module example.test/drawnalias\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root))))
	world := drawnAliasWorld()
	target := New(Config{Module: "example.test/drawnalias"})
	for name := range world {
		resolved := analysis.Resolve(world, name)
		if diagnostics := checks.Family(resolved); len(diagnostics) != 0 {
			t.Fatalf("%s: %v", name, diagnostics)
		}
		files, err := target.Render(render.Build(resolved))
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, file := range files {
			write(file.Path, file.Data)
		}
	}
	write("alias_test.go", []byte(drawnAliasGoProgram))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-parallel", "2", "-mod=mod", "-count=1", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated drawn aliases: %v\n%s", err, output)
	}
}

const drawnAliasGoProgram = `package drawnalias_test
import (
 "context"
 "encoding/json"
 "testing"
 consumer "example.test/drawnalias/api/go/consumer-protocol"
 binding "example.test/drawnalias/api/go/holder-binding"
 protocol "example.test/drawnalias/api/go/holder-protocol"
 source "example.test/drawnalias/api/go/source-protocol"
 "github.com/Bitspark/nightseam/duplex/go"
 "github.com/Bitspark/nightseam/runtime/go"
)
type value = protocol.Holder[source.Job]
type methods struct{}
func(methods) Hold(_ context.Context,v value)(value,error){return v,nil}
func(methods) Job(_ context.Context,v source.Job)(source.Job,error){return v,nil}
func TestAliasesKeepSourceTypeAndConversion(t *testing.T){
 ctx:=context.Background(); job:=source.Job{Label:"drawn"}; adapter:=source.AdapterJob()
 for _,alias:=range []runtime.ValueAdapter[source.Job]{protocol.AdapterJobAlias(adapter),protocol.AdapterJobChain(adapter)}{
  raw,err:=alias.Export(ctx,job);if err!=nil{t.Fatal(err)}
  got,err:=alias.Import(ctx,raw);if err!=nil||got!=job{t.Fatalf("alias: %+v %v",got,err)}
  if _,err:=alias.Import(ctx,json.RawMessage("{\"label\":5}"));err==nil{t.Fatal("alias lost source validation")}
 }
 identity:=protocol.AdapterIdentity(runtime.JSONAdapter[string]());raw,err:=identity.Export(ctx,"same");if err!=nil{t.Fatal(err)};if got,err:=identity.Import(ctx,raw);err!=nil||got!="same"{t.Fatalf("type slot alias: %q %v",got,err)}
 imported:=consumer.RemoteHolder[source.Job]{Job:job,Mapped:[]source.Job{job},Fixed:job};remoteAdapter:=consumer.AdapterRemoteHolder(adapter);raw,err=remoteAdapter.Export(ctx,imported);if err!=nil{t.Fatal(err)};if got,err:=remoteAdapter.Import(ctx,raw);err!=nil||got.Job!=job||got.Mapped[0]!=job||got.Fixed!=job{t.Fatalf("imported alias: %+v %v",got,err)}
 choice:=protocol.Choice[source.Job]{Job:&protocol.ChoiceJobValue[source.Job]{Value:job}}
 choiceAdapter:=protocol.AdapterChoice(adapter);raw,err=choiceAdapter.Export(ctx,choice);if err!=nil{t.Fatal(err)};if got,err:=choiceAdapter.Import(ctx,raw);err!=nil||got.Job.Value!=job{t.Fatalf("union alias: %+v %v",got,err)}
 wire,err:=binding.ToWire(func(protocol.Client[source.Job])(protocol.Server[source.Job],error){return protocol.Server[source.Job]{Methods:methods{}},nil},runtime.AdapterContext{},adapter);if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
 factory,err:=binding.FromWire(ctx,wire,runtime.AdapterContext{},adapter);if err!=nil{t.Fatal(err)};remote,err:=factory(protocol.Client[source.Job]{});if err!=nil{t.Fatal(err)}
 specimen:=value{Job:job,Jobs:[]source.Job{job},ByName:map[string]source.Job{"one":job}}
 if got,err:=remote.Methods.Hold(ctx,specimen);err!=nil||got.Job!=job||got.Jobs[0]!=job||got.ByName["one"]!=job{t.Fatalf("nested aliases: %+v %v",got,err)}
 if got,err:=remote.Methods.Job(ctx,job);err!=nil||got!=job{t.Fatalf("operation alias: %+v %v",got,err)}
}
`
