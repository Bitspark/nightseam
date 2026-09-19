package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
)

// TypeExpr is a type expression: one of the forms a declaration names a
// type in. It is sealed — only this package's types implement it — so that
// every consumer is one type switch over the variants and never a dispatch
// on the JSON shape. Absent is nil.
//
// On the wire a type expression is a string or an object with one form:
//
//	"string"                      a primitive
//	"Project"                     a type of this family, or a parameter in scope
//	"identity.User"               a type of an imported family
//	"S.Payload"                   a type drawn through a family parameter
//	{"array": T}  {"map": T}      a list, a string-keyed map
//	{"nullable": T}               a value that may be null
//	{"literal": "text"}           the one string value
//	{"ref": "Project"}            a reference to an entity by its key
//	{"apply": "Page", "with": {…}}          a generic type of this family, filled
//	{"apply": "carrier.Frame", "with": {…}} a generic type of an imported family
//	{"kind": "record", "fields": […]}       a shape written where a type is named
type TypeExpr interface {
	json.Marshaler
	typeExpr()
}

// Primitive is one of the wire primitives.
type Primitive string

// Named is a name in scope: a parameter of the declaration or of the
// family, or a type of this family. Which it is, resolution says.
type Named struct{ Name string }

// Imported is a type of an imported family, family.Type.
type Imported struct{ Family, Name string }

// Drawn is a type drawn through a family parameter, Param.Type: the bound
// family's Envelope, Handle, or any record or enum of it. A draw on a type
// parameter is refused: a type has no types of its own to draw.
type Drawn struct{ Parameter, Name string }

// Array is an ordered list of Elem.
type Array struct{ Elem TypeExpr }

// Map is a string-keyed map of Elem.
type Map struct{ Elem TypeExpr }

// Nullable is Elem or null. Presence is a fact of a member and stays
// there; nullness is a fact of a value and is written here, so that an
// array or a map of values that may be null can be declared.
type Nullable struct{ Elem TypeExpr }

// Literal is the type of one string value, retained inside a union payload
// like every other member of that payload.
type Literal struct{ Value string }

// Ref is a reference to an entity of this family by its key.
type Ref struct {
	Entity string
	// Family is the resolved lexical owner in rendering facts. Declarations
	// and the wire spelling remain local {"ref": "Entity"} expressions.
	Family string
}

// Apply fills the parameters of a generic type: Family is empty for a type
// of this family, and With maps each of that type's parameters to what
// fills it.
type Apply struct {
	Family, Name string
	With         map[string]Filler
}

// Inline is a record, entity, enum or union written where a type is named.
// It declares no name of its own; the generator derives one from the path
// to it, which naming.Derived spells.
type Inline struct{ Type *Type }

// Filler fills one parameter of an applied type: a type expression fills a
// type parameter, a named family fills a family parameter. Exactly one is
// set.
type Filler struct {
	Type   TypeExpr
	Family string
}

func (Primitive) typeExpr() {}
func (Named) typeExpr()     {}
func (Imported) typeExpr()  {}
func (Drawn) typeExpr()     {}
func (Array) typeExpr()     {}
func (Map) typeExpr()       {}
func (Nullable) typeExpr()  {}
func (Literal) typeExpr()   {}
func (Ref) typeExpr()       {}
func (Apply) typeExpr()     {}
func (Inline) typeExpr()    {}

// primitives are the wire primitives, in name order.
var primitives = []string{"boolean", "integer", "json", "number", "string", "timestamp"}

// isPrimitive reports whether a name is a wire primitive.
func isPrimitive(name string) bool {
	i := sort.SearchStrings(primitives, name)
	return i < len(primitives) && primitives[i] == name
}

// IsParameter reports whether a qualifier names a parameter rather than a
// family: a parameter is upper camel case, a family lower kebab case.
func IsParameter(qualifier string) bool {
	return qualifier != "" && qualifier[0] >= 'A' && qualifier[0] <= 'Z'
}

// IsFamilyName reports whether a name is spelled as a family is: lower
// kebab case, and not one of the primitives.
func IsFamilyName(name string) bool {
	if name == "" || isPrimitive(name) || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(name)-1:
		default:
			return false
		}
	}
	return true
}

// Name is the plain name a filler spells, empty when it spells anything
// else: what resolution reads to say whether a parameter or a type fills
// the slot.
func (f Filler) Name() string {
	if named, ok := f.Type.(Named); ok {
		return named.Name
	}
	return ""
}

// String is the filler's spelling: the family, or the type expression as it
// is written — a name unquoted, an object as its JSON.
func (f Filler) String() string {
	if f.Family != "" {
		return f.Family
	}
	spelled := String(f.Type)
	if len(spelled) > 1 && spelled[0] == '"' {
		var name string
		if json.Unmarshal([]byte(spelled), &name) == nil {
			return name
		}
	}
	return spelled
}

// MarshalJSON writes a filler as it is written in a contract.
func (f Filler) MarshalJSON() ([]byte, error) {
	if f.Family != "" {
		return json.Marshal(f.Family)
	}
	return json.Marshal(f.Type)
}

// Decode parses the JSON of one type expression, unlocated.
func Decode(raw json.RawMessage) (TypeExpr, error) { return DecodeAt(raw, diag.Location{}) }

// DecodeAt parses the JSON of one type expression, stamping an inline
// shape and everything under it with the location the expression sits at.
// A string is a primitive, a name in scope, or a qualified name whose
// qualifier is a parameter (upper camel) or a family (lower); an object
// holds exactly one form.
func DecodeAt(raw json.RawMessage, at diag.Location) (TypeExpr, error) {
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
	if _, inline := object["kind"]; inline {
		t, err := decodeType("", raw, at)
		if err != nil {
			return nil, err
		}
		return Inline{Type: t}, nil
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	switch strings.Join(keys, ",") {
	case "array":
		elem, err := DecodeAt(object["array"], at.Sub("array"))
		if err != nil {
			return nil, fmt.Errorf("array: %w", err)
		}
		return Array{Elem: elem}, nil
	case "map":
		elem, err := DecodeAt(object["map"], at.Sub("map"))
		if err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		return Map{Elem: elem}, nil
	case "nullable":
		elem, err := DecodeAt(object["nullable"], at.Sub("nullable"))
		if err != nil {
			return nil, fmt.Errorf("nullable: %w", err)
		}
		if _, twice := elem.(Nullable); twice {
			return nil, fmt.Errorf("nullable: a value is nullable once")
		}
		return Nullable{Elem: elem}, nil
	case "literal":
		var value string
		if err := json.Unmarshal(object["literal"], &value); err != nil {
			return nil, fmt.Errorf("literal: a literal is one string value: %w", err)
		}
		if value == "" {
			return nil, fmt.Errorf("literal: a literal is one string value, not the empty one")
		}
		return Literal{Value: value}, nil
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
		return decodeApply(object, at)
	case "apply":
		return nil, fmt.Errorf("apply needs with: what fills the applied type's parameters")
	}
	return nil, fmt.Errorf("a type expression object holds one of array, map, nullable, literal, ref, apply and with, or a kind, not %s", strings.Join(keys, ", "))
}

func decodeApply(object map[string]json.RawMessage, at diag.Location) (TypeExpr, error) {
	var reference string
	if err := json.Unmarshal(object["apply"], &reference); err != nil {
		return nil, fmt.Errorf("apply: %w", err)
	}
	family, name, qualified := strings.Cut(reference, ".")
	if !qualified {
		family, name = "", reference
	}
	if name == "" || strings.Contains(name, ".") || IsParameter(family) || (qualified && family == "") {
		return nil, fmt.Errorf("apply names a generic type of this family or of an imported one, Type or family.Type, not %q", reference)
	}
	if !IsParameter(name) {
		return nil, fmt.Errorf("apply names a type, which is upper camel case, not %q", reference)
	}
	var with map[string]json.RawMessage
	if err := json.Unmarshal(object["with"], &with); err != nil {
		return nil, fmt.Errorf("with: %w", err)
	}
	fillers := make(map[string]Filler, len(with))
	for parameter, raw := range with {
		filler, err := decodeFiller(raw, at.Sub("with", parameter))
		if err != nil {
			return nil, fmt.Errorf("with: %s: %w", parameter, err)
		}
		fillers[parameter] = filler
	}
	return Apply{Family: family, Name: name, With: fillers}, nil
}

func decodeFiller(raw json.RawMessage, at diag.Location) (Filler, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == `""` {
		return Filler{}, fmt.Errorf("filled by nothing")
	}
	if trimmed != "" && trimmed[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return Filler{}, err
		}
		if IsFamilyName(name) {
			return Filler{Family: name}, nil
		}
	}
	e, err := DecodeAt(raw, at)
	if err != nil {
		return Filler{}, err
	}
	return Filler{Type: e}, nil
}

func decodeName(name string) (TypeExpr, error) {
	if name == "" {
		return nil, fmt.Errorf("a type name is required")
	}
	if isPrimitive(name) {
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

func (p Primitive) MarshalJSON() ([]byte, error) { return json.Marshal(string(p)) }
func (n Named) MarshalJSON() ([]byte, error)     { return json.Marshal(n.Name) }
func (i Imported) MarshalJSON() ([]byte, error)  { return json.Marshal(i.Family + "." + i.Name) }
func (d Drawn) MarshalJSON() ([]byte, error)     { return json.Marshal(d.Parameter + "." + d.Name) }
func (a Array) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]TypeExpr{"array": a.Elem})
}
func (m Map) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]TypeExpr{"map": m.Elem}) }
func (n Nullable) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]TypeExpr{"nullable": n.Elem})
}
func (l Literal) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"literal": l.Value})
}
func (r Ref) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]string{"ref": r.Entity}) }
func (i Inline) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.Type)
}
func (a Apply) MarshalJSON() ([]byte, error) {
	reference := a.Name
	if a.Family != "" {
		reference = a.Family + "." + a.Name
	}
	return json.Marshal(struct {
		Apply string            `json:"apply"`
		With  map[string]Filler `json:"with"`
	}{reference, a.With})
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
// returns false to stop descending below an expression. An inline shape's
// own expressions are beneath it.
func Walk(e TypeExpr, visit func(TypeExpr) bool) {
	if e == nil || !visit(e) {
		return
	}
	switch x := e.(type) {
	case Array:
		Walk(x.Elem, visit)
	case Map:
		Walk(x.Elem, visit)
	case Nullable:
		Walk(x.Elem, visit)
	case Apply:
		for _, parameter := range sortedFillers(x.With) {
			if filler := x.With[parameter]; filler.Type != nil {
				Walk(filler.Type, visit)
			}
		}
	case Inline:
		x.Type.WalkExpressions(func(inner TypeExpr, _ diag.Location) { Walk(inner, visit) })
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
	case Nullable:
		return f(Nullable{Elem: Rewrite(x.Elem, f)})
	case Apply:
		with := make(map[string]Filler, len(x.With))
		for parameter, filler := range x.With {
			if filler.Type != nil {
				filler.Type = Rewrite(filler.Type, f)
			}
			with[parameter] = filler
		}
		return f(Apply{Family: x.Family, Name: x.Name, With: with})
	case Inline:
		return f(Inline{Type: x.Type.rewritten(f)})
	}
	return f(e)
}

// Equal reports whether two expressions are the same expression.
func Equal(a, b TypeExpr) bool { return String(a) == String(b) }

func sortedFillers(with map[string]Filler) []string {
	keys := make([]string, 0, len(with))
	for key := range with {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
