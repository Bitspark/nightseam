package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/render"
)

// unionBase is one applied base, spelled in the extending declaration's
// scope. Its alternatives use the base's payload types, so conversion
// preserves the selected pointer even across packages and applications.
type unionBase struct {
	typeName, widen, narrow string
	variants                []unionBaseVariant
}

type unionBaseVariant struct {
	baseField, field, kind string
}

func (f *file) emitUnionConversions(t *render.Type, bases []unionBase) {
	self := f.plan.types[t.Name] + apply(t.Uses)
	for _, base := range bases {
		f.linef("// %s includes every base alternative without changing its payload.", base.widen)
		f.w.Block(fmt.Sprintf("func %s%s(v %s) %s {", base.widen, declare(t.Uses), base.typeName, self), "}", func() {
			f.linef("return %s{", self)
			for _, variant := range base.variants {
				f.linef("%s: v.%s,", variant.field, variant.baseField)
			}
			f.line("}")
		})
		f.linef("// %s succeeds when exactly one alternative belonging to the base is selected.", base.narrow)
		f.w.Block(fmt.Sprintf("func %s%s(v %s) (%s, bool) {", base.narrow, declare(t.Uses), self, base.typeName), "}", func() {
			var kinds []string
			for _, variant := range base.variants {
				kinds = append(kinds, variant.kind)
			}
			f.w.Block(fmt.Sprintf("switch v.%s() {", identKind), "}", func() {
				if len(kinds) == 0 {
					return
				}
				f.linef("case %s:", strings.Join(kinds, ", "))
				f.linef("return %s{", base.typeName)
				for _, variant := range base.variants {
					f.linef("%s: v.%s,", variant.baseField, variant.field)
				}
				f.line("}, true")
			})
			f.linef("return %s{}, false", base.typeName)
		})
	}
}
