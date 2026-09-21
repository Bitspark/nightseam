package golang

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

var _ spi.Speller = (*target)(nil)

func (t *target) Spell(f *render.Family, e model.TypeExpr) string {
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return ""
	}
	ctx := &file{plan: p, family: f, config: t.config, imports: &emit.Imports{}, prefix: "protocol.", uses: f.Uses}
	return ctx.spell(e)
}

// Declare selects the declaration from the actual formatted type emitter.
// Keeping its source slice preserves gofmt's alignment and includes every
// resolved field, type parameter and override without a second renderer.
func (t *target) Declare(f *render.Family, declaration *model.Type) string {
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return ""
	}
	name := ""
	for _, typ := range f.Types {
		if typ.Declaration == declaration {
			name = p.types[typ.Name]
			break
		}
	}
	if name == "" {
		return ""
	}
	source, err := format.Source([]byte(t.file(p, f, "protocol", emitTypes)))
	if err != nil {
		return ""
	}
	positions := token.NewFileSet()
	parsed, err := parser.ParseFile(positions, "types.go", source, 0)
	if err != nil {
		return ""
	}
	for _, d := range parsed.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, spec := range g.Specs {
			if typ, ok := spec.(*ast.TypeSpec); ok && typ.Name.Name == name {
				return string(source[positions.Position(g.Pos()).Offset:positions.Position(g.End()).Offset])
			}
		}
	}
	return ""
}

func (t *target) Invoke(f *render.Family, side, op string) spi.Invocation {
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 || !f.HasProtocol() {
		return spi.Invocation{}
	}
	operations, caller, sender := f.Server, "client", "remote"
	if side == "client" {
		operations, caller, sender = f.Client, "remote", "client"
	} else if side != "server" {
		return spi.Invocation{}
	}
	for _, m := range operations.Methods {
		if m.Name != op {
			continue
		}
		args := "ctx"
		if m.Request != nil {
			args += ", params"
		}
		out := spi.Invocation{Call: caller + ".Methods." + p.operations[op] + "(" + args + ")"}
		if side == "server" {
			stubs, err := t.Scaffold(f, "impl")
			if err != nil {
				return spi.Invocation{}
			}
			for _, stub := range stubs {
				positions := token.NewFileSet()
				parsed, err := parser.ParseFile(positions, stub.Path, stub.Data, 0)
				if err != nil {
					continue
				}
				for _, d := range parsed.Decls {
					fn, ok := d.(*ast.FuncDecl)
					if ok && fn.Recv != nil && fn.Name.Name == p.operations[op] {
						out.Handle = strings.TrimSpace(string(stub.Data[positions.Position(fn.Pos()).Offset:positions.Position(fn.Body.Pos()).Offset]))
					}
				}
			}
		}
		return out
	}
	for _, e := range operations.Events {
		if e.Name == op {
			return spi.Invocation{Call: sender + ".Events." + p.operations[op] + "(ctx, data)"}
		}
	}
	return spi.Invocation{}
}
