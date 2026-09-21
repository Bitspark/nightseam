package render

import (
	"slices"

	"github.com/Bitspark/nightseam/internal/model"
)

// Apply views an explicitly applied declaration in the caller's lexical
// scope. It substitutes arguments without changing the source declaration,
// and retains each inherited member's original identity and location.
func (r *Family) Apply(a model.Apply, scope []model.Parameter) (*Type, bool) {
	owner := r.other(a.Family)
	if owner == nil {
		return nil, false
	}
	t := owner.Type(a.Name)
	if t == nil {
		return nil, false
	}
	out := *t
	out.Scope = slices.Clone(scope)
	out.Arguments = r.arguments(a)
	out.Parameters = nil
	for _, p := range t.Parameters {
		if _, filled := a.With[p.Name]; !filled {
			out.Parameters = append(out.Parameters, p)
		}
	}
	fields := func(fields []Field) []Field {
		out := slices.Clone(fields)
		for i := range out {
			out[i].Type = r.substitute(out[i].Type, owner, a.With, scope)
			out[i].DeclaredType = r.declared(out[i].DeclaredType, owner, a.With, scope)
			out[i].Scope = slices.Clone(scope)
		}
		return out
	}
	out.Fields, out.Own = fields(t.Fields), fields(t.Own)
	out.Alias = r.substitute(t.Alias, owner, a.With, scope)
	out.Request = r.substitute(t.Request, owner, a.With, scope)
	out.Result = r.substitute(t.Result, owner, a.With, scope)
	out.Variants = slices.Clone(t.Variants)
	for i := range out.Variants {
		out.Variants[i].Type = r.substitute(t.Variants[i].Type, owner, a.With, scope)
		out.Variants[i].DeclaredType = r.declared(t.Variants[i].DeclaredType, owner, a.With, scope)
		out.Variants[i].Scope = slices.Clone(scope)
		if out.Variants[i].Origin == t.Origin {
			out.Variants[i].Arguments = slices.Clone(out.Arguments)
		} else {
			out.Variants[i].Arguments = r.boundArguments(t.Variants[i].Arguments, owner, a.With, scope)
		}
	}
	out.Bases = nil
	for _, base := range t.Bases {
		arguments := r.boundArguments(base.Type.Arguments, owner, a.With, scope)
		with := argumentBindings(arguments)
		family := base.Type.Origin.Family
		if family == r.Name {
			family = ""
		}
		if view, ok := r.Apply(model.Apply{Family: family, Name: base.Type.Origin.Declaration, With: with}, scope); ok {
			out.Bases = append(out.Bases, Base{Edge: base.Edge, Type: view})
		}
	}
	out.OwnVariants = nil
	for _, variant := range out.Variants {
		if variant.Origin == out.Origin {
			out.OwnVariants = append(out.OwnVariants, variant)
		}
	}
	var sets [][]Use
	for _, field := range out.Fields {
		sets = append(sets, r.uses(field.Type, scope))
	}
	sets = append(sets, r.uses(out.Alias, scope))
	sets = append(sets, r.uses(out.Request, scope), r.uses(out.Result, scope))
	for _, variant := range out.Variants {
		sets = append(sets, r.uses(variant.Type, scope))
	}
	out.Uses = orderedUses(scope, sets...)
	r.resolvePayloads(&out)
	return &out, true
}

func (r *Family) substitute(e model.TypeExpr, source *Family, bindings map[string]model.Filler, scope []model.Parameter) model.TypeExpr {
	return r.transform(e, source, bindings, scope, false)
}
func (r *Family) declared(e model.TypeExpr, source *Family, bindings map[string]model.Filler, scope []model.Parameter) model.TypeExpr {
	return r.transform(e, source, bindings, scope, true)
}

// substitute qualifies source-local names while leaving a supplied filler
// in the caller's scope. Qualifying after substitution would capture a
// caller's same-named type in the imported declaration's family.
func (r *Family) transform(e model.TypeExpr, source *Family, bindings map[string]model.Filler, scope []model.Parameter, preserveReferences bool) model.TypeExpr {
	switch x := e.(type) {
	case model.Named:
		if filler, ok := bindings[x.Name]; ok && filler.Type != nil {
			return r.transform(filler.Type, r, nil, scope, preserveReferences)
		}
		if t := source.Type(x.Name); t != nil {
			family := ""
			if source != r {
				family = source.Name
			}
			with := capturedArguments(t, bindings, nil)
			if len(with) != 0 {
				return model.Apply{Family: family, Name: x.Name, With: with}
			}
			if source != r {
				return model.Imported{Family: source.Name, Name: x.Name}
			}
		}
	case model.Imported:
		// A plain imported family slot uses the source's single family
		// parameter. Capture its supplied argument before leaving that scope.
		with := map[string]model.Filler{}
		for _, argument := range source.arguments(model.Apply{Family: x.Family, Name: x.Name}) {
			if filler, bound := bindings[argument.Parameter]; bound && argument.Parameter != "" {
				with[argument.Use.Parameter] = filler
			}
		}
		if len(with) != 0 {
			return model.Apply{Family: x.Family, Name: x.Name, With: with}
		}
	case model.Drawn:
		if filler, ok := bindings[x.Parameter]; ok {
			if filler.Family != "" {
				return model.Imported{Family: filler.Family, Name: x.Name}
			}
			if name := filler.Name(); name != "" {
				return model.Drawn{Parameter: name, Name: x.Name}
			}
		}
	case model.Array:
		return model.Array{Elem: r.transform(x.Elem, source, bindings, scope, preserveReferences)}
	case model.Map:
		return model.Map{Elem: r.transform(x.Elem, source, bindings, scope, preserveReferences)}
	case model.Nullable:
		return model.Nullable{Elem: r.transform(x.Elem, source, bindings, scope, preserveReferences)}
	case model.Apply:
		with := map[string]model.Filler{}
		for name, filler := range x.With {
			if outer, ok := bindings[filler.Name()]; ok {
				filler = outer
			} else if filler.Type != nil {
				filler.Type = r.transform(filler.Type, source, bindings, scope, preserveReferences)
			}
			with[name] = filler
		}
		family := x.Family
		if family == "" {
			if source != r {
				family = source.Name
			}
			with = capturedArguments(source.Type(x.Name), bindings, with)
		}
		return model.Apply{Family: family, Name: x.Name, With: with}
	case model.Ref:
		if preserveReferences {
			if x.Family == "" {
				x.Family = source.Name
			}
			return x
		}
		owner := source
		if x.Family != "" {
			owner = r.other(x.Family)
		}
		if owner != nil && owner != r {
			if entity := owner.Type(x.Entity); entity != nil {
				for _, field := range entity.Fields {
					if field.Name == entity.Key {
						return r.transform(field.Type, owner, bindings, scope, preserveReferences)
					}
				}
			}
		}
	case model.Inline:
		clone := *x.Type
		clone.Fields = slices.Clone(x.Type.Fields)
		clone.Variants = slices.Clone(x.Type.Variants)
		if original := source.InlineType(x); original != nil {
			family := source.Name
			if source == r {
				family = ""
			}
			if view, ok := r.Apply(model.Apply{Family: family, Name: original.Name, With: bindings}, scope); ok {
				for i := range clone.Fields {
					clone.Fields[i].Type = view.Own[i].Type
					if preserveReferences {
						clone.Fields[i].Type = view.Own[i].DeclaredType
					}
				}
				for i := range clone.Variants {
					clone.Variants[i].Type = view.OwnVariants[i].Type
					if preserveReferences {
						clone.Variants[i].Type = view.OwnVariants[i].DeclaredType
					}
				}
				r.inlines[&clone] = view
				return model.Inline{Type: &clone}
			}
		}
		for i := range clone.Fields {
			clone.Fields[i].Type = r.transform(clone.Fields[i].Type, source, bindings, scope, preserveReferences)
		}
		for i := range clone.Variants {
			clone.Variants[i].Type = r.transform(clone.Variants[i].Type, source, bindings, scope, preserveReferences)
		}
		return model.Inline{Type: &clone}
	}
	return e
}

// capturedArguments carries used enclosing parameters across the family
// boundary. Arguments declared on the nested application take precedence;
// they were already substituted in their own lexical scope.
func capturedArguments(t *Type, bindings, with map[string]model.Filler) map[string]model.Filler {
	if t == nil {
		return with
	}
	for _, use := range t.Uses {
		if _, supplied := with[use.Parameter]; supplied {
			continue
		}
		if filler, bound := bindings[use.Parameter]; bound {
			if with == nil {
				with = map[string]model.Filler{}
			}
			with[use.Parameter] = filler
		}
	}
	return with
}

func (r *Family) resolvePayloads(t *Type) {
	for i := range t.Variants {
		v := &t.Variants[i]
		v.Form = VariantValue
		if v.Type == nil {
			v.Form = VariantEmpty
		}
		v.Payload = r.recordPayload(v.Type, t.Scope, map[string]bool{})
		v.Fields = nil
		if v.Payload != nil {
			v.Fields = slices.Clone(v.Payload.Fields)
		}
	}
	for i := range t.OwnVariants {
		for _, v := range t.Variants {
			if v.Origin == t.OwnVariants[i].Origin && v.Tag == t.OwnVariants[i].Tag {
				t.OwnVariants[i] = v
				break
			}
		}
	}
}

// recordPayload resolves only a non-null record/entity, never a map, union,
// or nullable record. Every shape still has the same adjacent wire carrier.
func (r *Family) recordPayload(e model.TypeExpr, scope []model.Parameter, seen map[string]bool) *Type {
	source := r
	var t *Type
	var application *model.Apply
	switch x := e.(type) {
	case model.Named:
		if _, isParameter := parameter(scope, x.Name); isParameter {
			return nil
		}
		t = r.Type(x.Name)
	case model.Imported:
		source = r.other(x.Family)
		if source == nil {
			return nil
		}
		t = source.Type(x.Name)
	case model.Apply:
		source = r.other(x.Family)
		if source == nil {
			return nil
		}
		t = source.Type(x.Name)
		application = &x
	case model.Inline:
		t = r.InlineType(x)
	default:
		return nil
	}
	if t == nil {
		return nil
	}
	key := source.Name + "." + t.Name + ":" + model.String(e)
	if seen[key] {
		return nil
	}
	seen[key] = true
	defer delete(seen, key)
	if t.Kind == model.KindAlias {
		var bindings map[string]model.Filler
		if application != nil {
			bindings = application.With
		}
		return r.recordPayload(r.substitute(t.Alias, source, bindings, scope), scope, seen)
	}
	if t.Kind != model.KindRecord && t.Kind != model.KindEntity {
		return nil
	}
	if application != nil {
		view, _ := r.Apply(*application, scope)
		return view
	}
	return t
}
