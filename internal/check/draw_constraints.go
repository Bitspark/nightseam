package check

import (
	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// An open draw is an obligation on its eventual filler. Uses already carries
// this obligation through aliases, applications and parameter forwarding.
func (c *checker) drawArguments(uses []analysis.Use, with map[string]model.Filler, at diag.Location) {
	seen := map[analysis.Use]bool{}
	for _, use := range uses {
		if use.Type == "" || model.Carried(use.Type) || seen[use] {
			continue
		}
		seen[use] = true
		filler := with[use.Parameter]
		provider := c.f.Imported[filler.Family]
		if provider == nil {
			continue // Scope, import and tier checks diagnose invalid fillers.
		}
		t := provider.Types[use.Type]
		reason := ""
		switch {
		case t == nil:
			reason = "does not declare the member"
		case t.Kind == model.KindAlias || t.Kind == model.KindCallable:
			reason = "does not declare a plain record, entity, enum or union"
		case len(t.Parameters) > 0 || len(provider.Generics().Types[use.Type]) > 0:
			reason = "declares a generic member"
		}
		if reason != "" {
			c.Addf(at.Sub("with", use.Parameter), "invalid_draw", "Family %s cannot supply %s.%s: it %s.", provider.Name, use.Parameter, use.Type, reason)
		}
	}
}
