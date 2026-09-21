package typescript

import (
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
	ctx := &file{plan: p, family: f, config: t.config, prefix: "Protocol.", scope: familyScope(f)}
	return ctx.spell(e)
}

func (t *target) Declare(f *render.Family, declaration *model.Type) string {
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return ""
	}
	for _, typ := range f.Types {
		if typ.Declaration != declaration {
			continue
		}
		ctx := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("  ")}
		ctx.emitType(typ)
		return strings.TrimSpace(ctx.w.String())
	}
	return ""
}

func (t *target) Invoke(f *render.Family, side, op string) spi.Invocation {
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 || !f.HasProtocol() {
		return spi.Invocation{}
	}
	operations := f.Server
	if side == "client" {
		operations = f.Client
	} else if side != "server" {
		return spi.Invocation{}
	}
	for _, m := range operations.Methods {
		if m.Name != op {
			continue
		}
		if side == "client" {
			return spi.Invocation{Handle: scaffoldSignature(p, m)}
		}
		args := "{}"
		if m.Request != nil {
			args = "params"
		}
		return spi.Invocation{Call: "await server.methods." + p.operations[op] + "(" + args + ")"}
	}
	for _, e := range operations.Events {
		if e.Name != op {
			continue
		}
		if side == "server" {
			return spi.Invocation{Handle: p.operations[op] + "(data, context)"}
		}
		return spi.Invocation{Call: "await server.events." + p.operations[op] + "(data)"}
	}
	return spi.Invocation{}
}

func scaffoldSignature(p *plan, m render.Method) string {
	return "async " + p.operations[m.Name] + "(params, context)"
}
