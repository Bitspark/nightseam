package check

import (
	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

func (c *checker) inheritanceArguments(edge model.Inheritance, wanted map[string]model.Parameter, at diag.Location, where site) {
	if edge.Applied() {
		c.arguments(edge.Name, wanted, edge.With, at, where)
	} else if len(wanted) > 0 {
		c.Addf(at, "unbound_parameter", "Inherited base %s is generic; bind its parameters explicitly with {\"apply\": \"%s\", \"with\": {…}}.", edge.Name, edge.Name)
	}
}

func (c *checker) inherits(t *model.Type, i int, edge model.Inheritance, where site, kind string) {
	at := t.At.Sub("extends", i)
	owner, inherited, ok := c.f.Base(edge)
	if !ok {
		c.Addf(at, "unresolved_type", "Unknown inherited type %s.", edge.Name)
		return
	}
	if kind == model.KindUnion && inherited.Kind != model.KindUnion {
		c.Addf(at, "invalid_inheritance", "A union extends a union; %s is a %s.", edge.Name, inherited.Kind)
		return
	}
	if kind == model.KindRecord && inherited.Kind != model.KindRecord && inherited.Kind != model.KindEntity {
		c.Add(at, "invalid_inheritance", "An inherited type must be a record.")
		return
	}
	if owner.Rank(inherited.Name) > where.context {
		c.tierViolation(at, where.context, edge.Name, inherited.At.File)
		return
	}
	wanted := map[string]model.Parameter{}
	for _, parameter := range owner.TypeParameters(inherited) {
		wanted[parameter.Name] = parameter
	}
	where.inline = false // inheritance binds a base, not a value's type
	c.inheritanceArguments(edge, wanted, at, where)
	if kind != model.KindUnion {
		if owner == c.f {
			where.edges[t.Name] = append(where.edges[t.Name], inherited.Name)
		}
		return
	}
	if reachesType(owner, inherited, t, map[*model.Type]bool{}) {
		c.Addf(at, "extends_cycle", "Union %s extends %s, which extends this one, directly or through what it extends.", t.Name, edge.Name)
		return
	}
	if t.Tag != "" && inherited.Tag != "" && t.Tag != inherited.Tag {
		c.Addf(at, "invalid_union", "Union %s discriminates on %s and extends %s, which discriminates on %s; a union that widens another reads the same member.", t.Name, t.Tag, edge.Name, inherited.Tag)
	}
	if t.ValueMember() != inherited.ValueMember() {
		c.Addf(at, "invalid_union", "Union %s carries its complete payload under %s and extends %s, which carries it under %s.", t.Name, t.ValueMember(), edge.Name, inherited.ValueMember())
	}
	for _, tag := range owner.VariantTags(inherited.Name) {
		if _, own := t.Variant(tag); own {
			c.Addf(at, "variant_collision", "Union %s extends %s and declares its variant %s again; an extending union adds variants.", t.Name, edge.Name, tag)
		}
	}
}

func reachesType(source *analysis.Family, from, target *model.Type, seen map[*model.Type]bool) bool {
	if from == target {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, edge := range from.Extends {
		if owner, base, ok := source.Base(edge); ok && reachesType(owner, base, target, seen) {
			return true
		}
	}
	return false
}

func (c *checker) inheritedVariantCollisions(t *model.Type) {
	type origin struct {
		declaration *model.Type
		payload     string
	}
	seen := map[string]origin{}
	var walk func(*analysis.Family, *model.Type, map[string]model.Filler, diag.Location, map[*model.Type]bool)
	walk = func(source *analysis.Family, base *model.Type, bindings map[string]model.Filler, at diag.Location, stack map[*model.Type]bool) {
		if base == nil || base == t || base.Kind != model.KindUnion || stack[base] {
			return
		}
		stack[base] = true
		defer delete(stack, base)
		for _, parent := range base.Extends {
			if owner, next, ok := source.Base(parent); ok {
				walk(owner, next, c.f.BindArguments(parent.With, source, bindings), at, stack)
			}
		}
		for _, variant := range base.Variants {
			payload := model.String(c.f.BindExpression(variant.Type, source, bindings))
			if previous, exists := seen[variant.Tag]; exists && (previous.declaration != base || previous.payload != payload) {
				c.Addf(at, "variant_collision", "Union %s inherits variant %s from both %s and %s; an inherited tag has one declaration and binding.", t.Name, variant.Tag, previous.declaration.Name, base.Name)
			} else {
				seen[variant.Tag] = origin{base, payload}
			}
		}
	}
	for i, edge := range t.Extends {
		if owner, base, ok := c.f.Base(edge); ok {
			walk(owner, base, edge.With, t.At.Sub("extends", i), map[*model.Type]bool{})
		}
	}
}
