package typescript

import (
	"fmt"
	"path"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Scaffold writes one client-side model factory. Reverse calls and received
// notifications are separate facets; the consumer fills in the behavior.
func (t *target) Scaffold(f *render.Family, dir string) ([]spi.File, error) {
	if err := t.config.Validate(); err != nil {
		return nil, err
	}
	if !f.HasModel() || len(f.Client.Methods) == 0 && len(f.Server.Events) == 0 {
		return nil, nil
	}
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("the family does not pass the TypeScript target's check: %s", diagnostics[0])
	}
	w := emit.NewWriter("  ")
	w.Linef("import type { ClientModel } from %s;", quote(t.config.pkg(f.Name)+"/types"))
	w.Line("")
	w.Linef("/** The %s client model. Fill in the behavior; nightseam writes this file once. */", f.Name)
	w.Block("export const model: ClientModel = remote => ({", "});", func() {
		w.Block("methods: {", "},", func() {
			for _, m := range f.Client.Methods {
				if m.Description != "" {
					w.Linef("/** %s */", comment(m.Description))
				}
				w.Block(scaffoldSignature(p, m)+" {", "},", func() { w.Linef("throw new Error(%s);", quote(m.Name+" is not implemented")) })
			}
		})
		w.Block("events: {", "},", func() {
			for _, e := range f.Server.Events {
				w.Linef("%s(data, context) { void data; void context; },", p.operations[e.Name])
			}
		})
	})
	return []spi.File{{Path: path.Join(dir, "handler.ts"), Data: []byte(w.String())}}, nil
}
