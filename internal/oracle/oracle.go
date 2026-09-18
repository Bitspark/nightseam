// Package oracle is test support: the left path of the diagram a generic
// rendering must commute with. Binding a family's parameters to families
// in the declaration and rendering it plain must give what rendering it
// generically and instantiating gives — gen(bind(C, F)) ≅ gen(C)[F] — and
// Substitute is bind. Nothing of the generator depends on it.
package oracle

import (
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
)

// Substitute binds parameters to families in a copy of the family: every
// type drawn from a bound parameter becomes the bound family's type, every
// application's filler likewise, the bound parameters are dropped and the
// bound families imported. A binding need not be total: an unbound
// parameter stays, and the family stays generic in it.
func Substitute(f *model.Family, bindings map[string]string) *model.Family {
	out := *f
	imports := map[string]bool{}
	for _, name := range f.Imports {
		imports[name] = true
	}
	for _, family := range bindings {
		imports[family] = true
	}
	fill := func(e model.TypeExpr) model.TypeExpr {
		return model.Rewrite(e, func(x model.TypeExpr) model.TypeExpr {
			switch v := x.(type) {
			case model.Drawn:
				if family, bound := bindings[v.Parameter]; bound {
					return model.Imported{Family: family, Name: v.Name}
				}
			case model.Apply:
				with := make(map[string]model.Filler, len(v.With))
				for parameter, filler := range v.With {
					if family, bound := bindings[filler.Parameter]; filler.Parameter != "" && bound {
						filler = model.Filler{Family: family}
					}
					with[parameter] = filler
				}
				return model.Apply{Family: v.Family, Name: v.Name, With: with}
			}
			return x
		})
	}
	out.Types = make(map[string]*model.Type, len(f.Types))
	for name, t := range f.Types {
		copied := *t
		copied.Fields = make([]model.Field, len(t.Fields))
		for i, field := range t.Fields {
			field.Type = fill(field.Type)
			copied.Fields[i] = field
		}
		copied.Alias = fill(t.Alias)
		out.Types[name] = &copied
	}
	if f.Protocol != nil {
		p := *f.Protocol
		p.Parameters = nil
		for _, parameter := range f.Protocol.Parameters {
			if _, bound := bindings[parameter.Name]; !bound {
				p.Parameters = append(p.Parameters, parameter)
			}
		}
		p.Server = substituteSide(f.Protocol.Server, fill)
		p.Client = substituteSide(f.Protocol.Client, fill)
		out.Protocol = &p
	}
	out.Imports = nil
	for name := range imports {
		if name != f.Name {
			out.Imports = append(out.Imports, name)
		}
	}
	sort.Strings(out.Imports)
	return &out
}

func substituteSide(s model.Side, fill func(model.TypeExpr) model.TypeExpr) model.Side {
	out := s
	out.Methods = make([]model.Method, len(s.Methods))
	for i, m := range s.Methods {
		m.Request = fill(m.Request)
		m.Result = fill(m.Result)
		out.Methods[i] = m
	}
	out.Events = make([]model.Event, len(s.Events))
	for i, e := range s.Events {
		e.Type = fill(e.Type)
		out.Events[i] = e
	}
	return out
}
