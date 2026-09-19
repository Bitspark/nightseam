package main

import (
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/kernel"
)

// The tool's side of the composition. The targets are named in
// internal/compose and nowhere else — the conformance suite composes the
// same ones through the same package, so that the suite renders what the
// tool renders — and this file is what the command reaches them by.

// targetNames names the targets, for the override files a family may carry.
var targetNames = compose.Names()

// toolKernel is the kernel composed with the tool's targets.
func toolKernel(module, scope, sibling string) *kernel.Kernel {
	return compose.Kernel(module, scope, sibling)
}
