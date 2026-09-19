package main

import (
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/kernel"
)

// The tool's side of the composition. The targets are named in
// internal/compose and nowhere else — the conformance suite composes the
// same ones through the same package, so that the suite renders what the
// tool renders — and this file is what the v2 CLI reaches them by. It
// takes over from languages when the CLI switches.

// targetNames names the targets, for the override files a family may carry.
var targetNames = compose.Names()

// v2Kernel is the v2 pipeline composed with the targets.
func v2Kernel(module, scope string) *kernel.Kernel { return compose.Kernel(module, scope) }
