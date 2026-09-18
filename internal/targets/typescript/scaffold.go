package typescript

import (
	"fmt"
	"path"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Scaffold writes the client's handler of what the server sends: an
// object implementing the client package's Handler with every method
// throwing, for the consumer to fill in. A family whose server sends
// nothing has no handler to write.
func (t *target) Scaffold(f *render.Family, dir string) ([]spi.File, error) {
	if err := t.config.Validate(); err != nil {
		return nil, err
	}
	if len(f.Client.Methods) == 0 {
		return nil, nil
	}
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("the family does not pass the TypeScript target's check: %s", diagnostics[0])
	}
	w := emit.NewWriter("  ")
	ctx := &file{plan: p, family: f, config: t.config, w: w, prefix: "Protocol."}
	pkg := t.config.pkg(f.Name)
	w.Linef("import type { RequestContext } from %s;", quote(t.config.Runtime))
	w.Linef("import type { %s } from %s;", identHandler, quote(pkg))
	w.Linef("import type * as %s from %s;", identProtocol, quote(pkg))
	w.Line("")
	w.Linef("/** The behavior of the %s family's client side: what the server calls, as its client package's Handler declares. Fill the methods in; nightseam wrote this file once and will not touch it again. */", f.Name)
	w.Block(fmt.Sprintf("export const handler: %s = {", identHandler), "};", func() {
		for _, m := range f.Client.Methods {
			if m.Description != "" {
				w.Linef("/** %s */", comment(m.Description))
			}
			w.Linef("async %s(params: %s, context: RequestContext): Promise<%s> {", p.operations[m.Name], ctx.request(m), ctx.spell(m.Result))
			w.In()
			w.Linef("throw new Error(%s);", quote(m.Name+" is not implemented"))
			w.Out()
			w.Line("},")
		}
	})
	return []spi.File{{Path: path.Join(dir, "handler.ts"), Data: []byte(w.String())}}, nil
}
