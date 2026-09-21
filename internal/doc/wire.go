package doc

import (
	"github.com/Bitspark/nightseam/internal/examples"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

type Frames = examples.Frames
type Refusal = examples.Refusal
type Weight = examples.Weight

const Timestamp = examples.Timestamp

// Reference is one place a type is used: an operation of a side, at its
// request, result or data, or a field of another type.
type Reference struct {
	Side string // server or client; empty for a type
	Kind string // method, event or type
	Name string // the operation's, or the type's
	At   string // request, result, data, or the field's name
}

// references indexes where every type of the family is used: by the fields
// of other types, and by the operations of both sides at their request,
// result or data, each once, in the order the declaration reaches them.
func references(f *render.Family) map[string][]Reference {
	out := map[string][]Reference{}
	seen := map[string]bool{}
	add := func(typeName string, r Reference) {
		key := typeName + "|" + r.Side + "|" + r.Kind + "|" + r.Name + "|" + r.At
		if !seen[key] {
			seen[key] = true
			out[typeName] = append(out[typeName], r)
		}
	}
	names := func(e model.TypeExpr, visit func(string)) {
		model.Walk(e, func(x model.TypeExpr) bool {
			switch v := x.(type) {
			case model.Named:
				if f.Type(v.Name) != nil {
					visit(v.Name)
				}
			case model.Ref:
				if v.Family == "" || v.Family == f.Name {
					visit(v.Entity)
				}
			case model.Apply:
				if v.Family == "" && f.Type(v.Name) != nil {
					visit(v.Name)
				}
			}
			return true
		})
	}
	for _, t := range f.Types {
		for _, field := range t.Own {
			names(field.Type, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: field.Name}) })
		}
		if t.Alias != nil {
			names(t.Alias, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: "alias"}) })
		}
		for _, variant := range t.Variants {
			names(variant.Type, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: variant.Tag}) })
		}
	}
	for _, side := range []struct {
		name string
		side render.Side
	}{{"server", f.Server}, {"client", f.Client}} {
		for _, m := range side.side.Methods {
			names(m.Request, func(name string) { add(name, Reference{Side: side.name, Kind: "method", Name: m.Name, At: "request"}) })
			names(m.Result, func(name string) { add(name, Reference{Side: side.name, Kind: "method", Name: m.Name, At: "result"}) })
		}
		for _, e := range side.side.Events {
			names(e.Type, func(name string) { add(name, Reference{Side: side.name, Kind: "event", Name: e.Name, At: "data"}) })
		}
	}
	return out
}
