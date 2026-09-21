// Package examples synthesizes validated declaration witnesses shared by
// documentation and generated consumer tests. It has no rendering target.
package examples

import (
	"encoding/json"

	"github.com/Bitspark/nightseam/internal/render"
)

func (b *Builder) Type(t *render.Type) (json.RawMessage, ExampleInfo) {
	x, info, schema := b.context(t.Scope, t.Uses)
	raw := validateExample(schema, ExampleExpression(t), x.example(t), &info)
	return raw, info
}

func (b *Builder) Variant(t *render.Type, v render.Variant) (json.RawMessage, ExampleInfo) {
	x, info, schema := b.context(t.Scope, t.Uses)
	raw := validateExample(schema, ExampleExpression(t), x.variantExample(t, v), &info)
	return raw, info
}

// Request is independent of result synthesis: a missing result witness does
// not prevent testing a method with a valid input.
func (b *Builder) Request(m render.Method) (json.RawMessage, ExampleInfo) {
	if m.Request == nil {
		return json.RawMessage(`{}`), ExampleInfo{}
	}
	x, info, schema := b.context(m.Scope, m.Uses)
	raw := validateExample(schema, m.Request, x.value(m.Request, "params", constraints{}), &info)
	return raw, info
}

func (b *Builder) Method(side string, m render.Method) (Frames, Weight, ExampleInfo) {
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
	if info.ExampleUnavailable != nil {
		return Frames{}, Weight{}, info
	}
	x, _, _ = b.context(m.Scope, m.Uses)
	return x.frames(side, m, b.f.Errors), Weight{Request: params.weight(), Result: result.weight()}, info
}

func (b *Builder) Event(e render.Event) (json.RawMessage, int, ExampleInfo) {
	x, info, schema := b.context(e.Scope, e.Uses)
	data := null
	if e.Type != nil {
		data = x.value(e.Type, "data", constraints{})
	}
	validateExample(schema, e.Type, data, &info)
	if info.ExampleUnavailable != nil {
		return nil, 0, info
	}
	return envelope("event", member{"event", text(e.Name)}, member{"data", data}), data.weight(), info
}
