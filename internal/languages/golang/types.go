package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/contract"
)

// generateTypes renders the protocol package's wire types: a struct per
// record with codecs that validate on both directions, a string type with
// constants per enum, an alias per alias, and the package's Tag, which every
// record and enum returns from Of: what an entry point of a generic package
// holds its type arguments to, so that every type drawn from one parameter
// comes from one family. A type that holds a slot of a parameter takes the
// slot as a type parameter, and so does every type that refers to it; a
// consumer instantiates them with the family that fills the slot, and the
// codec of that family's type validates what fills it.
func generateTypes(api contract.API, g contract.Generics, p paths) string {
	var b strings.Builder
	b.WriteString("// Tag is this family, as a type: what every record and enum of the package returns from Of, and what an entry point of a package generic in a family holds its type arguments to.\ntype Tag struct{}\n")
	for _, name := range api.TypeNames() {
		t := api.Types[name]
		kinds := g.Types[name]
		switch t.Kind {
		case "record":
			var body strings.Builder
			for _, field := range api.FlattenedFields(name) {
				tag := field.Name
				if !field.Required {
					tag += ",omitzero"
				}
				fmt.Fprintf(&body, "%s %s `json:%q`\n", fieldName(field), goFieldType(g, field), tag)
			}
			if t.Open {
				body.WriteString("AdditionalFields map[string]json.RawMessage `json:\"-\"`\n")
			}
			fmt.Fprintf(&b, "type %s%s struct {\n%s}\n", name, declare(kinds), body.String())
			// The codecs go through a wire type with the record's fields and no
			// methods, so that they do not recurse into themselves: a local type
			// for a plain record; for a generic one, since Go declares no type
			// inside a generic function, the struct literal itself, converted
			// to and from.
			self := name + apply(kinds)
			wire, local := "wire", fmt.Sprintf("type wire %s;", self)
			if len(kinds) > 0 {
				wire, local = "(struct {\n"+body.String()+"})", ""
			}
			fmt.Fprintf(&b, "func (v %s) MarshalJSON() ([]byte,error) { %sdata,err:=json.Marshal(%s(v));if err!=nil{return nil,err};", self, local, wire)
			if t.Open {
				fmt.Fprintf(&b, "var obj map[string]json.RawMessage;if err=json.Unmarshal(data,&obj);err!=nil{return nil,err};declared:=map[string]bool{};for _,key:=range recordFields(%q){declared[key]=true};for key,value:=range v.AdditionalFields{if declared[key]{return nil,fmt.Errorf(\"additional field overlaps declared field %%s\",key)};obj[key]=value};data,err=json.Marshal(obj);if err!=nil{return nil,err};", name)
			}
			fmt.Fprintf(&b, "if err=ValidateRaw(%q,data);err!=nil{return nil,err};return data,nil }\n", name)
			fmt.Fprintf(&b, "func (v *%s) UnmarshalJSON(data []byte) error {if err:=ValidateRaw(%q,data);err!=nil{return err};%svar decoded %s;if err:=json.Unmarshal(data,&decoded);err!=nil{return err};*v=%s(decoded);", self, name, local, wire, self)
			if t.Open {
				fmt.Fprintf(&b, "var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};for _,key:=range recordFields(%q){delete(fields,key)};v.AdditionalFields=fields;", name)
			}
			b.WriteString("return nil}\n")
			fmt.Fprintf(&b, "func (%s) Of() Tag { return Tag{} }\n", self)
		case "enum":
			fmt.Fprintf(&b, "type %s string\nconst(\n", name)
			for _, value := range t.Values {
				fmt.Fprintf(&b, "%s %s = %q\n", enumConstantName(name, value), name, value)
			}
			b.WriteString(")\n")
			fmt.Fprintf(&b, "func (%s) Of() Tag { return Tag{} }\n", name)
		case "alias":
			fmt.Fprintf(&b, "type %s%s = %s\n", name, declare(kinds), goType(g, t.Type, ""))
		}
	}
	// The public errors the family declares: a handler returns one as a
	// *runtime.PublicError, a caller tells them apart by code.
	if len(api.Errors) > 0 {
		b.WriteString("// The public errors of the family: what a handler returns, as the Code of a *runtime.PublicError, and a caller tells apart with IsError.\nconst (\n")
		var codes []string
		for _, e := range api.Errors {
			if e.Description != "" {
				fmt.Fprintf(&b, "// %s: %s\n", errorName(e.Code), e.Description)
			}
			fmt.Fprintf(&b, "%s = %q\n", errorName(e.Code), e.Code)
			codes = append(codes, errorName(e.Code))
		}
		fmt.Fprintf(&b, ")\n// Errors is every public error code the family declares.\nvar Errors = []string{%s}\n", strings.Join(codes, ", "))
		b.WriteString("// IsError reports whether an error is, or wraps, the family's public error of the code.\nfunc IsError(err error, code string) bool { var public *runtime.PublicError; return errors.As(err, &public) && public.Code == code }\n")
	}
	return goFile(api, "protocol", b.String(), p)
}

// errorName is the constant the protocol package declares for one public
// error: Error and the code in upper camel case, ErrorNotFound for
// not_found.
func errorName(code string) string { return "Error" + goName(code) }
