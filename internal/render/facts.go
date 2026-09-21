package render

import (
	"slices"
	"sort"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
)

type builder struct {
	families map[*analysis.Family]*Family
}

// ReferencedFamily exposes source declarations and their target overrides
// through the same resolved world, including transitive inherited sources.
func (r *Family) ReferencedFamily(name string) *Family { return r.other(name) }

func (r *Family) other(name string) *Family {
	if name == "" || name == r.Name {
		return r
	}
	if f := r.f.Imported[name]; f != nil {
		return r.builder.build(f)
	}
	seen := map[*analysis.Family]bool{}
	var find func(*analysis.Family) *analysis.Family
	find = func(f *analysis.Family) *analysis.Family {
		if seen[f] {
			return nil
		}
		seen[f] = true
		if f.Name == name {
			return f
		}
		for _, imported := range f.Imported {
			if found := find(imported); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(r.f); found != nil {
		return r.builder.build(found)
	}
	return nil
}

// InlineType associates an original inline expression with the declaration
// its target renders. Its source location, rather than a rewritten name,
// remains the identity used by diagnostics and the wire descriptor.
func (r *Family) InlineType(inline model.Inline) *Type {
	if t := r.inlines[inline.Type]; t != nil {
		return t
	}
	for _, family := range r.builder.families {
		if t := family.inlines[inline.Type]; t != nil {
			return t
		}
	}
	return nil
}

func (r *Family) complete() {
	for _, name := range r.References {
		r.other(name)
	}
	for _, inline := range r.f.Inlines() {
		t := inline.Type
		scope := slices.Clone(r.f.Parameters())
		if len(inline.Path) > 0 {
			if parent := r.Type(inline.Path[0]); parent != nil {
				scope = slices.Clone(parent.Scope)
			}
		}
		rt := &Type{Name: inline.Name, Kind: t.Kind, Description: t.Description, Key: t.Key,
			Parameters: t.Parameters, Scope: scope, Open: t.Open, Values: t.Values,
			Alias: t.Alias, Tag: t.Tag, Value: t.ValueMember(), Declaration: t,
			Origin: Origin{Family: r.Name, Declaration: inline.Name, At: t.At}, Inline: true, At: t.At}
		for _, own := range t.Fields {
			rt.Own = append(rt.Own, field(rt.Name, own))
		}
		rt.Fields = slices.Clone(rt.Own)
		r.Types = append(r.Types, rt)
		r.types[rt.Name] = rt
		r.inlines[t] = rt
	}
	sort.Slice(r.Types, func(i, j int) bool { return r.Types[i].Name < r.Types[j].Name })
	for _, t := range r.Types {
		for i := range t.Fields {
			at := &t.Fields[i]
			at.Origin = Origin{Family: t.Origin.Family, Declaration: at.Owner, At: at.At}
		}
		for i := range t.Own {
			at := &t.Own[i]
			at.Origin = Origin{Family: t.Origin.Family, Declaration: t.Name, At: at.At}
			at.DeclaredType = at.Type
			at.Scope = slices.Clone(t.Scope)
		}
	}
	r.resolveUses()
	for _, t := range r.Types {
		r.completeType(t, map[*Type]bool{})
	}
	if r.f.Protocol != nil {
		r.Server = r.resolvedSide(true)
		r.Client = r.resolvedSide(false)
		r.Errors = r.resolvedErrors()
	}
	r.Live = r.f.Live != nil
	r.Callables = r.f.Callables()
	r.LiveTypes = r.f.LiveTypes()
	live := map[string]bool{}
	for _, name := range r.LiveTypes {
		live[name] = true
	}
	for _, t := range r.Types {
		t.IsLive = live[t.Name] || r.f.IsLive(model.Inline{Type: t.Declaration})
		if t.Kind != model.KindCallable {
			continue
		}
		t.Request, t.Result = t.Declaration.Request, t.Declaration.Result
		t.Contract = Contract(r.f.CarriedFrom(t.Name), r.Name, t.Name)
	}
	r.resolveUses()
	for _, t := range r.Types {
		r.resolvePayloads(t)
	}
	r.surfaceReferences()
}

func (r *Family) surfaceReferences() {
	names := map[string]bool{}
	add := func(name string) {
		if name != "" && name != r.Name {
			names[name] = true
		}
	}
	for _, name := range r.References {
		names[name] = true
	}
	visit := func(e model.TypeExpr) {
		model.Walk(e, func(e model.TypeExpr) bool {
			switch x := e.(type) {
			case model.Imported:
				if x.Family != r.Name {
					names[x.Family] = true
				}
			case model.Apply:
				if x.Family != "" && x.Family != r.Name {
					names[x.Family] = true
				}
				for _, filler := range x.With {
					if filler.Family != "" && filler.Family != r.Name {
						names[filler.Family] = true
					}
				}
			}
			return true
		})
	}
	arguments := func(arguments []Argument) {
		for _, argument := range arguments {
			visit(argument.Type)
			add(argument.Family)
		}
	}
	for _, t := range r.Types {
		for _, field := range t.Fields {
			visit(field.Type)
		}
		visit(t.Alias)
		visit(t.Request)
		visit(t.Result)
		for _, base := range t.Bases {
			add(base.Type.Origin.Family)
			arguments(base.Type.Arguments)
		}
		for _, variant := range t.Variants {
			visit(variant.Type)
			add(variant.Origin.Family)
			arguments(variant.Arguments)
			if variant.Payload != nil {
				// A carried payload is rendered by its carrying family. Its
				// built-in origin records provenance, not a package dependency.
				if !variant.Payload.Carried {
					add(variant.Payload.Origin.Family)
				}
				arguments(variant.Payload.Arguments)
			}
		}
	}
	for _, side := range []Side{r.Server, r.Client} {
		for _, m := range side.Methods {
			visit(m.Request)
			visit(m.Result)
		}
		for _, e := range side.Events {
			visit(e.Type)
		}
	}
	r.References = nil
	for name := range names {
		r.References = append(r.References, name)
	}
	sort.Strings(r.References)
}

func (r *Family) resolvedSide(server bool) Side {
	declared := r.f.Protocol.Server
	if !server {
		declared = r.f.Protocol.Client
	}
	out := Side{Extends: slices.Clone(declared.Extends), At: declared.At}
	scope := r.f.Parameters()
	seenMethods := map[*model.Method]bool{}
	seenEvents := map[*model.Event]bool{}
	var visit func(*Family, map[string]model.Filler, map[*Family]bool)
	visit = func(source *Family, bindings map[string]model.Filler, stack map[*Family]bool) {
		if source == nil || source.f.Protocol == nil || stack[source] {
			return
		}
		stack[source] = true
		defer delete(stack, source)
		side := source.f.Protocol.Server
		if !server {
			side = source.f.Protocol.Client
		}
		for _, edge := range side.Extends {
			visit(source.other(edge.Name), r.bindArguments(edge.With, source, bindings, scope), stack)
		}
		for i := range side.Methods {
			m := &side.Methods[i]
			if seenMethods[m] {
				continue
			}
			seenMethods[m] = true
			bound := *m
			bound.Request = r.declared(m.Request, source, bindings, scope)
			bound.Result = r.declared(m.Result, source, bindings, scope)
			method := Method{Name: m.Name, Description: m.Description, Request: r.substitute(m.Request, source, bindings, scope), Result: r.substitute(m.Result, source, bindings, scope), Errors: slices.Clone(m.Errors), At: m.At, Declaration: m, BoundDeclaration: &bound, Scope: slices.Clone(scope), Origin: Origin{Family: source.Name, Declaration: m.Name, At: m.At}}
			out.Methods = append(out.Methods, method)
			if source == r {
				out.OwnMethods = append(out.OwnMethods, method)
			}
		}
		for i := range side.Events {
			e := &side.Events[i]
			if seenEvents[e] {
				continue
			}
			seenEvents[e] = true
			bound := *e
			bound.Type = r.declared(e.Type, source, bindings, scope)
			event := Event{Name: e.Name, Description: e.Description, Type: r.substitute(e.Type, source, bindings, scope), At: e.At, Declaration: e, BoundDeclaration: &bound, Scope: slices.Clone(scope), Origin: Origin{Family: source.Name, Declaration: e.Name, At: e.At}}
			out.Events = append(out.Events, event)
			if source == r {
				out.OwnEvents = append(out.OwnEvents, event)
			}
		}
		// The live tier adds to the surface the protocol tier produces, so
		// its operations are resolved on the same side, after the ones
		// beneath them. A family reached only as a base contributes its
		// live operations exactly when this family has the tier that can
		// carry them — which is why extending a base that has a live tier
		// requires one here, and why a protocol-only family never acquires
		// a live dependency from a side it extended.
		if source.f.Live == nil || r.f.Live == nil {
			return
		}
		liveSide := source.f.Live.Server
		if !server {
			liveSide = source.f.Live.Client
		}
		for i := range liveSide.Methods {
			m := &liveSide.Methods[i]
			if seenMethods[m] {
				continue
			}
			seenMethods[m] = true
			bound := *m
			bound.Request = r.declared(m.Request, source, bindings, scope)
			bound.Result = r.declared(m.Result, source, bindings, scope)
			method := Method{Name: m.Name, Description: m.Description, Request: r.substitute(m.Request, source, bindings, scope), Result: r.substitute(m.Result, source, bindings, scope), Errors: slices.Clone(m.Errors), At: m.At, Declaration: m, BoundDeclaration: &bound, Scope: slices.Clone(scope), Origin: Origin{Family: source.Name, Declaration: m.Name, At: m.At}}
			out.Methods = append(out.Methods, method)
			if source == r {
				out.OwnMethods = append(out.OwnMethods, method)
			}
		}
		for i := range liveSide.Events {
			e := &liveSide.Events[i]
			if seenEvents[e] {
				continue
			}
			seenEvents[e] = true
			bound := *e
			bound.Type = r.declared(e.Type, source, bindings, scope)
			event := Event{Name: e.Name, Description: e.Description, Type: r.substitute(e.Type, source, bindings, scope), At: e.At, Declaration: e, BoundDeclaration: &bound, Scope: slices.Clone(scope), Origin: Origin{Family: source.Name, Declaration: e.Name, At: e.At}}
			out.Events = append(out.Events, event)
			if source == r {
				out.OwnEvents = append(out.OwnEvents, event)
			}
		}
	}
	visit(r, nil, map[*Family]bool{})
	return out
}

func (r *Family) resolvedErrors() []Error {
	errors := map[string]Error{}
	seen := map[*analysis.Family]bool{}
	var visit func(*Family)
	visit = func(source *Family) {
		if seen[source.f] || source.f.Protocol == nil {
			return
		}
		seen[source.f] = true
		for _, side := range []model.Side{source.f.Protocol.Server, source.f.Protocol.Client} {
			for _, parent := range side.Extends {
				if base := source.other(parent.Name); base != nil {
					visit(base)
				}
			}
		}
		for _, e := range source.f.Protocol.Errors {
			errors[e.Code] = Error{Code: e.Code, Description: e.Description, At: e.At,
				Origin: Origin{Family: source.Name, Declaration: e.Code, At: e.At}}
		}
	}
	visit(r)
	var out []Error
	for _, e := range errors {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// qualify preserves the declaring family's scope when an inherited
// operation is viewed from another family. Parameter names stay parameters.
func (r *Family) qualify(e model.TypeExpr, source *Family) model.TypeExpr {
	if source == r {
		return e
	}
	return r.substitute(e, source, nil, source.f.Parameters())
}
