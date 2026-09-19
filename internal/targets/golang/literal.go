package golang

import (
	"fmt"
	"sort"
)

func (f *file) emitLiterals() {
	values := make([]string, 0, len(f.plan.literals))
	for value := range f.plan.literals {
		values = append(values, value)
	}
	sort.Strings(values)
	for _, value := range values {
		literal := f.plan.literals[value]
		f.linef("type %s string", literal.name)
		f.linef("const %s %s = %q", literal.constant, literal.name, value)
		f.linef("func (%s) %s() %s.TypeBinding { return %s.TypeBinding{Schema: schema, Type: map[string]any{\"literal\": %q}} }", literal.name, identWireType, f.runtime(), f.runtime(), value)
		f.linef("func (%s) %s() %s { return %s{} }", literal.name, identOf, identTag, identTag)
		f.w.Block(fmt.Sprintf("func (v %s) %s() ([]byte, error) {", literal.name, identMarshalJSON), "}", func() {
			f.linef("if v != %s { return nil, %s.Errorf(%q) }", literal.constant, f.std("fmt"), "expected literal "+quote(value))
			f.linef("return %s.Marshal(string(v))", f.std("json"))
		})
		f.w.Block(fmt.Sprintf("func (v *%s) %s(data []byte) error {", literal.name, identUnmarshalJSON), "}", func() {
			f.linef("if err := schema.%s(map[string]any{\"literal\": %q}, data); err != nil { return err }", identValidateExpressionRaw, value)
			f.linef("*v = %s", literal.constant)
			f.line("return nil")
		})
	}
}
