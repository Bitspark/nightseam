package golang

import (
	"strings"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/spi"
)

// generateValidation renders the protocol package's runtime validator, an
// interpreter over the contract's own types.
func generateValidation(api contract.API) string {
	// Contract data is embedded in each generated protocol package so no developer
	// library becomes an application runtime dependency.
	schema := string(canonicalJSON(api.Types))
	return spi.Header + "package " + pkg(api, "protocol") + "\n" + strings.ReplaceAll(goValidationTemplate, "CONTRACT_JSON", quote(schema))
}

const goValidationTemplate = `
import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
 "math"
 "strconv"
 "strings"
 "time"
)

type wireField struct {Name string; Type any; Required *bool; Nullable bool}
type wireType struct {Kind string; Fields []wireField; Extends []string; Open bool; Values []string; Type any}
var wireTypes = func() map[string]wireType {var result map[string]wireType; if err:=json.Unmarshal([]byte(CONTRACT_JSON),&result);err!=nil{panic(err)};return result}()

// ValidateRaw verifies a named contract value, including null and field presence.
func ValidateRaw(name string,data []byte) error {return ValidateExpressionRaw(name,data)}
// ValidateExpressionRaw verifies a type expression and rejects trailing values.
func ValidateExpressionRaw(expression any,data []byte) error {
 decoder:=json.NewDecoder(bytes.NewReader(data));decoder.UseNumber();var value any
 if err:=decoder.Decode(&value);err!=nil{return err};var extra any;if err:=decoder.Decode(&extra);err!=io.EOF{return fmt.Errorf("expected exactly one JSON value")}
 return validateWire(expression,value,"$")
}
// ValidateValue validates a typed value before publishing it on the wire.
func ValidateValue(expression any,value any) error {data,err:=json.Marshal(value);if err!=nil{return err};return ValidateExpressionRaw(expression,data)}
// TypeExpression decodes a generated expression; callers ordinarily use named types.
func TypeExpression(encoded string) any {var value any;if err:=json.Unmarshal([]byte(encoded),&value);err!=nil{panic(err)};return value}

func recordFields(name string) []string {var out []string; t:=wireTypes[name];for _,base:=range t.Extends{out=append(out,recordFields(base)...)};for _,field:=range t.Fields{out=append(out,field.Name)};return out}
func flattenedFields(name string) []wireField {var out []wireField;t:=wireTypes[name];for _,base:=range t.Extends{out=append(out,flattenedFields(base)...)};return append(out,t.Fields...)}
func validateWire(expression any,value any,location string) error {
 bad:=func(want string)error{return fmt.Errorf("%s: expected %s",location,want)}
 if composite,ok:=expression.(map[string]any);ok{
  if element,ok:=composite["array"];ok {items,ok:=value.([]any);if !ok{return bad("array")};for i,item:=range items{if err:=validateWire(element,item,fmt.Sprintf("%s[%d]",location,i));err!=nil{return err}};return nil}
  if element,ok:=composite["map"];ok {items,ok:=value.(map[string]any);if !ok{return bad("object")};for key,item:=range items{if err:=validateWire(element,item,location+"."+key);err!=nil{return err}};return nil}
  if _,ok:=composite["empty"];ok {items,ok:=value.(map[string]any);if !ok||len(items)!=0{return bad("empty object")};return nil}
  return bad("supported type expression")
 }
 name,ok:=expression.(string);if !ok{return bad("type expression")}
 switch name {
 case "json":return validateJSON(value,location)
 case "string":if _,ok:=value.(string);!ok{return bad("string")};return nil
 case "boolean":if _,ok:=value.(bool);!ok{return bad("boolean")};return nil
 case "number","integer":n,ok:=value.(json.Number);if !ok{return bad(name)};v,err:=n.Float64();if err!=nil||math.IsInf(v,0)||math.IsNaN(v){return bad("finite number")};if name=="integer"&&!safeInteger(string(n)){return bad("JavaScript-safe integer")};return nil
 case "timestamp":text,ok:=value.(string);if !ok{return bad("timestamp")};if _,err:=time.Parse(time.RFC3339Nano,text);err!=nil{return bad("RFC3339 timestamp")};return nil
 }
 t,ok:=wireTypes[name];if !ok{return bad("known type")}
 switch t.Kind {
 case "alias":return validateWire(t.Type,value,location)
 case "enum":text,ok:=value.(string);if !ok{return bad(name)};for _,option:=range t.Values{if option==text{return nil}};return bad(name)
 case "record":obj,ok:=value.(map[string]any);if !ok{return bad(name+" object")};allowed:=map[string]bool{};for _,field:=range flattenedFields(name){allowed[field.Name]=true;member,present:=obj[field.Name];required:=field.Required==nil||*field.Required;if !present{if required{return fmt.Errorf("%s.%s: required field missing",location,field.Name)};continue};if member==nil&&field.Nullable{continue};if member==nil&&!field.Nullable{return fmt.Errorf("%s.%s: null is not permitted",location,field.Name)};if err:=validateWire(field.Type,member,location+"."+field.Name);err!=nil{return err}};for key,value:=range obj{if !allowed[key]{if !t.Open{return fmt.Errorf("%s.%s: unknown field",location,key)};if err:=validateJSON(value,location+"."+key);err!=nil{return err}}};return nil
 }
 return bad("supported type")
}
func validateJSON(value any,location string)error{switch typed:=value.(type){case json.Number:if n,err:=typed.Float64();err!=nil||math.IsInf(n,0)||math.IsNaN(n){return fmt.Errorf("%s: expected finite JSON number",location)};case []any:for i,item:=range typed{if err:=validateJSON(item,fmt.Sprintf("%s[%d]",location,i));err!=nil{return err}};case map[string]any:for key,item:=range typed{if err:=validateJSON(item,location+"."+key);err!=nil{return err}}};return nil}
// Check decimal integer precision without constructing huge powers from an
// untrusted exponent. The JSON decoder has already checked number syntax.
func safeInteger(raw string)bool{
 text:=strings.TrimPrefix(raw,"-");mantissa,exponent:=text,"0";if at:=strings.IndexAny(text,"eE");at>=0{mantissa,exponent=text[:at],text[at+1:]};fraction:=0;if at:=strings.IndexByte(mantissa,'.');at>=0{fraction=len(mantissa)-at-1;mantissa=mantissa[:at]+mantissa[at+1:]};digits:=strings.TrimLeft(mantissa,"0");if digits==""{return true};power,err:=strconv.ParseInt(exponent,10,64);if err!=nil||power>int64(len(raw))+16||power< -int64(len(raw))-16{return false};scale:=power-int64(fraction);if scale<0{cut:= -scale;if cut>int64(len(digits)){return false};tail:=digits[len(digits)-int(cut):];if strings.Trim(tail,"0")!=""{return false};digits=digits[:len(digits)-int(cut)]}else{if int64(len(digits))+scale>16{return false};digits+=strings.Repeat("0",int(scale))};if len(digits)>16{return false};value,err:=strconv.ParseUint(digits,10,64);return err==nil&&value<=9007199254740991
}
`
