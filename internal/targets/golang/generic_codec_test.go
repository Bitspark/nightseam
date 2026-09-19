package golang

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratedGenericCodecsRetainConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compiles the generated codecs")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	testGenericCodecs(t, root)
}

func testGenericCodecs(t *testing.T, runtimeRoot string) {
	t.Helper()
	fam := family(map[string]string{"model.json": `{"nightseam":2,"types":{
		"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T"}]},
		"Bound":{"kind":"record","fields":[{"name":"item","type":"string"}]},
		"BoundMap":{"kind":"record","fields":[{"name":"item","type":{"map":"string"}}]},
		"Ready":{"kind":"alias","type":{"literal":"ready"}},
		"BoundLiteral":{"kind":"record","fields":[{"name":"item","type":{"literal":"ready"}}]},
		"Checked":{"kind":"record","fields":[{"name":"count","type":"integer","min":2}]}
	}}`})
	p, diagnostics := newPlan(fam)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	target := &target{Config{Module: "example.test/generic"}.settled()}
	source := target.file(p, fam, "protocol", emitTypes)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		t.Fatalf("format: %v\n%s", err, source)
	}
	directory := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", fmt.Sprintf("module example.test/generic\n\ngo 1.26.0\n\nrequire github.com/Bitspark/nightseam v0.0.0\n\nreplace github.com/Bitspark/nightseam => %s\n", filepath.ToSlash(runtimeRoot)))
	write("types_generated.go", string(formatted))
	write("validation_generated.go", target.file(p, fam, "protocol", emitValidation))
	write("codec_test.go", genericCodecProgram)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", "-v", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated generic codecs: %v\n%s\n%s", err, output, source)
	}
}

const genericCodecProgram = `package xprotocol
import (
 "encoding/json"
 "testing"
 runtime "github.com/Bitspark/nightseam/runtime/go"
)
func sameRefusal(t *testing.T, raw string, a, b any) {
 t.Helper()
 left,right:=json.Unmarshal([]byte(raw),a),json.Unmarshal([]byte(raw),b)
 if (left==nil)!=(right==nil) { t.Fatalf("%s: generic %v, bound %v",raw,left,right) }
 if left!=nil && left.Error()!=right.Error() { t.Fatalf("%s: generic %v, bound %v",raw,left,right) }
}
func TestStrings(t *testing.T) {
 for _,raw:=range []string{"{\"item\":\"hello\"}","{\"item\":null}","{\"item\":1}","{}"} {
  sameRefusal(t,raw,new(Box[string]),new(Bound))
 }
 var valid Box[string]
 if err:=json.Unmarshal([]byte("{\"item\":\"hello\"}"),&valid); err!=nil || valid.Item!="hello" { t.Fatalf("valid generic refused: %v",err) }
}
func TestNilMapsAndNullable(t *testing.T) {
 if _,err:=json.Marshal(Box[map[string]string]{}); err==nil { t.Fatal("nil generic map accepted") }
 if _,err:=json.Marshal(BoundMap{}); err==nil { t.Fatal("nil bound map accepted") }
 for _,raw:=range []string{"{\"item\":{}}","{\"item\":null}","{\"item\":{\"a\":null}}"} {
  sameRefusal(t,raw,new(Box[map[string]string]),new(BoundMap))
 }
 var nullable Box[runtime.Nullable[string]]
 if err:=json.Unmarshal([]byte("{\"item\":null}"),&nullable); err!=nil { t.Fatal(err) }
 if _,err:=json.Marshal(nullable); err!=nil { t.Fatal(err) }
}
func TestNamedAndNestedLiteral(t *testing.T) {
 for _,raw:=range []string{"{\"item\":\"ready\"}","{\"item\":\"wrong\"}","{\"item\":null}"} {
  sameRefusal(t,raw,new(Box[Ready]),new(BoundLiteral))
 }
 var nested Box[map[string][]Ready]
 if err:=json.Unmarshal([]byte("{\"item\":{\"a\":[\"ready\"]}}"),&nested); err!=nil { t.Fatal(err) }
 before:=nested.Item["a"][0]
 if err:=json.Unmarshal([]byte("{\"item\":{\"a\":[\"wrong\"]}}"),&nested); err==nil { t.Fatal("nested literal constraint lost") }
 if nested.Item["a"][0]!=before { t.Fatal("failed decoding changed receiver") }
 if _,err:=json.Marshal(Box[Ready]{Item:Ready("wrong")}); err==nil { t.Fatal("invalid literal encoded") }
}
func TestNamedRecordConstraintAndRecursion(t *testing.T) {
 var box Box[Checked]
 if err:=json.Unmarshal([]byte("{\"item\":{\"count\":3}}"),&box); err!=nil { t.Fatal(err) }
 if err:=json.Unmarshal([]byte("{\"item\":{\"count\":1}}"),&box); err==nil { t.Fatal("named field constraint lost") }
 var nested Box[Box[Checked]]
 if err:=json.Unmarshal([]byte("{\"item\":{\"item\":{\"count\":3}}}"),&nested); err!=nil { t.Fatal(err) }
 if _,err:=json.Marshal(nested); err!=nil { t.Fatal(err) }
}
`
