package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/contract"
)

// generateTypes renders the protocol package's wire types: a struct per
// record with codecs that validate on both directions, a string type with
// constants per enum, an alias per alias.
func generateTypes(api contract.API, p paths) string {
	var b strings.Builder
	for _, name := range api.TypeNames() {
		t := api.Types[name]
		switch t.Kind {
		case "record":
			fmt.Fprintf(&b, "type %s struct {\n", name)
			fields := api.FlattenedFields(name)
			for _, field := range fields {
				tag := field.Name
				if !field.Required {
					tag += ",omitzero"
				}
				fmt.Fprintf(&b, "%s %s `json:%q`\n", fieldName(field), goFieldType(field), tag)
			}
			if t.Open {
				b.WriteString("AdditionalFields map[string]json.RawMessage `json:\"-\"`\n")
			}
			b.WriteString("}\n")
			{
				fmt.Fprintf(&b, "func (v %s) MarshalJSON() ([]byte,error) { type wire %s; data,err:=json.Marshal(wire(v));if err!=nil{return nil,err};", name, name)
				if t.Open {
					fmt.Fprintf(&b, "var obj map[string]json.RawMessage;if err=json.Unmarshal(data,&obj);err!=nil{return nil,err};declared:=map[string]bool{};for _,key:=range recordFields(%q){declared[key]=true};for key,value:=range v.AdditionalFields{if declared[key]{return nil,fmt.Errorf(\"additional field overlaps declared field %%s\",key)};obj[key]=value};data,err=json.Marshal(obj);if err!=nil{return nil,err};", name)
				}
				fmt.Fprintf(&b, "if err=ValidateRaw(%q,data);err!=nil{return nil,err};return data,nil }\n", name)
				fmt.Fprintf(&b, "func (v *%s) UnmarshalJSON(data []byte) error {if err:=ValidateRaw(%q,data);err!=nil{return err};type wire %s;var decoded wire;if err:=json.Unmarshal(data,&decoded);err!=nil{return err};*v=%s(decoded);", name, name, name, name)
				if t.Open {
					fmt.Fprintf(&b, "var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};for _,key:=range recordFields(%q){delete(fields,key)};v.AdditionalFields=fields;", name)
				}
				b.WriteString("return nil}\n")
			}
		case "enum":
			fmt.Fprintf(&b, "type %s string\nconst(\n", name)
			for _, value := range t.Values {
				fmt.Fprintf(&b, "%s %s = %q\n", enumConstantName(name, value), name, value)
			}
			b.WriteString(")\n")
		case "alias":
			fmt.Fprintf(&b, "type %s = %s\n", name, goType(t.Type, ""))
		}
	}
	return goFile(api, "protocol", b.String(), p)
}
