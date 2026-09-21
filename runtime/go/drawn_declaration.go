package runtime

import "fmt"

// Bind's drawn-member table exists for validation and is not a declaration of
// a family. A generated member retains its complete source schema; recover that
// association lazily, when identity is requested, without changing Bind's error
// contract or the validation aliases it creates.
func (b *declarationBuilder) drawnFamily(s *Schema) (declarationGraph, error) {
	var source *Schema
	var selected declarationGraph
	var document string
	for _, member := range sortedKeys(s.drawn) {
		origin, err := drawnDeclarationSource(s.drawn[member])
		if err != nil {
			return selected, fmt.Errorf("declaration: drawn member %s: %w", member, err)
		}
		if origin == nil {
			continue
		}
		graph, err := b.family(origin)
		if err != nil {
			return selected, fmt.Errorf("declaration: drawn member %s: %w", member, err)
		}
		encoded, err := graph.document()
		if err != nil {
			return selected, err
		}
		if source == nil {
			source, selected, document = origin, graph, encoded
		} else if encoded != document {
			return selected, fmt.Errorf("declaration: drawn member %s has a different family interpretation", member)
		}
	}
	if source == nil {
		return selected, fmt.Errorf("declaration: drawn members have no canonical family provenance")
	}
	for _, member := range sortedKeys(s.drawn) {
		actual, err := b.expression(s.drawn[member], 0)
		if err != nil {
			return selected, fmt.Errorf("declaration: drawn member %s: %w", member, err)
		}
		expected, err := b.expression(expressionContext(source, member), 0)
		if err != nil {
			return selected, fmt.Errorf("declaration: drawn member %s: %w", member, err)
		}
		actualDocument, err := actual.document()
		if err != nil {
			return selected, err
		}
		expectedDocument, err := expected.document()
		if err != nil {
			return selected, err
		}
		if actualDocument != expectedDocument {
			return selected, fmt.Errorf("declaration: drawn member %s does not match its family declaration", member)
		}
	}
	return selected, nil
}

// Only declaration metadata supplies a family association. Primitive Go values
// may match an alias in an already associated family, but cannot name a family
// on their own. In particular, a reflect.Type is never an identity input.
func drawnDeclarationSource(e expression) (*Schema, error) {
	explicit := false
	for depth := 0; depth < 256; depth++ {
		if e.schema == nil {
			return nil, fmt.Errorf("expected schema for drawn member")
		}
		switch value := e.value.(type) {
		case TypeBinding:
			if value.Schema == nil {
				return nil, fmt.Errorf("expected schema for drawn member")
			}
			e, explicit = expressionContext(value.Schema, value.Type), true
			continue
		case goArgument:
			e.value = describeArgument(value.typ)
			continue
		case string:
			if argument := e.scope[value].typeExpression; argument != nil {
				e = *argument
				continue
			}
			if target, definition, _, err := e.named(value, "declaration"); err == nil {
				if target.schema.declaration != "" {
					source := *target.schema
					source.scope = target.scope
					return &source, nil
				}
				if definition.Kind == "alias" {
					e = target.child(definition.Type)
					continue
				}
			}
		}
		if explicit && e.schema.declaration != "" {
			source := *e.schema
			source.scope = e.scope
			return &source, nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("cyclic drawn member provenance")
}
