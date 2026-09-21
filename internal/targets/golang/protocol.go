package golang

import (
	"fmt"
	"strings"
)

// emitTypes renders the protocol package's wire types: a struct per
// record with codecs that validate on both directions, a string type with
// constants per enum, an alias per alias, and the package's Tag, which
// every record and enum returns from Of: what an entry point of a generic
// package holds its type arguments to, so that every type drawn from one
// parameter comes from one family. A type that draws on a parameter takes
// the drawn type as a type parameter, and so does every type that refers
// to it; a consumer instantiates them with the family that fills the slot,
// and the codec of that family's type validates what fills it.
func emitTypes(f *file) {
	p := f.plan
	f.line("// Tag is this family, as a type: what every record and enum of the package returns from Of, and what an entry point of a package generic in a family holds its type arguments to.")
	f.linef("type %s struct{}", identTag)
	f.emitLiterals()
	for _, t := range f.family.Types {
		f.uses = t.Uses
		name := p.types[t.Name]
		switch t.Kind {
		case "record", "entity":
			f.line("")
			if t.Description != "" {
				f.linef("// %s: %s", name, t.Description)
			}
			var fields, fieldNames []string
			for _, field := range t.Fields {
				fieldNames = append(fieldNames, quote(field.Name))
				tag := field.Name
				if !field.Required {
					tag += ",omitzero"
				}
				fieldName, _ := p.fieldName(field)
				fields = append(fields, fmt.Sprintf("%s %s `json:%q`", fieldName, f.fieldType(field), tag))
			}
			if t.Open {
				fields = append(fields, fmt.Sprintf("%s map[string]%s.RawMessage `json:\"-\"`", identAdditionalFields, f.std("json")))
			}
			f.w.Block(fmt.Sprintf("type %s%s struct {", name, declare(t.Uses)), "}", func() {
				for _, field := range fields {
					f.line(field)
				}
			})
			if t.IsLive {
				f.emitLiveRefusal(name+apply(t.Uses), t)
				f.linef("func (%s) %s() %s { return %s{} }", name+apply(t.Uses), identOf, identTag, identTag)
				f.emitWireType(t)
				continue
			}
			// The codecs go through a wire type with the record's fields and
			// no methods, so that they do not recurse into themselves: a local
			// type for a plain record; for a generic one, since Go declares no
			// type inside a generic function, the struct literal itself,
			// converted to and from.
			self := name + apply(t.Uses)
			wire, local := "wire", fmt.Sprintf("type wire %s", self)
			if len(t.Uses) > 0 {
				wire, local = "(struct {\n"+strings.Join(fields, "\n")+"\n})", ""
			}
			json := f.std("json")
			f.w.Block(fmt.Sprintf("func (v %s) %s() ([]byte, error) {", self, identMarshalJSON), "}", func() {
				if local != "" {
					f.line(local)
				}
				f.linef("data, err := %s.MarshalJSON(%s(v))", f.runtime(), wire)
				f.line("if err != nil { return nil, err }")
				if t.Open {
					f.linef("var obj map[string]%s.RawMessage", json)
					f.linef("if err = %s.Unmarshal(data, &obj); err != nil { return nil, err }", json)
					f.line("declared := map[string]bool{}")
					f.linef("for _, key := range []string{%s} { declared[key] = true }", strings.Join(fieldNames, ", "))
					f.linef("for key, value := range v.%s {", identAdditionalFields)
					f.linef("\tif declared[key] { return nil, %s.Errorf(\"additional field overlaps declared field %%s\", key) }", f.std("fmt"))
					f.line("\tobj[key] = value")
					f.line("}")
					f.linef("if data, err = %s.MarshalJSON(obj); err != nil { return nil, err }", f.runtime())
				}
				f.linef("if err = %s; err != nil { return nil, err }", f.validateType(t, "data"))
				f.line("return data, nil")
			})
			f.w.Block(fmt.Sprintf("func (v *%s) %s(data []byte) error {", self, identUnmarshalJSON), "}", func() {
				f.linef("if err := %s; err != nil { return err }", f.validateType(t, "data"))
				if local != "" {
					f.line(local)
				}
				f.linef("var decoded %s", wire)
				f.linef("if err := %s.Unmarshal(data, &decoded); err != nil { return err }", json)
				f.linef("*v = %s(decoded)", self)
				if t.Open {
					f.linef("var fields map[string]%s.RawMessage", json)
					f.linef("if err := %s.Unmarshal(data, &fields); err != nil { return err }", json)
					f.linef("for _, key := range []string{%s} { delete(fields, key) }", strings.Join(fieldNames, ", "))
					f.linef("v.%s = fields", identAdditionalFields)
				}
				f.line("return nil")
			})
			f.linef("func (%s) %s() %s { return %s{} }", self, identOf, identTag, identTag)
			f.emitWireType(t)
		case "enum":
			f.line("")
			if t.Description != "" {
				f.linef("// %s: %s", name, t.Description)
			}
			f.linef("type %s string", name)
			f.w.Block("const (", ")", func() {
				for _, value := range t.Values {
					f.linef("%s %s = %q", p.constants[t.Name+"."+value], name, value)
				}
			})
			f.linef("func (%s) %s() %s { return %s{} }", name, identOf, identTag, identTag)
			f.emitWireType(t)
		case "alias":
			f.line("")
			if t.Description != "" {
				f.linef("// %s: %s", name, t.Description)
			}
			f.linef("type %s%s = %s", name, declare(t.Uses), f.spell(t.Alias))
		case "union":
			f.emitUnion(t, f.unionVariants(t))
			f.emitWireType(t)
			f.emitUnionConversions(t, f.unionBases(t))
		}
	}
	f.uses = f.family.Uses
	f.emitLive()
	// The public errors the family declares: a handler returns one as a
	// *runtime.PublicError, a caller tells them apart by code.
	if len(f.family.Errors) > 0 {
		f.line("")
		f.line("// The public errors of the family: what a handler returns, as the Code of a *runtime.PublicError, and a caller tells apart with IsError.")
		var names []string
		f.w.Block("const (", ")", func() {
			for _, e := range f.family.Errors {
				name := p.errors[e.Code]
				if e.Description != "" {
					f.linef("// %s: %s", name, e.Description)
				}
				f.linef("%s = %q", name, e.Code)
				names = append(names, name)
			}
		})
		f.linef("// %s is every public error code the family declares.", identErrors)
		f.linef("var %s = []string{%s}", identErrors, strings.Join(names, ", "))
		f.linef("// %s reports whether an error is, or wraps, the family's public error of the code.", identIsError)
		f.linef("func %s(err error, code string) bool { var public *%s.PublicError; return %s.As(err, &public) && public.Code == code }", identIsError, f.runtime(), f.std("errors"))
	}
}

// emitValidation renders the protocol package's validator: the family's
// wire description, read by the runtime's schema, and the validators of
// the families it refers to, each validating its own types.
func emitValidation(f *file) {
	runtime := f.runtime()
	var imported []string
	for _, family := range f.family.References {
		imported = append(imported, fmt.Sprintf("%q: %s.%s()", family, f.peer(family), identWireSchema))
	}
	f.line("// schema is the family's wire description, as the runtime validates it; a type of another family is validated by that family's own validator.")
	f.linef("var schema = %s.MustSchema(%s, %s(), map[string]*%s.Schema{%s}).MustWithDeclaration(%s())", runtime, quote(f.family.Wire), identWireDigest, runtime, strings.Join(imported, ", "), identWireDeclaration)
	f.linef("// %s supplies the family's descriptor and imports for scoped validation.", identWireSchema)
	f.linef("func %s() *%s.Schema { return schema }", identWireSchema, runtime)
	f.linef("// %s is the family's canonical wire-visible declaration, including reachable imported contracts.", identWireDeclaration)
	f.linef("func %s() string { return %q }", identWireDeclaration, f.family.Declaration)
	f.linef("// %s is the SHA-256 digest of the exact UTF-8 bytes returned by %s.", identWireDigest, identWireDeclaration)
	f.linef("func %s() string { return %q }", identWireDigest, f.family.WireDigest)
	f.line("")
	f.linef("// %s verifies a named contract value, including null and field presence; at roots the diagnostic where a family that imports this one holds the value.", identValidateRaw)
	f.linef("func %s(name string, data []byte, at ...string) error { return schema.%s(name, data, at...) }", identValidateRaw, identValidateRaw)
	f.linef("// %s verifies a type expression and rejects trailing values.", identValidateExpressionRaw)
	f.linef("func %s(expression any, data []byte, at ...string) error { return schema.%s(expression, data, at...) }", identValidateExpressionRaw, identValidateExpressionRaw)
	f.linef("// %s validates a typed value before publishing it on the wire.", identValidateValue)
	f.linef("func %s(expression any, value any) error { return schema.%s(expression, value) }", identValidateValue, identValidateValue)
	f.linef("// %s decodes a generated expression and panics on one it cannot read; callers ordinarily use named types.", identMustTypeExpression)
	f.linef("func %s(encoded string) any { return %s.%s(encoded) }", identMustTypeExpression, runtime, identMustTypeExpression)
}
