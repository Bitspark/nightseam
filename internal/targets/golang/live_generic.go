package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// liveBoundary keeps the caller's error mapping while expressions (including
// anonymous containers and applications) use the same recursive conversion.
func (f *file) liveBoundary(e model.TypeExpr, src, dst string, export bool) {
	result := f.spell(e)
	if export {
		result = f.std("json") + ".RawMessage"
	}
	open, close := fmt.Sprintf("%s, err := func() (%s, error) {", dst, result), "}()"
	if export {
		open, close = fmt.Sprintf("%s, err := scope.ExportValue(func(scope *%s.Scope) (%s, error) {", dst, f.live(), result), "})"
	}
	f.w.Block(open, close, func() {
		f.linef("var zero %s", result)
		if !export {
			f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(e, src))
		}
		f.liveExpr(e, src, "converted", export, "zero")
		if export {
			f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(e, "converted"))
		}
		f.line("return converted, nil")
	})
}

// A generic data declaration has no live dependency. Its conversion helpers
// take per-argument codecs; a live caller closes those codecs over its scope.
func (f *file) converterParameters(t *render.Type, export bool) string {
	var out strings.Builder
	for _, use := range t.Uses {
		name := parameterName(use)
		from, to := name, f.std("json")+".RawMessage"
		if !export {
			from, to = to, from
		}
		if export && t.IsLive {
			from = "*" + f.live() + ".Scope, " + from
		}
		fmt.Fprintf(&out, ", convert%s func(%s) (%s, error), type%s %s.TypeBinding", name, from, to, name, f.runtime())
	}
	return out.String()
}

func (f *file) parameterConverter(e model.TypeExpr) string {
	for _, use := range f.codecs {
		switch x := e.(type) {
		case model.Named:
			if use.Type == "" && x.Name == use.Parameter {
				return "convert" + parameterName(use)
			}
		case model.Drawn:
			if x.Parameter == use.Parameter && x.Name == use.Type {
				return "convert" + parameterName(use)
			}
		}
	}
	return ""
}

func (f *file) needsConversion(e model.TypeExpr) bool {
	if f.parameterConverter(e) != "" {
		return true
	}
	switch x := e.(type) {
	case model.Array:
		return f.needsConversion(x.Elem)
	case model.Map:
		return f.needsConversion(x.Elem)
	case model.Nullable:
		return f.needsConversion(x.Elem)
	}
	if t, args := f.family.Conversion(e); t != nil && len(f.codecs) > 0 && len(args) > 0 {
		return true
	}
	return f.family.IsLive(e)
}

func (f *file) conversionCall(e model.TypeExpr, src, dst string, export bool) (string, string) {
	t, arguments := f.family.Conversion(e)
	call := f.liveCall(e, export) + f.arguments(arguments)
	passed := []string{}
	if t.IsLive {
		passed = append(passed, "scope")
	}
	passed = append(passed, src)
	for i, argument := range arguments {
		expr := argument.Expression()
		name := fmt.Sprintf("%sConvert%d", dst, i)
		from, to := f.spell(expr), f.std("json")+".RawMessage"
		if !export {
			from, to = to, from
		}
		parameters := "input " + from
		if export && t.IsLive {
			parameters = "scope *" + f.live() + ".Scope, " + parameters
		}
		f.w.Block(fmt.Sprintf("%s := func(%s) (%s, error) {", name, parameters, to), "}", func() {
			fail := "nil"
			if !export {
				f.linef("var zero %s", to)
				fail = "zero"
			}
			f.liveExpr(expr, "input", "converted", export, fail)
			f.line("return converted, nil")
		})
		binding := f.runtime() + ".TypeBinding{Schema: " + f.boundSchema(f.uses) + ", Type: " + f.runtime() + ".MustTypeExpression(" + expression(expr) + ")}"
		passed = append(passed, name, binding)
	}
	return call, strings.Join(passed, ", ")
}
