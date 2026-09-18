// Package golang renders an API family as Go: a protocol package of wire
// types with their validator, a binding package for a server, a client
// package for a caller. It implements spi.Language and is named nowhere but
// where the tool is composed.
//
// It descends from Nightshift's dev/go/repo/generate/generate.go, duplex.go
// and validation.go, and the Go checks of its contract.go (D-001).
package golang

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/Bitspark/nightseam/internal/contract"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Options places the generated packages. Module is the import path they are
// rooted at and is required; Runtime, Seam and Tunnel are the import paths
// of the runtime they bind to, of the seam beneath it and of the tunnel
// over it, Nightseam's own when left empty. A path left empty is derived
// from the family's name when a contract is rendered.
type Options struct {
	Module       string
	Runtime      string
	Seam         string
	Tunnel       string
	ProtocolPath string
	BindingPath  string
	ClientPath   string
}

// The packages the generated ones bind to unless the options name others:
// the Go peer of the nightseam.duplex/1 profile, the seam it speaks over,
// and the tunnel that multiplexes channels over it.
const (
	DefaultRuntime = "github.com/Bitspark/nightseam/runtime/go"
	DefaultSeam    = "github.com/Bitspark/nightseam/duplex/go"
	DefaultTunnel  = "github.com/Bitspark/nightseam/tunnel/go"
)

// New returns the Go language with its options.
func New(options Options) spi.Language { return &language{options} }

type language struct{ options Options }

func (*language) Name() string { return "go" }

// paths are the options resolved for one family.
type paths struct{ module, runtime, seam, tunnel, protocol, binding, client string }

var goImportPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./~-]*$`)

func (l *language) resolve(api contract.API) (paths, error) {
	p := paths{l.options.Module, l.options.Runtime, l.options.Seam, l.options.Tunnel, l.options.ProtocolPath, l.options.BindingPath, l.options.ClientPath}
	if p.module == "" {
		return paths{}, fmt.Errorf("a Go module path is required to root the generated packages")
	}
	if p.runtime == "" {
		p.runtime = DefaultRuntime
	}
	if p.seam == "" {
		p.seam = DefaultSeam
	}
	if p.tunnel == "" {
		p.tunnel = DefaultTunnel
	}
	if p.protocol == "" {
		p.protocol = "api/go/" + api.Name + "-protocol"
	}
	if p.binding == "" {
		p.binding = "api/go/" + api.Name + "-binding"
	}
	if p.client == "" {
		p.client = "api/go/" + api.Name + "-client"
	}
	for _, s := range []string{p.module, p.runtime, p.seam, p.tunnel, p.protocol, p.binding, p.client} {
		if !goImportPattern.MatchString(s) || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
			return paths{}, fmt.Errorf("invalid Go module or package path %q", s)
		}
	}
	for _, s := range []string{p.protocol, p.binding, p.client} {
		if s == "." || s == ".." || strings.Contains(s, "\\") || strings.Contains(s, ":") || strings.HasPrefix(s, "/") || path.Clean(s) != s || strings.HasPrefix(s, "../") {
			return paths{}, fmt.Errorf("invalid output path %q", s)
		}
	}
	if p.protocol == p.binding || p.protocol == p.client || p.binding == p.client {
		return paths{}, fmt.Errorf("Go output package paths must be distinct")
	}
	return p, nil
}

// Render emits the four Go files, each through gofmt.
func (l *language) Render(api contract.API) ([]spi.File, error) {
	p, err := l.resolve(api)
	if err != nil {
		return nil, err
	}
	g := api.Generics()
	var files []spi.File
	put := func(dir, file, source string) error {
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return fmt.Errorf("format generated %s: %w\n%s", file, err, source)
		}
		files = append(files, spi.File{Path: path.Join(dir, file), Data: formatted})
		return nil
	}
	if err := put(p.protocol, "types_generated.go", generateTypes(api, g, p)); err != nil {
		return nil, err
	}
	if err := put(p.protocol, "validation_generated.go", generateValidation(api, p)); err != nil {
		return nil, err
	}
	if err := put(p.binding, "binding_generated.go", generateBinding(api, g, p)); err != nil {
		return nil, err
	}
	if err := put(p.client, "client_generated.go", generateClient(api, g, p)); err != nil {
		return nil, err
	}
	return files, nil
}

// reservedTypes are the identifiers the generated Go packages and the
// runtime declare or would shadow; a contract type of that name is refused.
var reservedTypes = strings.Fields("API Client Caller Server Binding Handler Handlers ClientHandlers ServerHandlers Peer PublicError Optional Remote NewHandler Dial Decides Asks Conversation ValidateRaw ValidateExpressionRaw ValidateValue TypeExpression Attach Serve Open Tag Of Errors IsError")
var reservedMethods = strings.Fields("Close Call Notify Handle Connect")

// Check reports the names the contract would make Go generate that it
// cannot: identifiers Go refuses, names the runtime reserves, and collisions
// in the protocol package, the enum constants, each record's fields and the
// client's and remote's members.
func (*language) Check(api contract.API) []contract.Diagnostic {
	diagnostics := []contract.Diagnostic{}
	add := func(code, pointer, message string) {
		diagnostics = append(diagnostics, contract.Diagnostic{Code: code, Pointer: pointer, Message: message})
	}
	names := api.TypeNames()
	declarations := map[string]string{}
	for _, name := range names {
		declarations[name] = "/types/" + contract.EscapePointer(name)
	}
	for _, name := range names {
		t := api.Types[name]
		pointer := "/types/" + contract.EscapePointer(name)
		if slices.Contains(reservedTypes, name) {
			add("reserved_name", pointer, "Type name is reserved by the generated Go packages: "+name+".")
		}
		if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
			add("invalid_name", pointer, "Type name must be a Go identifier.")
		}
		switch t.Kind {
		case "record":
			for i, field := range t.Fields {
				p := fmt.Sprintf("%s/fields/%d", pointer, i)
				goName := fieldName(field)
				if goName == "" || !token.IsExported(goName) || !token.IsIdentifier(goName) {
					add("invalid_name", p+"/go_name", "Generated field name must be an exported Go identifier.")
				}
				if goName == "MarshalJSON" || goName == "UnmarshalJSON" || goName == "Of" {
					add("reserved_name", p+"/go_name", "Field name collides with a generated method.")
				}
				if t.Open && goName == "AdditionalFields" {
					add("reserved_name", p+"/go_name", "Field name collides with the open-record extension storage.")
				}
			}
		case "enum":
			for i, value := range t.Values {
				p := fmt.Sprintf("%s/values/%d", pointer, i)
				constant := enumConstantName(name, value)
				if previous, exists := declarations[constant]; exists {
					add("generated_name_collision", p, "Generated enum constant "+constant+" collides with "+previous+".")
				} else {
					declarations[constant] = p
				}
				if slices.Contains(reservedTypes, constant) {
					add("reserved_name", p, "Generated enum constant is reserved by the generated Go packages.")
				}
			}
		}
	}
	// Walk each inheritance path separately: a diamond also duplicates Go fields.
	for _, name := range names {
		if api.Types[name].Kind != "record" {
			continue
		}
		open := api.Types[name].Open
		goNames := map[string]string{}
		api.WalkFields(name, func(at contract.FieldAt) {
			goName := fieldName(at.Field)
			if open && goName == "AdditionalFields" {
				add("reserved_name", at.Pointer+"/go_name", "Inherited field name collides with the open-record extension storage.")
			}
			if previous, ok := goNames[goName]; ok {
				add("generated_name_collision", at.Pointer+"/go_name", fmt.Sprintf("Record %s repeats Go field %s already provided at %s.", name, goName, previous))
			} else {
				goNames[goName] = at.Pointer
			}
		})
	}
	// A generic type takes its parameters as Go type parameters, which shadow
	// any type of the same name in the generated package; so does the tag an
	// entry point takes for each.
	index := map[string]int{}
	for i, parameter := range api.Parameters {
		index[parameter.Name] = i
	}
	generated := map[string]string{}
	for _, use := range api.Generics().Family {
		for _, name := range []string{parameterName(use), tagName(use.Parameter)} {
			pointer := fmt.Sprintf("/parameters/%d/name", index[use.Parameter])
			if previous, exists := declarations[name]; exists {
				add("generated_name_collision", pointer, "Generated Go type parameter "+name+" collides with "+previous+".")
			} else if previous, exists := generated[name]; exists && previous != use.Parameter {
				add("generated_name_collision", pointer, "Generated Go type parameter "+name+" is also generated for parameter "+previous+".")
			}
			generated[name] = use.Parameter
		}
	}
	// A public error becomes a constant of the protocol package, beside the
	// types and the enum constants.
	for i, e := range api.Errors {
		p := fmt.Sprintf("/errors/%d/code", i)
		name := errorName(e.Code)
		if name == "Error" {
			add("invalid_name", p, "Public error code yields no Go identifier.")
			continue
		}
		if previous, exists := declarations[name]; exists {
			add("generated_name_collision", p, "Generated error constant "+name+" collides with "+previous+".")
		} else {
			declarations[name] = p
		}
	}
	// The Go namespace is shared across methods and events in each direction.
	operations := map[string]string{}
	operation := func(kind string, index int, goName, direction string) {
		p := fmt.Sprintf("/%s/%d", kind, index)
		key := direction + ":" + goName
		if previous, ok := operations[key]; ok {
			add("operation_collision", p+"/go_name", "Operation name collides with "+previous+".")
		} else {
			operations[key] = p
		}
		if slices.Contains(reservedMethods, goName) {
			add("reserved_name", p+"/go_name", "Operation name is reserved by the generated Go client.")
		}
	}
	for i, method := range api.Methods {
		operation("methods", i, method.GoName, method.Direction)
	}
	for i, event := range api.Events {
		operation("events", i, event.GoName, event.Direction)
	}
	// Events add receive helpers as well as emit helpers, so their receiver
	// namespace crosses the protocol's direction boundary.
	client := map[string]string{"Peer": "generated client field", "Close": "generated client method"}
	remote := map[string]string{"Peer": "generated remote field"}
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
			register(client, method.GoName, p+"/go_name")
		} else {
			register(remote, method.GoName, p+"/go_name")
		}
	}
	for i, event := range api.Events {
		p := fmt.Sprintf("/events/%d", i)
		if event.Direction == "client_to_server" {
			register(client, "Emit"+event.GoName, p+"/go_name")
			register(remote, "On"+event.GoName, p+"/go_name")
		} else {
			register(client, "On"+event.GoName, p+"/go_name")
			register(remote, "Emit"+event.GoName, p+"/go_name")
		}
	}
	return diagnostics
}

func pkg(api contract.API, suffix string) string {
	return strings.ReplaceAll(api.Name, "-", "") + suffix
}
func quote(value string) string      { encoded, _ := json.Marshal(value); return string(encoded) }
func expression(value any) string    { encoded, _ := json.Marshal(value); return string(encoded) }
func canonicalJSON(value any) []byte { data, _ := json.Marshal(value); return data }

var wordPattern = regexp.MustCompile(`[A-Za-z0-9]+`)

// goName joins the alphanumeric words of a name in upper camel case, with
// the acronyms Go spells in capitals.
func goName(name string) string {
	var out strings.Builder
	for _, part := range wordPattern.FindAllString(name, -1) {
		switch strings.ToLower(part) {
		case "id", "api", "url", "http", "json", "uuid":
			out.WriteString(strings.ToUpper(part))
		default:
			out.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return out.String()
}

// DefaultGoName is the deterministic field-name mapping used when a field
// names no go_name: each underscore-separated part in upper camel case.
func DefaultGoName(name string) string {
	var result strings.Builder
	for _, part := range strings.Split(name, "_") {
		if slices.Contains([]string{"id", "api", "url", "http", "json", "uuid"}, strings.ToLower(part)) {
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

func fieldName(field contract.Field) string {
	if field.GoName != "" {
		return field.GoName
	}
	return DefaultGoName(field.Name)
}

// enumConstantName is the constant the protocol package declares for one
// enum value; the checker and the emitter share it by construction.
func enumConstantName(typeName, value string) string { return typeName + goName(value) }

// parameterName is the Go type parameter one use becomes: the contract's
// parameter followed by the type drawn from it, SEnvelope and SHandle for a
// parameter S drawn at its Envelope and its Handle. Go has no associated
// types, so a parameter drawn at two types becomes two Go parameters; the
// tag pairs them again, see tagName.
func parameterName(use contract.Use) string { return use.Parameter + use.Type }

// tagName is the type parameter an entry point takes for a parameter's
// family itself: STag for a parameter S. Every type a family's protocol
// package declares carries an Of method returning the package's Tag, and an
// entry point constrains every type parameter drawn from S to runtime.Of of
// STag, so that all of them come from one family or the call does not
// compile. STag is inferred from any of them and is never spelled.
func tagName(parameter string) string { return parameter + "Tag" }

// entry renders the type parameters of an entry point that makes the uses:
// each drawn type constrained to its parameter's tag, then the tags, which
// come last so that a caller spelling the drawn types may stop there.
func entry(uses []contract.Use) string {
	if len(uses) == 0 {
		return ""
	}
	var constrained, tags []string
	for _, use := range uses {
		constrained = append(constrained, parameterName(use)+" runtime.Of["+tagName(use.Parameter)+"]")
		if tag := tagName(use.Parameter); !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return "[" + strings.Join(constrained, ", ") + ", " + strings.Join(tags, ", ") + " any]"
}

func parameters(uses []contract.Use) []string {
	names := make([]string, len(uses))
	for i, use := range uses {
		names[i] = parameterName(use)
	}
	return names
}

// declare renders the type parameters of a declaration that makes the uses,
// [SEnvelope, SHandle any], or nothing for a plain one.
func declare(uses []contract.Use) string {
	if len(uses) == 0 {
		return ""
	}
	return "[" + strings.Join(parameters(uses), ", ") + " any]"
}

// apply renders the type arguments a reference passes on, [SEnvelope,
// SHandle], or nothing.
func apply(uses []contract.Use) string {
	if len(uses) == 0 {
		return ""
	}
	return "[" + strings.Join(parameters(uses), ", ") + "]"
}

func goType(g contract.Generics, expr any, prefix string) string {
	switch t := expr.(type) {
	case string:
		switch t {
		case "string":
			return "string"
		case "boolean":
			return "bool"
		case "number":
			return "float64"
		case "integer":
			return "int64"
		case "timestamp":
			return "time.Time"
		case "json":
			return "any"
		default:
			if target, typeName, ok := contract.Slot(t); ok {
				return parameterName(contract.Use{Parameter: target, Type: typeName})
			}
			if family, name, ok := contract.Reference(t); ok {
				return importAlias(family) + "." + name + apply(g.Imported[family][name])
			}
			return prefix + t + apply(g.Types[t])
		}
	case map[string]any:
		if reference, with, ok := contract.Apply(t); ok {
			family, name, isReference := contract.Reference(reference)
			if !isReference {
				return "any"
			}
			var args []string
			for _, use := range g.Imported[family][name] {
				target, bound := with[use.Parameter]
				if !bound {
					target = use.Parameter
				}
				if contract.Parameterized(target) {
					args = append(args, parameterName(contract.Use{Parameter: target, Type: use.Type}))
					continue
				}
				args = append(args, importAlias(target)+"."+use.Type)
			}
			rendered := importAlias(family) + "." + name
			if len(args) > 0 {
				rendered += "[" + strings.Join(args, ", ") + "]"
			}
			return rendered
		}
		if target, typeName, ok := contract.Slot(t); ok {
			if contract.Parameterized(target) {
				return parameterName(contract.Use{Parameter: target, Type: typeName})
			}
			return importAlias(target) + "." + typeName
		}
		if a, ok := t["array"]; ok {
			return "[]" + goType(g, a, prefix)
		}
		if m, ok := t["map"]; ok {
			return "map[string]" + goType(g, m, prefix)
		}
	}
	return "any"
}
func goFieldType(g contract.Generics, field contract.Field) string {
	t := goType(g, field.Type, "")
	if field.Nullable {
		t = "runtime.Nullable[" + t + "]"
	}
	if !field.Required {
		t = "runtime.Optional[" + t + "]"
	}
	return t
}

// importsFor derives a file's imports from the selectors its body uses, so
// an emitter never declares an import it does not use.
func importsFor(source string, fixed map[string]string) string {
	imports := map[string]string{}
	if parsed, err := parser.ParseFile(token.NewFileSet(), "generated.go", "package generated\n"+source, 0); err == nil {
		ast.Inspect(parsed, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				if name, ok := selector.X.(*ast.Ident); ok {
					if p, ok := fixed[name.Name]; ok {
						imports[name.Name] = p
					}
				}
			}
			return true
		})
	}
	keys := make([]string, 0, len(imports))
	for alias := range imports {
		keys = append(keys, alias)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, alias := range keys {
		fmt.Fprintf(&b, "%s %q\n", alias, imports[alias])
	}
	b.WriteString(")\n")
	return b.String()
}

// importAlias is the identifier a generated file refers to an imported
// family's protocol package by: the family's name without its dashes, as
// the family's own protocol package is named.
func importAlias(family string) string { return strings.ReplaceAll(family, "-", "") + "protocol" }

// importPath is where an imported family's protocol package lives: the
// place the generator renders every family's, by convention.
func importPath(module, family string) string { return module + "/api/go/" + family + "-protocol" }

func goFile(api contract.API, suffix, source string, p paths) string {
	fixed := map[string]string{"bytes": "bytes", "context": "context", "json": "encoding/json", "io": "io", "math": "math", "strconv": "strconv", "strings": "strings", "time": "time", "fmt": "fmt", "errors": "errors", "http": "net/http", "protocol": p.module + "/" + p.protocol, "runtime": p.runtime, "duplex": p.seam, "tunnel": p.tunnel}
	for _, family := range api.References() {
		fixed[importAlias(family)] = importPath(p.module, family)
	}
	return spi.Header + "package " + pkg(api, suffix) + "\n" + importsFor(source, fixed) + source
}
func paramSignature(g contract.Generics, method contract.Method) string {
	if method.Request == nil {
		return ""
	}
	return ", params " + goType(g, method.Request, "protocol.")
}
func paramValue(method contract.Method) string {
	if method.Request == nil {
		return "struct{}{}"
	}
	return "params"
}
