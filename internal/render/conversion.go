package render

import "github.com/Bitspark/nightseam/internal/model"

// Conversion identifies the declaration and native arguments whose boundary
// helper converts an expression. Containers are handled by their caller.
func (r *Family) Conversion(e model.TypeExpr) (*Type, []Argument) {
	var a model.Apply
	switch x := e.(type) {
	case model.Named:
		a.Name = x.Name
	case model.Imported:
		a.Family, a.Name = x.Family, x.Name
	case model.Apply:
		a = x
	case model.Inline:
		t := r.InlineType(x)
		if t == nil {
			return nil, nil
		}
		if t.Arguments != nil {
			return t, t.Arguments
		}
		a.Family, a.Name = t.Origin.Family, t.Name
	default:
		return nil, nil
	}
	owner := r.other(a.Family)
	if owner == nil {
		return nil, nil
	}
	return owner.Type(a.Name), r.Arguments(a)
}

// Expression is the argument in the caller's lexical scope, including a
// family's associated type when that is the native parameter being supplied.
func (a Argument) Expression() model.TypeExpr {
	if a.Type != nil {
		return a.Type
	}
	if a.Parameter != "" {
		return model.Drawn{Parameter: a.Parameter, Name: a.Use.Type}
	}
	return model.Imported{Family: a.Family, Name: a.Use.Type}
}
