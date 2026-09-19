package golang

import (
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

func (f *file) unionVariants(t *render.Type) []unionVariant {
	u := f.plan.unions[t.Name]
	var variants []unionVariant
	for _, v := range t.Variants {
		planned := unionVariant{tag: v.Tag, field: u.fields[v.Tag], kind: u.constants[v.Tag], empty: v.Form == render.VariantEmpty}
		source := f.family.ReferencedFamily(v.Origin.Family)
		declaration := source.Type(v.Origin.Declaration)
		original := v
		for _, candidate := range declaration.OwnVariants {
			if candidate.Tag == v.Tag {
				original = candidate
				break
			}
		}
		switch {
		case planned.empty:
			planned.typeName = "struct{}"
		case original.Payload != nil:
			planned.typeName = f.spell(v.Type)
		default:
			planned.wrapped = true
			if v.Origin == t.Origin {
				planned.wrapper = u.wrappers[v.Tag]
				planned.typeName = planned.wrapper + apply(t.Uses)
				planned.payloadType = f.spell(v.Type)
			} else {
				name := declaration.Name
				if override, ok := source.Override(Name, name); ok {
					name = override
				}
				name += naming.UpperCamel(v.Tag) + "Value"
				if source.Name != f.family.Name {
					name = f.peer(source.Name) + "." + name
				}
				planned.typeName = name + f.arguments(v.Arguments)
			}
		}
		variants = append(variants, planned)
	}
	return variants
}

func (f *file) unionBases(t *render.Type) []unionBase {
	u := f.plan.unions[t.Name]
	var bases []unionBase
	for _, planned := range u.bases {
		base := unionBase{typeName: f.named(planned.typeView.Origin.Family, planned.typeView.Name) + f.arguments(planned.typeView.Arguments), widen: planned.widen, narrow: planned.narrow}
		for _, variant := range planned.typeView.Variants {
			base.variants = append(base.variants, unionBaseVariant{baseField: naming.UpperCamel(variant.Tag), field: u.fields[variant.Tag], kind: u.constants[variant.Tag]})
		}
		bases = append(bases, base)
	}
	return bases
}
