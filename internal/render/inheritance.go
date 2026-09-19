package render

import (
	"slices"

	"github.com/Bitspark/nightseam/internal/model"
)

func (r *Family) completeType(t *Type, stack map[*Type]bool) {
	if t == nil || t.resolved || stack[t] {
		return
	}
	stack[t] = true
	defer delete(stack, t)
	t.Fields, t.Variants, t.OwnVariants, t.Bases = nil, nil, nil, nil
	for _, edge := range t.Extends {
		owner, declaration, ok := r.f.Base(edge)
		if !ok {
			continue
		}
		source := r.other(owner.Name)
		base := source.Type(declaration.Name)
		source.completeType(base, stack)
		family := source.Name
		if source == r {
			family = ""
		}
		view, ok := r.Apply(model.Apply{Family: family, Name: base.Name, With: edge.With}, t.Scope)
		if !ok {
			continue
		}
		t.Bases = append(t.Bases, Base{Edge: edge, Type: view})
		t.Fields = append(t.Fields, view.Fields...)
		t.Variants = append(t.Variants, view.Variants...)
	}
	t.Fields = append(t.Fields, t.Own...)
	for _, v := range t.Declaration.Variants {
		variant := Variant{Variant: v, Origin: t.Origin, DeclaredType: v.Type, Scope: slices.Clone(t.Scope)}
		t.Variants = append(t.Variants, variant)
		t.OwnVariants = append(t.OwnVariants, variant)
	}
	// A checked diamond may reach the same declaration with the same binding.
	// The checker refuses independently declared tags and differing bindings.
	type key struct {
		Origin       Origin
		Tag, Payload string
	}
	seen := map[key]bool{}
	var variants []Variant
	for _, variant := range t.Variants {
		id := key{variant.Origin, variant.Tag, model.String(variant.Type)}
		if !seen[id] {
			seen[id] = true
			variants = append(variants, variant)
		}
	}
	t.Variants = variants
	t.resolved = true
}

func (r *Family) bindArguments(with map[string]model.Filler, source *Family, bindings map[string]model.Filler, scope []model.Parameter) map[string]model.Filler {
	if with == nil {
		return nil
	}
	out := map[string]model.Filler{}
	for name, filler := range with {
		if outer, ok := bindings[filler.Name()]; ok && filler.Family == "" {
			out[name] = outer
		} else {
			filler.Type = r.declared(filler.Type, source, bindings, scope)
			out[name] = filler
		}
	}
	return out
}

func argumentBindings(arguments []Argument) map[string]model.Filler {
	with := map[string]model.Filler{}
	for _, argument := range arguments {
		filler := model.Filler{Type: argument.Type, Family: argument.Family}
		if argument.Parameter != "" {
			filler.Type = model.Named{Name: argument.Parameter}
		}
		with[argument.Use.Parameter] = filler
	}
	return with
}

func (r *Family) boundArguments(arguments []Argument, source *Family, bindings map[string]model.Filler, scope []model.Parameter) []Argument {
	out := slices.Clone(arguments)
	for i, arg := range out {
		if arg.Type != nil {
			out[i].Type = r.substitute(arg.Type, source, bindings, scope)
		}
		if arg.Parameter != "" {
			if filler, ok := bindings[arg.Parameter]; ok {
				out[i].Parameter = ""
				if filler.Family != "" {
					out[i].Family = filler.Family
				} else {
					out[i].Parameter = filler.Name()
				}
			}
		}
	}
	return out
}
