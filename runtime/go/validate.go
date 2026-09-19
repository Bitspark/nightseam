package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Schema validates wire values against a family's types, as the generator
// describes them: a record's fields with their presence, nullness and
// constraints, its parents, an enum's values, an alias's target. A
// generated protocol package embeds its family's description and builds
// one Schema of it; a type of another family is validated by that family's
// validator, which the package hands over by name; a type drawn from a
// parameter is JSON to the schema, since the family that fills it is
// chosen where the generic type is instantiated, and its codec validates
// there.
//
// A type expression is read as the contract writes it: a primitive, a
// type of the family, "family.Type" for an imported family's, "S.Type" for
// a parameter's, {"array": T}, {"map": T}, {"ref": "Entity"} for the
// entity's key, {"apply": "family.Type", "with": {...}} for an imported
// generic type, and {"empty": true} for a request that takes nothing.
//
// A refusal is one string: a JSON pointer naming the member that was wrong,
// then the fact about it — "$.count: required field missing", "$.note: null
// is not permitted", "$.zzz: unknown field". The TypeScript runtime prints
// the same string for the same value, word for word, and
// conformance/tables/validator.json holds both to it, so a consumer whose
// server is in one language and client in the other reads one spelling of
// one refusal.
type Schema struct {
	types    map[string]wireType
	imported map[string]Imported
}

// Imported validates a named type of another family: that family's own
// ValidateRaw, which a generated protocol package hands to every family
// that refers to it. at is the location the value sits at in the value
// being validated, which a family passes when it reaches into another's
// type, so that one refusal carries the one pointer the consumer handed
// its value in at rather than a root per family the value crossed; absent,
// the value is its own root.
type Imported func(name string, data []byte, at ...string) error

type wireType struct {
	Kind    string
	Key     string
	Fields  []wireField
	Extends []string
	Open    bool
	Values  []string
	Type    any
}

type wireField struct {
	Name     string
	Type     any
	Required bool
	Nullable bool
	Unique   bool
	Min, Max *json.Number
	Length   *struct{ Min, Max *int }
	Pattern  string
}

// NewSchema reads a family's wire description; imported maps each family
// it refers to to that family's ValidateRaw.
func NewSchema(wire []byte, imported map[string]Imported) (*Schema, error) {
	s := &Schema{imported: imported}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := decoder.Decode(&s.types); err != nil {
		return nil, err
	}
	if s.imported == nil {
		s.imported = map[string]Imported{}
	}
	return s, nil
}

// MustSchema is NewSchema for a description the generator wrote.
func MustSchema(wire string, imported map[string]Imported) *Schema {
	s, err := NewSchema([]byte(wire), imported)
	if err != nil {
		panic(err)
	}
	return s
}

// MustTypeExpression decodes a type expression the generator wrote, and
// panics on one it cannot read, as MustSchema does: its caller is generated
// code passing a constant, for which a malformed expression is a generator
// bug and not a consumer's error. A hand-written expression is decoded with
// json.Unmarshal.
func MustTypeExpression(encoded string) any {
	var value any
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		panic(err)
	}
	return value
}

// ValidateRaw verifies a named type's value, including null and field
// presence. at roots the diagnostic where a family that imports this one
// holds the value; absent, the value is its own root.
func (s *Schema) ValidateRaw(name string, data []byte, at ...string) error {
	return s.ValidateExpressionRaw(name, data, at...)
}

// ValidateExpressionRaw verifies a type expression's value and rejects
// trailing values. at roots the diagnostic, as ValidateRaw's does.
func (s *Schema) ValidateExpressionRaw(expression any, data []byte, at ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	location := "$"
	if len(at) > 0 {
		location = at[0]
	}
	return s.validate(expression, value, location)
}

// ValidateValue validates a typed value before publishing it on the wire.
func (s *Schema) ValidateValue(expression any, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.ValidateExpressionRaw(expression, data)
}

// Fields names a record's wire fields, its parents' first.
func (s *Schema) Fields(name string) []string {
	var out []string
	for _, field := range s.flattened(name) {
		out = append(out, field.Name)
	}
	return out
}

func (s *Schema) flattened(name string) []wireField {
	var out []wireField
	t := s.types[name]
	for _, base := range t.Extends {
		out = append(out, s.flattened(base)...)
	}
	return append(out, t.Fields...)
}

func (s *Schema) validate(expression any, value any, location string) error {
	bad := func(want string) error { return fmt.Errorf("%s: expected %s", location, want) }
	if composite, ok := expression.(map[string]any); ok {
		if element, ok := composite["array"]; ok {
			items, ok := value.([]any)
			if !ok {
				return bad("array")
			}
			for i, item := range items {
				if err := s.validate(element, item, fmt.Sprintf("%s[%d]", location, i)); err != nil {
					return err
				}
			}
			return nil
		}
		if element, ok := composite["map"]; ok {
			items, ok := value.(map[string]any)
			if !ok {
				return bad("object")
			}
			for key, item := range items {
				if err := s.validate(element, item, location+"."+key); err != nil {
					return err
				}
			}
			return nil
		}
		if entity, ok := composite["ref"].(string); ok {
			t, known := s.types[entity]
			if !known {
				return bad("known entity")
			}
			for _, field := range s.flattened(entity) {
				if field.Name == t.Key {
					return s.validate(field.Type, value, location)
				}
			}
			return bad("entity with a key")
		}
		if reference, ok := composite["apply"].(string); ok {
			return s.foreign(reference, value, location)
		}
		if _, ok := composite["empty"]; ok {
			items, ok := value.(map[string]any)
			if !ok || len(items) != 0 {
				return bad("empty object")
			}
			return nil
		}
		return bad("supported type expression")
	}
	name, ok := expression.(string)
	if !ok {
		return bad("type expression")
	}
	switch name {
	case "json":
		return validateJSON(value, location)
	case "string":
		if _, ok := value.(string); !ok {
			return bad("string")
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad("boolean")
		}
		return nil
	case "number", "integer":
		n, ok := value.(json.Number)
		if !ok {
			return bad(name)
		}
		v, err := n.Float64()
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return bad("finite number")
		}
		if name == "integer" && !safeInteger(string(n)) {
			return bad("JavaScript-safe integer")
		}
		return nil
	case "timestamp":
		text, ok := value.(string)
		if !ok {
			return bad("timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
			return bad("RFC3339 timestamp")
		}
		return nil
	}
	if strings.IndexByte(name, '.') >= 0 {
		if name[0] >= 'A' && name[0] <= 'Z' {
			// A type drawn from a parameter: the family that fills it
			// validates where the generic type is instantiated.
			return validateJSON(value, location)
		}
		return s.foreign(name, value, location)
	}
	t, ok := s.types[name]
	if !ok {
		return bad("known type")
	}
	switch t.Kind {
	case "alias":
		return s.validate(t.Type, value, location)
	case "enum":
		text, ok := value.(string)
		if !ok {
			return bad(name)
		}
		for _, option := range t.Values {
			if option == text {
				return nil
			}
		}
		return bad(name)
	case "record", "entity":
		obj, ok := value.(map[string]any)
		if !ok {
			return bad(name + " object")
		}
		allowed := map[string]bool{}
		for _, field := range s.flattened(name) {
			allowed[field.Name] = true
			member, present := obj[field.Name]
			if !present {
				if field.Required {
					return fmt.Errorf("%s.%s: required field missing", location, field.Name)
				}
				continue
			}
			if member == nil {
				if field.Nullable {
					continue
				}
				return fmt.Errorf("%s.%s: null is not permitted", location, field.Name)
			}
			at := location + "." + field.Name
			if err := s.validate(field.Type, member, at); err != nil {
				return err
			}
			if err := constrain(field, member, at); err != nil {
				return err
			}
		}
		for key, value := range obj {
			if allowed[key] {
				continue
			}
			if !t.Open {
				return fmt.Errorf("%s.%s: unknown field", location, key)
			}
			if err := validateJSON(value, location+"."+key); err != nil {
				return err
			}
		}
		return nil
	}
	return bad("supported type")
}

// foreign validates a value of another family's type by that family's
// validator: family.Type, or the type an application names. The location
// goes with the value, so that the other family's validator names the
// member that was wrong at the pointer the consumer's own value has it at.
func (s *Schema) foreign(reference string, value any, location string) error {
	family, typeName, _ := strings.Cut(reference, ".")
	validate, ok := s.imported[family]
	if !ok {
		return fmt.Errorf("%s: expected known family", location)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return validate(typeName, data, location)
}

// constrain holds a field's value to its constraints: min and max on a
// number or a timestamp, length on a string or an array, pattern on a
// string.
func constrain(field wireField, value any, location string) error {
	if field.Min != nil || field.Max != nil {
		switch v := value.(type) {
		case json.Number:
			n, _ := v.Float64()
			if field.Min != nil {
				if min, _ := field.Min.Float64(); n < min {
					return fmt.Errorf("%s: expected at least %s", location, field.Min)
				}
			}
			if field.Max != nil {
				if max, _ := field.Max.Float64(); n > max {
					return fmt.Errorf("%s: expected at most %s", location, field.Max)
				}
			}
		case string:
			if field.Min != nil && v < field.Min.String() {
				return fmt.Errorf("%s: expected at or after %s", location, field.Min)
			}
			if field.Max != nil && v > field.Max.String() {
				return fmt.Errorf("%s: expected at or before %s", location, field.Max)
			}
		}
	}
	if field.Length != nil {
		length := -1
		switch v := value.(type) {
		case string:
			length = len([]rune(v))
		case []any:
			length = len(v)
		}
		if length >= 0 {
			if field.Length.Min != nil && length < *field.Length.Min {
				return fmt.Errorf("%s: expected a length of at least %d", location, *field.Length.Min)
			}
			if field.Length.Max != nil && length > *field.Length.Max {
				return fmt.Errorf("%s: expected a length of at most %d", location, *field.Length.Max)
			}
		}
	}
	if field.Pattern != "" {
		if text, ok := value.(string); ok {
			matched, err := regexp.MatchString(field.Pattern, text)
			if err != nil || !matched {
				return fmt.Errorf("%s: expected a match of %s", location, field.Pattern)
			}
		}
	}
	return nil
}

func validateJSON(value any, location string) error {
	switch typed := value.(type) {
	case json.Number:
		if n, err := typed.Float64(); err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return fmt.Errorf("%s: expected finite JSON number", location)
		}
	case []any:
		for i, item := range typed {
			if err := validateJSON(item, fmt.Sprintf("%s[%d]", location, i)); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if err := validateJSON(item, location+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

// safeInteger checks decimal integer precision without constructing huge
// powers from an untrusted exponent. The JSON decoder has already checked
// number syntax.
func safeInteger(raw string) bool {
	text := strings.TrimPrefix(raw, "-")
	mantissa, exponent := text, "0"
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		mantissa, exponent = text[:at], text[at+1:]
	}
	fraction := 0
	if at := strings.IndexByte(mantissa, '.'); at >= 0 {
		fraction = len(mantissa) - at - 1
		mantissa = mantissa[:at] + mantissa[at+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return true
	}
	power, err := strconv.ParseInt(exponent, 10, 64)
	if err != nil || power > int64(len(raw))+16 || power < -int64(len(raw))-16 {
		return false
	}
	scale := power - int64(fraction)
	if scale < 0 {
		cut := -scale
		if cut > int64(len(digits)) {
			return false
		}
		tail := digits[len(digits)-int(cut):]
		if strings.Trim(tail, "0") != "" {
			return false
		}
		digits = digits[:len(digits)-int(cut)]
	} else {
		if int64(len(digits))+scale > 16 {
			return false
		}
		digits += strings.Repeat("0", int(scale))
	}
	if len(digits) > 16 {
		return false
	}
	value, err := strconv.ParseUint(digits, 10, 64)
	return err == nil && value <= 9007199254740991
}
