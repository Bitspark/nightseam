package compose

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/spi"
)

func names(targets []spi.Target) []string {
	var out []string
	for _, target := range targets {
		out = append(out, target.Name())
	}
	return out
}

func sections(text string) load.Config {
	var c struct {
		Disabled []string
		Targets  map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		panic(err)
	}
	return load.Config{Disabled: c.Disabled, Targets: c.Targets}
}

// TestASectionReachesItsTarget: a target's section of the checkout's config
// is decoded into that target's own config and shapes what it renders —
// the Markdown writer's layout moved by its section moves its pages — and
// a target the config disables is composed no more, the others as before.
func TestASectionReachesItsTarget(t *testing.T) {
	targets, diagnostics := Configure(sections(`{"targets": {"markdown": {"layout": "docs/{family}"}}}`), "example.com/api", "@example", "")
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if got := names(targets); strings.Join(got, ",") != "go,typescript,markdown" {
		t.Fatalf("composed %v", got)
	}
	if owns := targets[2].Owns("x"); len(owns) != 1 || owns[0] != "docs/x" {
		t.Fatalf("the Markdown writer owns %v; its section did not reach it", owns)
	}
	targets, diagnostics = Configure(sections(`{"disabled": ["typescript"]}`), "example.com/api", "@example", "")
	if len(diagnostics) != 0 || strings.Join(names(targets), ",") != "go,markdown" {
		t.Fatalf("with typescript disabled: %v, %v", names(targets), diagnostics)
	}
}

// TestASectionIsHeldByItsTarget: a member the target's config lacks, a
// value the target refuses, and a member the tool's flag sets are each
// reported at the section, as the checkout's own, and the target is still
// composed, so that validate reports every problem and generate refuses.
func TestASectionIsHeldByItsTarget(t *testing.T) {
	for name, tc := range map[string]struct{ config, want string }{
		"a member the config lacks":  {`{"targets": {"markdown": {"colour": "night"}}}`, `nightseam.json#/targets/markdown: the markdown section: json: unknown field "colour" [invalid_config]`},
		"a value the target refuses": {`{"targets": {"markdown": {"layout": "docs"}}}`, `nightseam.json#/targets/markdown: a layout pattern names the family with {family}: "docs" [invalid_config]`},
		"the Go module":              {`{"targets": {"go": {"module": "example.com/other"}}}`, `nightseam.json#/targets/go: module is set by the tool's --module flag, not by nightseam.json [invalid_config]`},
		"the npm scope":              {`{"targets": {"typescript": {"scope": "@other"}}}`, `nightseam.json#/targets/typescript: scope is set by the tool's --scope flag, not by nightseam.json [invalid_config]`},
		"a Go import path":           {`{"targets": {"go": {"runtime": "not a path"}}}`, `nightseam.json#/targets/go: invalid Go import path "not a path" [invalid_config]`},
	} {
		targets, diagnostics := Configure(sections(tc.config), "example.com/api", "@example", "")
		if len(diagnostics) != 1 || diagnostics[0].String() != tc.want || diagnostics[0].Family != "" {
			t.Errorf("%s: said %v, want %s", name, diagnostics, tc.want)
		}
		if len(targets) != 3 {
			t.Errorf("%s: composed %v", name, names(targets))
		}
	}
}

// TestDefaultsAreTheConfiguredTargetsWithNoConfig: what Targets composes
// is what Configure composes with an empty config, so that the conformance
// suite and a checkout with no nightseam.json render alike.
func TestDefaultsAreTheConfiguredTargetsWithNoConfig(t *testing.T) {
	plain := Targets("example.com/api", "@example", "")
	configured, diagnostics := Configure(load.Config{}, "example.com/api", "@example", "")
	if len(diagnostics) != 0 || strings.Join(names(plain), ",") != strings.Join(names(configured), ",") {
		t.Fatalf("%v is not %v: %v", names(plain), names(configured), diagnostics)
	}
	if strings.Join(Names(), ",") != strings.Join(names(plain), ",") {
		t.Fatalf("Names says %v, the targets are %v", Names(), names(plain))
	}
}
