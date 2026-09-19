package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/compose"
)

// named is how the root command's help names each target it is composed
// with. The help speaks a reader's words rather than the target's own —
// go is Go, markdown is the specification it writes — so the two cannot be
// compared directly, and this table is the join. A target added to
// compose.Targets and not to this table fails the test below, which is the
// point: the help said Go and TypeScript alone for as long as spec had been
// composed, and a reader of nightseam --help was never told the family's
// specification is generated at all.
var named = map[string]string{
	"go":         "Go",
	"typescript": "TypeScript",
	"markdown":   "Markdown",
	"atlas":      "HTML atlas",
}

func TestHelpNamesEveryComposedTarget(t *testing.T) {
	// The tool describes itself twice: in the root command's help, and in
	// the package comment pkg.go.dev shows; both are held.
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ParseComments|parser.PackageClauseOnly)
	if err != nil {
		t.Fatal(err)
	}
	if file.Doc == nil {
		t.Fatal("main.go has no package comment")
	}
	for where, text := range map[string]string{"the root command's help": newCommand().Long, "the package comment": file.Doc.Text()} {
		for _, name := range compose.Names() {
			phrase, ok := named[name]
			if !ok {
				t.Errorf("target %q is composed and this test does not say how the help names it; add it to named, and to the help", name)
				continue
			}
			if !strings.Contains(text, phrase) {
				t.Errorf("target %q is composed and %s does not name it (%q)", name, where, phrase)
			}
		}
	}
	for name := range named {
		if !slicesContains(compose.Names(), name) {
			t.Errorf("target %q is named here and is composed no longer; drop it here and from the help", name)
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
