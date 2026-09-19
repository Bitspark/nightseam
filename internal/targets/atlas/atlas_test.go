package atlas

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

func TestDocumentIsEmbeddedLosslessly(t *testing.T) {
	c := &doc.Checkout{Families: []*doc.Family{{Name: "hostile", Types: []*doc.Type{{Name: "Text", Description: `</script><script>alert("x")</script>`, Example: json.RawMessage(`"</script>"`)}}}}}
	files, err := New(Config{Title: `A <title> & "quotes"`}).Checkout(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "api/spec/index.html" {
		t.Fatalf("files: %#v", files)
	}
	html := string(files[0].Data)
	start := strings.Index(html, `<script type="application/json" id="nightseam-document">`) + len(`<script type="application/json" id="nightseam-document">`)
	end := strings.Index(html[start:], "</script>") + start
	embedded := html[start:end]
	if strings.Contains(embedded, "<") || !strings.Contains(embedded, `\u003c/script`) {
		t.Fatalf("unsafe embedding: %s", embedded)
	}
	var got, want any
	if err := json.Unmarshal([]byte(embedded), &got); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("the embedded document changed")
	}
	if !strings.Contains(html, "A &lt;title&gt; &amp; &#34;quotes&#34;") {
		t.Fatal("title is not escaped")
	}
	if regexp.MustCompile(`(?i)<(?:link|script)[^>]+(?:href|src)="https?://`).MatchString(html) {
		t.Fatal("default page loads a network resource")
	}
	if err := New(Config{}).Layout().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigTakesEveryPageTokenAndRefusesUnknownTokens(t *testing.T) {
	known := map[string]bool{}
	for _, token := range tokens {
		known[token] = true
		if err := (Config{Tokens: map[string]string{token: "initial"}}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, match := range regexp.MustCompile(`(--[a-z0-9-]+):`).FindAllStringSubmatch(readAsset("style.css"), -1) {
		if !known[match[1]] {
			t.Errorf("page token %s cannot be configured", match[1])
		}
	}
	// A token's later dark-mode declaration is not an additional token.
	for _, token := range tokens {
		if !strings.Contains(readAsset("style.css"), token+":") {
			t.Errorf("unused token %s", token)
		}
	}
	for _, c := range []Config{{Tokens: map[string]string{"--unknown": "red"}}, {Tokens: map[string]string{"--ink": "red; } </style>"}}, {Tokens: map[string]string{"--bg": "url(https://example.org/x)"}}} {
		if c.Validate() == nil {
			t.Errorf("accepted %#v", c)
		}
		if _, err := New(c).Checkout(&doc.Checkout{}); err == nil {
			t.Error("writer accepted invalid config")
		}
	}
	files, err := New(Config{Webfonts: true}).Checkout(&doc.Checkout{})
	if err != nil || !strings.Contains(string(files[0].Data), "fonts.googleapis.com") {
		t.Fatalf("webfonts: %v", err)
	}
}

func proof(t *testing.T) *doc.Checkout {
	t.Helper()
	root := filepath.Join("..", "..", "..", "cmd", "nightseam", "testdata", "proof")
	families := map[string]map[string]string{}
	for _, name := range []string{"probe", "proof"} {
		entries, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		families[name] = map[string]string{}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(root, name, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			families[name][entry.Name()] = string(data)
		}
	}
	w := analysis.World(modeltest.World(families))
	c := &doc.Checkout{}
	for _, name := range []string{"probe", "proof"} {
		c.Families = append(c.Families, doc.Build(render.Build(analysis.Resolve(w, name))))
	}
	return c
}

func TestAtlasBuilders(t *testing.T) {
	if testing.Short() {
		t.Skip("Node builders run in the full tier")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("the full tier requires Node: ", err)
	}
	data, err := json.Marshal(proof(t))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "proof.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--test", "atlas.test.mjs")
	cmd.Env = append(os.Environ(), "ATLAS_PROOF="+file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("atlas builders: %v\n%s", err, out)
	}
}
