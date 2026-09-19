package golang

import (
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

type literalPlan struct {
	name, constant string
}

// A literal has a Go type of its own so an instantiated generic codec
// can recover its single permitted value. Equal literals share that type;
// aliases retain its metadata, including when nested in containers.
func (p *plan) planLiterals() {
	p.literals = map[string]literalPlan{}
	visit := func(e model.TypeExpr, at diag.Location) {
		model.Walk(e, func(e model.TypeExpr) bool {
			literal, ok := e.(model.Literal)
			if !ok {
				return true
			}
			if _, seen := p.literals[literal.Value]; seen {
				return false
			}
			name := "Literal" + naming.UpperCamel(literal.Value)
			planned := literalPlan{name: name, constant: name + "Value"}
			p.literals[literal.Value] = planned
			for _, declaration := range []struct{ name, what string }{{planned.name, "literal type"}, {planned.constant, "literal constant"}} {
				if p.identifier(declaration.name, at, declaration.what, true) {
					p.declare(p.packages, declaration.name, at, declaration.what)
				}
			}
			return false
		})
	}
	for _, t := range p.family.Types {
		for _, field := range t.Fields {
			visit(field.Type, field.At.Sub("type"))
		}
		visit(t.Alias, t.At.Sub("type"))
		for _, variant := range t.Variants {
			visit(variant.Type, variant.At)
		}
	}
	for _, side := range []render.Side{p.family.Server, p.family.Client} {
		for _, method := range side.Methods {
			visit(method.Request, method.At.Sub("request"))
			visit(method.Result, method.At.Sub("result"))
		}
		for _, event := range side.Events {
			visit(event.Type, event.At.Sub("type"))
		}
	}
}
