// Package compose is the composition root: the only place a target is
// named. The kernel and the targets meet here and nowhere else, and both
// the generator and the conformance suite reach them through this package,
// so that what the suite renders is what the tool renders — a target
// added, or a target's config defaulted differently, reaches the two
// together or reaches neither.
package compose

import (
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/markdown"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// Targets composes the targets. The order is the order families are
// rendered in. module roots the Go packages and scope names the npm scope
// the TypeScript ones are published under; the runtime the targets bind
// to is each target's default, Nightseam's own.
func Targets(module, scope, sibling string) []spi.Target {
	return []spi.Target{
		golang.New(golang.Config{Module: module}),
		typescript.New(typescript.Config{Scope: scope, Sibling: sibling}),
		doc.Target(markdown.New(markdown.Config{})),
	}
}

// Names names the targets, for the override files a family may carry. It
// answers without a module or a scope, which loading a checkout does not
// need.
func Names() []string { return []string{golang.Name, typescript.Name, markdown.Name} }

// Kernel is the pipeline composed with the targets.
func Kernel(module, scope, sibling string) *kernel.Kernel {
	return kernel.New(Targets(module, scope, sibling)...)
}
