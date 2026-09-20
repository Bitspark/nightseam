package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// boundSchema supplies every Go type argument at its exact descriptor
// name. Draws bind S.Envelope directly, retaining the supplied type's own
// schema; ordinary T has no family tag or user-supplied codec parameter.
func (f *file) boundSchema(uses []render.Use) string {
	schema := "schema"
	if f.prefix != "" {
		schema = f.proto() + identWireSchema + "()"
	}
	if len(uses) == 0 {
		return schema
	}
	var bindings []string
	for _, use := range uses {
		name := use.Parameter
		if use.Type != "" {
			name += "." + use.Type
		}
		value := fmt.Sprintf("%s.TypeArgument[%s]()", f.runtime(), parameterName(use))
		for _, codec := range f.codecs {
			if codec == use {
				value = "type" + parameterName(use)
			}
		}
		bindings = append(bindings, fmt.Sprintf("%q: %s", name, value))
	}
	return schema + ".Bind(map[string]any{" + strings.Join(bindings, ", ") + "}, nil)"
}

func (f *file) emitWireType(t *render.Type) {
	self := f.plan.types[t.Name] + apply(t.Uses)
	f.linef("func (%s) %s() %s.TypeBinding { return %s.TypeBinding{Schema: %s, Type: %s} }", self, identWireType, f.runtime(), f.runtime(), f.boundSchema(t.Uses), f.typeExpression(t))
}

func (f *file) typeExpression(t *render.Type) string {
	if t.Inline {
		return f.runtime() + ".MustTypeExpression(" + expression(model.Inline{Type: t.Declaration}) + ")"
	}
	return quote(t.Name)
}

func (f *file) validateType(t *render.Type, data string) string {
	return f.boundSchema(t.Uses) + ".ValidateExpressionRaw(" + f.typeExpression(t) + ", " + data + ")"
}
