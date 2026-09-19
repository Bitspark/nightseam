package main

import (
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
)

// The tool's side of the composition. The targets are named in
// internal/compose and nowhere else — the conformance suite composes the
// same ones through the same package, so that the suite renders what the
// tool renders — and this file is what the command reaches them by.

// targetNames names the targets, for the override files a family may carry.
var targetNames = compose.Names()

// toolTargets composes the tool's targets with the checkout's config. A
// test replaces it to compose one more beside them — a target of a kind the
// tool has none of yet — and nothing else does.
var toolTargets = compose.Configure

// toolKernel is the kernel composed with the tool's targets, and what
// composing them with the checkout's config had to say.
func toolKernel(c load.Config, module, scope, sibling string) (*kernel.Kernel, []diag.Diagnostic) {
	targets, diagnostics := toolTargets(c, module, scope, sibling)
	return kernel.New(targets...), diagnostics
}
