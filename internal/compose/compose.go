// Package compose is the composition root: the only place a target is
// named. The kernel and the targets meet here and nowhere else, and both
// the generator and the conformance suite reach them through this package,
// so that what the suite renders is what the tool renders — a target
// added, or a target's config defaulted differently, reaches the two
// together or reaches neither. A checkout's config reaches the targets
// here too: each target's section, raw from the loader, is decoded into
// that target's own Config and validated by it, and the kernel reads one
// key of the file, the targets disabled.
package compose

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/atlas"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/markdown"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// Targets composes the targets with their defaults. The order is the order
// families are rendered in. module roots the Go packages and scope names
// the npm scope the TypeScript ones are published under; the runtime the
// targets bind to is each target's default, Nightseam's own.
func Targets(module, scope, sibling string) []spi.Target {
	targets, _ := Configure(load.Config{}, module, scope, sibling)
	return targets
}

// Configure composes the targets with a checkout's config: each target's
// section decoded into its Config, with no member the Config lacks, and
// validated by the target; a target the config disables is left out. What
// the tool's own flags set — the Go module, the npm scope, how generated
// TypeScript packages refer to one another — a section may not set, since
// they are the tool's to know of the checkout it runs in. What is wrong is
// reported at the section, as the checkout's own diagnostics.
func Configure(c load.Config, module, scope, sibling string) ([]spi.Target, []diag.Diagnostic) {
	var diagnostics []diag.Diagnostic
	var targets []spi.Target
	compose := func(name string, target func() spi.Target) {
		if !c.Disables(name) {
			targets = append(targets, target())
		}
	}
	report := func(name string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, diag.New("", diag.Location{File: load.ConfigFile}.Sub("targets", name), "invalid_config", err.Error()))
		}
	}

	var g golang.Config
	report(golang.Name, section(c, golang.Name, &g))
	report(golang.Name, flagged(g.Module != "", "module", "--module"))
	g.Module = module
	report(golang.Name, g.Validate())
	compose(golang.Name, func() spi.Target { return golang.New(g) })

	var ts typescript.Config
	report(typescript.Name, section(c, typescript.Name, &ts))
	report(typescript.Name, flagged(ts.Scope != "", "scope", "--scope"))
	report(typescript.Name, flagged(ts.Sibling != "", "sibling", "--ts-sibling"))
	ts.Scope, ts.Sibling = scope, sibling
	report(typescript.Name, ts.Validate())
	compose(typescript.Name, func() spi.Target { return typescript.New(ts) })
	spellers := map[string]spi.Speller{}
	for _, target := range targets {
		if speller, ok := target.(spi.Speller); ok {
			spellers[target.Name()] = speller
		}
	}

	var md markdown.Config
	report(markdown.Name, section(c, markdown.Name, &md))
	report(markdown.Name, markdown.New(md).Layout().Validate())
	compose(markdown.Name, func() spi.Target { return doc.Target(markdown.New(md), spellers) })

	var a atlas.Config
	report(atlas.Name, section(c, atlas.Name, &a))
	report(atlas.Name, a.Validate())
	compose(atlas.Name, func() spi.Target { return doc.Target(atlas.New(a), spellers) })

	return targets, diagnostics
}

// section decodes a target's section of the config into its Config,
// refusing a member the Config does not have.
func section(c load.Config, name string, into any) error {
	raw, ok := c.Targets[name]
	if !ok {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("the %s section: %s", name, err.Error())
	}
	return nil
}

// flagged refuses a member of a section that the tool's flag sets.
func flagged(set bool, member, flag string) error {
	if !set {
		return nil
	}
	return fmt.Errorf("%s is set by the tool's %s flag, not by %s", member, flag, load.ConfigFile)
}

// Names names the targets, for the override files a family may carry. It
// answers without a module or a scope, which loading a checkout does not
// need, and names a disabled target too, whose override file is still its
// own.
func Names() []string { return []string{golang.Name, typescript.Name, markdown.Name, atlas.Name} }

// Kernel is the pipeline composed with the targets and their defaults.
func Kernel(module, scope, sibling string) *kernel.Kernel {
	return kernel.New(Targets(module, scope, sibling)...)
}
