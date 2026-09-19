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
		}
		return out
	}
	out.Fields, out.Own = fields(t.Fields), fields(t.Own)
	out.Alias = r.substitute(t.Alias, owner, a.With, scope)
	out.Variants = slices.Clone(t.Variants)
	for i := range out.Variants {
		out.Variants[i].Type = r.substitute(t.Variants[i].Type, owner, a.With, scope)
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
	for _, variant := range out.Variants {
		sets = append(sets, r.uses(variant.Type, scope))
	}
	out.Uses = orderedUses(scope, sets...)
	r.resolvePayloads(&out)
	return &out, true
}

// substitute qualifies source-local names while leaving a supplied filler
// in the caller's scope. Qualifying after substitution would capture a
// caller's same-named type in the imported declaration's family.
func (r *Family) substitute(e model.TypeExpr, source *Family, bindings map[string]model.Filler, scope []model.Parameter) model.TypeExpr {
	switch x := e.(type) {
	case model.Named:
		if filler, ok := bindings[x.Name]; ok && filler.Type != nil {
			return filler.Type
		}
		if source != r {
			if t := source.Type(x.Name); t != nil {
				with := capturedArguments(t, bindings, nil)
				if len(with) != 0 {
					return model.Apply{Family: source.Name, Name: x.Name, With: with}
				}
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
		return model.Array{Elem: r.substitute(x.Elem, source, bindings, scope)}
	case model.Map:
		return model.Map{Elem: r.substitute(x.Elem, source, bindings, scope)}
	case model.Nullable:
		return model.Nullable{Elem: r.substitute(x.Elem, source, bindings, scope)}
	case model.Apply:
		with := map[string]model.Filler{}
		for name, filler := range x.With {
			if outer, ok := bindings[filler.Name()]; ok {
				filler = outer
			} else if filler.Type != nil {
				filler.Type = r.substitute(filler.Type, source, bindings, scope)
			}
			with[name] = filler
		}
		family := x.Family
		if family == "" && source != r {
			family = source.Name
			with = capturedArguments(source.Type(x.Name), bindings, with)
		}
		return model.Apply{Family: family, Name: x.Name, With: with}
	case model.Ref:
		if source != r {
			if entity := source.Type(x.Entity); entity != nil {
				for _, field := range entity.Fields {
					if field.Name == entity.Key {
						return r.substitute(field.Type, source, bindings, scope)
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
				}
				for i := range clone.Variants {
					clone.Variants[i].Type = view.OwnVariants[i].Type
				}
				r.inlines[&clone] = view
				return model.Inline{Type: &clone}
			}
		}
		for i := range clone.Fields {
			clone.Fields[i].Type = r.substitute(clone.Fields[i].Type, source, bindings, scope)
		}
		for i := range clone.Variants {
			clone.Variants[i].Type = r.substitute(clone.Variants[i].Type, source, bindings, scope)
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
		v.Form, v.Fields = r.payload(v.Type, t.Scope, map[string]bool{})
		if v.Form == VariantObject {
			for _, field := range v.Fields {
				if literal, ok := field.Type.(model.Literal); ok && field.Name == t.Tag && literal.Value == v.Tag && field.Required && !field.Nullable {
					v.Form = VariantTagged
				}
			}
		}
	}
	for i := range t.OwnVariants {
		for _, variant := range t.Variants {
			if variant.Origin == t.OwnVariants[i].Origin && variant.Tag == t.OwnVariants[i].Tag {
				t.OwnVariants[i] = variant
				break
			}
		}
	}
}

func (r *Family) payload(e model.TypeExpr, scope []model.Parameter, seen map[string]bool) (VariantForm, []Field) {
	var t *Type
	source := r
	switch x := e.(type) {
	case model.Named:
		if _, ok := parameter(scope, x.Name); ok {
			return VariantDynamic, nil
		}
		t = r.Type(x.Name)
	case model.Imported:
		source = r.other(x.Family)
		if source != nil {
			t = source.Type(x.Name)
		}
	case model.Apply:
		// Resolve an application without recursively resolving its variants:
		// payload recursion below owns the visited set.
		source = r.other(x.Family)
		if source == nil {
			return VariantValue, nil
		}
		original := source.Type(x.Name)
		if original == nil {
			return VariantValue, nil
		}
		clone := *original
		clone.Fields = slices.Clone(original.Fields)
		for i := range clone.Fields {
			clone.Fields[i].Type = r.substitute(clone.Fields[i].Type, source, x.With, scope)
		}
		clone.Alias = r.substitute(original.Alias, source, x.With, scope)
		t, source = &clone, r
	case model.Inline:
		t = &Type{Kind: x.Type.Kind, At: x.Type.At}
		for _, f := range x.Type.Fields {
			t.Fields = append(t.Fields, field("", f))
		}
		if original := r.InlineType(x); original != nil {
			t.Name, t.Origin = original.Name, original.Origin
			for i := range t.Fields {
				t.Fields[i].Owner = original.Name
				t.Fields[i].Origin = Origin{Family: original.Origin.Family, Declaration: original.Name, At: t.Fields[i].At}
			}
		}
	case model.Drawn:
		return VariantDynamic, nil
	case model.Map:
		return VariantObject, nil
	case model.Nullable:
		form, fields := r.payload(x.Elem, scope, seen)
		if form == VariantValue {
			return VariantValue, nil
		}
		return VariantDynamic, fields
	case model.Primitive:
		if x == "json" {
			return VariantDynamic, nil
		}
		return VariantValue, nil
	default:
		return VariantValue, nil
	}
	if t == nil {
		return VariantValue, nil
	}
	if t.Kind == model.KindAlias {
		key := source.Name + "." + t.Name + ":" + model.String(e)
		if seen[key] {
			return VariantDynamic, nil
		}
		seen[key] = true
		defer delete(seen, key)
		return r.payload(r.qualify(t.Alias, source), scope, seen)
	}
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		fields := slices.Clone(t.Fields)
		for i := range fields {
			fields[i].Type = r.qualify(fields[i].Type, source)
		}
		return VariantObject, fields
	case model.KindUnion:
		return VariantObject, nil
	}
	return VariantValue, nil
}
