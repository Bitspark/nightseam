package render

import (
	"slices"
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
)

func parameter(scope []model.Parameter, name string) (model.Parameter, bool) {
	for _, p := range scope {
		if p.Name == name {
			return p, true
		}
	}
	return model.Parameter{}, false
}

// orderedUses keeps declaration order; a family's associated types use
// the existing Envelope, Handle, then lexical convention in both targets.
func orderedUses(scope []model.Parameter, sets ...[]Use) []Use {
	out := union(sets...)
	order := map[string]int{}
	for i, p := range scope {
		order[p.Name] = i
	}
	rank := func(name string) int {
		switch name {
		case model.EnvelopeType:
			return 0
		case model.HandleType:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Parameter != b.Parameter {
			if order[a.Parameter] != order[b.Parameter] {
				return order[a.Parameter] < order[b.Parameter]
			}
			return a.Parameter < b.Parameter
		}
		if rank(a.Type) != rank(b.Type) {
			return rank(a.Type) < rank(b.Type)
		}
		return a.Type < b.Type
	})
	return out
}

func (r *Family) uses(e model.TypeExpr, scope []model.Parameter) []Use {
	switch x := e.(type) {
	case model.Named:
		if p, ok := parameter(scope, x.Name); ok && !p.IsFamily() {
			return []Use{{Parameter: p.Name}}
		}
		if t := r.Type(x.Name); t != nil {
			return t.Uses
		}
	case model.Drawn:
		if p, ok := parameter(scope, x.Parameter); ok && p.IsFamily() {
			return []Use{{Parameter: p.Name, Type: x.Name}}
		}
	case model.Array:
		return r.uses(x.Elem, scope)
	case model.Map:
		return r.uses(x.Elem, scope)
	case model.Nullable:
		return r.uses(x.Elem, scope)
	case model.Inline:
		if t := r.InlineType(x); t != nil {
			return t.Uses
		}
	case model.Imported:
		return r.applicationUses(model.Apply{Family: x.Family, Name: x.Name}, scope)
	case model.Apply:
		return r.applicationUses(x, scope)
	case model.Ref:
		if entity := r.Type(x.Entity); entity != nil {
			for _, field := range entity.Fields {
				if field.Name == entity.Key {
					return r.uses(field.Type, scope)
				}
			}
		}
	}
	return nil
}

func (r *Family) applicationUses(a model.Apply, scope []model.Parameter) []Use {
	var sets [][]Use
	for _, argument := range r.arguments(a) {
		if argument.Type != nil {
			sets = append(sets, r.uses(argument.Type, scope))
		} else if argument.Parameter != "" {
			sets = append(sets, []Use{{Parameter: argument.Parameter, Type: argument.Use.Type}})
		}
	}
	return orderedUses(scope, sets...)
}

func (r *Family) resolveUses() {
	declared := r.f.Generics()
	for _, t := range r.Types {
		if t.Inline {
			t.Uses = r.f.UsesIn(model.Inline{Type: t.Declaration}, t.Scope)
		} else {
			t.Uses = slices.Clone(declared.Types[t.Name])
		}
	}
	var familySets [][]Use
	for _, t := range r.Types {
		for _, use := range t.Uses {
			if _, declared := parameter(r.f.Parameters(), use.Parameter); declared {
				familySets = append(familySets, []Use{use})
			}
		}
	}
	for _, side := range []*Side{&r.Server, &r.Client} {
		for i := range side.Methods {
			m := &side.Methods[i]
			m.Uses = orderedUses(r.f.Parameters(), r.uses(m.Request, r.f.Parameters()), r.uses(m.Result, r.f.Parameters()))
			familySets = append(familySets, m.Uses)
		}
		for i := range side.Events {
			e := &side.Events[i]
			e.Uses = orderedUses(r.f.Parameters(), r.uses(e.Type, r.f.Parameters()))
			familySets = append(familySets, e.Uses)
		}
		for i := range side.OwnMethods {
			for _, method := range side.Methods {
				if method.Name == side.OwnMethods[i].Name {
					side.OwnMethods[i].Uses = method.Uses
					break
				}
			}
		}
		for i := range side.OwnEvents {
			for _, event := range side.Events {
				if event.Name == side.OwnEvents[i].Name {
					side.OwnEvents[i].Uses = event.Uses
					break
				}
			}
		}
	}
	r.Uses = orderedUses(r.f.Parameters(), familySets...)
	r.Generic = len(r.Uses) != 0
	for i := range r.Parameters {
		r.Parameters[i].Uses = nil
		for _, use := range r.Uses {
			if use.Parameter == r.Parameters[i].Name {
				r.Parameters[i].Uses = append(r.Parameters[i].Uses, use)
			}
		}
	}
}

func (r *Family) arguments(a model.Apply) []Argument {
	owner := r.other(a.Family)
	if owner == nil {
		return nil
	}
	t := owner.Type(a.Name)
	if t == nil {
		return nil
	}
	var out []Argument
	for _, use := range t.Uses {
		slot, _ := parameter(t.Scope, use.Parameter)
		argument := Argument{Use: use, Slot: slot}
		filler, supplied := a.With[use.Parameter]
		if !supplied {
			name := use.Parameter
			if a.Family != "" && slot.IsFamily() && len(r.familyParameters()) == 1 {
				name = r.familyParameters()[0]
			}
			filler.Type = model.Named{Name: name}
		}
		if !slot.IsFamily() {
			argument.Type = filler.Type
		} else if filler.Family != "" {
			argument.Family = filler.Family
		} else {
			argument.Parameter = filler.Name()
		}
		out = append(out, argument)
	}
	return out
}
