package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

func converterName(use render.Use) string { return "convert_" + use.Parameter + "_" + use.Type }
func useExpression(use render.Use) model.TypeExpr {
	if use.Type == "" {
		return model.Named{Name: use.Parameter}
	}
	return model.Drawn{Parameter: use.Parameter, Name: use.Type}
}

func (f *file) converterParameters(t *render.Type, export bool) string {
	var out strings.Builder
	for _, use := range t.Uses {
		from, to := f.spell(useExpression(use)), "unknown"
		if !export {
			from, to = to, from
		}
		fmt.Fprintf(&out, ", %s: (value: %s) => %s", converterName(use), from, to)
	}
	return out.String()
}

func (f *file) parameterConverter(e model.TypeExpr) string {
	for _, use := range f.codecs {
		switch x := e.(type) {
		case model.Named:
			if use.Type == "" && x.Name == use.Parameter {
				return converterName(use)
			}
		case model.Drawn:
			if x.Parameter == use.Parameter && x.Name == use.Type {
				return converterName(use)
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

func (f *file) conversionCall(e model.TypeExpr, src string, export bool) string {
	t, arguments := f.family.Conversion(e)
	passed := []string{}
	if t.IsLive {
		passed = append(passed, "scope")
	}
	if export {
		src = "(" + src + ") as " + f.spell(e)
	}
	passed = append(passed, src)
	for _, argument := range arguments {
		expr := argument.Expression()
		from, to := f.spell(expr), "unknown"
		if !export {
			from, to = to, from
		}
		converted := f.liveExpr(expr, "input", export)
		if !export {
			converted = "(" + converted + ") as " + to
		}
		passed = append(passed, "(input: "+from+"): "+to+" => "+converted)
	}
	return f.liveCall(e, export) + f.renderArguments(arguments) + "(" + strings.Join(passed, ", ") + ")"
}
