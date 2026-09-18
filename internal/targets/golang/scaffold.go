package golang

import (
	"fmt"
	"go/format"
	"path"
	"regexp"
	"strings"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Scaffold writes the server's handler: a type implementing the binding
// package's Handler with every method returning an unimplemented error,
// for the consumer to fill in. A generic family's handler is generic in
// the same type parameters.
func (t *target) Scaffold(f *render.Family, dir string) ([]spi.File, error) {
	if err := t.config.Validate(); err != nil {
		return nil, err
	}
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("the family does not pass the Go target's check: %s", diagnostics[0])
	}
	ctx := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("\t"), imports: &emit.Imports{}, prefix: "protocol."}
	binding := ctx.use("binding", t.config.Module+"/"+expand(t.config.layout(f.Name).Binding, f.Name))
	decl, args := declare(f.Uses), apply(f.Uses)
	ctx.linef("// Handler is the behavior of the %s family's server side: what its", f.Name)
	ctx.linef("// binding's %s.Handler%s declares, one method per operation the server", binding, args)
	ctx.line("// implements. Fill the methods in; nightseam wrote this file once and will")
	ctx.line("// not touch it again.")
	ctx.linef("type Handler%s struct{}", decl)
	ctx.line("")
	if f.Generic {
		ctx.linef("func _%s() { var _ %s.Handler%s = Handler%s{} }", decl, binding, args, args)
	} else {
		ctx.linef("var _ %s.Handler = Handler{}", binding)
	}
	for _, m := range f.Server.Methods {
		ctx.line("")
		if m.Description != "" {
			ctx.linef("// %s: %s", p.operations[m.Name], m.Description)
		}
		result := ctx.spell(m.Result)
		ctx.w.Block(fmt.Sprintf("func (Handler%s) %s(ctx %s.Context, remote *%s.Remote%s%s) (%s, error) {", args, p.operations[m.Name], ctx.std("context"), binding, args, ctx.request(m), result), "}", func() {
			ctx.linef("var result %s", result)
			ctx.linef("return result, &%s.PublicError{Code: \"unimplemented\", Message: %q}", ctx.runtime(), m.Name+" is not implemented")
		})
	}
	var out strings.Builder
	fmt.Fprintf(&out, "package %s\n\n", packageName(dir))
	out.WriteString("import (\n")
	for _, i := range ctx.imports.Sorted() {
		fmt.Fprintf(&out, "\t%s %q\n", i.Alias, i.Path)
	}
	out.WriteString(")\n\n")
	out.WriteString(ctx.w.String())
	formatted, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("format scaffold: %w\n%s", err, out.String())
	}
	return []spi.File{{Path: path.Join(dir, "handler.go"), Data: formatted}}, nil
}

var notIdentifier = regexp.MustCompile(`[^a-z0-9]`)

// packageName is a Go package name for a directory: its last segment,
// lowered, with what Go refuses removed.
func packageName(dir string) string {
	name := notIdentifier.ReplaceAllString(strings.ToLower(path.Base(dir)), "")
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "impl" + name
	}
	return name
}
