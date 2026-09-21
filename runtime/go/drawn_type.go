package runtime

import "fmt"

// ValidateDrawnType checks the original declaration selected by a supplied
// family interpretation. It does not normalize an alias into an allowed kind.
func ValidateDrawnType(binding TypeBinding, member string, requireObject bool) error {
	if binding.Schema == nil {
		return fmt.Errorf("family binding: missing drawn declaration")
	}
	e := expressionContext(binding.Schema, binding.Type)
	name, ok := e.value.(string)
	if !ok {
		return fmt.Errorf("family binding: a draw requires a plain declared member")
	}
	target, definition, _, err := e.named(name, "family binding")
	if err != nil {
		return err
	}
	if err := validateDrawnDefinition(target.schema, definition, requireObject); err != nil {
		return err
	}
	return validateDrawnDefinition(target.schema, target.schema.types[member], requireObject)
}

func validateDrawnDefinition(schema *Schema, definition *wireType, requireObject bool) error {
	if definition == nil {
		return fmt.Errorf("family binding: missing drawn member")
	}
	if len(definition.Parameters) > 0 || len(schema.freeParameters(definition, map[*wireType]bool{})) > 0 {
		return fmt.Errorf("family binding: a draw requires a nongeneric member")
	}
	switch definition.Kind {
	case "record", "entity", "union":
		return nil
	case "enum":
		if !requireObject {
			return nil
		}
	}
	if requireObject {
		return fmt.Errorf("family binding: a draw requires a plain member with an object request shape")
	}
	return fmt.Errorf("family binding: a draw requires a plain member")
}
