package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/Bitspark/nightseam/internal/scalarjson"
)

// declarationGraph is the shared declaration rendering, not a validator descriptor.
// Argument graphs have their own definition scopes: two arguments may name two
// revisions of the same declaration without either overwriting the other.
type declarationGraph struct {
	Definitions map[string]any `json:"definitions"`
	Root        any            `json:"root"`
	Version     int            `json:"version"`
}

func readDeclaration(document string) (declarationGraph, error) {
	var graph declarationGraph
	if err := scalarjson.Raw([]byte(document)); err != nil {
		return graph, err
	}
	if err := json.Unmarshal([]byte(document), &graph); err != nil {
		return graph, err
	}
	if graph.Version != 1 || graph.Definitions == nil || graph.Root == nil {
		return graph, fmt.Errorf("declaration: expected a version 1 declaration graph")
	}
	return graph, nil
}

func (g declarationGraph) document() (string, error) {
	data, err := json.Marshal(g)
	return string(data), err
}

func declarationHash(document string) string {
	digest := sha256.Sum256([]byte(document))
	return hex.EncodeToString(digest[:])
}

// WithDeclaration returns a schema carrying the canonical declaration graph
// emitted beside its validator descriptor. It leaves the explicit digest and
// validation behavior unchanged. Bind retains this provenance.
func (s *Schema) WithDeclaration(document string) (*Schema, error) {
	if s == nil {
		return nil, fmt.Errorf("declaration: expected schema")
	}
	graph, err := readDeclaration(document)
	if err != nil {
		return nil, err
	}
	canonical, err := graph.document()
	if err != nil {
		return nil, err
	}
	if canonical != document {
		return nil, fmt.Errorf("declaration: expected canonical bytes")
	}
	if s.digest != "" && s.digest != declarationHash(document) {
		return nil, fmt.Errorf("declaration: digest does not identify its canonical graph")
	}
	bound := *s
	bound.declaration = document
	return &bound, nil
}

// MustWithDeclaration is WithDeclaration for a graph the generator wrote.
func (s *Schema) MustWithDeclaration(document string) *Schema {
	bound, err := s.WithDeclaration(document)
	if err != nil {
		panic(err)
	}
	return bound
}

// BoundDeclaration returns this family's closed canonical application. Every
// declared parameter must be supplied; an incomplete interpretation never
// silently acquires the identity of its template.
func (s *Schema) BoundDeclaration() (string, error) {
	graph, err := (&declarationBuilder{active: map[*Schema]bool{}}).family(s)
	if err != nil {
		return "", err
	}
	return graph.document()
}

// DeclarationDigest identifies the closed family interpretation retained by Bind.
func (s *Schema) DeclarationDigest() (string, error) {
	document, err := s.BoundDeclaration()
	if err != nil {
		return "", err
	}
	return declarationHash(document), nil
}

// Declaration selects the argument's reachable declaration content, retaining
// nested applications and the schemas in which their arguments were supplied.
func (b TypeBinding) Declaration() (string, error) {
	if b.Schema == nil {
		return "", fmt.Errorf("declaration: expected schema for type argument")
	}
	graph, err := (&declarationBuilder{active: map[*Schema]bool{}}).expression(expressionContext(b.Schema, b.Type), 0)
	if err != nil {
		return "", err
	}
	return graph.document()
}

type declarationBuilder struct{ active map[*Schema]bool }

func (b *declarationBuilder) family(s *Schema) (declarationGraph, error) {
	if s == nil {
		return declarationGraph{}, fmt.Errorf("declaration: expected canonical family provenance")
	}
	if b.active[s] {
		return declarationGraph{}, fmt.Errorf("declaration: cyclic supplied bindings")
	}
	b.active[s] = true
	defer delete(b.active, s)
	if len(s.drawn) != 0 {
		return b.drawnFamily(s)
	}
	if s.declaration == "" {
		return declarationGraph{}, fmt.Errorf("declaration: expected canonical family provenance")
	}
	graph, err := readDeclaration(s.declaration)
	if err != nil {
		return graph, err
	}
	root, _ := graph.Root.(map[string]any)
	path, _ := root["ref"].(string)
	definition, _ := graph.Definitions[path].(map[string]any)
	if definition["kind"] != "family" {
		return graph, fmt.Errorf("declaration: expected family root")
	}
	return b.named(graph, path, expressionContext(s, ""), 0)
}

func declarationParameters(definition map[string]any) []map[string]any {
	var result []map[string]any
	for _, key := range []string{"captures", "parameters"} {
		items, _ := definition[key].([]any)
		for _, item := range items {
			if parameter, ok := item.(map[string]any); ok {
				result = append(result, parameter)
			}
		}
	}
	return result
}

func (b *declarationBuilder) named(graph declarationGraph, path string, e expression, depth int) (declarationGraph, error) {
	definition, ok := graph.Definitions[path].(map[string]any)
	if !ok {
		return graph, fmt.Errorf("declaration: unknown declaration %s", path)
	}
	parameters := declarationParameters(definition)
	root := any(map[string]any{"ref": path})
	if len(parameters) != 0 {
		arguments := make([]any, 0, len(parameters))
		for _, parameter := range parameters {
			name, _ := parameter["name"].(string)
			argument, ok := e.scope[name]
			if !ok {
				return graph, fmt.Errorf("declaration: missing required binding %s", name)
			}
			var supplied declarationGraph
			var err error
			if parameter["of"] != "" {
				if argument.family == nil {
					return graph, fmt.Errorf("declaration: expected family binding %s", name)
				}
				supplied, err = b.family(argument.family)
			} else {
				if argument.typeExpression == nil {
					return graph, fmt.Errorf("declaration: expected type binding %s", name)
				}
				supplied, err = b.expression(*argument.typeExpression, depth+1)
			}
			if err != nil {
				return graph, err
			}
			arguments = append(arguments, map[string]any{"graph": supplied})
		}
		root = map[string]any{"apply": path, "arguments": arguments}
	}
	return selectDeclaration(graph, root)
}

func (b *declarationBuilder) expression(e expression, depth int) (declarationGraph, error) {
	if depth > 256 {
		return declarationGraph{}, fmt.Errorf("declaration: cyclic supplied bindings")
	}
	switch value := e.value.(type) {
	case TypeBinding:
		if value.Schema == nil {
			return declarationGraph{}, fmt.Errorf("declaration: expected schema for type argument")
		}
		return b.expression(expressionContext(value.Schema, value.Type), depth+1)
	case goArgument:
		return b.expression(e.child(describeArgument(value.typ)), depth+1)
	case string:
		if argument := e.scope[value].typeExpression; argument != nil {
			return b.expression(*argument, depth+1)
		}
	case map[string]any:
		if name, ok := value["apply"].(string); ok {
			return b.applied(e, name, value["with"], depth)
		}
	}
	if name, ok := e.value.(string); ok && e.scope[name].typeExpression == nil {
		if target, definition, local, err := e.named(name, "declaration"); err == nil && definition.Kind == "alias" && target.schema.declaration != "" {
			graph, err := readDeclaration(target.schema.declaration)
			if err != nil {
				return graph, err
			}
			root, _ := graph.Root.(map[string]any)
			family, _ := root["ref"].(string)
			familyNode, _ := graph.Definitions[family].(map[string]any)
			types, _ := familyNode["types"].(map[string]any)
			if normalized := types[local]; normalized != nil {
				if alias, ok := graph.Definitions[family+"/"+local].(map[string]any); ok && alias["kind"] == "alias" {
					normalized = alias["type"]
				}
				return b.canonical(graph, normalized, target, depth+1)
			}
		}
	}
	resolved, err := e.resolve("declaration")
	if err != nil {
		return declarationGraph{}, err
	}
	e = resolved.expression
	if resolved.definition != nil {
		if _, inline := e.value.(map[string]any); inline {
			return b.inline(resolved.definition, e, depth)
		}
		if e.schema.declaration == "" {
			return declarationGraph{}, fmt.Errorf("declaration: expected canonical type provenance")
		}
		graph, err := readDeclaration(e.schema.declaration)
		if err != nil {
			return graph, err
		}
		root, _ := graph.Root.(map[string]any)
		family, _ := root["ref"].(string)
		if graph.Definitions[family+"/"+resolved.name] == nil && (resolved.name == "Envelope" || resolved.name == "Handle") {
			closed, err := b.named(graph, family, e, depth)
			if err != nil {
				return graph, err
			}
			return declarationGraph{Version: 1, Definitions: map[string]any{}, Root: map[string]any{"projection": resolved.name, "family": map[string]any{"graph": closed}}}, nil
		}
		return b.named(graph, family+"/"+resolved.name, e, depth)
	}
	graph := declarationGraph{Version: 1, Definitions: map[string]any{}}
	switch value := e.value.(type) {
	case string:
		switch value {
		case "string", "integer", "number", "boolean", "json", "timestamp":
			graph.Root = map[string]any{"primitive": value}
			return graph, nil
		}
	case map[string]any:
		for _, key := range []string{"array", "map", "nullable"} {
			if child, ok := value[key]; ok {
				inner, err := b.expression(e.child(child), depth+1)
				if err != nil {
					return graph, err
				}
				graph.Root = map[string]any{key: map[string]any{"graph": inner}}
				return graph, nil
			}
		}
		if literal, ok := value["literal"].(string); ok {
			graph.Root = map[string]any{"literal": literal}
			return graph, nil
		}
		if value["empty"] == true {
			graph.Root = map[string]any{"empty": true}
			return graph, nil
		}
		if entity, ok := value["ref"].(string); ok {
			inner, err := b.expression(e.child(entity), depth+1)
			if err != nil {
				return graph, err
			}
			graph.Root = map[string]any{"entity": map[string]any{"graph": inner}}
			return graph, nil
		}
	}
	return graph, fmt.Errorf("declaration: type argument has no canonical declaration provenance")
}

func (b *declarationBuilder) applied(caller expression, name string, fillers any, depth int) (declarationGraph, error) {
	target, definition, local, err := caller.named(name, "declaration")
	if err != nil {
		return declarationGraph{}, err
	}
	graph := declarationGraph{}
	path := ""
	if target.schema.declaration != "" {
		graph, err = readDeclaration(target.schema.declaration)
		if err != nil {
			return graph, err
		}
		root, _ := graph.Root.(map[string]any)
		family, _ := root["ref"].(string)
		path = family + "/" + local
	}
	parameters := map[string]wireParameter{}
	if node, ok := graph.Definitions[path].(map[string]any); ok {
		for _, p := range declarationParameters(node) {
			n, _ := p["name"].(string)
			of, _ := p["of"].(string)
			parameters[n] = wireParameter{Name: n, Of: of}
		}
	}
	for _, p := range definition.Parameters {
		parameters[p.Name] = p
	}
	var normalized any
	if definition.Kind == "alias" && graph.Definitions != nil {
		if node, ok := graph.Definitions[path].(map[string]any); ok && node["kind"] == "alias" {
			normalized = node["type"]
		} else {
			family := path[:strings.LastIndex(path, "/")]
			familyNode, _ := graph.Definitions[family].(map[string]any)
			types, _ := familyNode["types"].(map[string]any)
			normalized = types[local]
		}
		needed := canonicalBindingNames(graph, normalized)
		for _, p := range target.schema.parameters {
			if needed[p.Name] {
				parameters[p.Name] = p
			}
		}
	}
	values, ok := fillers.(map[string]any)
	if !ok {
		return graph, fmt.Errorf("declaration: expected application arguments")
	}
	scope := map[string]argument{}
	for name, value := range target.scope {
		scope[name] = value
	}
	for name, value := range values {
		parameter, ok := parameters[name]
		if !ok {
			return graph, fmt.Errorf("declaration: unknown parameter %s", name)
		}
		if parameter.Of == "" {
			captured := caller.child(value)
			scope[name] = argument{typeExpression: &captured}
		} else {
			family, ok := value.(string)
			if !ok {
				return graph, fmt.Errorf("declaration: expected family binding %s", name)
			}
			schema := caller.scope[family].family
			if schema == nil {
				schema = caller.schema.imported[family]
			}
			if schema == nil {
				return graph, fmt.Errorf("declaration: expected family binding %s", name)
			}
			scope[name] = argument{family: schema}
		}
	}
	target.scope = scope
	for _, parameter := range definition.Parameters {
		if _, ok := scope[parameter.Name]; !ok {
			return graph, fmt.Errorf("declaration: missing required binding %s", parameter.Name)
		}
	}
	if definition.Kind == "alias" {
		if normalized != nil {
			return b.canonical(graph, normalized, target, depth+1)
		}
		return b.expression(target.child(definition.Type), depth+1)
	}
	if graph.Definitions == nil {
		return graph, fmt.Errorf("declaration: expected canonical type provenance")
	}
	return b.named(graph, path, target, depth)
}

func canonicalBindingNames(graph declarationGraph, value any) map[string]bool {
	names := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if path, ok := node["parameter"].(string); ok {
				names[path[strings.LastIndex(path, "/")+1:]] = true
			}
			if path, ok := node["ref"].(string); ok {
				definition, _ := graph.Definitions[path].(map[string]any)
				for _, p := range declarationParameters(definition) {
					if name, ok := p["name"].(string); ok {
						names[name] = true
					}
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(value)
	return names
}

func (b *declarationBuilder) canonical(graph declarationGraph, value any, e expression, depth int) (declarationGraph, error) {
	if depth > 256 {
		return graph, fmt.Errorf("declaration: cyclic supplied bindings")
	}
	node, ok := value.(map[string]any)
	if !ok {
		return graph, fmt.Errorf("declaration: expected canonical expression")
	}
	if nested, ok := node["graph"].(map[string]any); ok {
		data, _ := json.Marshal(nested)
		inner, err := readDeclaration(string(data))
		if err != nil {
			return graph, err
		}
		return b.canonical(inner, inner.Root, e, depth+1)
	}
	if path, ok := node["parameter"].(string); ok {
		name := path[strings.LastIndex(path, "/")+1:]
		arg, ok := e.scope[name]
		if !ok {
			return graph, fmt.Errorf("declaration: missing required binding %s", name)
		}
		if arg.typeExpression != nil {
			return b.expression(*arg.typeExpression, depth+1)
		}
		return b.family(arg.family)
	}
	if path, ok := node["ref"].(string); ok {
		return b.named(graph, path, e, depth)
	}
	if source, ok := node["draw"].(map[string]any); ok {
		member, _ := node["name"].(string)
		var family *Schema
		if parameter, ok := source["parameter"].(string); ok {
			family = e.scope[parameter[strings.LastIndex(parameter, "/")+1:]].family
		}
		if path, ok := source["ref"].(string); ok {
			family = e.schema.imported[path]
		}
		if family != nil {
			return b.expression(expressionContext(family, member), depth+1)
		}
		return graph, fmt.Errorf("declaration: missing family binding for draw %s", member)
	}
	closed := map[string]any{}
	for key, value := range node {
		closed[key] = value
	}
	child := func(value any) (any, error) {
		inner, err := b.canonical(graph, value, e, depth+1)
		if err != nil {
			return nil, err
		}
		return map[string]any{"graph": inner}, nil
	}
	if _, applied := node["apply"]; applied {
		arguments := []any{}
		for _, arg := range node["arguments"].([]any) {
			arg, err := child(arg)
			if err != nil {
				return graph, err
			}
			arguments = append(arguments, arg)
		}
		closed["arguments"] = arguments
	}
	for _, key := range []string{"array", "map", "nullable", "entity", "draw", "family", "type", "request", "result"} {
		if _, ok := node[key].(map[string]any); ok {
			value, err := child(node[key])
			if err != nil {
				return graph, err
			}
			closed[key] = value
		}
	}
	if fields, ok := node["fields"].([]any); ok {
		result := []any{}
		for _, item := range fields {
			field := map[string]any{}
			for key, value := range item.(map[string]any) {
				field[key] = value
			}
			value, err := child(field["type"])
			if err != nil {
				return graph, err
			}
			field["type"] = value
			result = append(result, field)
		}
		closed["fields"] = result
	}
	if bases, ok := node["extends"].([]any); ok {
		result := []any{}
		for _, base := range bases {
			value, err := child(base)
			if err != nil {
				return graph, err
			}
			result = append(result, value)
		}
		closed["extends"] = result
	}
	if variants, ok := node["variants"].(map[string]any); ok {
		result := map[string]any{}
		for tag, variant := range variants {
			value, err := child(variant)
			if err != nil {
				return graph, err
			}
			result[tag] = value
		}
		closed["variants"] = result
	}
	return selectDeclaration(graph, closed)
}

func (b *declarationBuilder) inline(definition *wireType, e expression, depth int) (declarationGraph, error) {
	graph := declarationGraph{Version: 1, Definitions: map[string]any{}}
	if len(definition.Parameters) != 0 {
		return graph, fmt.Errorf("declaration: inline type has unbound parameters")
	}
	node := map[string]any{"kind": definition.Kind, "parameters": []any{}}
	child := func(value any) (any, error) {
		closed, err := b.expression(e.child(value), depth+1)
		if err != nil {
			return nil, err
		}
		return map[string]any{"graph": closed}, nil
	}
	if len(definition.Extends) > 0 {
		extends := []any{}
		for _, base := range definition.Extends {
			value, err := child(base)
			if err != nil {
				return graph, err
			}
			extends = append(extends, value)
		}
		node["extends"] = extends
	}
	switch definition.Kind {
	case "record", "entity":
		node["open"] = definition.Open
		fields := []any{}
		for _, field := range definition.Fields {
			typ, err := child(field.Type)
			if err != nil {
				return graph, err
			}
			entry := map[string]any{"name": field.Name, "type": typ, "required": field.Required, "nullable": field.Nullable, "unique": field.Unique}
			for key, bound := range map[string]*json.Number{"min": field.Min, "max": field.Max} {
				if bound != nil {
					entry[key] = declarationDecimal(*bound)
				}
			}
			if field.Length != nil {
				length := map[string]any{}
				if field.Length.Min != nil {
					length["min"] = strconv.Itoa(*field.Length.Min)
				}
				if field.Length.Max != nil {
					length["max"] = strconv.Itoa(*field.Length.Max)
				}
				entry["length"] = length
			}
			if field.Pattern != "" {
				entry["pattern"] = field.Pattern
			}
			fields = append(fields, entry)
		}
		node["fields"] = fields
		if definition.Kind == "entity" {
			node["key"] = definition.Key
		}
	case "enum":
		values := append([]string{}, definition.Values...)
		slices.Sort(values)
		node["values"] = slices.Compact(values)
	case "union":
		node["tag"] = definition.Tag
		value := definition.Value
		if value == "" {
			value = "value"
		}
		node["value"] = value
		variants := map[string]any{}
		for tag, expression := range definition.Variants {
			value, err := child(expression)
			if err != nil {
				return graph, err
			}
			variants[tag] = value
		}
		node["variants"] = variants
	default:
		return graph, fmt.Errorf("declaration: expected canonical provenance for %s", definition.Kind)
	}
	graph.Root = node
	return graph, nil
}

// The shared declaration encoding retains exact decimal meaning, including
// integer bounds beyond the exact range of binary floating point.
func declarationDecimal(number json.Number) string {
	value := string(number)
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(value), "e")
	power := new(big.Int)
	if hasExponent {
		power.SetString(exponent, 10)
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}
	power.Sub(power, big.NewInt(int64(len(fraction))))
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if negative {
		trimmed = "-" + trimmed
	}
	if power.Sign() != 0 {
		trimmed += "e" + power.String()
	}
	return trimmed
}

// Selection follows lexical references, but never crosses an argument graph's
// definition scope. Definition cycles are visited once, without hash recursion.
func selectDeclaration(source declarationGraph, root any) (declarationGraph, error) {
	selected := declarationGraph{Version: 1, Root: root, Definitions: map[string]any{}}
	var visit func(any) error
	include := func(path string) error { return nil }
	include = func(path string) error {
		if _, exists := selected.Definitions[path]; exists {
			return nil
		}
		definition, exists := source.Definitions[path]
		if !exists {
			return fmt.Errorf("declaration: unknown reference %s", path)
		}
		selected.Definitions[path] = definition
		return visit(definition)
	}
	visit = func(value any) error {
		switch value := value.(type) {
		case map[string]any:
			if _, scoped := value["graph"]; scoped {
				return nil
			}
			for _, key := range []string{"ref", "apply"} {
				if path, ok := value[key].(string); ok {
					if err := include(path); err != nil {
						return err
					}
				}
			}
			for key, child := range value {
				if key != "ref" && key != "apply" {
					if err := visit(child); err != nil {
						return err
					}
				}
			}
		case []any:
			for _, child := range value {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return selected, err
	}
	return selected, nil
}
