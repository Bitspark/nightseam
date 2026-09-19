package load

import (
	"strings"
	"testing"
)

// TestCheckoutReadsItsConfig: nightseam.json beside the families is the
// checkout's own, not a stray family — the targets it disables are read,
// each target's section is kept raw for the target, and a section keyed by
// no composed target, or a disabled name that is none, is reported at its
// place in the file.
func TestCheckoutReadsItsConfig(t *testing.T) {
	fsys := checkout(map[string]string{
		"x/model.json":   `{"nightseam": 2, "types": {}}`,
		"nightseam.json": `{"disabled": ["typescript", "rust"], "targets": {"markdown": {"layout": "docs/{family}"}, "haskell": {}}}`,
	})
	world, diagnostics := Checkout(fsys, "api/contracts", []string{"go", "typescript", "markdown"})
	if len(world.Names) != 1 || world.Names[0] != "x" {
		t.Fatalf("families are %v", world.Names)
	}
	if !world.Config.Disables("typescript") || world.Config.Disables("go") {
		t.Fatalf("disabled is %v", world.Config.Disabled)
	}
	if got := string(world.Config.Targets["markdown"]); got != `{"layout": "docs/{family}"}` {
		t.Fatalf("the markdown section is %q, not raw", got)
	}
	var said []string
	for _, d := range diagnostics {
		said = append(said, d.String())
	}
	want := []string{
		`nightseam.json#/disabled/1: No target named "rust" is composed; the targets are go, typescript, markdown. [unknown_target]`,
		`nightseam.json#/targets/haskell: No target named "haskell" is composed, so its section configures nothing; the targets are go, typescript, markdown. [unknown_target]`,
	}
	if strings.Join(said, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the config's diagnostics are:\n%s\nwant:\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}
	for _, d := range diagnostics {
		if d.Family != "" {
			t.Errorf("%s is reported as a family's, not the checkout's", d)
		}
	}
}

// TestCheckoutConfigIsHeldToItsShape: a config with a member the file does
// not have, or a section that is no object, is refused by shape and read as
// empty, so that a misspelling configures nothing silently.
func TestCheckoutConfigIsHeldToItsShape(t *testing.T) {
	for name, text := range map[string]string{
		"a member the file lacks": `{"disable": ["go"]}`,
		"a section that is text":  `{"targets": {"go": "fast"}}`,
		"not an object":           `["go"]`,
	} {
		fsys := checkout(map[string]string{"x/model.json": `{"nightseam": 2, "types": {}}`, "nightseam.json": text})
		world, diagnostics := Checkout(fsys, "api/contracts", []string{"go"})
		if len(diagnostics) == 0 || diagnostics[0].File != ConfigFile || diagnostics[0].Family != "" {
			t.Errorf("%s: not refused as the checkout's config: %v", name, diagnostics)
		}
		if len(world.Config.Disabled) != 0 || len(world.Config.Targets) != 0 {
			t.Errorf("%s: read as %v", name, world.Config)
		}
	}
}
