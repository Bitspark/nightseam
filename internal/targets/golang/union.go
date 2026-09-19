package golang

import (
	"fmt"

	"github.com/Bitspark/nightseam/internal/render"
)

// unionVariant is one planned Go alternative. A record uses its existing
// type; another payload has a Value wrapper. An inherited wrapper has no
// local declaration, but its selected value is still read through Value.
type unionVariant struct {
	tag, field, kind     string
	typeName             string
	wrapper, payloadType string
	wrapped, empty       bool
}

// emitUnion writes a concrete, composable union codec. The adjacent wire
// envelope keeps payload keys and null separate from the discriminator.
// A selected pointer distinguishes a variant's zero payload from absence.
func (f *file) emitUnion(t *render.Type, variants []unionVariant) {
	name := f.plan.types[t.Name]
	u := f.plan.unions[t.Name]
	self := name + apply(t.Uses)
	json := f.std("json")
	for _, variant := range variants {
		if variant.wrapper != "" {
			f.linef("type %s%s struct { Value %s }", variant.wrapper, declare(t.Uses), variant.payloadType)
		}
	}
	f.linef("type %s string", u.kind)
	f.w.Block("const (", ")", func() {
		for _, variant := range variants {
			f.linef("%s %s = %q", variant.kind, u.kind, variant.tag)
		}
	})
	f.w.Block(fmt.Sprintf("type %s%s struct {", name, declare(t.Uses)), "}", func() {
		for _, variant := range variants {
			f.linef("%s *%s", variant.field, variant.typeName)
		}
	})
	f.w.Block(fmt.Sprintf("func (v %s) %s() %s {", self, identKind, u.kind), "}", func() {
		f.linef("var kind %s", u.kind)
		f.line("selected := 0")
		for _, variant := range variants {
			f.linef("if v.%s != nil { selected++; kind = %s }", variant.field, variant.kind)
		}
		f.line("if selected != 1 { return \"\" }")
		f.line("return kind")
	})
	f.w.Block(fmt.Sprintf("func (v %s) %s() ([]byte, error) {", self, identMarshalJSON), "}", func() {
		f.linef("kind := v.%s()", identKind)
		f.linef("if kind == \"\" { return nil, %s.Errorf(%q) }", f.std("fmt"), "union "+t.Name+" requires exactly one selected variant")
		f.linef("envelope := map[string]any{%q: kind}", t.Tag)
		f.w.Block("switch kind {", "}", func() {
			for _, variant := range variants {
				f.linef("case %s:", variant.kind)
				if variant.empty {
					continue
				}
				payload := "v." + variant.field
				if variant.wrapped {
					payload += ".Value"
				}
				f.linef("envelope[%q] = %s", t.Value, payload)
			}
		})
		f.linef("data, err := %s.MarshalJSON(envelope)", f.runtime())
		f.line("if err != nil { return nil, err }")
		f.linef("if err = %s; err != nil { return nil, err }", f.validateType(t, "data"))
		f.line("return data, nil")
	})
	f.w.Block(fmt.Sprintf("func (v *%s) %s(data []byte) error {", self, identUnmarshalJSON), "}", func() {
		f.linef("if err := %s; err != nil { return err }", f.validateType(t, "data"))
		f.linef("var envelope map[string]%s.RawMessage", json)
		f.linef("if err := %s.Unmarshal(data, &envelope); err != nil { return err }", json)
		f.linef("var kind %s", u.kind)
		f.linef("if err := %s.Unmarshal(envelope[%q], &kind); err != nil { return err }", json, t.Tag)
		f.linef("var decoded %s", self)
		f.w.Block("switch kind {", "}", func() {
			for _, variant := range variants {
				f.linef("case %s:", variant.kind)
				f.linef("decoded.%s = new(%s)", variant.field, variant.typeName)
				if variant.empty {
					continue
				}
				payload := "decoded." + variant.field
				if variant.wrapped {
					payload = "&" + payload + ".Value"
				}
				f.linef("if err := %s.Unmarshal(envelope[%q], %s); err != nil { return err }", json, t.Value, payload)
			}
			f.line("default:")
			f.linef("return %s.Errorf(%q, kind)", f.std("fmt"), "unknown "+t.Name+" union tag %q")
		})
		f.line("*v = decoded")
		f.line("return nil")
	})
	f.linef("func (%s) %s() %s { return %s{} }", self, identOf, identTag, identTag)
}
