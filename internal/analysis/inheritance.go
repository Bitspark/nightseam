package analysis

import (
	"slices"

	"github.com/Bitspark/nightseam/internal/model"
)

// Base resolves an inheritance target without discarding its source family.
func (f *Family) Base(edge model.Inheritance) (*Family, *model.Type, bool) {
	switch e := edge.Expression().(type) {
	case model.Named:
		t := f.Types[e.Name]
		return f, t, t != nil
	case model.Imported:
		if source := f.Imported[e.Family]; source != nil {
			t := source.Types[e.Name]
			return source, t, t != nil
		}
	case model.Apply:
		if e.Family == "" {
			t := f.Types[e.Name]
			return f, t, t != nil
		}
		if source := f.Imported[e.Family]; source != nil {
			t := source.Types[e.Name]
			return source, t, t != nil
		}
	}
	return nil, nil, false
}

// TypeParameters includes a type's own parameters and the family parameters
// that its members capture, in lexical declaration order.
func (f *Family) TypeParameters(t *model.Type) []model.Parameter {
	used := map[string]bool{}
	for _, use := range f.Generics().Types[t.Name] {
		used[use.Parameter] = true
	}
	var parameters []model.Parameter
	for _, parameter := range f.Parameters() {
		if used[parameter.Name] {
			parameters = append(parameters, parameter)
		}
	}
	return append(parameters, t.Parameters...)
}

// BindExpression views a source expression in this family's scope. Supplied
// arguments already belong to the caller and are never captured again by
// same-named parameters in the source declaration.
func (f *Family) BindExpression(e model.TypeExpr, source *Family, with map[string]model.Filler) model.TypeExpr {
	switch x := e.(type) {
	case model.Named:
		if filler, ok := with[x.Name]; ok && filler.Type != nil {
			return f.BindExpression(filler.Type, f, nil)
		}
		if source.Types[x.Name] != nil {
			bindings := map[string]model.Filler{}
			for _, use := range source.Generics().Types[x.Name] {
				if filler, ok := with[use.Parameter]; ok {
					bindings[use.Parameter] = filler
				}
			}
			if len(bindings) > 0 {
				family := ""
				if source != f {
					family = source.Name
				}
				return model.Apply{Family: family, Name: x.Name, With: bindings}
			}
			if source != f {
				return model.Imported{Family: source.Name, Name: x.Name}
			}
		}
	case model.Drawn:
		if filler, ok := with[x.Parameter]; ok {
			if filler.Family != "" {
				return model.Imported{Family: filler.Family, Name: x.Name}
			}
			if name := filler.Name(); name != "" {
				return model.Drawn{Parameter: name, Name: x.Name}
			}
		}
	case model.Ref:
		if x.Family == "" {
			x.Family = source.Name
		}
		return x
	case model.Array:
		return model.Array{Elem: f.BindExpression(x.Elem, source, with)}
	case model.Map:
		return model.Map{Elem: f.BindExpression(x.Elem, source, with)}
	case model.Nullable:
		return model.Nullable{Elem: f.BindExpression(x.Elem, source, with)}
	case model.Apply:
		bindings := f.BindArguments(x.With, source, with)
		if x.Family == "" {
			if source != f {
				x.Family = source.Name
			}
			for _, use := range source.Generics().Types[x.Name] {
				if _, supplied := bindings[use.Parameter]; supplied {
					continue
				}
				if filler, ok := with[use.Parameter]; ok {
					bindings[use.Parameter] = filler
				}
			}
		}
		x.With = bindings
		return x
	case model.Inline:
		clone := *x.Type
		clone.Fields = slices.Clone(x.Type.Fields)
		clone.Variants = slices.Clone(x.Type.Variants)
		for i := range clone.Fields {
			clone.Fields[i].Type = f.BindExpression(clone.Fields[i].Type, source, with)
		}
		for i := range clone.Variants {
			clone.Variants[i].Type = f.BindExpression(clone.Variants[i].Type, source, with)
		}
		return model.Inline{Type: &clone}
	}
	return e
}

// BindArguments composes one inheritance edge with the caller's bindings.
func (f *Family) BindArguments(arguments map[string]model.Filler, source *Family, with map[string]model.Filler) map[string]model.Filler {
	if arguments == nil {
		return nil
	}
	out := map[string]model.Filler{}
	for name, filler := range arguments {
		if outer, ok := with[filler.Name()]; ok && filler.Family == "" {
			out[name] = outer
		} else {
			filler.Type = f.BindExpression(filler.Type, source, with)
			out[name] = filler
		}
	}
	return out
}
