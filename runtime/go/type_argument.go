package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// TypeBinding retains the schema where a supplied type expression belongs.
// Generated WireType methods return it so aliases, literals and constraints
// survive use as Go type arguments, including inside another family.
type TypeBinding struct {
	Schema *Schema
	Type   any
}

// TypeArgument supplies an instantiated Go type without a consumer codec
// argument or registry. Discovery is lazy: recursive generated types may
// return bindings for their arguments without recursively describing them.
func TypeArgument[T any]() TypeBinding {
	return TypeBinding{Schema: typeArgumentSchema, Type: goArgument{reflect.TypeFor[T]()}}
}

var typeArgumentSchema = &Schema{types: map[string]*wireType{}}

type goArgument struct{ typ reflect.Type }
type goValue struct{ typ reflect.Type }
type wireTyper interface{ WireType() TypeBinding }
type argumentDescriber interface{ typeArgument() any }

func (Nullable[T]) typeArgument() any {
	return map[string]any{"nullable": TypeArgument[T]()}
}

func describeArgument(typ reflect.Type) TypeBinding {
	binding := TypeBinding{Schema: typeArgumentSchema}
	if typ.Kind() == reflect.Pointer {
		binding.Type = map[string]any{"nullable": goArgument{typ.Elem()}}
		return binding
	}
	if described, ok := reflect.Zero(typ).Interface().(argumentDescriber); ok {
		binding.Type = described.typeArgument()
		return binding
	}
	if described, ok := reflect.New(typ).Interface().(wireTyper); ok {
		return described.WireType()
	}
	if typ == reflect.TypeFor[Raw]() || typ == reflect.TypeFor[json.RawMessage]() {
		binding.Type = "json"
		return binding
	}
	if typ == reflect.TypeFor[time.Time]() {
		binding.Type = "timestamp"
		return binding
	}
	if reflect.PointerTo(typ).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		binding.Type = goValue{typ}
		return binding
	}
	switch typ.Kind() {
	case reflect.String:
		binding.Type = "string"
	case reflect.Bool:
		binding.Type = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		binding.Type = "integer"
	case reflect.Float32, reflect.Float64:
		binding.Type = "number"
	case reflect.Slice, reflect.Array:
		if typ == reflect.TypeFor[[]byte]() {
			// encoding/json carries a byte slice as a base64 string.
			binding.Type = "string"
		} else {
			binding.Type = map[string]any{"array": goArgument{typ.Elem()}}
		}
	case reflect.Map:
		if typ.Key().Kind() == reflect.String {
			binding.Type = map[string]any{"map": goArgument{typ.Elem()}}
		} else {
			binding.Type = goValue{typ}
		}
	case reflect.Interface:
		if typ.NumMethod() == 0 {
			binding.Type = "json"
		} else {
			binding.Type = goValue{typ}
		}
	default:
		binding.Type = goValue{typ}
	}
	return binding
}

// A consumer's own Go type can supply its normal JSON codec. Generated
// types instead use WireType above, preserving the declaration's stronger
// constraints before their decoder runs.
func (g goValue) validate(value any, location string) error {
	if err := validateJSON(value, location); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, reflect.New(g.typ).Interface()); err != nil {
		return fmt.Errorf("%s: invalid Go type argument %s: %w", location, g.typ, err)
	}
	return nil
}
