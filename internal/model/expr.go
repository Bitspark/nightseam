package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TypeExpr is a type expression: one of the eight forms a declaration names
// a type in. It is sealed — only this package's types implement it — so
// that every consumer is one type switch over the variants and never a
// dispatch on the JSON shape. Absent is nil.
//
// On the wire a type expression is a string or an object with one form:
//
//	"string"                      a primitive
//	"Project"                     a type of this family
//	"identity.User"               a type of an imported family
//	"S.Payload"                   a type drawn from a parameter (upper camel)
//	{"array": T}  {"map": T}      a list, a string-keyed map
//	{"ref": "Project"}            a reference to an entity by its key
//	{"apply": "carrier.Frame", "with": {"S": "codex"}}
//	                              a generic type of an imported family, filled
type TypeExpr interface {
	json.Marshaler
	typeExpr()
}

// Primitive is one of the wire primitives.
type Primitive string

// Named is a type of this family.
type Named struct{ Name string }

// Imported is a type of an imported family, family.Type.
type Imported struct{ Family, Name string }

// Drawn is a type drawn from a parameter of this family, Param.Type: the
// bound family's Envelope, Handle, or any record or enum of it.
type Drawn struct{ Parameter, Name string }

// Array is an ordered list of Elem.
type Array struct{ Elem TypeExpr }

// Map is a string-keyed map of Elem.
type Map struct{ Elem TypeExpr }

// Ref is a reference to an entity of this family by its key.
type Ref struct{ Entity string }

// Apply is a generic type of an imported family with its parameters filled:
// With maps the imported type's parameters to what fills each.
type Apply struct {
	Family, Name string
	With         map[string]Filler
}

// Filler fills a parameter of an applied type: a parameter of this family
// or a named family; exactly one is set.
type Filler struct{ Parameter, Family string }

func (Primitive) typeExpr() {}
func (Named) typeExpr()     {}
func (Imported) typeExpr()  {}
func (Drawn) typeExpr()     {}
func (Array) typeExpr()     {}
func (Map) typeExpr()       {}
func (Ref) typeExpr()       {}
func (Apply) typeExpr()     {}

// Primitives are the wire primitives, in name order.
var Primitives = []string{"boolean", "integer", "json", "number", "string", "timestamp"}

// IsPrimitive reports whether a name is a wire primitive.
func IsPrimitive(name string) bool {
	i := sort.SearchStrings(Primitives, name)
	return i < len(Primitives) && Primitives[i] == name
}

// IsParameter reports whether a qualifier names a parameter rather than a
// family: a parameter is upper camel case, a family lower kebab case.
func IsParameter(qualifier string) bool {
	return qualifier != "" && qualifier[0] >= 'A' && qualifier[0] <= 'Z'
}

// FillerOf reads what fills a parameter: a parameter of this family when
// spelled in upper camel case, a named family otherwise.
func FillerOf(name string) Filler {
	if IsParameter(name) {
		return Filler{Parameter: name}
	}
	return Filler{Family: name}
}

// String is the filler's spelling: the parameter or the family.
func (f Filler) String() string {
	if f.Parameter != "" {
		return f.Parameter
	}
	return f.Family
}

// Decode parses the JSON of one type expression. A string is a primitive, a
// type of this family, or a qualified name whose qualifier is a parameter
// (upper camel) or a family (lower); an object holds exactly one form.
func Decode(raw json.RawMessage) (TypeExpr, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("a type expression is required")
	}
	if trimmed[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, err
		}
		return decodeName(name)
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("a type expression is a string or an object, not %s", trimmed)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	switch strings.Join(keys, ",") {
	case "array":
		elem, err := Decode(object["array"])
		if err != nil {
			return nil, fmt.Errorf("array: %w", err)
		}
		return Array{Elem: elem}, nil
	case "map":
		elem, err := Decode(object["map"])
		if err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		return Map{Elem: elem}, nil
	case "ref":
		var entity string
		if err := json.Unmarshal(object["ref"], &entity); err != nil {
			return nil, fmt.Errorf("ref: %w", err)
		}
		if entity == "" || strings.Contains(entity, ".") {
			return nil, fmt.Errorf("ref names an entity of this family, not %q", entity)
		}
		return Ref{Entity: entity}, nil
	case "apply,with":
		var reference string
		if err := json.Unmarshal(object["apply"], &reference); err != nil {
			return nil, fmt.Errorf("apply: %w", err)
		}
		family, name, ok := strings.Cut(reference, ".")
		if !ok || family == "" || name == "" || IsParameter(family) {
			return nil, fmt.Errorf("apply names a generic type of an imported family, family.Type, not %q", reference)
		}
		var with map[string]string
		if err := json.Unmarshal(object["with"], &with); err != nil {
			return nil, fmt.Errorf("with: %w", err)
		}
		fillers := make(map[string]Filler, len(with))
		for parameter, filler := range with {
			if filler == "" {
				return nil, fmt.Errorf("with: %s is filled by nothing", parameter)
			}
			fillers[parameter] = FillerOf(filler)
		}
		return Apply{Family: family, Name: name, With: fillers}, nil
	case "apply":
		return nil, fmt.Errorf("apply needs with: what fills the applied type's parameters")
	}
	return nil, fmt.Errorf("a type expression object holds one of array, map, ref, or apply and with, not %s", strings.Join(keys, ", "))
}

func decodeName(name string) (TypeExpr, error) {
	if name == "" {
		return nil, fmt.Errorf("a type name is required")
	}
	if IsPrimitive(name) {
		return Primitive(name), nil
	}
	qualifier, typeName, qualified := strings.Cut(name, ".")
	if !qualified {
		return Named{Name: name}, nil
	}
	if qualifier == "" || typeName == "" || strings.Contains(typeName, ".") {
		return nil, fmt.Errorf("a qualified type is family.Type or Param.Type, not %q", name)
	}
	if IsParameter(qualifier) {
		return Drawn{Parameter: qualifier, Name: typeName}, nil
	}
	return Imported{Family: qualifier, Name: typeName}, nil
}

// MustDecode decodes a type expression a test writes by hand.
func MustDecode(source string) TypeExpr {
	expr, err := Decode(json.RawMessage(source))
	if err != nil {
		panic(err)
	}
	return expr
}

func (p Primitive) MarshalJSON() ([]byte, error) { return json.Marshal(string(p)) }
func (n Named) MarshalJSON() ([]byte, error)     { return json.Marshal(n.Name) }
func (i Imported) MarshalJSON() ([]byte, error)  { return json.Marshal(i.Family + "." + i.Name) }
func (d Drawn) MarshalJSON() ([]byte, error)     { return json.Marshal(d.Parameter + "." + d.Name) }
func (a Array) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]TypeExpr{"array": a.Elem})
}
func (m Map) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]TypeExpr{"map": m.Elem}) }
func (r Ref) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]string{"ref": r.Entity}) }
func (a Apply) MarshalJSON() ([]byte, error) {
	with := make(map[string]string, len(a.With))
	for parameter, filler := range a.With {
		with[parameter] = filler.String()
	}
	return json.Marshal(struct {
		Apply string            `json:"apply"`
		With  map[string]string `json:"with"`
	}{a.Family + "." + a.Name, with})
}

// String spells an expression as it is written in a contract.
func String(e TypeExpr) string {
	if e == nil {
		return ""
	}
	data, _ := json.Marshal(e)
	return string(data)
}

// Walk visits e and every expression beneath it, parents first; visit
// returns false to stop descending below an expression.
func Walk(e TypeExpr, visit func(TypeExpr) bool) {
	if e == nil || !visit(e) {
		return
	}
	switch x := e.(type) {
	case Array:
		Walk(x.Elem, visit)
	case Map:
		Walk(x.Elem, visit)
	}
}

// Rewrite rebuilds e from the leaves up: each expression, its children
// already rewritten, is replaced by what f returns for it.
func Rewrite(e TypeExpr, f func(TypeExpr) TypeExpr) TypeExpr {
	if e == nil {
		return nil
	}
	switch x := e.(type) {
	case Array:
		return f(Array{Elem: Rewrite(x.Elem, f)})
	case Map:
		return f(Map{Elem: Rewrite(x.Elem, f)})
	}
	return f(e)
}

// Equal reports whether two expressions are the same expression.
func Equal(a, b TypeExpr) bool { return String(a) == String(b) }
