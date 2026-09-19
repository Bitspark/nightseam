package markdown

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/spi"
)

func TestLanguageBlocksUseOnlyDocumentMetadata(t *testing.T) {
	f := &doc.Family{Name: "x", Protocol: true,
		Types: []*doc.Type{{Name: "Value", Kind: "alias", Alias: model.Primitive("string"), Languages: map[string]doc.Language{
			"future": {Name: "Value", Declare: "value ``` literal"}, "zulu": {Declare: "zulu Value"}, "names-only": {Name: "Value"},
		}}},
		Server: doc.Side{Methods: []doc.Method{{Name: "run", Languages: map[string]doc.Language{"future": {Invoke: spi.Invocation{Call: "run(value)", Handle: "handle run(value)"}}}}}, Events: []doc.Event{{Name: "changed", Languages: map[string]doc.Language{"future": {Invoke: spi.Invocation{Call: "send(value)"}}}}}},
	}
	files, err := New(Config{}).Family(f)
	if err != nil {
		t.Fatal(err)
	}
	page := string(files[0].Data)
	for _, want := range []string{"````future\nvalue ``` literal\n````", "```zulu\nzulu Value\n```", "```future\nrun(value)\n\nhandle run(value)\n```", "```future\nsend(value)\n```"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing language block %q", want)
		}
	}
	if strings.Index(page, "````future") > strings.Index(page, "```zulu") {
		t.Error("language blocks are not in target-name order")
	}
	if strings.Contains(page, "```names-only") {
		t.Error("a language without code emitted an empty block")
	}
}
