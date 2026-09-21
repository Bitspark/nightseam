package typescript

import (
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

func familyScope(f *render.Family) []model.Parameter {
	var out []model.Parameter
	for _, parameter := range f.Parameters {
		out = append(out, model.Parameter{Name: parameter.Name, Of: parameter.Of, At: parameter.At})
	}
	return out
}

func (f *file) parameter(name string) (model.Parameter, bool) {
	for _, parameter := range f.scope {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return model.Parameter{}, false
}

func (f *file) bindingType(name string) string {
	parameter, _ := f.parameter(name)
	if parameter.IsFamily() {
		return identFamilyBinding + "<" + name + ">"
	}
	return "ValueAdapter<" + name + ">"
}

// Type parameters admit a value type; family parameters expose associated
// types. A default at every slot also allows either kind to precede another.
func (f *file) declare(uses []render.Use) string {
	var declarations []string
	for _, name := range parameters(uses) {
		parameter, _ := f.parameter(name)
		if !parameter.IsFamily() {
			declarations = append(declarations, name+" = unknown")
			continue
		}
		var drawn []string
		for _, use := range uses {
			if use.Parameter == name && !model.Carried(use.Type) {
				drawn = append(drawn, quote(use.Type)+": unknown")
			}
		}
		constraint := identAnyFamily
		if len(drawn) > 0 {
			constraint += " & { " + strings.Join(drawn, "; ") + " }"
		}
		declarations = append(declarations, name+" extends "+constraint+" = "+constraint)
	}
	if len(declarations) == 0 {
		return ""
	}
	return "<" + strings.Join(declarations, ", ") + ">"
}

// typeName reads an imported declaration's override in its source family.
func (f *file) typeName(family, name string) string {
	if family == "" || family == f.family.Name {
		return f.prefix + f.plan.types[name]
	}
	if source := f.family.ReferencedFamily(family); source != nil {
		if overridden, ok := source.Override(Name, name); ok {
			name = overridden
		}
	}
	return alias(family) + "." + name
}

func (f *file) typeArguments(a model.Apply) string {
	return f.renderArguments(f.family.Arguments(a))
}

func (f *file) renderArguments(arguments []render.Argument) string {
	var args []string
	seen := map[string]bool{}
	for _, argument := range arguments {
		if seen[argument.Use.Parameter] {
			continue
		}
		seen[argument.Use.Parameter] = true
		switch {
		case argument.Type != nil:
			args = append(args, f.spell(argument.Type))
		case argument.Parameter != "":
			args = append(args, argument.Parameter)
		default:
			args = append(args, alias(argument.Family)+"."+identFamily)
		}
	}
	if len(args) == 0 {
		return ""
	}
	return "<" + strings.Join(args, ", ") + ">"
}

// spell renders every admitted expression from the shared resolved facts.
func (f *file) spell(e model.TypeExpr) string {
	switch x := e.(type) {
	case model.Primitive:
		switch x {
		case "string", "boolean":
			return string(x)
		case "number", "integer":
			return "number"
		case "timestamp":
			return "string"
		default:
			return "unknown"
		}
	case model.Named:
		if _, parameter := f.parameter(x.Name); parameter {
			return x.Name
		}
		return f.typeName("", x.Name) + f.typeArguments(model.Apply{Name: x.Name})
	case model.Imported:
		return f.typeName(x.Family, x.Name) + f.typeArguments(model.Apply{Family: x.Family, Name: x.Name})
	case model.Drawn:
		return x.Parameter + "[" + quote(x.Name) + "]"
	case model.Array:
		return "Array<" + f.spell(x.Elem) + ">"
	case model.Map:
		return "Record<string, " + f.spell(x.Elem) + ">"
	case model.Nullable:
		return f.spell(x.Elem) + " | null"
	case model.Literal:
		return quote(x.Value)
	case model.Ref:
		source := f.family.ReferencedFamily(x.Family)
		if source != nil {
			if entity := source.Type(x.Entity); entity != nil {
				for _, field := range entity.Fields {
					if field.Name == entity.Key {
						return f.spell(field.Type)
					}
				}
			}
		}
	case model.Apply:
		return f.typeName(x.Family, x.Name) + f.typeArguments(x)
	case model.Inline:
		if t := f.family.InlineType(x); t != nil {
			if len(t.Arguments) != 0 {
				return f.typeName(t.Origin.Family, t.Name) + f.renderArguments(t.Arguments)
			}
			return f.typeName(t.Origin.Family, t.Name) + apply(t.Uses)
		}
	}
	return "unknown"
}
