// Package generate validates API contracts and renders deterministic owned sources.
package generate

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// TypeExpr is a primitive or named type string, or an object with one array/map expression.
type TypeExpr = any

// API is a versioned, language-independent wire contract.
type API struct {
	SchemaVersion int             `json:"schema_version"`
	Profile       string          `json:"profile"`
	Name          string          `json:"name"`
	Types         map[string]Type `json:"types"`
	Methods       []Method        `json:"methods"`
	Events        []Event         `json:"events"`
	Errors        []PublicError   `json:"errors,omitempty"`
}

type Type struct {
	Kind        string   `json:"kind"`
	Description string   `json:"description,omitempty"`
	Fields      []Field  `json:"fields,omitempty"`
	Extends     []string `json:"extends,omitempty"`
	Open        bool     `json:"open,omitempty"`
	Values      []string `json:"values,omitempty"`
	Type        TypeExpr `json:"type,omitempty"`
}

type Field struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	GoName      string   `json:"go_name,omitempty"`
	Type        TypeExpr `json:"type"`
	Required    bool     `json:"required"`
	Nullable    bool     `json:"nullable,omitempty"`
}

// UnmarshalJSON applies the contract's required-by-default field semantics.
func (f *Field) UnmarshalJSON(data []byte) error {
	type plain Field
	v := plain{Required: true}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = Field(v)
	return nil
}

type Method struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	GoName      string   `json:"go_name"`
	TSName      string   `json:"ts_name"`
	Direction   string   `json:"direction"`
	Request     string   `json:"request,omitempty"`
	Result      TypeExpr `json:"result"`
}

type Event struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	GoName      string   `json:"go_name"`
	TSName      string   `json:"ts_name"`
	Direction   string   `json:"direction"`
	Type        TypeExpr `json:"type"`
}

type PublicError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Pointer string `json:"pointer"`
	Message string `json:"message"`
}

//go:embed api.schema.json
var apiSchema []byte

// EmbeddedSchema returns an independent copy of the offline API-contract schema.
func EmbeddedSchema() []byte { return bytes.Clone(apiSchema) }

type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resources are disabled: %s", url)
}

var compiledAPISchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(apiSchema))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(offlineLoader{})
	const id = "urn:nighthall:api-contract:1"
	if err := compiler.AddResource(id, value); err != nil {
		return nil, err
	}
	return compiler.Compile(id)
})

// Parse validates a JSON-compatible contract value and returns its typed form.
// It performs no filesystem, process, or network operations.
func Parse(input map[string]any) (API, []Diagnostic) {
	api := API{}
	diagnostics := []Diagnostic{}
	data, err := json.Marshal(input)
	if err != nil {
		return api, []Diagnostic{{"invalid_json", "", err.Error()}}
	}
	// Normalization also makes typed slices/maps and json.Number behave identically.
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return api, []Diagnostic{{"invalid_json", "", err.Error()}}
	}
	schema, err := compiledAPISchema()
	if err != nil {
		return api, []Diagnostic{{"schema_error", "", err.Error()}}
	}
	if err := schema.Validate(value); err != nil {
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			appendSchemaDiagnostics(&diagnostics, validationErr)
		} else {
			diagnostics = append(diagnostics, Diagnostic{"schema_error", "", err.Error()})
		}
		sortDiagnostics(diagnostics)
		return api, diagnostics
	}
	if err := json.Unmarshal(data, &api); err != nil {
		return API{}, []Diagnostic{{"invalid_json", "", err.Error()}}
	}
	diagnostics = validateSemantics(api)
	sortDiagnostics(diagnostics)
	return api, diagnostics
}

// Validate returns schema and semantic diagnostics without producing sources.
func Validate(input map[string]any) []Diagnostic { _, diagnostics := Parse(input); return diagnostics }

func appendSchemaDiagnostics(out *[]Diagnostic, err *jsonschema.ValidationError) {
	if len(err.Causes) > 0 {
		for _, cause := range err.Causes {
			appendSchemaDiagnostics(out, cause)
		}
		return
	}
	pointer := ""
	for _, part := range err.InstanceLocation {
		pointer += "/" + escapePointer(part)
	}
	*out = append(*out, Diagnostic{"schema_validation", pointer, err.Error()})
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.Slice(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		if a.Pointer != b.Pointer {
			return a.Pointer < b.Pointer
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

var primitiveTypes = map[string]bool{"string": true, "boolean": true, "number": true, "integer": true, "timestamp": true, "json": true}
var reservedTypes = strings.Fields("API Client Server Binding Handler Handlers ClientHandlers ServerHandlers Peer Error PublicError Optional Remote NewHandler Dial ValidateRaw ValidateExpressionRaw ValidateValue TypeExpression WireType WireField Array Record Promise AbortSignal Date Number Object Set")
var reservedGoMethods = strings.Fields("Close Call Notify Handle Connect")

// A method named then would also make the client a Promise-like value, breaking
// the async dial factory through JavaScript's thenable assimilation.
var reservedTSMethods = strings.Fields("constructor close call notify handle connect then")
var tsReserved = strings.Fields("break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with as implements interface let package private protected public static yield any boolean number string symbol type from of namespace unknown never object")

func nameIn(value string, names []string) bool {
	for _, name := range names {
		if value == name {
			return true
		}
	}
	return false
}

// DefaultGoName is the deterministic field-name mapping used by generators.
func DefaultGoName(name string) string {
	var result strings.Builder
	for _, part := range strings.Split(name, "_") {
		if nameIn(strings.ToLower(part), []string{"id", "api", "url", "http", "json", "uuid"}) {
			result.WriteString(strings.ToUpper(part))
			continue
		}
		for i, r := range part {
			if i == 0 {
				r = unicode.ToUpper(r)
			}
			result.WriteRune(r)
		}
	}
	return result.String()
}

var identifierWords = regexp.MustCompile(`[A-Za-z0-9]+`)

func enumConstantName(typeName, value string) string {
	return typeName + DefaultGoName(strings.Join(identifierWords.FindAllString(value, -1), "_"))
}

func validateSemantics(api API) []Diagnostic {
	diagnostics := []Diagnostic{}
	add := func(code, pointer, message string) {
		diagnostics = append(diagnostics, Diagnostic{code, pointer, message})
	}
	names := make([]string, 0, len(api.Types))
	for name := range api.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	edges := map[string][]string{}
	declarations := map[string]string{}
	for _, name := range names {
		declarations[name] = "/types/" + escapePointer(name)
	}
	var expression func(TypeExpr, string, string)
	expression = func(expr TypeExpr, pointer, owner string) {
		switch x := expr.(type) {
		case string:
			if primitiveTypes[x] {
				return
			}
			if _, ok := api.Types[x]; !ok {
				add("unresolved_type", pointer, "Unknown type "+x+".")
				return
			}
			if owner != "" {
				edges[owner] = append(edges[owner], x)
			}
		case map[string]any:
			for _, kind := range []string{"array", "map"} {
				if child, ok := x[kind]; ok {
					expression(child, pointer+"/"+kind, owner)
				}
			}
		}
	}
	for _, name := range names {
		t := api.Types[name]
		pointer := "/types/" + escapePointer(name)
		if nameIn(name, reservedTypes) {
			add("reserved_name", pointer, "Type name is reserved by the generated runtime: "+name+".")
		}
		if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
			add("invalid_name", pointer, "Type name must be a Go identifier.")
		}
		switch t.Kind {
		case "record":
			for i, parent := range t.Extends {
				p := fmt.Sprintf("%s/extends/%d", pointer, i)
				if inherited, ok := api.Types[parent]; !ok {
					add("unresolved_type", p, "Unknown inherited type "+parent+".")
				} else if inherited.Kind != "record" {
					add("invalid_inheritance", p, "An inherited type must be a record.")
				} else {
					edges[name] = append(edges[name], parent)
				}
			}
			for i, field := range t.Fields {
				p := fmt.Sprintf("%s/fields/%d", pointer, i)
				expression(field.Type, p+"/type", name)
				goName := field.GoName
				if goName == "" {
					goName = DefaultGoName(field.Name)
				}
				if goName == "" || !token.IsExported(goName) || !token.IsIdentifier(goName) {
					add("invalid_name", p+"/go_name", "Generated field name must be an exported Go identifier.")
				}
				if nameIn(goName, []string{"MarshalJSON", "UnmarshalJSON"}) {
					add("reserved_name", p+"/go_name", "Field name collides with a generated codec method.")
				}
				if t.Open && goName == "AdditionalFields" {
					add("reserved_name", p+"/go_name", "Field name collides with the open-record extension storage.")
				}
			}
		case "alias":
			expression(t.Type, pointer+"/type", name)
		case "enum":
			for i, value := range t.Values {
				p := fmt.Sprintf("%s/values/%d", pointer, i)
				constant := enumConstantName(name, value)
				if previous, exists := declarations[constant]; exists {
					add("generated_name_collision", p, "Generated enum constant "+constant+" collides with "+previous+".")
				} else {
					declarations[constant] = p
				}
				if nameIn(constant, reservedTypes) {
					add("reserved_name", p, "Generated enum constant is reserved by the runtime.")
				}
			}
		}
	}
	// All recursive value graphs are unsupported, including recursion through arrays.
	state := map[string]int{}
	var visit func(string)
	visit = func(name string) {
		if state[name] == 2 {
			return
		}
		state[name] = 1
		for _, next := range edges[name] {
			if state[next] == 1 {
				add("cyclic_type", "/types/"+escapePointer(name), "Value schema cycle reaches "+next+".")
			} else if state[next] == 0 {
				visit(next)
			}
		}
		state[name] = 2
	}
	for _, name := range names {
		if state[name] == 0 {
			visit(name)
		}
	}
	// Walk each inheritance path separately: a diamond also duplicates wire fields.
	for _, name := range names {
		if api.Types[name].Kind != "record" {
			continue
		}
		wireNames, goNames := map[string]string{}, map[string]string{}
		var fields func(string, map[string]bool)
		fields = func(current string, stack map[string]bool) {
			if stack[current] {
				return
			}
			stack[current] = true
			defer delete(stack, current)
			t := api.Types[current]
			for _, parent := range t.Extends {
				if p, ok := api.Types[parent]; ok && p.Kind == "record" {
					fields(parent, stack)
				}
			}
			for i, field := range t.Fields {
				p := fmt.Sprintf("/types/%s/fields/%d", escapePointer(current), i)
				if previous, ok := wireNames[field.Name]; ok {
					add("field_collision", p+"/name", fmt.Sprintf("Record %s repeats field %s already provided at %s.", name, field.Name, previous))
				} else {
					wireNames[field.Name] = p
				}
				goName := field.GoName
				if goName == "" {
					goName = DefaultGoName(field.Name)
				}
				if api.Types[name].Open && goName == "AdditionalFields" {
					add("reserved_name", p+"/go_name", "Inherited field name collides with the open-record extension storage.")
				}
				if previous, ok := goNames[goName]; ok {
					add("generated_name_collision", p+"/go_name", fmt.Sprintf("Record %s repeats Go field %s already provided at %s.", name, goName, previous))
				} else {
					goNames[goName] = p
				}
			}
		}
		fields(name, map[string]bool{})
	}
	goMethods, tsMethods, wireMethods := map[string]string{}, map[string]string{}, map[string]string{}
	checkOperation := func(kind string, index int, name, goName, tsName, direction string) {
		p := fmt.Sprintf("/%s/%d", kind, index)
		// The wire namespace is shared across methods and events in each direction.
		for _, check := range []struct {
			value, pointer string
			seen           map[string]string
		}{{name, "name", wireMethods}, {goName, "go_name", goMethods}, {tsName, "ts_name", tsMethods}} {
			key := direction + ":" + check.value
			if previous, ok := check.seen[key]; ok {
				add("operation_collision", p+"/"+check.pointer, "Operation name collides with "+previous+".")
			} else {
				check.seen[key] = p
			}
		}
		if nameIn(goName, reservedGoMethods) || nameIn(tsName, reservedTSMethods) {
			add("reserved_name", p, "Operation name is reserved by the runtime.")
		}
		if nameIn(tsName, tsReserved) {
			add("reserved_name", p+"/ts_name", "Operation name is a reserved TypeScript identifier.")
		}
	}
	for i, method := range api.Methods {
		p := fmt.Sprintf("/methods/%d", i)
		checkOperation("methods", i, method.Name, method.GoName, method.TSName, method.Direction)
		if method.Request != "" {
			if t, ok := api.Types[method.Request]; !ok {
				add("unresolved_type", p+"/request", "Unknown request type "+method.Request+".")
			} else if t.Kind != "record" {
				add("invalid_request", p+"/request", "Method request must name a record.")
			}
		}
		expression(method.Result, p+"/result", "")
	}
	for i, event := range api.Events {
		checkOperation("events", i, event.Name, event.GoName, event.TSName, event.Direction)
		expression(event.Type, fmt.Sprintf("/events/%d/type", i), "")
	}
	// Events add receive helpers as well as emit helpers, so their receiver
	// namespace crosses the protocol's direction boundary.
	goClient := map[string]string{"Peer": "generated client field", "Close": "generated client method"}
	goRemote := map[string]string{"Peer": "generated remote field"}
	tsClient := map[string]string{"peer": "generated client field", "close": "generated client method", "constructor": "generated client constructor"}
	register := func(seen map[string]string, name, pointer string) {
		if previous, exists := seen[name]; exists {
			add("generated_name_collision", pointer, "Generated member "+name+" collides with "+previous+".")
		} else {
			seen[name] = pointer
		}
	}
	for i, method := range api.Methods {
		p := fmt.Sprintf("/methods/%d", i)
		if method.Direction == "client_to_server" {
			register(goClient, method.GoName, p+"/go_name")
			register(tsClient, method.TSName, p+"/ts_name")
		} else {
			register(goRemote, method.GoName, p+"/go_name")
		}
	}
	for i, event := range api.Events {
		p := fmt.Sprintf("/events/%d", i)
		tsName := string(unicode.ToUpper(rune(event.TSName[0]))) + event.TSName[1:]
		if event.Direction == "client_to_server" {
			register(goClient, "Emit"+event.GoName, p+"/go_name")
			register(goRemote, "On"+event.GoName, p+"/go_name")
			register(tsClient, "emit"+tsName, p+"/ts_name")
		} else {
			register(goClient, "On"+event.GoName, p+"/go_name")
			register(goRemote, "Emit"+event.GoName, p+"/go_name")
			register(tsClient, "on"+tsName, p+"/ts_name")
		}
	}
	errorsSeen := map[string]bool{}
	for i, publicError := range api.Errors {
		if errorsSeen[publicError.Code] {
			add("duplicate_error", fmt.Sprintf("/errors/%d/code", i), "Duplicate public error code "+publicError.Code+".")
		}
		errorsSeen[publicError.Code] = true
	}
	return diagnostics
}
