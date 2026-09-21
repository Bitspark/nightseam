package doc

import (
	"encoding/json"
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

// ExampleInfo states the instantiation an example demonstrates, or why no
// example is available. A missing example is never a JSON null witness.
type ExampleInfo struct {
	ExampleBindings    map[string]ExampleBinding `json:",omitempty"`
	ExampleUnavailable *ExampleUnavailable       `json:",omitempty"`
}

// ExampleBinding binds a parameter to a primitive witness or a named family.
type ExampleBinding struct {
	Type   string `json:",omitempty"`
	Family string `json:",omitempty"`
}

// ExampleUnavailable distinguishes a bounded search from an impossibility
// proof. The synthesizer currently makes only the former claim (kind limit).
type ExampleUnavailable struct{ Kind, Reason string }

func unavailable(reason string) value {
	return value{unavailable: &ExampleUnavailable{Kind: "limit", Reason: reason}}
}

func (v value) problem() *ExampleUnavailable {
	if v.unavailable != nil {
		return v.unavailable
	}
	for _, member := range v.members {
		if reason := member.value.problem(); reason != nil {
			return reason
		}
	}
	for _, item := range v.items {
		if reason := item.problem(); reason != nil {
			return reason
		}
	}
	return nil
}

type exampleBuilder struct {
	f          *render.Family
	families   map[string]*render.Family
	schemas    map[string]*runtime.Schema
	candidates []string
}

func newExampleBuilder(f *render.Family) *exampleBuilder {
	b := &exampleBuilder{f: f, families: map[string]*render.Family{}, schemas: map[string]*runtime.Schema{}}
	var collect func(*render.Family)
	collect = func(f *render.Family) {
		if f == nil || b.families[f.Name] != nil {
			return
		}
		b.families[f.Name] = f
		for _, name := range append(append([]string{}, f.References...), f.Carries...) {
			collect(f.ReferencedFamily(name))
		}
	}
	collect(f)
	for name, family := range b.families {
		b.schemas[name] = runtime.MustSchema(family.Wire, family.WireDigest, b.schemas)
		if !family.Builtin && len(family.Parameters) == 0 {
			b.candidates = append(b.candidates, name)
		}
	}
	sort.Strings(b.candidates)
	// Existing explicit applications are the document author's preferred
	// witnesses. Otherwise use a compatible visible family in name order.
	preferred := []string{}
	visit := func(e model.TypeExpr) {
		model.Walk(e, func(e model.TypeExpr) bool {
			if a, ok := e.(model.Apply); ok {
				for _, arg := range f.Arguments(a) {
					if arg.Family != "" {
						preferred = append(preferred, arg.Family)
					}
				}
			}
			return true
		})
	}
	for _, t := range f.Types {
		visit(t.Alias)
		for _, field := range t.Fields {
			visit(field.Type)
		}
		for _, variant := range t.Variants {
			visit(variant.Type)
		}
	}
	for _, side := range []render.Side{f.Server, f.Client} {
		for _, m := range side.Methods {
			visit(m.Request)
			visit(m.Result)
		}
		for _, e := range side.Events {
			visit(e.Type)
		}
	}
	b.candidates = append(preferred, b.candidates...)
	return b
}

func (b *exampleBuilder) context(scope []model.Parameter, uses []render.Use) (*exampler, ExampleInfo, *runtime.Schema) {
	x := newExampler(b.f)
	info := ExampleInfo{}
	types := map[string]any{}
	families := map[string]*runtime.Schema{}
	for _, p := range scope {
		used := false
		for _, use := range uses {
			if use.Parameter == p.Name {
				used = true
			}
		}
		if !used {
			continue
		}
		binding := ExampleBinding{Type: "string"}
		if p.Of == "" {
			types[p.Name] = "string"
			x.subst[p.Name] = func(name string, c constraints) value { return x.primitive("string", name, c) }
		} else {
			binding = ExampleBinding{}
			for _, name := range b.candidates {
				candidate := b.families[name]
				if candidate == nil || candidate.Builtin || len(candidate.Parameters) != 0 {
					continue
				}
				compatible := p.Of == "model" || p.Of == "protocol" && candidate.HasProtocol() || p.Of == "live" && candidate.Live
				for _, use := range uses {
					if use.Parameter == p.Name && use.Type != "" && candidate.Type(use.Type) == nil {
						compatible = false
					}
				}
				if compatible {
					binding.Family = name
					break
				}
			}
			if binding.Family == "" {
				info.ExampleUnavailable = &ExampleUnavailable{Kind: "limit", Reason: "no compatible concrete family is available for " + p.Name}
				continue
			}
			candidate := b.families[binding.Family]
			families[p.Name] = b.schemas[binding.Family]
			for _, t := range candidate.Types {
				x.subst[p.Name+"."+t.Name] = func(name string, c constraints) value { return x.in(candidate).typed(t, name, c) }
			}
		}
		if info.ExampleBindings == nil {
			info.ExampleBindings = map[string]ExampleBinding{}
		}
		info.ExampleBindings[p.Name] = binding
	}
	return x, info, b.schemas[b.f.Name].Bind(types, families)
}

func validateExample(schema *runtime.Schema, expression model.TypeExpr, v value, info *ExampleInfo) json.RawMessage {
	if info.ExampleUnavailable == nil {
		info.ExampleUnavailable = v.problem()
	}
	if info.ExampleUnavailable == nil && expression != nil {
		encoded, _ := json.Marshal(expression)
		if err := schema.ValidateExpressionRaw(runtime.MustTypeExpression(string(encoded)), v.raw()); err != nil {
			info.ExampleUnavailable = &ExampleUnavailable{Kind: "limit", Reason: "synthesized candidate was refused: " + err.Error()}
		}
	}
	if info.ExampleUnavailable != nil {
		return nil
	}
	return v.raw()
}

// ExampleExpression names the exact declaration instantiated by a type's
// example, including the captured scope of a derived inline declaration.
func ExampleExpression(t *render.Type) model.TypeExpr {
	if t.Inline {
		return model.Inline{Type: t.Declaration}
	}
	if len(t.Parameters) == 0 {
		return model.Named{Name: t.Name}
	}
	with := map[string]model.Filler{}
	for _, p := range t.Parameters {
		with[p.Name] = model.Filler{Type: model.Primitive("string")}
	}
	return model.Apply{Name: t.Name, With: with}
}

func addExamples(d *Family, f *render.Family) {
	b := newExampleBuilder(f)
	for _, t := range append(append([]*Type{}, d.Types...), d.Carried...) {
		rt := f.Type(t.Name)
		x, info, schema := b.context(rt.Scope, rt.Uses)
		t.Example = validateExample(schema, ExampleExpression(rt), x.example(rt), &info)
		t.ExampleInfo = info
		for i := range t.Variants {
			x, info, schema := b.context(rt.Scope, rt.Uses)
			t.Variants[i].Example = validateExample(schema, ExampleExpression(rt), x.variantExample(rt, rt.Variants[i]), &info)
			t.Variants[i].ExampleInfo = info
		}
	}
	for _, side := range []struct {
		name   string
		out    *Side
		source render.Side
	}{{"server", &d.Server, f.Server}, {"client", &d.Client, f.Client}} {
		for i, m := range side.source.Methods {
			x, info, schema := b.context(m.Scope, m.Uses)
			params, result := object(), null
			if m.Request != nil {
				params = x.value(m.Request, "params", constraints{})
			}
			if m.Result != nil {
				result = x.value(m.Result, "result", constraints{})
			}
			validateExample(schema, m.Request, params, &info)
			validateExample(schema, m.Result, result, &info)
			out := &side.out.Methods[i]
			out.ExampleInfo = info
			out.Frames, out.Weight = Frames{}, Weight{}
			if info.ExampleUnavailable == nil {
				x, _, _ = b.context(m.Scope, m.Uses)
				out.Frames = x.frames(side.name, m, f.Errors)
				out.Weight = Weight{Request: params.weight(), Result: result.weight()}
			}
		}
		for i, e := range side.source.Events {
			x, info, schema := b.context(e.Scope, e.Uses)
			data := null
			if e.Type != nil {
				data = x.value(e.Type, "data", constraints{})
			}
			validateExample(schema, e.Type, data, &info)
			out := &side.out.Events[i]
			out.ExampleInfo = info
			out.Frame, out.Weight = nil, 0
			if info.ExampleUnavailable == nil {
				out.Frame = envelope("event", member{"event", text(e.Name)}, member{"data", data})
				out.Weight = data.weight()
			}
		}
	}
}
