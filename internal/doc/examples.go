package doc

import (
	"github.com/Bitspark/nightseam/internal/examples"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// Document metadata and consumer witnesses use the same synthesizer.
type ExampleInfo = examples.ExampleInfo
type ExampleBinding = examples.ExampleBinding
type ExampleUnavailable = examples.ExampleUnavailable

func ExampleExpression(t *render.Type) model.TypeExpr { return examples.ExampleExpression(t) }

func addExamples(d *Family, f *render.Family) {
	b := examples.New(f)
	for _, t := range append(append([]*Type{}, d.Types...), d.Carried...) {
		rt := f.Type(t.Name)
		t.Example, t.ExampleInfo = b.Type(rt)
		for i := range t.Variants {
			t.Variants[i].Example, t.Variants[i].ExampleInfo = b.Variant(rt, rt.Variants[i])
		}
	}
	for _, side := range []struct {
		name   string
		out    *Side
		source render.Side
	}{{"server", &d.Server, f.Server}, {"client", &d.Client, f.Client}} {
		for i, m := range side.source.Methods {
			side.out.Methods[i].Frames, side.out.Methods[i].Weight, side.out.Methods[i].ExampleInfo = b.Method(side.name, m)
		}
		for i, e := range side.source.Events {
			side.out.Events[i].Frame, side.out.Events[i].Weight, side.out.Events[i].ExampleInfo = b.Event(e)
		}
	}
}
