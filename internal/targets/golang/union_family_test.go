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
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func TestGeneratedUnionFamiliesPreservePayloadsAndInheritance(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles and runs generated families")
	}
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base": {
			"model.json": `{"nightseam":2,"types":{
				"Payload":{"kind":"record","fields":[{"name":"value","type":{"nullable":"integer"}}]},
				"Generic":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
				"Choice":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","variants":{"some":"T","record":"Payload","nothing":{"empty":true}}}
			}}`,
			"go.json": `{"names":{"Choice":"Pick","Payload":"Body","Payload.value":"Amount"}}`,
		},
		"middle": {"model.json": `{"nightseam":2,"imports":["base"],"types":{
			"Middle":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","extends":[{"apply":"base.Choice","with":{"T":"T"}}],"variants":{"flag":"boolean"}}
		}}`},
		"child": {"model.json": `{"nightseam":2,"imports":["base","middle"],"types":{
			"Rich":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","extends":[{"apply":"middle.Middle","with":{"T":"T"}}],"variants":{"count":"integer"}},
			"Fixed":{"kind":"union","tag":"kind","extends":[{"apply":"base.Choice","with":{"T":{"literal":"ready"}}}],"variants":{"done":{"empty":true}}},
			"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
			"Bound":{"kind":"record","fields":[{"name":"item","type":{"apply":"Rich","with":{"T":"string"}}}]},
			"InheritedRecord":{"kind":"record","extends":["base.Payload"],"fields":[{"name":"label","type":"string"}]},
			"AppliedRecord":{"kind":"record","parameters":[{"name":"T"}],"extends":[{"apply":"base.Generic","with":{"T":{"array":"T"}}}],"fields":[]},
			"OpenInline":{"kind":"record","fields":[{"name":"body","type":{"kind":"record","open":true,"fields":[{"name":"value","type":"string"}]}}]},
			"Part":{"kind":"union","tag":"type","value":"data","variants":{
				"json":"json","map":{"map":"string"},"maybe":{"nullable":"base.Payload"},"record":"base.Payload","nothing":{"empty":true},
				"inline":{"kind":"record","fields":[{"name":"kind","type":{"literal":"inner"}}]},
				"nested":{"kind":"union","tag":"mode","variants":{"ready":{"literal":"ready"},"nothing":{"empty":true}}}
			}}
		}}`},
	}))
	directory := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	write("go.mod", fmt.Sprintf("module example.test/unions\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(root)))
	target := &target{Config{Module: "example.test/unions"}.settled()}
	for _, name := range []string{"base", "middle", "child"} {
		resolved := analysis.Resolve(world, name)
		if diagnostics := checks.Family(resolved); len(diagnostics) != 0 {
			t.Fatalf("%s: %v", name, diagnostics)
		}
		fam := render.Build(resolved)
		files, err := target.Render(fam)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if len(files) != 3 || !strings.HasSuffix(files[2].Path, "/familytest/examples_generated.go") {
			t.Fatalf("model-only family %s did not render just types, validation and examples", name)
		}
		for _, file := range files {
			write(file.Path, string(file.Data))
		}
	}
	write("union_test.go", unionFamilyProgram)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", "-v", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated union families: %v\n%s", err, output)
	}
}

const unionFamilyProgram = `package unions
import (
 "encoding/json"
 "reflect"
 "testing"
 base "example.test/unions/api/go/base-protocol"
 middle "example.test/unions/api/go/middle-protocol"
 child "example.test/unions/api/go/child-protocol"
)
func equalJSON(t *testing.T,a []byte,b string) {
 t.Helper(); var av,bv any
 if err:=json.Unmarshal(a,&av); err!=nil { t.Fatal(err) }
 if err:=json.Unmarshal([]byte(b),&bv); err!=nil { t.Fatal(err) }
 if !reflect.DeepEqual(av,bv) { t.Fatalf("got %s, want %s",a,b) }
}
func TestAdjacentRoundTrips(t *testing.T) {
 for _,raw:=range []string{
  "{\"type\":\"json\",\"data\":3}","{\"type\":\"json\",\"data\":{\"value\":3}}",
  "{\"type\":\"maybe\",\"data\":null}","{\"type\":\"maybe\",\"data\":{\"value\":null}}",
  "{\"type\":\"map\",\"data\":{}}","{\"type\":\"map\",\"data\":{\"kind\":\"map\"}}",
  "{\"type\":\"record\",\"data\":{\"value\":3}}","{\"type\":\"nothing\"}",
  "{\"type\":\"inline\",\"data\":{\"kind\":\"inner\"}}",
  "{\"type\":\"nested\",\"data\":{\"mode\":\"ready\",\"value\":\"ready\"}}",
 } {
  var value child.Part
  if err:=json.Unmarshal([]byte(raw),&value); err!=nil { t.Fatalf("%s: %v",raw,err) }
  result,err:=json.Marshal(value); if err!=nil { t.Fatal(err) }; equalJSON(t,result,raw)
 }
}
func TestInheritedPointersAndWire(t *testing.T) {
 payload:=&base.PickSomeValue[string]{Value:"hello"}
 original:=base.Pick[string]{Some:payload}
 mid:=middle.WidenMiddleFromBasePick(original)
 rich:=child.WidenRichFromMiddleMiddle(mid)
 if rich.Some!=payload { t.Fatal("inherited wrapper was replaced") }
 data,err:=json.Marshal(rich); if err!=nil { t.Fatal(err) }
 equalJSON(t,data,"{\"kind\":\"some\",\"value\":\"hello\"}")
 narrowed,ok:=child.NarrowRichToMiddleMiddle(rich); if !ok || narrowed.Some!=payload { t.Fatal("narrow failed") }
 only:=child.Rich[string]{Count:&child.RichCountValue[string]{Value:3}}
 if _,ok:=child.NarrowRichToMiddleMiddle(only); ok { t.Fatal("extra alternative narrowed") }
 if err:=base.ValidateRaw("Choice",[]byte("{\"kind\":\"count\",\"value\":3}")); err==nil { t.Fatal("base accepts extended-only tag") }
 var decoded child.Rich[string]
 if err:=json.Unmarshal(data,&decoded); err!=nil || decoded.Some.Value!="hello" { t.Fatalf("decode: %v",err) }
}
func TestBoundAndGenericUnionAgree(t *testing.T) {
 for _,raw:=range []string{"{\"item\":{\"kind\":\"some\",\"value\":\"hello\"}}","{\"item\":{\"kind\":\"some\",\"value\":null}}","{\"item\":{\"kind\":\"unknown\"}}"} {
  var generic child.Box[child.Rich[string]]; var bound child.Bound
  a,b:=json.Unmarshal([]byte(raw),&generic),json.Unmarshal([]byte(raw),&bound)
  if (a==nil)!=(b==nil) { t.Fatalf("%s: generic %v, bound %v",raw,a,b) }
  if a==nil { data,err:=json.Marshal(generic); if err!=nil { t.Fatal(err) }; equalJSON(t,data,raw) }
 }
}
func TestFixedLiteralBase(t *testing.T) {
 var fixed child.Fixed
 if err:=json.Unmarshal([]byte("{\"kind\":\"some\",\"value\":\"ready\"}"),&fixed); err!=nil { t.Fatal(err) }
 var reused *base.PickSomeValue[child.LiteralReady] = fixed.Some
 if reused.Value!=child.LiteralReadyValue { t.Fatal("wrong fixed value") }
 if err:=json.Unmarshal([]byte("{\"kind\":\"some\",\"value\":\"wrong\"}"),&fixed); err==nil { t.Fatal("fixed literal was widened to string") }
}
func TestInvalidSelectionsAndPayloads(t *testing.T) {
 for _,value:=range []child.Part{{},{Nothing:&struct{}{},JSON:&child.PartJSONValue{Value:3}}} {
  if _,err:=json.Marshal(value); err==nil { t.Fatal("invalid selection encoded") }
 }
 for _,raw:=range []string{"{\"type\":\"nothing\",\"data\":null}","{\"type\":\"record\",\"value\":3}","{\"type\":\"map\",\"data\":null}","{\"type\":\"inline\",\"data\":{\"kind\":\"wrong\"}}"} {
  value:=child.Part{Nothing:&struct{}{}}
  if err:=json.Unmarshal([]byte(raw),&value); err==nil { t.Fatalf("accepted %s",raw) }
  if value.Nothing==nil { t.Fatal("failed decode changed selection") }
 }
}
func TestUnionAndOpenRecordRefuseMalformedUnicode(t *testing.T) {
 bad:=string([]byte{0xff})
 for _,value:=range []any{
  base.Pick[string]{Some:&base.PickSomeValue[string]{Value:bad}},
  child.Part{JSON:&child.PartJSONValue{Value:map[string]any{bad:1}}},
 } { if _,err:=json.Marshal(value); err==nil { t.Fatalf("encoded malformed Unicode in %T",value) } }
 var open child.OpenInline
 if err:=json.Unmarshal([]byte("{\"body\":{\"value\":\"ok\"}}"),&open); err!=nil { t.Fatal(err) }
 open.Body.AdditionalFields=map[string]json.RawMessage{bad:json.RawMessage("1")}
 if _,err:=json.Marshal(open); err==nil { t.Fatal("encoded malformed additional-field name") }
}
func TestInheritedAndOpenInlineRecords(t *testing.T) {
 var inherited child.InheritedRecord
 if err:=json.Unmarshal([]byte("{\"value\":3,\"label\":\"ok\"}"),&inherited); err!=nil || inherited.Amount.Value!=3 { t.Fatalf("inherited field override: %#v %v",inherited,err) }
 var applied child.AppliedRecord[string]
 if err:=json.Unmarshal([]byte("{\"item\":[\"a\",\"b\"]}"),&applied); err!=nil || len(applied.Item)!=2 { t.Fatalf("applied record: %#v %v",applied,err) }
 raw:="{\"body\":{\"value\":\"ok\",\"extra\":3}}"
 var open child.OpenInline
 if err:=json.Unmarshal([]byte(raw),&open); err!=nil { t.Fatal(err) }
 if len(open.Body.AdditionalFields)!=1 { t.Fatal("inline open storage contains declared fields") }
 result,err:=json.Marshal(open); if err!=nil { t.Fatal(err) }; equalJSON(t,result,raw)
 open.Body.AdditionalFields["value"]=json.RawMessage("\"wrong\"")
 if _,err:=json.Marshal(open); err==nil { t.Fatal("additional field replaced a declared field") }
}
`
