package golang

import (
	"context"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/internal/render"
)

// This executable test isolates the generated codec from schema resolution.
// The validator seam is injected below and its errors must be propagated;
// generated-family integration tests exercise the actual runtime validator.
func TestConcreteUnionCodecCompilesAndRoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles the generated codec")
	}
	fam := family(map[string]string{"model.json": `{"nightseam":2,"types":{}}`})
	p, diagnostics := newPlan(fam)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	p.types["Part"], p.types["Choice"], p.types["Extended"] = "Part", "Choice", "Extended"
	p.unions["Part"] = unionPlan{kind: "PartKind"}
	p.unions["Choice"] = unionPlan{kind: "ChoiceKind"}
	p.unions["Extended"] = unionPlan{kind: "ExtendedKind"}
	part := &render.Type{Name: "Part", Tag: "kind", Value: "value"}
	choice := &render.Type{Name: "Choice", Tag: "type", Value: "data", Uses: []render.Use{{Parameter: "T"}}}
	extended := &render.Type{Name: "Extended", Tag: "type", Value: "data", Uses: choice.Uses}
	variants := []unionVariant{
		{tag: "count", field: "Count", kind: "PartKindCount", typeName: "PartCount", wrapper: "PartCount", payloadType: "int64", wrapped: true},
		{tag: "json", field: "JSON", kind: "PartKindJSON", typeName: "PartJSON", wrapper: "PartJSON", payloadType: "any", wrapped: true},
		{tag: "maybe", field: "Maybe", kind: "PartKindMaybe", typeName: "PartMaybe", wrapper: "PartMaybe", payloadType: "*Payload", wrapped: true},
		{tag: "map", field: "Map", kind: "PartKindMap", typeName: "PartMap", wrapper: "PartMap", payloadType: "map[string]string", wrapped: true},
		{tag: "record", field: "Record", kind: "PartKindRecord", typeName: "Payload"},
		{tag: "nothing", field: "Nothing", kind: "PartKindNothing", typeName: "struct{}", empty: true},
	}
	target := &target{Config{Module: "example.test/codec"}.settled()}
	choiceVariants := []unionVariant{{tag: "some", field: "Some", kind: "ChoiceKindSome", typeName: "ChoiceSome[T]", wrapper: "ChoiceSome", payloadType: "T", wrapped: true}}
	source := target.file(p, fam, "protocol", func(f *file) {
		f.emitUnion(part, variants)
		f.emitUnion(choice, choiceVariants)
		f.use("base", "example.test/codec/base")
		f.emitUnion(extended, []unionVariant{
			{tag: "some", field: "Some", kind: "ExtendedKindSome", typeName: "base.ChoiceSome[T]", wrapped: true},
			{tag: "nothing", field: "Nothing", kind: "ExtendedKindNothing", typeName: "struct{}", empty: true},
		})
		f.emitUnionConversions(extended, []unionBase{{
			typeName: "base.Choice[T]", widen: "WidenExtendedFromChoice", narrow: "NarrowExtendedToChoice",
			variants: []unionBaseVariant{{baseField: "Some", field: "Some", kind: "ExtendedKindSome"}},
		}})
	})
	formatted, err := format.Source([]byte(source))
	if err != nil {
		t.Fatalf("format generated codec: %v\n%s", err, source)
	}
	if strings.Contains(source, `"reflect"`) {
		t.Fatal("a generated union must not use reflection")
	}
	directory := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	module, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	version := ""
	for line := range strings.Lines(string(module)) {
		if strings.HasPrefix(line, "go ") {
			version = line
			break
		}
	}
	if version == "" {
		t.Fatal("repository go.mod has no language version")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	write("go.mod", []byte("module example.test/codec\n\n"+version+"\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => "+filepath.ToSlash(root)+"\n"))
	write("types.go", formatted)
	write("codec_test.go", []byte(unionCodecProgram))
	baseSource := target.file(p, fam, "protocol", func(f *file) { f.emitUnion(choice, choiceVariants) })
	if err := os.Mkdir(filepath.Join(directory, "base"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("base/types.go", []byte(baseSource))
	write("base/validation.go", []byte(`package xprotocol
import runtime "github.com/Bitspark/nightseam/runtime/go"
type Tag struct{}
type schemaStub struct{}
var schema schemaStub
func (s schemaStub) Bind(map[string]any, map[string]*runtime.Schema) schemaStub { return s }
func (schemaStub) ValidateExpressionRaw(any, []byte, ...string) error { return nil }
`))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated codec: %v\n%s\n%s", err, output, source)
	}
}

const unionCodecProgram = `package xprotocol

import (
 "encoding/json"
 "errors"
 "reflect"
 "testing"
 base "example.test/codec/base"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)

type Tag struct{}
type Payload struct { Value *int64 ` + "`json:\"value\"`" + `; Kind string ` + "`json:\"kind,omitempty\"`" + ` }
var validationFailure error
type schemaStub struct{}
var schema schemaStub
func (s schemaStub) Bind(map[string]any, map[string]*runtime.Schema) schemaStub { return s }
func (schemaStub) ValidateExpressionRaw(any, []byte, ...string) error { return validationFailure }
func integer(v int64) *int64 { return &v }
func sameJSON(t *testing.T, got []byte, want string) {
 t.Helper()
 var a, b any
 if err := json.Unmarshal(got, &a); err != nil { t.Fatal(err) }
 if err := json.Unmarshal([]byte(want), &b); err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(a,b) { t.Fatalf("got %s, want %s",got,want) }
}
func TestRoundTrips(t *testing.T) {
 cases := []struct { value Part; wire string }{
  {Part{Count:&PartCount{Value:3}}, "{\"kind\":\"count\",\"value\":3}"},
  {Part{JSON:&PartJSON{Value:float64(3)}}, "{\"kind\":\"json\",\"value\":3}"},
  {Part{JSON:&PartJSON{Value:map[string]any{"value":float64(3)}}}, "{\"kind\":\"json\",\"value\":{\"value\":3}}"},
  {Part{JSON:&PartJSON{Value:nil}}, "{\"kind\":\"json\",\"value\":null}"},
  {Part{Maybe:&PartMaybe{Value:nil}}, "{\"kind\":\"maybe\",\"value\":null}"},
  {Part{Maybe:&PartMaybe{Value:&Payload{}}}, "{\"kind\":\"maybe\",\"value\":{\"value\":null}}"},
  {Part{Maybe:&PartMaybe{Value:&Payload{Value:integer(3)}}}, "{\"kind\":\"maybe\",\"value\":{\"value\":3}}"},
  {Part{Map:&PartMap{Value:map[string]string{}}}, "{\"kind\":\"map\",\"value\":{}}"},
  {Part{Map:&PartMap{Value:map[string]string{"kind":"map"}}}, "{\"kind\":\"map\",\"value\":{\"kind\":\"map\"}}"},
  {Part{Record:&Payload{Value:integer(3),Kind:"record"}}, "{\"kind\":\"record\",\"value\":{\"value\":3,\"kind\":\"record\"}}"},
  {Part{Nothing:&struct{}{}}, "{\"kind\":\"nothing\"}"},
 }
 for _, c := range cases { t.Run(c.wire,func(t *testing.T) {
  data,err := json.Marshal(c.value); if err != nil { t.Fatal(err) }; sameJSON(t,data,c.wire)
  var decoded Part
  if err := json.Unmarshal(data,&decoded); err != nil { t.Fatal(err) }
  if decoded.Kind()!=c.value.Kind() || !reflect.DeepEqual(decoded,c.value) { t.Fatalf("changed payload: %#v -> %#v",c.value,decoded) }
  again,err := json.Marshal(decoded); if err != nil { t.Fatal(err) }; sameJSON(t,again,c.wire)
 }) }
}
func TestSelectionAndTransactionalDecode(t *testing.T) {
 for _, value := range []Part{{},{Count:&PartCount{Value:1},JSON:&PartJSON{Value:true}}} {
  if value.Kind()!="" { t.Fatal("invalid selection has a kind") }
  if _,err := json.Marshal(value); err==nil { t.Fatal("invalid selection encoded") }
 }
 original := Part{Count:&PartCount{Value:9}}
 for _, raw := range []string{"null","{}","{\"kind\":\"unknown\"}","{\"kind\":\"count\"}","{\"kind\":\"count\",\"value\":\"wrong\"}"} {
  decoded := original
  if err := json.Unmarshal([]byte(raw),&decoded); err==nil { t.Fatalf("accepted %s",raw) }
  if !reflect.DeepEqual(decoded,original) { t.Fatal("failed decoding mutated its receiver") }
 }
 decoded := original
 if err := json.Unmarshal([]byte("{\"kind\":\"nothing\"}"),&decoded); err!=nil { t.Fatal(err) }
 if decoded.Count!=nil || decoded.Nothing==nil { t.Fatal("successful decoding kept an old variant") }
}
func TestGenericComposition(t *testing.T) {
 type Box[T any] struct { Item T ` + "`json:\"item\"`" + ` }
 raw := "{\"item\":[{\"type\":\"some\",\"data\":{\"first\":{\"kind\":\"json\",\"value\":{\"value\":3}}}}]}"
 var generic Box[[]*Choice[map[string]*Part]]
 var bound struct { Item []*Choice[map[string]*Part] ` + "`json:\"item\"`" + ` }
 if err := json.Unmarshal([]byte(raw),&generic); err!=nil { t.Fatal(err) }
 if err := json.Unmarshal([]byte(raw),&bound); err!=nil { t.Fatal(err) }
 a,err := json.Marshal(generic); if err!=nil { t.Fatal(err) }; sameJSON(t,a,raw)
 b,err := json.Marshal(bound); if err!=nil { t.Fatal(err) }; sameJSON(t,b,string(a))
 if !reflect.DeepEqual(generic.Item,bound.Item) { t.Fatal("generic and bound values differ") }
}
func TestValidatorErrorsPropagate(t *testing.T) {
 validationFailure=errors.New("schema refused")
 defer func(){ validationFailure=nil }()
 value:=Part{Count:&PartCount{Value:3}}
 if _,err:=value.MarshalJSON(); !errors.Is(err,validationFailure) { t.Fatalf("marshal bypassed schema: %v",err) }
 original:=value
 if err:=value.UnmarshalJSON([]byte("{\"kind\":\"nothing\"}")); !errors.Is(err,validationFailure) { t.Fatalf("decode bypassed schema: %v",err) }
 if !reflect.DeepEqual(original,value) { t.Fatal("validator refusal mutated receiver") }
}
func TestWideningAndNarrowingReusePayloads(t *testing.T) {
 payload := &base.ChoiceSome[map[string]*Part]{Value:map[string]*Part{"first":{Count:&PartCount{Value:3}}}}
 original := base.Choice[map[string]*Part]{Some:payload}
 extended := WidenExtendedFromChoice(original)
 if extended.Some!=payload { t.Fatal("widening copied or replaced the base payload") }
 before,err:=json.Marshal(original); if err!=nil { t.Fatal(err) }
 after,err:=json.Marshal(extended); if err!=nil { t.Fatal(err) }; sameJSON(t,after,string(before))
 narrowed,ok:=NarrowExtendedToChoice(extended)
 if !ok || narrowed.Some!=payload { t.Fatal("narrowing did not preserve the base variant") }
 for _,value:=range []Extended[map[string]*Part]{
  {}, {Nothing:&struct{}{}}, {Some:payload,Nothing:&struct{}{}},
 } {
  narrowed,ok:=NarrowExtendedToChoice(value)
  if ok || narrowed.Some!=nil { t.Fatal("narrowing accepted an invalid or extended-only selection") }
 }
}
`
