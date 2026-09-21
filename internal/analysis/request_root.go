package analysis

import "github.com/Bitspark/nightseam/internal/model"

// RequestRoot follows transparent aliases while preserving application scope.
// A draw at the resulting root imposes an object requirement on its filler.
func (f *Family) RequestRoot(e model.TypeExpr) model.TypeExpr {
	seen := map[string]bool{}
	for {
		key := model.String(e)
		if seen[key] {
			return e
		}
		seen[key] = true
		owner, name := f, ""
		var with map[string]model.Filler
		switch x := e.(type) {
		case model.Named:
			name = x.Name
		case model.Imported:
			owner, name = f.Imported[x.Family], x.Name
		case model.Apply:
			name, with = x.Name, x.With
			if x.Family != "" {
				owner = f.Imported[x.Family]
			}
		default:
			return e
		}
		if owner == nil || owner.Types[name] == nil || owner.Types[name].Kind != model.KindAlias {
			return e
		}
		if owner != f && with == nil {
			with = map[string]model.Filler{}
			if parameters := f.FamilyParameters(); len(parameters) == 1 {
				for _, use := range owner.Generics().Types[name] {
					with[use.Parameter] = model.Filler{Type: model.Named{Name: parameters[0].Name}}
				}
			}
		}
		e = f.BindExpression(owner.Types[name].Alias, owner, with)
	}
}
