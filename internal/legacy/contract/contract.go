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
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// TypeExpr is a primitive or named type string, or an object with one array/map expression.
type TypeExpr = any

// API is a versioned, language-independent wire contract.
//
// Imports names the families whose types this one refers to as family.Type;
// Imported is those families as parsed, which the kernel supplies from the
// world it renders; Families is every family in that world, which a slot may
// name, Sessions those of them that declare the session role, and Members
// those parsed, which a slot of a parameter's type is checked against. None
// of the four is part of the contract's bytes.
type API struct {
	SchemaVersion int               `json:"schema_version"`
	Profile       string            `json:"profile"`
	Name          string            `json:"name"`
	Role          string            `json:"role,omitempty"`
	Layer         string            `json:"layer,omitempty"`
	Layers        map[string]string `json:"layers,omitempty"`
	Parameters    []Parameter       `json:"parameters,omitempty"`
	Imports       []string          `json:"imports,omitempty"`
	Types         map[string]Type   `json:"types"`
	Methods       []Method          `json:"methods"`
	Events        []Event           `json:"events"`
	Errors        []PublicError     `json:"errors,omitempty"`
	Session       *Session          `json:"session,omitempty"`
	Imported      map[string]API    `json:"-"`
	Families      []string          `json:"-"`
	Sessions      []string          `json:"-"`
	Members       map[string]API    `json:"-"`
}

// Parameter is a hole in a family: a slot names it, and a consumer binds it
// to a family that declares its role. A family with parameters is generic in
// them, and Nightseam renders it once, generically, rather than once per
// binding. Of is the role a family must declare to bind the parameter; today
// the only role is the session role.
type Parameter struct {
	Name        string `json:"name"`
	Of          string `json:"of"`
	Description string `json:"description,omitempty"`
}

// ParameterNames is the parameters' names, in declaration order: the order
// every language declares its type parameters in.
func (api API) ParameterNames() []string {
	names := make([]string, len(api.Parameters))
	for i, parameter := range api.Parameters {
		names[i] = parameter.Name
	}
	return names
}

// Parameterized reports whether a slot target names a parameter rather than
// a family. The two are told apart by case, as the schema spells them: a
// parameter is upper camel case, a family lower.
func Parameterized(target string) bool {
	return target != "" && target[0] >= 'A' && target[0] <= 'Z'
}

// Session is the sess layer of a family: how a connection of its RPC is
// governed and driven. Decides names the methods that need control to send,
// Asks the server-to-client methods that raise a request, Conversation where
// the agent's own conversation id arrives; Options and Platforms are the
// launch options a cell accepts and where each mode exists, carried for the
// harness and the web app and not interpreted here.
type Session struct {
	Decides      []string      `json:"decides,omitempty"`
	Asks         []string      `json:"asks,omitempty"`
	Conversation *Conversation `json:"conversation,omitempty"`
	Options      any           `json:"options,omitempty"`
	Platforms    any           `json:"platforms,omitempty"`
}

// Conversation is an event and the path to the conversation id in its data.
type Conversation struct {
	Event string `json:"event"`
	Path  string `json:"path"`
}

// The layers a family is declared in, lowest first. A declaration refers to
// its own layer or a lower one, never a higher one: data refers to data;
// operations to operations and data, and may hold a slot — an envelope of
// another family's operations, or a handle to a channel that speaks them;
// a session to sessions, operations and data.
const (
	LayerDTO  = "dto"
	LayerRPC  = "rpc"
	LayerSess = "sess"
)

// Layers is every layer, lowest first.
var Layers = []string{LayerDTO, LayerRPC, LayerSess}

// layerRank orders the layers; an unknown layer ranks below every one.
func layerRank(layer string) int {
	for i, known := range Layers {
		if known == layer {
			return i
		}
	}
	return -1
}

// The types every family carries so that another family may hold one of its
// messages, or a handle to a channel that speaks it: an envelope is one
// nightseam.duplex/1 message, a handle is a channel reference on the carrying
// connection. A contract may not declare either itself.
const (
	EnvelopeType = "Envelope"
	HandleType   = "Handle"
)

// SessionRole is the role a family declares when a session can speak it; a
// slot naming "session" is of whichever such family applies.
const SessionRole = "session"

// envelopeType is one nightseam.duplex/1 message of a family: version and
// kind (request, response, event or cancel); the id that correlates a
// response or a cancel with its request; the method a request names and its
// params; a response's result or error; the event an event frame names and
// its data. Neither injected type carries a description: parsing invents no
// content, and the generated code emits none.
func envelopeType() Type {
	return Type{
		Kind: "record",
		Fields: []Field{
			{Name: "version", Type: "integer", Required: true},
			{Name: "kind", Type: "string", Required: true},
			{Name: "id", Type: "string", Required: false},
			{Name: "method", Type: "string", Required: false},
			{Name: "params", Type: "json", Required: false},
			{Name: "result", Type: "json", Required: false},
			{Name: "error", Type: "json", Required: false},
			{Name: "event", Type: "string", Required: false},
			{Name: "data", Type: "json", Required: false},
		},
	}
}

// handleType is a reference to a channel on the carrying connection that
// speaks a family.
func handleType() Type {
	return Type{Kind: "record", Fields: []Field{{Name: "channel", Type: "integer", Required: true}}}
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
	Request     TypeExpr `json:"request,omitempty"`
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
	const id = "urn:nightseam:contract:1"
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
	diagnostics := []Diagnostic{}
	for name, implicit := range map[string]Type{EnvelopeType: envelopeType(), HandleType: handleType()} {
		if _, declared := api.Types[name]; declared {
			diagnostics = append(diagnostics, Diagnostic{"reserved_name", "/types/" + EscapePointer(name), "Type name is carried by every family and may not be declared: " + name + "."})
			continue
		}
		api.Types[name] = implicit
	}
	if len(diagnostics) != 0 {
		Sort(diagnostics)
		return api, diagnostics
	}
	return api, nil
}

// sections is what a layer file may carry, beyond what every file carries.
var sections = map[string][]string{
	LayerDTO:  {"imports", "types"},
	LayerRPC:  {"imports", "parameters", "types", "methods", "events", "errors"},
	LayerSess: {"imports", "parameters", "types", "session"},
}

// Merge joins a family's layer files, by layer, into the one contract the
// generator renders: the union of their imports and types, the operations of
// the rpc layer, the session section of the sess layer, and a record of the
// layer each type was declared in, which Check holds the direction rule
// against. A family with a sess layer is a session family. Each file must
// name the family and its own layer and carry only its layer's sections; a
// type declared twice is refused.
func Merge(files map[string]map[string]any) (map[string]any, []Diagnostic) {
	var diagnostics []Diagnostic
	add := func(pointer, code, message string) {
		diagnostics = append(diagnostics, Diagnostic{code, pointer, message})
	}
	merged := map[string]any{"types": map[string]any{}, "methods": []any{}, "events": []any{}}
	layers := map[string]any{}
	imports := map[string]bool{}
	name := ""
	for _, layer := range Layers {
		file, present := files[layer]
		if !present {
			continue
		}
		if file["layer"] != layer {
			add("/"+layer+"/layer", "wrong_layer", fmt.Sprintf("The %s file declares layer %v.", layer, file["layer"]))
		}
		fileName, _ := file["name"].(string)
		if name == "" {
			name = fileName
		} else if fileName != name {
			add("/"+layer+"/name", "wrong_family", fmt.Sprintf("The %s file names family %q; the family is %q.", layer, fileName, name))
		}
		for key, value := range file {
			switch key {
			case "schema_version", "profile", "name", "layer":
				merged[key] = value
			case "imports":
				for _, imported := range asList(value) {
					if text, ok := imported.(string); ok {
						imports[text] = true
					}
				}
			case "types":
				types, _ := value.(map[string]any)
				for typeName, t := range types {
					if _, declared := layers[typeName]; declared {
						add("/"+layer+"/types/"+EscapePointer(typeName), "duplicate_type", fmt.Sprintf("Type %s is declared in the %s layer as well as in %v.", typeName, layer, layers[typeName]))
						continue
					}
					merged["types"].(map[string]any)[typeName] = t
					layers[typeName] = layer
				}
			case "parameters", "methods", "events", "errors", "session":
				if !slices.Contains(sections[layer], key) {
					add("/"+layer+"/"+key, "wrong_section", fmt.Sprintf("The %s layer does not carry %s.", layer, key))
					continue
				}
				merged[key] = value
			default:
				add("/"+layer+"/"+key, "unknown_section", fmt.Sprintf("A layer file does not carry %s.", key))
			}
		}
	}
	delete(merged, "layer")
	if len(imports) > 0 {
		names := make([]string, 0, len(imports))
		for imported := range imports {
			names = append(names, imported)
		}
		sort.Strings(names)
		list := make([]any, len(names))
		for i, imported := range names {
			list[i] = imported
		}
		merged["imports"] = list
	}
	merged["layers"] = layers
	if _, sess := files[LayerSess]; sess {
		merged["role"] = SessionRole
	}
	Sort(diagnostics)
	return merged, diagnostics
}

func asList(value any) []any {
	list, _ := value.([]any)
	return list
}

// Reference splits a type expression that names another family's type,
// family.Type, and reports whether it was one.
func Reference(expr TypeExpr) (family, name string, ok bool) {
	text, isString := expr.(string)
	if !isString {
		return "", "", false
	}
	at := strings.IndexByte(text, '.')
	if at <= 0 || Parameterized(text[:at]) {
		return "", "", false
	}
	return text[:at], text[at+1:], true
}

// Slot reports a slot: a type drawn from another family, named where the
// generated code is instantiated when the family is a parameter. It is
// spelled three ways — {"envelope": X} for X's Envelope, {"connection": X}
// for X's Handle, and "X.T" for any type T of X where X is a parameter — and
// reported one way: the target, a parameter or a named family, and the type
// drawn from it. "f.T" with f a named family is a Reference, not a slot.
func Slot(expr TypeExpr) (target, typeName string, ok bool) {
	switch x := expr.(type) {
	case string:
		at := strings.IndexByte(x, '.')
		if at <= 0 || !Parameterized(x[:at]) {
			return "", "", false
		}
		return x[:at], x[at+1:], true
	case map[string]any:
		if value, present := x[EnvelopeSlot]; present {
			target, _ := value.(string)
			return target, EnvelopeType, true
		}
		if value, present := x[ConnectionSlot]; present {
			target, _ := value.(string)
			return target, HandleType, true
		}
	}
	return "", "", false
}

// Apply reports an application expression, {"apply": "b.Thing", "with":
// {...}}: a generic type of an imported family with what fills each of its
// parameters, each a parameter of this family or a named family. A family
// that refers to a generic imported type says so this way; a plain
// reference to one is refused.
func Apply(expr TypeExpr) (reference string, with map[string]string, ok bool) {
	object, isObject := expr.(map[string]any)
	if !isObject {
		return "", nil, false
	}
	name, named := object["apply"].(string)
	if !named {
		return "", nil, false
	}
	with = map[string]string{}
	if bindings, isObject := object["with"].(map[string]any); isObject {
		for parameter, value := range bindings {
			if target, isString := value.(string); isString {
				with[parameter] = target
			}
		}
	}
	return name, with, true
}

// Substitute binds parameters to families and returns the contract that
// results: a slot of a bound parameter becomes family.Handle or
// family.Envelope and the family joins the imports; a slot of a named family
// keeps its own; a slot of a parameter no binding names stays a slot, so a
// partial substitution stays generic in what it did not bind. This is the
// left path of the diagram a generic rendering must commute with: rendering
// the result is what an instantiation of the generic rendering must equal.
func Substitute(input map[string]any, bindings map[string]string) map[string]any {
	data, _ := json.Marshal(input)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	imports := map[string]bool{}
	if declared, ok := out["imports"].([]any); ok {
		for _, name := range declared {
			if text, ok := name.(string); ok {
				imports[text] = true
			}
		}
	}
	var fill func(expr any) any
	fill = func(expr any) any {
		if _, _, ok := Apply(expr); ok {
			object := expr.(map[string]any)
			if with, isObject := object["with"].(map[string]any); isObject {
				for parameter, value := range with {
					target, isString := value.(string)
					if !isString {
						continue
					}
					if bound, isParameter := bindings[target]; isParameter {
						with[parameter] = bound
						imports[bound] = true
						continue
					}
					if !Parameterized(target) {
						imports[target] = true
					}
				}
			}
			return expr
		}
		if of, typeName, ok := Slot(expr); ok {
			if bound, isParameter := bindings[of]; isParameter {
				of = bound
			} else if Parameterized(of) {
				// A parameter no binding names keeps its slot: the result is
				// still generic in it, which a partial substitution intends.
				return expr
			}
			imports[of] = true
			return of + "." + typeName
		}
		if object, ok := expr.(map[string]any); ok {
			for _, key := range []string{"array", "map"} {
				if child, ok := object[key]; ok {
					object[key] = fill(child)
				}
			}
		}
		return expr
	}
	if types, ok := out["types"].(map[string]any); ok {
		for _, raw := range types {
			t, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if expr, ok := t["type"]; ok {
				t["type"] = fill(expr)
			}
			if fields, ok := t["fields"].([]any); ok {
				for _, raw := range fields {
					if field, ok := raw.(map[string]any); ok {
						field["type"] = fill(field["type"])
					}
				}
			}
		}
	}
	if methods, ok := out["methods"].([]any); ok {
		for _, raw := range methods {
			if method, ok := raw.(map[string]any); ok {
				if request, ok := method["request"]; ok {
					method["request"] = fill(request)
				}
				method["result"] = fill(method["result"])
			}
		}
	}
	if events, ok := out["events"].([]any); ok {
		for _, raw := range events {
			if event, ok := raw.(map[string]any); ok {
				event["type"] = fill(event["type"])
			}
		}
	}
	// A parameter every binding named is gone; one left unbound stays, and so
	// does its declaration.
	if declared, ok := out["parameters"].([]any); ok {
		var kept []any
		for _, raw := range declared {
			parameter, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := parameter["name"].(string); ok {
				if _, bound := bindings[name]; bound {
					continue
				}
			}
			kept = append(kept, parameter)
		}
		if len(kept) == 0 {
			delete(out, "parameters")
		} else {
			out["parameters"] = kept
		}
	}
	delete(imports, out["name"].(string))
	if len(imports) > 0 {
		names := make([]string, 0, len(imports))
		for name := range imports {
			names = append(names, name)
		}
		sort.Strings(names)
		list := make([]any, len(names))
		for i, name := range names {
			list[i] = name
		}
		out["imports"] = list
	}
	return out
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
	imported := map[string]bool{}
	for i, name := range api.Imports {
		p := fmt.Sprintf("/imports/%d", i)
		if name == api.Name {
			add("self_import", p, "A family cannot import itself.")
			continue
		}
		other, ok := api.Imported[name]
		if !ok {
			add("unresolved_import", p, "Unknown family "+name+": it is not among the contracts rendered together.")
			continue
		}
		if len(other.Imports) > 0 {
			add("nested_import", p, "Imported family "+name+" imports families itself; an imported family imports nothing.")
		}
		imported[name] = true
	}
	families := map[string]bool{}
	for _, name := range api.Families {
		families[name] = true
	}
	sessions := len(api.Sessions)
	parameters := map[string]bool{}
	for i, parameter := range api.Parameters {
		p := fmt.Sprintf("/parameters/%d", i)
		if parameters[parameter.Name] {
			add("duplicate_parameter", p+"/name", "Parameter "+parameter.Name+" is declared twice.")
		}
		parameters[parameter.Name] = true
		if _, collides := api.Types[parameter.Name]; collides {
			add("reserved_name", p+"/name", "Parameter "+parameter.Name+" shares its name with a type of this family.")
		}
		switch {
		case parameter.Of != SessionRole:
			add("unknown_role", p+"/of", "Unknown role "+parameter.Of+": a parameter is of the session role.")
		case len(api.Families) > 0 && sessions == 0:
			add("unresolved_type", p+"/of", "No family declares the session role, so a parameter of session has nothing to bind.")
		}
	}
	// The layer a declaration is in, and the layer a type is declared in. A
	// type the loader did not record — one every family carries, or one of
	// a contract merged by hand — is data. Without a record of layers, no
	// direction is checked: the contract was not written in layers.
	layered := api.Layers != nil
	layerOf := func(typeName string, in map[string]string) int {
		if layer, ok := in[typeName]; ok {
			return layerRank(layer)
		}
		return layerRank(LayerDTO)
	}
	// context is the rank of the declaration an expression sits in: its
	// declaring type's layer, or rpc for a method or an event.
	var expression func(TypeExpr, string, string, int)
	expression = func(expr TypeExpr, pointer, owner string, context int) {
		if target, typeName, ok := Slot(expr); ok {
			switch {
			case Parameterized(target):
				if !parameters[target] {
					add("unresolved_parameter", pointer, "Unknown parameter "+target+": this family declares no parameter of that name.")
				} else if typeName != EnvelopeType && typeName != HandleType {
					// A type beyond the two every family carries must be one
					// every family that may bind the parameter declares, as a
					// record or an enum of its own — an alias has no identity
					// for a language to hold it to — and plainly.
					for _, member := range api.Sessions {
						other, parsed := api.Members[member]
						if !parsed {
							continue
						}
						t, declared := other.Types[typeName]
						switch {
						case !declared:
							add("unresolved_type", pointer, "Type "+typeName+" of "+target+": the session family "+member+" declares no type of that name, and every family that may bind "+target+" must.")
						case t.Kind == "alias":
							add("unresolved_type", pointer, "Type "+typeName+" of "+target+": in the session family "+member+" it is an alias, and a slot draws a record or an enum.")
						case len(other.Generics().Types[typeName]) > 0:
							add("unresolved_type", pointer, "Type "+typeName+" of "+target+": in the session family "+member+" it is generic, and a slot draws a plain type.")
						}
					}
				}
			case target == api.Name:
				add("self_slot", pointer, "A slot cannot be of the family that declares it.")
			case !families[target]:
				add("unresolved_type", pointer, "Unknown family "+target+": it is not among the contracts rendered together.")
			case typeName != EnvelopeType && typeName != HandleType:
				add("unresolved_type", pointer, "A slot of a named family draws its Envelope or its Handle; for another of its types, import the family and refer to "+target+"."+typeName+".")
			}
			// A slot is an operation's datum — one message of another family,
			// or a handle to a channel that speaks it — and sits in the rpc
			// layer or above: data holds no other family's operations.
			if layered && context < layerRank(LayerRPC) {
				add("layer_violation", pointer, fmt.Sprintf("A slot holds another family's operations and may sit in the %s layer or above, not in %s.", LayerRPC, Layers[context]))
			}
			return
		}
		if family, name, ok := Reference(expr); ok {
			if !imported[family] {
				add("unresolved_type", pointer, "Type "+family+"."+name+" names a family this contract does not import.")
				return
			}
			other := api.Imported[family]
			if _, ok := other.Types[name]; !ok {
				add("unresolved_type", pointer, "Unknown type "+name+" in family "+family+".")
				return
			}
			if layered && other.Layers != nil && layerOf(name, other.Layers) > context {
				add("layer_violation", pointer, fmt.Sprintf("A %s declaration refers to %s.%s, declared in %s; a declaration refers to its own layer or a lower one.", Layers[context], family, name, other.Layers[name]))
			}
			// A generic type of an imported family has parameters of its own,
			// which this family must fill. A family with one parameter fills
			// them all with it; with any other number the plain reference says
			// nothing about which fills which, so an application is required
			// rather than a filling silently guessed.
			if len(other.Generics().Types[name]) > 0 && len(api.Parameters) != 1 {
				add("ambiguous_application", pointer, "Type "+family+"."+name+" is generic and this family declares "+fmt.Sprint(len(api.Parameters))+" parameters; say what fills each with {\"apply\": \""+family+"."+name+"\", \"with\": {…}}.")
			}
			return
		}
		if reference, with, ok := Apply(expr); ok {
			family, name, isReference := Reference(reference)
			switch {
			case !isReference:
				add("unresolved_type", pointer+"/apply", "An application names a type of another family, family.Type.")
			case !imported[family]:
				add("unresolved_type", pointer+"/apply", "Type "+reference+" names a family this contract does not import.")
			default:
				other := api.Imported[family]
				if _, declared := other.Types[name]; !declared {
					add("unresolved_type", pointer+"/apply", "Unknown type "+name+" in family "+family+".")
					return
				}
				wanted := map[string]bool{}
				for _, use := range other.Generics().Types[name] {
					wanted[use.Parameter] = true
				}
				if len(wanted) == 0 {
					add("needless_application", pointer+"/apply", "Type "+reference+" is not generic; refer to it by name.")
				}
				for parameter := range wanted {
					if _, bound := with[parameter]; !bound {
						add("unbound_parameter", pointer+"/with", "The application leaves "+family+"'s parameter "+parameter+" unbound.")
					}
				}
				for parameter, target := range with {
					at := pointer + "/with/" + EscapePointer(parameter)
					if !wanted[parameter] {
						add("unresolved_parameter", at, "Type "+reference+" has no parameter "+parameter+".")
					}
					switch {
					case Parameterized(target):
						if !parameters[target] {
							add("unresolved_parameter", at, "Unknown parameter "+target+": this family declares no parameter of that name.")
						}
					case target == api.Name:
						add("self_slot", at, "A parameter cannot be filled with the family that declares it.")
					case !families[target]:
						add("unresolved_type", at, "Unknown family "+target+": it is not among the contracts rendered together.")
					}
				}
				if layered && other.Layers != nil && layerOf(name, other.Layers) > context {
					add("layer_violation", pointer, fmt.Sprintf("A %s declaration refers to %s, declared in %s; a declaration refers to its own layer or a lower one.", Layers[context], reference, other.Layers[name]))
				}
			}
			return
		}
		switch x := expr.(type) {
		case string:
			if Primitive(x) {
				return
			}
			if _, ok := api.Types[x]; !ok {
				add("unresolved_type", pointer, "Unknown type "+x+".")
				return
			}
			if layered && layerOf(x, api.Layers) > context {
				add("layer_violation", pointer, fmt.Sprintf("A %s declaration refers to %s, declared in %s; a declaration refers to its own layer or a lower one.", Layers[context], x, api.Layers[x]))
			}
			if owner != "" {
				edges[owner] = append(edges[owner], x)
			}
		case map[string]any:
			for _, kind := range []string{"array", "map"} {
				if child, ok := x[kind]; ok {
					expression(child, pointer+"/"+kind, owner, context)
				}
			}
		}
	}
	if api.Session != nil {
		methods := map[string]Method{}
		for _, method := range api.Methods {
			methods[method.Name] = method
		}
		events := map[string]bool{}
		for _, event := range api.Events {
			events[event.Name] = true
		}
		for i, name := range api.Session.Decides {
			if _, ok := methods[name]; !ok {
				add("unknown_operation", fmt.Sprintf("/session/decides/%d", i), "Decides names "+name+", which is not a method of this family.")
			}
		}
		for i, name := range api.Session.Asks {
			method, ok := methods[name]
			switch {
			case !ok:
				add("unknown_operation", fmt.Sprintf("/session/asks/%d", i), "Asks names "+name+", which is not a method of this family.")
			case method.Direction != "server_to_client":
				add("invalid_direction", fmt.Sprintf("/session/asks/%d", i), "Asks names "+name+", which the client sends; an asking method is one the server sends.")
			}
		}
		if c := api.Session.Conversation; c != nil && !events[c.Event] {
			add("unknown_operation", "/session/conversation/event", "The conversation arrives in "+c.Event+", which is not an event of this family.")
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
				expression(field.Type, fmt.Sprintf("%s/fields/%d/type", pointer, i), name, layerOf(name, api.Layers))
			}
		case "alias":
			expression(t.Type, pointer+"/type", name, layerOf(name, api.Layers))
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
		if method.Request != nil {
			expression(method.Request, p+"/request", "", layerRank(LayerRPC))
			if !api.object(method.Request) {
				add("invalid_request", p+"/request", "Method request must be a record, a record of an imported family, or a slot: params is an object on the wire.")
			}
		}
		expression(method.Result, p+"/result", "", layerRank(LayerRPC))
	}
	for i, event := range api.Events {
		operation("events", i, event.Name, event.Direction)
		expression(event.Type, fmt.Sprintf("/events/%d/type", i), "", layerRank(LayerRPC))
	}
	used := map[string]bool{}
	api.Expressions(func(expr TypeExpr) {
		if target, _, ok := Slot(expr); ok && Parameterized(target) {
			used[target] = true
		}
		if _, with, ok := Apply(expr); ok {
			for _, target := range with {
				if Parameterized(target) {
					used[target] = true
				}
			}
		}
	})
	for i, parameter := range api.Parameters {
		if !used[parameter.Name] {
			add("unused_parameter", fmt.Sprintf("/parameters/%d/name", i), "Parameter "+parameter.Name+" is declared and nothing names it: no slot, and no application of an imported type.")
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

// object reports whether an expression is an object on the wire, which a
// method's params must be: a record of this family or of an imported one, or
// a slot, since both an Envelope and a Handle are records.
func (api API) object(expr TypeExpr) bool {
	if target, typeName, ok := Slot(expr); ok {
		if typeName == EnvelopeType || typeName == HandleType {
			return true
		}
		if !Parameterized(target) {
			return false
		}
		for _, member := range api.Sessions {
			if other, parsed := api.Members[member]; parsed && other.Types[typeName].Kind != "record" {
				return false
			}
		}
		return true
	}
	name, isName := expr.(string)
	if !isName {
		return false
	}
	if family, typeName, ok := Reference(name); ok {
		return api.Imported[family].Types[typeName].Kind == "record"
	}
	return api.Types[name].Kind == "record"
}

// The two kinds of slot: an envelope is one message of a family, a
// connection a handle to a channel that speaks it.
const (
	EnvelopeSlot   = "envelope"
	ConnectionSlot = "connection"
)

// DrawnBefore orders the types a parameter is drawn at, which is the order a
// language declares the type parameters they become: Envelope, Handle, then
// the rest by name.
func DrawnBefore(a, b string) bool {
	rank := func(name string) int {
		switch name {
		case EnvelopeType:
			return 0
		case HandleType:
			return 1
		}
		return 2
	}
	if rank(a) != rank(b) {
		return rank(a) < rank(b)
	}
	return a < b
}

// Expressions visits every type expression the contract holds — each
// record's own fields, each alias's target, each method's result and each
// event's type — and the elements of the arrays and maps within them.
func (api API) Expressions(visit func(expr TypeExpr)) {
	var walk func(expr TypeExpr)
	walk = func(expr TypeExpr) {
		visit(expr)
		if object, ok := expr.(map[string]any); ok {
			for _, key := range []string{"array", "map"} {
				if child, ok := object[key]; ok {
					walk(child)
				}
			}
		}
	}
	for _, name := range api.TypeNames() {
		t := api.Types[name]
		for _, field := range t.Fields {
			walk(field.Type)
		}
		if t.Kind == "alias" {
			walk(t.Type)
		}
	}
	for _, method := range api.Methods {
		if method.Request != nil {
			walk(method.Request)
		}
		walk(method.Result)
	}
	for _, event := range api.Events {
		walk(event.Type)
	}
}

// SlotFamilies names the families the contract's slots name, sorted: each
// fills its slot with its own Envelope or Handle, whether or not the family
// is imported, together with the families an application binds a parameter
// of an imported type to. A parameter is not a family and is not among them:
// what fills it is chosen where the generated code is instantiated.
func (api API) SlotFamilies() []string {
	seen := map[string]bool{}
	api.Expressions(func(expr TypeExpr) {
		if target, _, ok := Slot(expr); ok && !Parameterized(target) {
			seen[target] = true
		}
		if reference, with, ok := Apply(expr); ok {
			if family, _, isReference := Reference(reference); isReference {
				seen[family] = true
			}
			for _, target := range with {
				if !Parameterized(target) {
					seen[target] = true
				}
			}
		}
	})
	return sortedNames(seen)
}

// References names every family whose generated package this one's refers
// to: the families it imports and the families its slots name, sorted.
func (api API) References() []string {
	seen := map[string]bool{}
	for _, family := range api.Imports {
		seen[family] = true
	}
	for _, family := range api.SlotFamilies() {
		seen[family] = true
	}
	return sortedNames(seen)
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Use is one parameter drawn at one type: the pair a language turns into a
// type parameter. A family generic in S and T, where S is drawn at its
// Envelope and its Handle and T at its Envelope, has the uses {S,Envelope},
// {S,Handle}, {T,Envelope} — in that order, by the parameter's declaration
// and then by DrawnBefore.
type Use struct {
	Parameter string
	Type      string
}

// Generics is what makes a family generic. A slot of a parameter is filled
// where the generated code is instantiated, so a type that holds one —
// directly, or through the types it refers to, its own family's or an
// imported one's — is generic in that parameter, and so is a family with
// such a type. Types maps each generic type to the uses it makes, in
// parameter order; Family is the union over every type and operation;
// Imported is Types of each imported family, with the imported family's
// parameters renamed to the parameter of this one that fills them. A plain
// type or family makes no uses. A slot of a named family is not generic: it
// is that family's Envelope or Handle.
type Generics struct {
	Types    map[string][]Use
	Family   []Use
	Imported map[string]map[string][]Use
}

// Generic reports whether the family is generic at all.
func (g Generics) Generic() bool { return len(g.Family) > 0 }

// Generics computes what is generic in the family.
func (api API) Generics() Generics {
	g := Generics{Types: map[string][]Use{}, Imported: map[string]map[string][]Use{}}
	order := map[string]int{}
	for i, parameter := range api.Parameters {
		order[parameter.Name] = i
	}
	// An imported family's parameters are not this family's, so an
	// application says what fills each: through a parameter of this family,
	// which is a use of it, or through a named family, which is not. A family
	// with one parameter may refer to a generic imported type plainly and
	// fill every parameter of it with that one; Check refuses the plain
	// reference from a family with any other number.
	rename := func(uses []Use, with map[string]string) []Use {
		out := make([]Use, 0, len(uses))
		for _, use := range uses {
			target, bound := with[use.Parameter]
			if !bound {
				if len(api.Parameters) != 1 {
					continue
				}
				target = api.Parameters[0].Name
			}
			if !Parameterized(target) {
				continue
			}
			if renamed := (Use{target, use.Type}); !slices.Contains(out, renamed) {
				out = append(out, renamed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	for name, other := range api.Imported {
		g.Imported[name] = other.Generics().Types
	}
	union := func(sets ...[]Use) []Use {
		var out []Use
		for _, set := range sets {
			for _, use := range set {
				if !slices.Contains(out, use) {
					out = append(out, use)
				}
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Parameter != out[j].Parameter {
				return order[out[i].Parameter] < order[out[j].Parameter]
			}
			return DrawnBefore(out[i].Type, out[j].Type)
		})
		return out
	}
	// usesOf is the uses an expression makes, given the uses known of every
	// type so far; the fixpoint below reaches the types through references.
	var usesOf func(expr TypeExpr) []Use
	usesOf = func(expr TypeExpr) []Use {
		if target, typeName, ok := Slot(expr); ok {
			if Parameterized(target) {
				if _, declared := order[target]; declared {
					return []Use{{target, typeName}}
				}
			}
			return nil
		}
		if reference, with, ok := Apply(expr); ok {
			if family, name, isReference := Reference(reference); isReference {
				return rename(g.Imported[family][name], with)
			}
			return nil
		}
		switch x := expr.(type) {
		case string:
			if family, name, ok := Reference(x); ok {
				return rename(g.Imported[family][name], nil)
			}
			return g.Types[x]
		case map[string]any:
			for _, key := range []string{"array", "map"} {
				if child, ok := x[key]; ok {
					return usesOf(child)
				}
			}
		}
		return nil
	}
	for changed := true; changed; {
		changed = false
		for _, name := range api.TypeNames() {
			t := api.Types[name]
			var sets [][]Use
			switch t.Kind {
			case "record":
				for _, field := range api.FlattenedFields(name) {
					sets = append(sets, usesOf(field.Type))
				}
			case "alias":
				sets = append(sets, usesOf(t.Type))
			}
			if uses := union(sets...); len(uses) > len(g.Types[name]) {
				g.Types[name] = uses
				changed = true
			}
		}
	}
	var sets [][]Use
	for _, uses := range g.Types {
		sets = append(sets, uses)
	}
	for _, method := range api.Methods {
		if method.Request != nil {
			sets = append(sets, usesOf(method.Request))
		}
		sets = append(sets, usesOf(method.Result))
	}
	for _, event := range api.Events {
		sets = append(sets, usesOf(event.Type))
	}
	g.Family = union(sets...)
	return g
}
