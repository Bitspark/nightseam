package analysis

import (
	"slices"
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
)

func scopeParameter(scope []model.Parameter, name string) (model.Parameter, bool) {
	for _, parameter := range scope {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return model.Parameter{}, false
}

func orderedUses(scope []model.Parameter, sets ...[]Use) []Use {
	var out []Use
	for _, set := range sets {
		for _, use := range set {
			if !slices.Contains(out, use) {
				out = append(out, use)
			}
		}
	}
	order := map[string]int{}
	for i, parameter := range scope {
		order[parameter.Name] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Parameter != out[j].Parameter {
			if order[out[i].Parameter] != order[out[j].Parameter] {
				return order[out[i].Parameter] < order[out[j].Parameter]
			}
			return out[i].Parameter < out[j].Parameter
		}
		return drawnBefore(out[i].Type, out[j].Type)
	})
	return out
}

type parameterUses struct {
	family   *Family
	generics *Generics
	walking  map[*model.Type]bool
}

func (u *parameterUses) declaration(t *model.Type, scope []model.Parameter) []Use {
	if t == nil || u.walking[t] {
		return nil
	}
	if u.walking == nil {
		u.walking = map[*model.Type]bool{}
	}
	u.walking[t] = true
	defer delete(u.walking, t)
	var sets [][]Use
	for _, parent := range t.Extends {
		sets = append(sets, u.expression(parent.Expression(), scope))
	}
	for _, field := range t.Fields {
		sets = append(sets, u.expression(field.Type, scope))
	}
	sets = append(sets, u.expression(t.Alias, scope))
	for _, variant := range t.Variants {
		sets = append(sets, u.expression(variant.Type, scope))
	}
	return orderedUses(scope, sets...)
}

func (u *parameterUses) expression(e model.TypeExpr, scope []model.Parameter) []Use {
	switch x := e.(type) {
	case model.Named:
		if parameter, ok := scopeParameter(scope, x.Name); ok && !parameter.IsFamily() {
			return []Use{{Parameter: x.Name}}
		}
		return u.generics.Types[x.Name]
	case model.Drawn:
		if parameter, ok := scopeParameter(scope, x.Parameter); ok && parameter.IsFamily() {
			return []Use{{Parameter: x.Parameter, Type: x.Name}}
		}
	case model.Array:
		return u.expression(x.Elem, scope)
	case model.Map:
		return u.expression(x.Elem, scope)
	case model.Nullable:
		return u.expression(x.Elem, scope)
	case model.Inline:
		return u.declaration(x.Type, scope)
	case model.Apply:
		return u.application(x, scope)
	case model.Imported:
		return u.application(model.Apply{Family: x.Family, Name: x.Name}, scope)
	case model.Ref:
		if entity := u.family.Types[x.Entity]; entity != nil {
			if u.walking[entity] {
				return nil
			}
			if u.walking == nil {
				u.walking = map[*model.Type]bool{}
			}
			u.walking[entity] = true
			defer delete(u.walking, entity)
			for _, field := range u.family.FlattenedFields(x.Entity) {
				if field.Name == entity.Key {
					return u.expression(field.Type, scope)
				}
			}
		}
	}
	return nil
}

func (u *parameterUses) application(a model.Apply, scope []model.Parameter) []Use {
	owner, uses := u.family, u.generics.Types[a.Name]
	if a.Family != "" {
		owner, uses = u.family.Imported[a.Family], u.generics.Imported[a.Family][a.Name]
	}
	if owner == nil || owner.Types[a.Name] == nil {
		return nil
	}
	slots := append(slices.Clone(owner.Parameters()), owner.Types[a.Name].Parameters...)
	var sets [][]Use
	for _, use := range uses {
		slot, ok := scopeParameter(slots, use.Parameter)
		if !ok {
			continue
		}
		filler, supplied := a.With[use.Parameter]
		if !supplied {
			name := use.Parameter
			if a.Family != "" {
				parameters := u.family.FamilyParameters()
				if !slot.IsFamily() || len(parameters) != 1 {
					continue
				}
				name = parameters[0].Name
			}
			filler.Type = model.Named{Name: name}
		}
		if slot.IsFamily() {
			if name := filler.Name(); name != "" {
				if parameter, ok := scopeParameter(scope, name); ok && parameter.IsFamily() {
					sets = append(sets, []Use{{Parameter: name, Type: use.Type}})
				}
			}
		} else {
			sets = append(sets, u.expression(filler.Type, scope))
		}
	}
	return orderedUses(scope, sets...)
}
