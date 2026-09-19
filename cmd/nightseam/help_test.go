package main

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/compose"
)

// named is how the root command's help names each target it is composed
// with. The help speaks a reader's words rather than the target's own —
// golang is Go, spec is the specification it writes — so the two cannot be
// compared directly, and this table is the join. A target added to
// compose.Targets and not to this table fails the test below, which is the
// point: the help said Go and TypeScript alone for as long as spec had been
// composed, and a reader of nightseam --help was never told the family's
// specification is generated at all.
var named = map[string]string{
	"go":         "Go",
	"typescript": "TypeScript",
	"spec":       "specification",
}

func TestHelpNamesEveryComposedTarget(t *testing.T) {
	help := newCommand().Long
	for _, name := range compose.Names() {
		phrase, ok := named[name]
		if !ok {
			t.Errorf("target %q is composed and this test does not say how the help names it; add it to named, and to the help", name)
			continue
		}
		if !strings.Contains(help, phrase) {
			t.Errorf("target %q is composed and the root command's help does not name it (%q)", name, phrase)
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
