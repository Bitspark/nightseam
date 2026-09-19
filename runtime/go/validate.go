package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Bitspark/nightseam/internal/pattern"
)

// Schema validates a family's descriptor: {"types": {...}, "parameters":
// [...]}. Its imports retain their own descriptors, so an application can
// fill type and family parameters without losing the caller's scope.
//
// Expressions include primitives, named and imported types, family draws,
// arrays, maps, entity references, applications, literals, nullable values,
// inline shapes and the empty object. Records and internally tagged unions
// inherit their fields or variants. Field presence is separate from nullness.
//
// Bind supplies parameters to raw validation. An explicitly applied argument
// is always validated. A generated Go generic codec may instead leave a
// family parameter unbound: the schema checks the enclosing shape and the
// instantiated Go codec checks that slot during marshal or unmarshal.
//
// A refusal gives the location and the fact about it. Both runtimes read
// conformance/tables/validator.json, including the same refusal strings.
type Schema struct {
	types      map[string]*wireType
	imported   map[string]*Schema
	scope      map[string]argument
	parameters []wireParameter
}

type argument struct {
	typeExpression *expression
	family         *Schema
	unboundFamily  bool
}

// An expression retains the schema and bindings where it was written. In
// particular, an argument to an imported generic belongs to its caller.
type expression struct {
	schema  *Schema
	value   any
	scope   map[string]argument
	aliases map[*wireType]bool
}

type wireParameter struct{ Name, Of string }

type wireType struct {
	Kind       string
	Key        string
	Fields     []wireField
	Extends    []string
	Open       bool
	Values     []string
	Type       any
	Parameters []wireParameter
	Tag, Value string
	Variants   map[string]any
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
// it refers to to that family's Schema.
func NewSchema(wire []byte, imported map[string]*Schema) (*Schema, error) {
	s := &Schema{imported: imported}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	var description struct {
		Types      map[string]*wireType
		Parameters []wireParameter
	}
	if err := decoder.Decode(&description); err != nil {
		return nil, err
	}
	s.types, s.parameters = description.Types, description.Parameters
	if s.types == nil {
		return nil, fmt.Errorf("expected family descriptor with types")
	}
	for _, name := range sortedKeys(s.types) {
		if err := checkPatterns(s.types[name]); err != nil {
			return nil, err
		}
	}
	if s.imported == nil {
		s.imported = map[string]*Schema{}
	}
	return s, nil
}

// MustSchema is NewSchema for a description the generator wrote.
func MustSchema(wire string, imported map[string]*Schema) *Schema {
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
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
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
	if err := checkPatterns(expression); err != nil {
		return err
	}
	for _, name := range sortedKeys(s.scope) {
		if argument := s.scope[name].typeExpression; argument != nil {
			if err := checkPatterns(argument.value); err != nil {
				return err
			}
		}
	}
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
	return (expressionContext(s, expression)).validate(value, location)
}

// Patterns are descriptor constraints, so an invalid one is refused even
// when its optional field is absent or its containing collection is empty.
func checkPatterns(value any) error {
	check := func(value string) error {
		if pattern.Check(value) != nil {
			quoted, _ := json.Marshal(value)
			return fmt.Errorf("pattern %s: outside Nightseam dialect", quoted)
		}
		return nil
	}
	switch value := value.(type) {
	case *wireType:
		if value == nil {
			return nil
		}
		for _, field := range value.Fields {
			if err := check(field.Pattern); err != nil {
				return err
			}
			if err := checkPatterns(field.Type); err != nil {
				return err
			}
		}
		if err := checkPatterns(value.Type); err != nil {
			return err
		}
		for _, tag := range sortedKeys(value.Variants) {
			if err := checkPatterns(value.Variants[tag]); err != nil {
				return err
			}
		}
	case map[string]any:
		if value, ok := value["pattern"].(string); ok {
			if err := check(value); err != nil {
				return err
			}
		}
		for _, key := range sortedKeys(value) {
			if err := checkPatterns(value[key]); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range value {
			if err := checkPatterns(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func expressionContext(s *Schema, value any) expression {
	return expression{schema: s, value: value, scope: s.scope}
}

// Bind returns a schema with type and family parameters filled. Type
// arguments are interpreted in the receiver's scope, before this binding.
func (s *Schema) Bind(types map[string]any, families map[string]*Schema) *Schema {
	bound := *s
	bound.scope = map[string]argument{}
	for name, arg := range s.scope {
		bound.scope[name] = arg
	}
	for name, value := range types {
		e := expressionContext(s, value)
		bound.scope[name] = argument{typeExpression: &e}
	}
	for name, family := range families {
		bound.scope[name] = argument{family: family}
	}
	return &bound
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
	resolved, err := expressionContext(s, name).resolve("$")
	if err != nil {
		return nil
	}
	fields, err := resolved.fields("$", map[*wireType]bool{})
	if err != nil {
		return nil
	}
	var out []string
	for _, field := range fields {
		out = append(out, field.field.Name)
	}
	return out
}

func (e expression) child(value any) expression { e.value = value; e.aliases = nil; return e }

type resolvedExpression struct {
	expression
	definition *wireType
	name       string
}

func expected(location, want string) error { return fmt.Errorf("%s: expected %s", location, want) }

// freeParameters follows declarations, including local applications, but
// leaves a supplied imported argument in the caller's scope. A type needs
// only the enclosing family's parameters that it actually uses.
func (s *Schema) freeParameters(t *wireType, seen map[*wireType]bool) []wireParameter {
	if t == nil || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)
	used := map[string]bool{}
	var walk func(any)
	inherit := func(t *wireType) {
		for _, p := range s.freeParameters(t, seen) {
			used[p.Name] = true
		}
	}
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			prefix, member, dotted := strings.Cut(v, ".")
			for _, p := range s.parameters {
				if prefix == p.Name {
					used[p.Name] = true
					return
				}
			}
			if !dotted {
				inherit(s.types[v])
				return
			}
			if imported := s.imported[prefix]; imported != nil {
				target := imported.types[member]
				if target == nil {
					return
				}
				needed := append(imported.freeParameters(target, seen), target.Parameters...)
				for _, parameter := range needed {
					if parameter.Of != "" {
						if p, ok := s.singleFamilyParameter(); ok {
							used[p.Name] = true
						}
					}
				}
			}
		case map[string]any:
			for _, key := range []string{"array", "map", "nullable"} {
				if inner, ok := v[key]; ok {
					walk(inner)
					return
				}
			}
			if target, ok := v["apply"].(string); ok {
				if !strings.Contains(target, ".") {
					inherit(s.types[target])
				}
				if fillers, ok := v["with"].(map[string]any); ok {
					for _, filler := range fillers {
						walk(filler)
					}
				}
				return
			}
			if name, ok := v["ref"].(string); ok {
				if entity := s.types[name]; entity != nil {
					for _, field := range entity.Fields {
						if field.Name == entity.Key {
							walk(field.Type)
						}
					}
				}
				return
			}
			if _, inline := v["kind"]; inline {
				for _, key := range []string{"fields", "extends"} {
					if items, ok := v[key].([]any); ok {
						for _, item := range items {
							if field, ok := item.(map[string]any); ok {
								walk(field["type"])
							} else {
								walk(item)
							}
						}
					}
				}
				if variants, ok := v["variants"].(map[string]any); ok {
					for _, variant := range variants {
						walk(variant)
					}
				}
			}
		}
	}
	walk(t.Type)
	for _, field := range t.Fields {
		walk(field.Type)
	}
	for _, base := range t.Extends {
		walk(base)
	}
	for _, variant := range t.Variants {
		walk(variant)
	}
	var result []wireParameter
	for _, p := range s.parameters {
		if used[p.Name] {
			result = append(result, p)
		}
	}
	return result
}

func (s *Schema) singleFamilyParameter() (wireParameter, bool) {
	var result wireParameter
	count := 0
	for _, p := range s.parameters {
		if p.Of != "" {
			result = p
			count++
		}
	}
	return result, count == 1
}

// named looks up a declaration without expanding aliases. Its scope belongs
// to the owner, while supplied arguments keep their original lexical scope.
func (e expression) named(name, location string) (expression, *wireType, string, error) {
	if family, member, dotted := strings.Cut(name, "."); dotted {
		caller := e
		var schema *Schema
		if family[0] >= 'A' && family[0] <= 'Z' {
			schema = e.scope[family].family
			if schema == nil {
				return e, nil, "", expected(location, "a binding of the parameter "+family)
			}
		} else {
			schema = e.schema.imported[family]
			if schema == nil {
				return e, nil, "", expected(location, "known family")
			}
		}
		e = expressionContext(schema, member)
		if family[0] >= 'a' && family[0] <= 'z' {
			if source, ok := caller.schema.singleFamilyParameter(); ok {
				if target := schema.types[member]; target != nil {
					e.scope = map[string]argument{}
					for name, arg := range schema.scope {
						e.scope[name] = arg
					}
					for _, parameter := range append(schema.freeParameters(target, map[*wireType]bool{}), target.Parameters...) {
						if parameter.Of == "" {
							continue
						}
						if arg, ok := caller.scope[source.Name]; ok {
							e.scope[parameter.Name] = arg
						} else if caller.unboundFamily(source.Name) {
							e.scope[parameter.Name] = argument{unboundFamily: true}
						}
					}
				}
			}
		}
		name = member
	}
	t, ok := e.schema.types[name]
	if !ok || t == nil {
		return e, nil, "", expected(location, "known type")
	}
	e.value = name
	return e, t, name, nil
}

func (e expression) resolve(location string) (resolvedExpression, error) {
	// Alias/application cycles have no value constructor at which recursion
	// can make progress. Records may recurse: each child starts a new resolve.
	aliases := copyAliases(e.aliases)
	for {
		var definition *wireType
		name := ""
		switch v := e.value.(type) {
		case string:
			if arg, ok := e.scope[v]; ok {
				if arg.typeExpression == nil {
					return resolvedExpression{}, expected(location, "type argument")
				}
				e = *arg.typeExpression
				aliases = copyAliases(e.aliases)
				continue
			}
			if family, _, drawn := strings.Cut(v, "."); drawn && e.unboundFamily(family) {
				// A generated Go generic codec checks its supplied Go type at
				// marshal/unmarshal time. This schema checks the enclosing
				// shape; an explicitly supplied family never takes this path.
				return resolvedExpression{expression: e.child("json")}, nil
			}
			switch v {
			case "json", "string", "boolean", "number", "integer", "timestamp":
				return resolvedExpression{expression: e}, nil
			}
			var err error
			e, definition, name, err = e.named(v, location)
			if err != nil {
				return resolvedExpression{}, err
			}
		case map[string]any:
			if reference, ok := v["apply"].(string); ok {
				target, t, targetName, err := e.named(reference, location)
				if err != nil {
					return resolvedExpression{}, err
				}
				fillers, ok := v["with"].(map[string]any)
				if !ok {
					return resolvedExpression{}, expected(location, "application arguments")
				}
				scope := map[string]argument{}
				for parameter, arg := range target.scope {
					scope[parameter] = arg
				}
				parameters := append([]wireParameter{}, t.Parameters...)
				if strings.Contains(reference, ".") {
					parameters = append(target.schema.freeParameters(t, map[*wireType]bool{}), parameters...)
				}
				allowed := map[string]bool{}
				for _, parameter := range parameters {
					allowed[parameter.Name] = true
					filler, exists := fillers[parameter.Name]
					if !exists {
						return resolvedExpression{}, expected(location, "an argument for "+parameter.Name)
					}
					if parameter.Of == "" {
						captured := e.child(filler)
						// Restoring this ancestry when the argument is read
						// distinguishes Id<Id<T>> from A<T> = Id<A<T>>.
						captured.aliases = copyAliases(aliases)
						scope[parameter.Name] = argument{typeExpression: &captured}
					} else {
						family, ok := filler.(string)
						if !ok {
							return resolvedExpression{}, expected(location, "family argument for "+parameter.Name)
						}
						if e.unboundFamily(family) {
							scope[parameter.Name] = argument{unboundFamily: true}
							continue
						}
						schema := e.schema.imported[family]
						if arg, exists := e.scope[family]; exists {
							schema = arg.family
						}
						if schema == nil {
							return resolvedExpression{}, expected(location, "known family argument for "+parameter.Name)
						}
						scope[parameter.Name] = argument{family: schema}
					}
				}
				for _, parameter := range sortedKeys(fillers) {
					if !allowed[parameter] {
						return resolvedExpression{}, expected(location, "known parameter "+parameter)
					}
				}
				target.scope = scope
				e, definition, name = target, t, targetName
			} else if _, ok := v["kind"]; ok {
				data, err := json.Marshal(v)
				if err != nil {
					return resolvedExpression{}, err
				}
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.UseNumber()
				if err = decoder.Decode(&definition); err != nil {
					return resolvedExpression{}, err
				}
				name = definition.Kind
			} else {
				return resolvedExpression{expression: e}, nil
			}
		default:
			return resolvedExpression{}, expected(location, "type expression")
		}
		if definition.Kind == "alias" {
			if aliases[definition] {
				return resolvedExpression{}, expected(location, "acyclic type expression")
			}
			aliases[definition] = true
			e.value = definition.Type
			continue
		}
		return resolvedExpression{expression: e, definition: definition, name: name}, nil
	}
}

func copyAliases(source map[*wireType]bool) map[*wireType]bool {
	result := map[*wireType]bool{}
	for definition := range source {
		result[definition] = true
	}
	return result
}

func (e expression) unboundFamily(name string) bool {
	if arg, ok := e.scope[name]; ok {
		return arg.unboundFamily
	}
	for _, parameter := range e.schema.parameters {
		if parameter.Name == name && parameter.Of != "" {
			return true
		}
	}
	return false
}

type scopedField struct {
	field      wireField
	expression expression
}

func (r resolvedExpression) fields(location string, seen map[*wireType]bool) ([]scopedField, error) {
	if r.definition == nil {
		return nil, expected(location, "record")
	}
	if seen[r.definition] {
		return nil, expected(location, "acyclic inheritance")
	}
	seen[r.definition] = true
	defer delete(seen, r.definition)
	var fields []scopedField
	for _, base := range r.definition.Extends {
		parent, err := r.child(base).resolve(location)
		if err != nil {
			return nil, err
		}
		inherited, err := parent.fields(location, seen)
		if err != nil {
			return nil, err
		}
		fields = append(fields, inherited...)
	}
	for _, field := range r.definition.Fields {
		fields = append(fields, scopedField{field, r.child(field.Type)})
	}
	return fields, nil
}

func (r resolvedExpression) variants(location string, seen map[*wireType]bool) (map[string]expression, error) {
	if r.definition == nil || r.definition.Kind != "union" {
		return nil, expected(location, "union")
	}
	if seen[r.definition] {
		return nil, expected(location, "acyclic inheritance")
	}
	seen[r.definition] = true
	defer delete(seen, r.definition)
	variants := map[string]expression{}
	for _, base := range r.definition.Extends {
		parent, err := r.child(base).resolve(location)
		if err != nil {
			return nil, err
		}
		inherited, err := parent.variants(location, seen)
		if err != nil {
			return nil, err
		}
		for tag, expression := range inherited {
			variants[tag] = expression
		}
	}
	for tag, variant := range r.definition.Variants {
		variants[tag] = r.child(variant)
	}
	return variants, nil
}

func (e expression) nullable(location string) (bool, error) {
	r, err := e.resolve(location)
	if err != nil {
		return false, err
	}
	composite, ok := r.value.(map[string]any)
	if !ok {
		return false, nil
	}
	_, ok = composite["nullable"]
	return ok, nil
}

// carrier reports whether this payload uses the enclosing union object,
// and whether a record declares the discriminator itself.
func (e expression) carrier(tag string, wrapped any, location string) (bool, bool, error) {
	r, err := e.resolve(location)
	if err != nil {
		return false, false, err
	}
	if r.definition != nil {
		switch r.definition.Kind {
		case "record", "entity":
			fields, err := r.fields(location, map[*wireType]bool{})
			if err != nil {
				return false, false, err
			}
			for _, field := range fields {
				if field.field.Name == tag {
					return true, true, nil
				}
			}
			return true, false, nil
		case "union":
			return true, r.definition.Tag == tag, nil
		}
		return false, false, nil
	}
	if v, ok := r.value.(map[string]any); ok {
		if inner, nullable := v["nullable"]; nullable {
			if wrapped == nil {
				return false, false, nil
			}
			return r.child(inner).carrier(tag, wrapped, location)
		}
		if _, ok := v["map"]; ok {
			return true, false, nil
		}
		if _, ok := v["empty"]; ok {
			return true, false, nil
		}
	}
	name, _ := r.value.(string)
	return name == "json", false, nil
}

func (e expression) validate(value any, location string) error {
	r, err := e.resolve(location)
	if err != nil {
		return err
	}
	bad := func(want string) error { return expected(location, want) }
	if r.definition != nil {
		t := r.definition
		switch t.Kind {
		case "enum":
			for _, option := range t.Values {
				if value == option {
					return nil
				}
			}
			return bad(r.name)
		case "record", "entity":
			obj, ok := value.(map[string]any)
			if !ok {
				return bad(r.name + " object")
			}
			fields, err := r.fields(location, map[*wireType]bool{})
			if err != nil {
				return err
			}
			allowed := map[string]bool{}
			for _, scoped := range fields {
				field := scoped.field
				allowed[field.Name] = true
				at := location + "." + field.Name
				member, present := obj[field.Name]
				if !present {
					if field.Required {
						return fmt.Errorf("%s: required field missing", at)
					}
					continue
				}
				if member == nil {
					if field.Nullable {
						continue
					}
					nullable, err := scoped.expression.nullable(at)
					if err != nil {
						return err
					}
					if !nullable {
						return fmt.Errorf("%s: null is not permitted", at)
					}
				}
				if err := scoped.expression.validate(member, at); err != nil {
					return err
				}
				if member != nil {
					if err := constrain(field, member, at); err != nil {
						return err
					}
				}
			}
			for _, key := range sortedKeys(obj) {
				if allowed[key] {
					continue
				}
				if !t.Open {
					return fmt.Errorf("%s.%s: unknown field", location, key)
				}
				if err := validateJSON(obj[key], location+"."+key); err != nil {
					return err
				}
			}
			return nil
		case "union":
			obj, ok := value.(map[string]any)
			if !ok {
				return bad(r.name + " object")
			}
			tag, present := obj[t.Tag]
			if !present {
				return fmt.Errorf("%s.%s: required field missing", location, t.Tag)
			}
			tagName, ok := tag.(string)
			if !ok {
				return expected(location+"."+t.Tag, "known variant")
			}
			variants, err := r.variants(location, map[*wireType]bool{})
			if err != nil {
				return err
			}
			variant, known := variants[tagName]
			if !known {
				return expected(location+"."+t.Tag, "known variant")
			}
			member := t.Value
			if member == "" {
				member = "value"
			}
			wrapped, present := obj[member]
			// A missing value cannot be a wrapped null.
			probe := wrapped
			if !present || len(obj) != 2 {
				probe = struct{}{}
			}
			object, declaresTag, err := variant.carrier(t.Tag, probe, location)
			if err != nil {
				return err
			}
			if object {
				payload := map[string]any{}
				for key, value := range obj {
					if key != t.Tag || declaresTag {
						payload[key] = value
					}
				}
				return variant.validate(payload, location)
			}
			if !present {
				return fmt.Errorf("%s.%s: required field missing", location, member)
			}
			if err := variant.validate(wrapped, location+"."+member); err != nil {
				return err
			}
			for _, key := range sortedKeys(obj) {
				if key != t.Tag && key != member {
					return fmt.Errorf("%s.%s: unknown field", location, key)
				}
			}
			return nil
		}
		return bad("supported type")
	}
	if composite, ok := r.value.(map[string]any); ok {
		if inner, ok := composite["nullable"]; ok {
			if value == nil {
				return nil
			}
			return r.child(inner).validate(value, location)
		}
		if literal, ok := composite["literal"]; ok {
			printable := literal
			if number, ok := literal.(json.Number); ok {
				printable, _ = number.Float64()
			}
			want, _ := json.Marshal(printable)
			if sameLiteral(literal, value) {
				return nil
			}
			return bad("literal " + string(want))
		}
		if element, ok := composite["array"]; ok {
			items, ok := value.([]any)
			if !ok {
				return bad("array")
			}
			for i, item := range items {
				if err := r.child(element).validate(item, fmt.Sprintf("%s[%d]", location, i)); err != nil {
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
			for _, key := range sortedKeys(items) {
				if err := r.child(element).validate(items[key], location+"."+key); err != nil {
					return err
				}
			}
			return nil
		}
		if entity, ok := composite["ref"].(string); ok {
			target, t, _, err := r.named(entity, location)
			if err != nil {
				return bad("known entity")
			}
			resolved, err := target.resolve(location)
			if err != nil {
				return err
			}
			fields, err := resolved.fields(location, map[*wireType]bool{})
			if err != nil {
				return err
			}
			for _, field := range fields {
				if field.field.Name == t.Key {
					return field.expression.validate(value, location)
				}
			}
			return bad("entity with a key")
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
	name := r.value.(string)
	switch name {
	case "json":
		return validateJSON(value, location)
	case "string":
		if _, ok := value.(string); !ok {
			return bad("string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad("boolean")
		}
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
	case "timestamp":
		text, ok := value.(string)
		if !ok {
			return bad("timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
			return bad("RFC3339 timestamp")
		}
	default:
		return bad("known type")
	}
	return nil
}

func sameLiteral(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		xn, xerr := x.Float64()
		yn, yerr := y.Float64()
		return xerr == nil && yerr == nil && xn == yn
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	}
	return false
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	// Match JavaScript's string ordering, including keys outside the BMP.
	slices.SortFunc(keys, func(a, b string) int {
		return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
	})
	return keys
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
		for _, key := range sortedKeys(typed) {
			if err := validateJSON(typed[key], location+"."+key); err != nil {
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
