package pattern

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The engine translation is held against the specification's reference
// behavior, including syntax refusals. Node is a full-tier dependency.
func TestECMAScriptUnicode(t *testing.T) {
	if testing.Short() {
		t.Skip("the full tier compares against Node's Unicode engine")
	}
	patterns := []string{
		``, `[]`, `[^]`, `a{`, `a}`, `a]`, `a{,2}`, `a{2,1}`, `a{1,2,3}`, `^*`, `\b+`, `a++`,
		`\!`, `\-`, `[\_]`, `[\d-a]`, `[a-\s]`, `[z-a]`, `\01`, `\x0`, `\u00`, `\u{}`, `\u{110000}`,
		`\cA`, `\cz`, `\c_`, `\0`, `[\B]`, `[\b]`, `[a\-z]`, `[--0]`, `[-a]`, `[a-]`,
		`[()?=]`, `[(?=]`, `\(\?=`, `\uD83D\uDE00`, `[\uD83D\uDE00]`, `\u{1F600}`,
		`[\u{1F600}-\u{1F603}]`, `^(a{500}){3}$`, `^a{1001}$`, `a{0002}`, `a{2,0003}`,
		`^a{999999999999999999999999}$`, `^(?:a{1001}){1001}$`,
		`^\uD800$`, `^\uDFFF$`, `^[\uD800-\uDFFF]$`, `^[^\uD800]$`, `^[\uD7FF-\uD801]$`,
		strings.Repeat("(?:", 1001) + "a" + strings.Repeat(")", 1001),
	}
	atoms := []string{`a`, `.`, `é`, `😀`, `[a-z]`, `[^a]`, `[\s]`, `[\S]`, `[\S\s]`, `[^\S]`, `\s`, `\S`, `\d`, `\D`, `\w`, `\W`, `\u0061`, `\x61`, `(?:a|😀)`, `(?:)`}
	for _, atom := range atoms {
		for _, repeat := range []string{"", "?", "*", "+", "{2}", "{1,3}?"} {
			for _, anchors := range [][2]string{{"", ""}, {"^", "$"}, {`\b`, `\b`}, {`\B`, `\B`}} {
				patterns = append(patterns, anchors[0]+atom+repeat+anchors[1])
			}
		}
	}
	values := []string{"", "a", "aa", "ab", "word", "a_Z09", "é", "😀", "😁", "a😀", "😀😀", "١", "-", " ", "\n", "a\n", "\r", "\u2028", "\u2029", "\u00a0", "\u0085", "\ufeff", "\ufffd", "\ud7ff", "\b", "\x01", "\x1a", "\x00", strings.Repeat("a", 1001), strings.Repeat("a", 1500)}
	input, err := json.Marshal(struct{ Patterns, Values []string }{patterns, values})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "--input-type=module", "-e", `import fs from 'node:fs'; const x=JSON.parse(fs.readFileSync(0,'utf8')); console.log(JSON.stringify(x.Patterns.map(p=>{let r;try{r=new RegExp(p,'u')}catch{return {valid:false}}return {valid:true,matches:x.Values.map(v=>r.test(v))}})));`)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Node's Unicode engine: %v: %s", err, output)
	}
	var verdicts []struct {
		Valid   bool
		Matches []bool
	}
	if err := json.Unmarshal(output, &verdicts); err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != len(patterns) {
		t.Fatal("missing reference verdicts")
	}
	for index, source := range patterns {
		compiled, err := Compile(source)
		if (err == nil) != verdicts[index].Valid {
			t.Errorf("%q: Unicode syntax valid=%v, Go: %v", source, verdicts[index].Valid, err)
			continue
		}
		if err != nil {
			continue
		}
		for j, value := range values {
			if got := compiled.MatchString(value); got != verdicts[index].Matches[j] {
				t.Errorf("%q on %q: ECMAScript %v, Go %v", source, value, verdicts[index].Matches[j], got)
			}
			if len(value) < 64 {
				fallback := &Regexp{tree: compiled.tree}
				if got := fallback.MatchString(value); got != verdicts[index].Matches[j] {
					t.Errorf("%q on %q: ECMAScript %v, parsed matcher %v", source, value, verdicts[index].Matches[j], got)
				}
			}
		}
	}
}

func TestCountedPatternsBeyondNativeCapacity(t *testing.T) {
	for _, c := range []struct {
		pattern, value string
		valid          bool
	}{
		{`^a{1001}$`, strings.Repeat("a", 1001), true},
		{`^a{1001}$`, strings.Repeat("a", 1000), false},
		{`^(?:a{500}){3}$`, strings.Repeat("a", 1500), true},
		{`^a{1001,1003}$`, strings.Repeat("a", 1004), false},
		{`^a{1001,}$`, strings.Repeat("a", 1004), true},
		{`^a{999999999999999999999999}$`, "a", false},
		{`^(?:a?){999999999999999999999999}$`, "aaa", true},
		{`^(?:a?){999999999999999999999999}$`, "", true},
		{`^(?:a?){999999999999999999999999}$`, "b", false},
		{`^(?:a{1001}){1001}$`, "a", false},
	} {
		r, err := Compile(c.pattern)
		if err != nil {
			t.Errorf("%s: %v", c.pattern, err)
			continue
		}
		if got := r.MatchString(c.value); got != c.valid {
			t.Errorf("%s on %q: got %v, want %v", c.pattern, c.value, got, c.valid)
		}
	}
}
