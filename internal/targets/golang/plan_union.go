package golang

import (
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

const identKind = "Kind"

// unionPlan holds the names shared by a concrete union's declaration and
// codecs. Variant fields are exported pointers; the codec must refuse
// values with zero or multiple selected variants. Wire representation does
// not determine these identifiers.
type unionPlan struct {
	kind      string
	fields    map[string]string // wire tag → pointer field
	constants map[string]string // wire tag → typed kind constant
	wrappers  map[string]string // own non-record payloads only
	bases     []unionBasePlan
}

type unionBasePlan struct {
	typeView      *render.Type
	widen, narrow string
}

func (p *plan) planUnions() {
	p.unions = map[string]unionPlan{}
	for _, t := range p.family.Types {
		if t.Carried || t.Kind != model.KindUnion {
			continue
		}
		name, at := p.resolve(t.Name, t.Name, t.At)
		u := unionPlan{
			kind: name + identKind, fields: map[string]string{}, constants: map[string]string{}, wrappers: map[string]string{},
		}
		if p.identifier(u.kind, at, "Union kind type", true) {
			p.declare(p.packages, u.kind, at, "union kind type")
		}
		fields := emit.NewNamespace("union " + t.Name)
		fields.Fix("generated union method", identKind, identMarshalJSON, identUnmarshalJSON, identOf, identWireType)
		for _, variant := range t.Variants {
			field := naming.UpperCamel(variant.Tag)
			u.fields[variant.Tag] = field
			u.constants[variant.Tag] = u.kind + field
			if p.identifier(field, variant.At, "Union variant field", true) {
				p.declare(fields, field, variant.At, "union variant field")
			}
			if p.identifier(u.constants[variant.Tag], variant.At, "Union kind constant", true) {
				p.declare(p.packages, u.constants[variant.Tag], variant.At, "union kind constant")
			}
		}
		for _, variant := range t.OwnVariants {
			if variant.Form == render.VariantEmpty || variant.Payload != nil {
				continue
			}
			wrapper := name + naming.UpperCamel(variant.Tag) + "Value"
			u.wrappers[variant.Tag] = wrapper
			if p.identifier(wrapper, variant.At, "Union payload wrapper", true) {
				p.declare(p.packages, wrapper, variant.At, "union payload wrapper")
			}
		}
		for _, base := range t.Bases {
			source := p.family.ReferencedFamily(base.Type.Origin.Family)
			baseName := base.Type.Name
			if override, ok := source.Override(Name, baseName); ok {
				baseName = override
			}
			if source.Name != p.family.Name {
				baseName = naming.UpperCamel(source.Name) + baseName
			}
			planned := unionBasePlan{typeView: base.Type, widen: "Widen" + name + "From" + baseName, narrow: "Narrow" + name + "To" + baseName}
			p.declare(p.packages, planned.widen, base.Edge.At, "union widening helper")
			p.declare(p.packages, planned.narrow, base.Edge.At, "union narrowing helper")
			u.bases = append(u.bases, planned)
		}
		p.unions[t.Name] = u
	}
}
