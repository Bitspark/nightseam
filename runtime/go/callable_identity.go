package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CallableIdentity identifies a closed nominal callable interpretation. Its
// application path and digest come from the same canonical declaration graph;
// a nongeneric callable retains its declaring family's revision digest.
func CallableIdentity(binding TypeBinding) (DeclarationIdentity, error) {
	if binding.Schema == nil {
		return DeclarationIdentity{}, fmt.Errorf("declaration: expected schema for callable binding")
	}
	expression := expressionContext(binding.Schema, binding.Type)
	resolved, err := expression.resolve("declaration")
	if err != nil {
		return DeclarationIdentity{}, err
	}
	return resolved.callableIdentity(expression)
}

func (r resolvedExpression) callableIdentity(source expression) (DeclarationIdentity, error) {
	if r.definition == nil || r.definition.Kind != "callable" {
		return DeclarationIdentity{}, fmt.Errorf("declaration: expected callable declaration")
	}
	graph, err := (&declarationBuilder{active: map[*Schema]bool{}}).expression(source, 0)
	if err != nil {
		return DeclarationIdentity{}, err
	}
	document, err := graph.document()
	if err != nil {
		return DeclarationIdentity{}, err
	}
	// The builder retains nested graphs as structs; reading the canonical bytes
	// presents the exact same expression objects to both runtime printers.
	graph, err = readDeclaration(document)
	if err != nil {
		return DeclarationIdentity{}, err
	}
	root, ok := graph.Root.(map[string]any)
	if !ok {
		return DeclarationIdentity{}, fmt.Errorf("declaration: expected nominal callable root")
	}
	nominal, applied := root["apply"].(string)
	if !applied {
		nominal, ok = root["ref"].(string)
		if !ok {
			return DeclarationIdentity{}, fmt.Errorf("declaration: expected nominal callable root")
		}
	}
	definition, _ := graph.Definitions[nominal].(map[string]any)
	if definition["kind"] != "callable" {
		return DeclarationIdentity{}, fmt.Errorf("declaration: expected callable declaration")
	}
	path, err := callableExpressionName(root)
	if err != nil {
		return DeclarationIdentity{}, err
	}
	digest := r.schema.digest
	if applied {
		digest = declarationHash(document)
	}
	return DeclarationIdentity{Path: path, Digest: digest}, nil
}

// Plain descriptors used without generated provenance retain the ordinary
// nongeneric reference check. Required arguments never take that path.
func (r resolvedExpression) expectedCallableIdentity(source expression) (DeclarationIdentity, error) {
	if r.schema.declaration == "" && len(r.definition.Parameters) == 0 && len(r.schema.freeParameters(r.definition, map[*wireType]bool{})) == 0 {
		return DeclarationIdentity{Path: r.definition.Contract, Digest: r.schema.digest}, nil
	}
	return r.callableIdentity(source)
}

func callableExpressionName(value any) (string, error) {
	node, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("declaration: expected closed argument expression")
	}
	if nested, ok := node["graph"].(map[string]any); ok {
		return callableExpressionName(nested["root"])
	}
	for _, key := range []string{"primitive", "ref"} {
		if name, ok := node[key].(string); ok {
			return name, nil
		}
	}
	if name, ok := node["apply"].(string); ok {
		arguments, ok := node["arguments"].([]any)
		if !ok {
			return "", fmt.Errorf("declaration: expected closed application arguments")
		}
		names := make([]string, len(arguments))
		for i, argument := range arguments {
			var err error
			names[i], err = callableExpressionName(argument)
			if err != nil {
				return "", err
			}
		}
		return name + "<" + strings.Join(names, ",") + ">", nil
	}
	for _, form := range []struct{ key, prefix, suffix string }{{"array", "[", "]"}, {"map", "{", "}"}, {"nullable", "", "?"}, {"entity", "&", ""}} {
		if child, ok := node[form.key]; ok {
			name, err := callableExpressionName(child)
			return form.prefix + name + form.suffix, err
		}
	}
	if name, ok := node["projection"].(string); ok {
		family, err := callableExpressionName(node["family"])
		return family + "/" + name, err
	}
	if literal, ok := node["literal"].(string); ok {
		encoded, err := json.Marshal(literal)
		return string(encoded), err
	}
	if node["empty"] == true {
		return "()", nil
	}
	if _, ok := node["kind"].(string); ok {
		plain, err := callableShapeName(node)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(plain)
		return "shape(" + string(encoded) + ")", err
	}
	return "", fmt.Errorf("declaration: expected closed argument expression")
}

// Anonymous shapes have structural expression names, never target-derived
// nominal names. Definition content remains in the digest, not this spelling.
func callableShapeName(value any) (any, error) {
	switch node := value.(type) {
	case map[string]any:
		if nested, ok := node["graph"].(map[string]any); ok {
			return callableShapeName(nested["root"])
		}
		if node["parameter"] != nil || node["draw"] != nil || node["unresolved"] != nil {
			return nil, fmt.Errorf("declaration: expected closed argument expression")
		}
		result := map[string]any{}
		for key, child := range node {
			converted, err := callableShapeName(child)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	case []any:
		result := make([]any, len(node))
		for i, child := range node {
			var err error
			result[i], err = callableShapeName(child)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	default:
		return value, nil
	}
}
