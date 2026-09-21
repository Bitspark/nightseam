package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

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
	if s == nil || s.declaration == "" {
		return declarationGraph{}, fmt.Errorf("declaration: expected canonical family provenance")
	}
	if b.active[s] {
		return declarationGraph{}, fmt.Errorf("declaration: cyclic supplied bindings")
	}
	b.active[s] = true
	defer delete(b.active, s)
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
	resolved, err := e.resolve("declaration")
	if err != nil {
		return declarationGraph{}, err
	}
	e = resolved.expression
	if resolved.definition != nil {
		if e.schema.declaration == "" {
			return declarationGraph{}, fmt.Errorf("declaration: expected canonical type provenance")
		}
		graph, err := readDeclaration(e.schema.declaration)
		if err != nil {
			return graph, err
		}
		root, _ := graph.Root.(map[string]any)
		family, _ := root["ref"].(string)
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
				inner.Root = map[string]any{key: inner.Root}
				return inner, nil
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
			inner.Root = map[string]any{"entity": inner.Root}
			return inner, nil
		}
	}
	return graph, fmt.Errorf("declaration: type argument has no canonical declaration provenance")
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
