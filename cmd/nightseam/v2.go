package main

import (
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/spec"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// targets is the v2 composition root: the only place a target is named.
// The order is the order families are rendered in. module roots the Go
// packages; the runtime the targets bind to is each target's default,
// Nightseam's own. It takes over from languages when the CLI switches.
func targets(module, scope string) []spi.Target {
	return []spi.Target{
		golang.New(golang.Config{Module: module}),
		typescript.New(typescript.Config{Scope: scope}),
		spec.New(spec.Config{}),
	}
}

// targetNames names the targets, for the override files a family may carry.
var targetNames = []string{golang.Name, typescript.Name, spec.Name}

// v2Kernel is the v2 pipeline composed with the targets.
func v2Kernel(module, scope string) *kernel.Kernel { return kernel.New(targets(module, scope)...) }
