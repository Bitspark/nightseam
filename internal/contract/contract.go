// Package contract is the input side of the generator: the API contract as a
// typed value, its schema, and the rules that hold for every language. What a
// particular language cannot generate is that language's to say, behind the
// seam in package spi. Nothing here reads a checkout, a process or the network.
//
// It descends from Nightshift's dev/go/repo/generate/contract.go (D-001), less
// the checks that name Go or TypeScript.
package contract

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

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

// Type, Field and their tags are also the bytes each language embeds in its
// generated runtime validator; their field order and tags are part of the
// generated output.
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

// Diagnostic locates one problem in a contract by JSON pointer.
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

// Parse validates a JSON-compatible contract value against the schema and
// returns its typed form. The diagnostics are the schema's alone; a contract
// that passes is well-formed enough for Check and for every language's Check.
// It performs no filesystem, process, or network operations.
func Parse(input map[string]any) (API, []Diagnostic) {
	api := API{}
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
		diagnostics := []Diagnostic{}
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			appendSchemaDiagnostics(&diagnostics, validationErr)
		} else {
			diagnostics = append(diagnostics, Diagnostic{"schema_error", "", err.Error()})
		}
		Sort(diagnostics)
		return api, diagnostics
	}
	if err := json.Unmarshal(data, &api); err != nil {
		return API{}, []Diagnostic{{"invalid_json", "", err.Error()}}
	}
	return api, nil
}

// Validate returns the schema's diagnostics, or else the contract's own, sorted.
// It is the contract's view alone; the kernel adds every language's.
func Validate(input map[string]any) []Diagnostic {
	api, diagnostics := Parse(input)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	diagnostics = Check(api)
	Sort(diagnostics)
	return diagnostics
}

func appendSchemaDiagnostics(out *[]Diagnostic, err *jsonschema.ValidationError) {
	if len(err.Causes) > 0 {
		for _, cause := range err.Causes {
			appendSchemaDiagnostics(out, cause)
		}
		return
	}
	pointer := ""
	for _, part := range err.InstanceLocation {
		pointer += "/" + EscapePointer(part)
	}
	*out = append(*out, Diagnostic{"schema_validation", pointer, err.Error()})
}

// EscapePointer escapes one JSON-pointer segment.
func EscapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

// Sort orders diagnostics by pointer, code and message, so that a merged set
// from several checkers reads the same on every run.
func Sort(diagnostics []Diagnostic) {
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

// Primitive reports whether a type name is one of the wire primitives rather
// than a declared type.
func Primitive(name string) bool { return primitiveTypes[name] }

// TypeNames returns the declared type names in byte order, the order every
// language declares them in.
func (api API) TypeNames() []string {
	names := make([]string, 0, len(api.Types))
	for name := range api.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FieldAt is one field of a record as inheritance reaches it: the record
// that declares it, its index there, and its JSON pointer in the contract.
type FieldAt struct {
	Owner   string
	Index   int
	Pointer string
	Field   Field
}

// WalkFields visits a record's fields in wire order: each inherited record's
// fields first, in extends order and depth first, then its own. It walks each
// inheritance path separately, so a diamond visits its shared fields twice,
// as the wire would carry them twice; a cycle is cut where it closes, and a
// parent that is not a record is skipped, so the walk is safe on any contract
// the schema accepts.
func (api API) WalkFields(name string, visit func(FieldAt)) {
	var walk func(string, map[string]bool)
	walk = func(current string, stack map[string]bool) {
		if stack[current] {
			return
		}
		stack[current] = true
		defer delete(stack, current)
		t := api.Types[current]
		for _, parent := range t.Extends {
			if p, ok := api.Types[parent]; ok && p.Kind == "record" {
				walk(parent, stack)
			}
		}
		for i, field := range t.Fields {
			visit(FieldAt{current, i, fmt.Sprintf("/types/%s/fields/%d", EscapePointer(current), i), field})
		}
	}
	walk(name, map[string]bool{})
}

// FlattenedFields is a record's fields in wire order, inherited first.
func (api API) FlattenedFields(name string) []Field {
	var fields []Field
	api.WalkFields(name, func(at FieldAt) { fields = append(fields, at.Field) })
	return fields
}

// Check reports what breaks in a contract for every language: a reference to
// no type, inheritance from something other than a record, a value cycle, a
// wire field or operation name carried twice, a request that is not a record,
// a public error code repeated. It runs on any contract Parse accepted.
func Check(api API) []Diagnostic {
	diagnostics := []Diagnostic{}
	add := func(code, pointer, message string) {
		diagnostics = append(diagnostics, Diagnostic{code, pointer, message})
	}
	names := api.TypeNames()
	edges := map[string][]string{}
	var expression func(TypeExpr, string, string)
	expression = func(expr TypeExpr, pointer, owner string) {
		switch x := expr.(type) {
		case string:
			if Primitive(x) {
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
		pointer := "/types/" + EscapePointer(name)
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
				expression(field.Type, fmt.Sprintf("%s/fields/%d/type", pointer, i), name)
			}
		case "alias":
			expression(t.Type, pointer+"/type", name)
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
				add("cyclic_type", "/types/"+EscapePointer(name), "Value schema cycle reaches "+next+".")
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
		wireNames := map[string]string{}
		api.WalkFields(name, func(at FieldAt) {
			if previous, ok := wireNames[at.Field.Name]; ok {
				add("field_collision", at.Pointer+"/name", fmt.Sprintf("Record %s repeats field %s already provided at %s.", name, at.Field.Name, previous))
			} else {
				wireNames[at.Field.Name] = at.Pointer
			}
		})
	}
	// The wire namespace is shared across methods and events in each direction.
	wireOperations := map[string]string{}
	operation := func(kind string, index int, name, direction string) {
		p := fmt.Sprintf("/%s/%d", kind, index)
		key := direction + ":" + name
		if previous, ok := wireOperations[key]; ok {
			add("operation_collision", p+"/name", "Operation name collides with "+previous+".")
		} else {
			wireOperations[key] = p
		}
	}
	for i, method := range api.Methods {
		p := fmt.Sprintf("/methods/%d", i)
		operation("methods", i, method.Name, method.Direction)
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
		operation("events", i, event.Name, event.Direction)
		expression(event.Type, fmt.Sprintf("/events/%d/type", i), "")
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
