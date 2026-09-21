package render

import "github.com/Bitspark/nightseam/internal/model"

// CallableView gives a callable alias its own specialized conversion body.
// The public name, declaration, scope and uses remain the alias's; the signature,
// nominal origin and ordered arguments come from its resolved application.
// Identity is still selected through the original declaration, so no target
// manufactures a nominal name for a source specialization.
func (r *Family) CallableView(t *Type) (*Type, bool) {
	if t == nil {
		return nil, false
	}
	if t.Kind == model.KindCallable {
		return t, true
	}
	if t.Kind != model.KindAlias {
		return nil, false
	}
	target := r.callableExpression(t.Alias, t.Scope, map[string]bool{})
	if target == nil {
		return nil, false
	}
	out := *t
	out.Kind = model.KindCallable
	out.Request, out.Result = target.Request, target.Result
	out.Contract = target.Contract
	out.Origin, out.Arguments = target.Origin, target.Arguments
	return &out, true
}

func (r *Family) callableExpression(e model.TypeExpr, scope []model.Parameter, seen map[string]bool) *Type {
	var application model.Apply
	switch x := e.(type) {
	case model.Named:
		if _, slot := parameter(scope, x.Name); slot {
			return nil
		}
		application.Name = x.Name
	case model.Imported:
		application.Family, application.Name = x.Family, x.Name
	case model.Apply:
		application = x
	default:
		return nil
	}
	key := model.String(e)
	if seen[key] {
		return nil
	}
	seen[key] = true
	defer delete(seen, key)
	// A plain imported declaration may forward the caller's sole family slot.
	// Materialize that existing argument rule before substituting its body.
	if application.Family != "" && application.With == nil {
		application.With = argumentBindings(r.arguments(application))
	}
	view, ok := r.Apply(application, scope)
	if !ok {
		return nil
	}
	if view.Kind == model.KindAlias {
		return r.callableExpression(view.Alias, scope, seen)
	}
	if view.Kind != model.KindCallable {
		return nil
	}
	return view
}
