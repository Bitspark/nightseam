// Package model is the typed declaration of one family across its tiers:
// the types of its model, the sides, operations and errors of its protocol,
// the governance of its session, and the raw override files of its targets.
// It knows nothing of the world a family is rendered in, of resolution, or
// of any target: it is what the loader decodes and what analysis reads.
//
// Every declaration carries the location it was decoded from, so that a
// diagnostic about it names the tier file and the JSON pointer.
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Bitspark/nightseam/internal/diag"
)

// Version is the declaration language this package reads, the value of
// the "nightseam" key of model.json.
const Version = 2

// Family is one family as declared: its name from the directory, and each
// tier's declarations where the tier file exists.
type Family struct {
	Name      string
	Source    string                     // directory in the checkout, or nightseam:<name> for a built-in
	Imports   []string                   // the union of every tier's imports, sorted
	Types     map[string]*Type           // every tier's types by name; At.File says which tier declared each
	Protocol  *Protocol                  // nil without protocol.json
	Session   *Session                   // nil without session.json
	Overrides map[string]json.RawMessage // a target's override file, raw, by target name
	Files     []string                   // the tier files present, lowest tier first
}

// Has reports whether a tier file is present.
func (f *Family) Has(file string) bool {
	for _, present := range f.Files {
		if present == file {
			return true
		}
	}
	return false
}

// TypeNames is the declared type names in byte order: the order every
// rendering lists them in.
func (f *Family) TypeNames() []string {
	names := make([]string, 0, len(f.Types))
	for name := range f.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// The kinds of type.
const (
	KindEntity = "entity"
	KindRecord = "record"
	KindEnum   = "enum"
	KindAlias  = "alias"
	KindUnion  = "union"
)

// DefaultValueMember is the member a union carries its complete payload
// under, beside its tag; a union may name another.
const DefaultValueMember = "value"

// Type is one declared type.
type Type struct {
	Name        string
	Kind        string
	Description string
	Key         string        // entity: the field that identifies it
	Parameters  []Parameter   // record, entity, union, alias: the holes in it
	Extends     []Inheritance // bases with their explicit parameter bindings
	Open        bool          // record, entity: fields beyond the declared ones are kept
	Fields      []Field       // record, entity: in wire order, own fields only
	Values      []string      // enum
	Alias       TypeExpr      // alias
	Tag         string        // union: the member that discriminates
	Value       string        // union: the member the complete payload rides under
	Variants    []Variant     // union: own variants, by tag
	At          diag.Location
}

// Variant is one arm of a union: the value of the discriminator that names
// it and what it carries.
type Variant struct {
	Tag  string
	Type TypeExpr // nil is a no-payload arm, spelled {"empty":true}
	At   diag.Location
}

// Variant finds an own variant by its tag.
func (t *Type) Variant(tag string) (*Variant, bool) {
	for i := range t.Variants {
		if t.Variants[i].Tag == tag {
			return &t.Variants[i], true
		}
	}
	return nil, false
}

// ValueMember is the member a union carries its complete payload under.
func (t *Type) ValueMember() string {
	if t.Value != "" {
		return t.Value
	}
	return DefaultValueMember
}

// Parameter finds a parameter of the type by name.
func (t *Type) Parameter(name string) (*Parameter, bool) {
	for i := range t.Parameters {
		if t.Parameters[i].Name == name {
			return &t.Parameters[i], true
		}
	}
	return nil, false
}

// Field is one field of a record or entity.
type Field struct {
	Name        string
	Description string
	Type        TypeExpr
	Required    bool // present; default true
	Nullable    bool // may be null
	Unique      bool // no two instances of the entity share the value
	Min, Max    *json.Number
	Length      *Length
	Pattern     string
	At          diag.Location
}

// Length bounds a string's or array's length.
type Length struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// Protocol is the protocol tier: the profile the family speaks, the
// parameters it is generic in, its two sides and its public errors.
type Protocol struct {
	Profile    string
	Parameters []Parameter
	Server     Side
	Client     Side
	Errors     []Error // by code
	At         diag.Location
}

// Parameter is a hole in a declaration — a family, a record, a union, an
// alias — of one of two sorts, which Of names: a type parameter, filled by
// a type expression and written where a type is named; or a family
// parameter, Of the tier a bound family must carry, filled by a family and
// drawn through, P.Type. One mechanism, two sorts: a family is not a type,
// so a family parameter is not a type parameter with a bound.
type Parameter struct {
	Name        string
	Of          string // empty: a type parameter; otherwise the tier a bound family carries
	Description string
	At          diag.Location
}

// IsFamily reports whether the parameter is filled by a family rather than
// by a type.
func (p Parameter) IsFamily() bool { return p.Of != "" }

// Side is one peer's interface: the methods it implements and the events
// it emits. The server side is implemented by the server and called by the
// client; the client side is the reverse. A side may extend the same side
// of another family, taking its operations under their own names.
type Side struct {
	Extends []Inheritance       // the families whose same side this one extends, with bindings
	Methods []Method            // by name, own only
	Events  []Event             // by name, own only
	CRUD    map[string][]string // entity → operations; parsed, expanded by nothing yet
	At      diag.Location
}

// Method is one operation a side implements.
type Method struct {
	Name        string
	Description string
	Request     TypeExpr // nil: the method takes nothing
	Result      TypeExpr
	Errors      []string // codes of the family's errors it may return
	At          diag.Location
}

// Event is one notification a side emits.
type Event struct {
	Name        string
	Description string
	Type        TypeExpr
	At          diag.Location
}

// Error is one public error of the family.
type Error struct {
	Code        string
	Description string
	At          diag.Location
}

// Session is the session tier: how a connection of the protocol is
// governed. Extensions is carried for other tools and not read here.
type Session struct {
	Decides      []string
	Asks         []string
	Conversation *Conversation
	Extensions   json.RawMessage
	At           diag.Location
}

// Conversation is where the conversation id arrives: an event and the path
// to the id in its data.
type Conversation struct {
	Event string `json:"event"`
	Path  string `json:"path"`
}

// Overrides is a target's override file as decoded: names by path key.
type Overrides struct {
	Names map[string]string `json:"names"`
}

// The wire form of the declarations, decoded by the functions below. Type
// expressions arrive as raw JSON and are decoded by Decode.

type typeJSON struct {
	Kind        string                     `json:"kind"`
	Description string                     `json:"description"`
	Key         string                     `json:"key"`
	Parameters  []parameterJSON            `json:"parameters"`
	Extends     []json.RawMessage          `json:"extends"`
	Open        bool                       `json:"open"`
	Fields      []fieldJSON                `json:"fields"`
	Values      []string                   `json:"values"`
	Type        json.RawMessage            `json:"type"`
	Tag         string                     `json:"tag"`
	Value       string                     `json:"value"`
	Variants    map[string]json.RawMessage `json:"variants"`
}

type fieldJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        json.RawMessage `json:"type"`
	Required    *bool           `json:"required"`
	Nullable    bool            `json:"nullable"`
	Unique      bool            `json:"unique"`
	Min         *json.Number    `json:"min"`
	Max         *json.Number    `json:"max"`
	Length      *Length         `json:"length"`
	Pattern     string          `json:"pattern"`
}

type protocolJSON struct {
	Profile    string            `json:"profile"`
	Parameters []parameterJSON   `json:"parameters"`
	Server     sideJSON          `json:"server"`
	Client     sideJSON          `json:"client"`
	Errors     map[string]string `json:"errors"`
}

type parameterJSON struct {
	Name        string `json:"name"`
	Of          string `json:"of"`
	Description string `json:"description"`
}

type sideJSON struct {
	Extends []json.RawMessage     `json:"extends"`
	Methods map[string]methodJSON `json:"methods"`
	Events  map[string]eventJSON  `json:"events"`
	CRUD    map[string][]string   `json:"crud"`
}

type methodJSON struct {
	Description string          `json:"description"`
	Request     json.RawMessage `json:"request"`
	Result      json.RawMessage `json:"result"`
	Errors      []string        `json:"errors"`
}

type eventJSON struct {
	Description string          `json:"description"`
	Type        json.RawMessage `json:"type"`
}

type sessionJSON struct {
	Decides      []string        `json:"decides"`
	Asks         []string        `json:"asks"`
	Conversation *Conversation   `json:"conversation"`
	Extensions   json.RawMessage `json:"extensions"`
}

// DecodeTypes decodes a tier file's types section, at /types of the file.
func DecodeTypes(file string, raw json.RawMessage) (map[string]*Type, error) {
	var wire map[string]typeJSON
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("%s#/types: %w", file, err)
	}
	types := make(map[string]*Type, len(wire))
	for name, w := range wire {
		at := diag.Location{File: file, Pointer: "/types/" + diag.Escape(name)}
		t, err := buildType(name, w, at)
		if err != nil {
			return nil, err
		}
		types[name] = t
	}
	return types, nil
}

// decodeType decodes one type's body from its JSON: what DecodeTypes does
// for a named type and what an inline shape in a type expression is.
func decodeType(name string, raw json.RawMessage, at diag.Location) (*Type, error) {
	var w typeJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&w); err != nil {
		return nil, err
	}
	return buildType(name, w, at)
}

func buildType(name string, w typeJSON, at diag.Location) (*Type, error) {
	t := &Type{Name: name, Kind: w.Kind, Description: w.Description, Key: w.Key, Open: w.Open, Values: w.Values, Tag: w.Tag, Value: w.Value, At: at}
	var err error
	if t.Extends, err = decodeBases(w.Extends, at); err != nil {
		return nil, err
	}
	for i, p := range w.Parameters {
		t.Parameters = append(t.Parameters, Parameter{Name: p.Name, Of: p.Of, Description: p.Description, At: at.Sub("parameters", i)})
	}
	for i, f := range w.Fields {
		field, err := decodeField(f, at.Sub("fields", i))
		if err != nil {
			return nil, err
		}
		t.Fields = append(t.Fields, field)
	}
	if w.Type != nil {
		alias, err := DecodeAt(w.Type, at.Sub("type"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", at.Sub("type"), err)
		}
		t.Alias = alias
	}
	for _, tag := range sortedRaw(w.Variants) {
		here := at.Sub("variants", tag)
		var marker map[string]json.RawMessage
		if json.Unmarshal(w.Variants[tag], &marker) == nil && len(marker) == 1 && string(bytes.TrimSpace(marker["empty"])) == "true" {
			t.Variants = append(t.Variants, Variant{Tag: tag, At: here})
			continue
		}
		e, err := DecodeAt(w.Variants[tag], here)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", here, err)
		}
		t.Variants = append(t.Variants, Variant{Tag: tag, Type: e, At: here})
	}
	return t, nil
}

func sortedRaw(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func decodeField(w fieldJSON, at diag.Location) (Field, error) {
	expr, err := DecodeAt(w.Type, at.Sub("type"))
	if err != nil {
		return Field{}, fmt.Errorf("%s: %w", at.Sub("type"), err)
	}
	return Field{
		Name: w.Name, Description: w.Description, Type: expr,
		Required: w.Required == nil || *w.Required, Nullable: w.Nullable, Unique: w.Unique,
		Min: w.Min, Max: w.Max, Length: w.Length, Pattern: w.Pattern,
		At: at,
	}, nil
}

// DecodeProtocol decodes the protocol tier's own sections from the file's
// object; the types and imports every tier carries are decoded apart.
func DecodeProtocol(file string, raw json.RawMessage) (*Protocol, error) {
	var w protocolJSON
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	root := diag.Location{File: file}
	p := &Protocol{Profile: w.Profile, At: root}
	for i, parameter := range w.Parameters {
		p.Parameters = append(p.Parameters, Parameter{Name: parameter.Name, Of: parameter.Of, Description: parameter.Description, At: root.Sub("parameters", i)})
	}
	var err error
	if p.Server, err = decodeSide(w.Server, root.Sub("server")); err != nil {
		return nil, err
	}
	if p.Client, err = decodeSide(w.Client, root.Sub("client")); err != nil {
		return nil, err
	}
	for code, description := range w.Errors {
		p.Errors = append(p.Errors, Error{Code: code, Description: description, At: root.Sub("errors", code)})
	}
	sort.Slice(p.Errors, func(i, j int) bool { return p.Errors[i].Code < p.Errors[j].Code })
	return p, nil
}

func decodeSide(w sideJSON, at diag.Location) (Side, error) {
	side := Side{CRUD: w.CRUD, At: at}
	var err error
	if side.Extends, err = decodeBases(w.Extends, at); err != nil {
		return side, err
	}
	for name, m := range w.Methods {
		method := Method{Name: name, Description: m.Description, Errors: m.Errors, At: at.Sub("methods", name)}
		var err error
		if m.Request != nil {
			if method.Request, err = DecodeAt(m.Request, method.At.Sub("request")); err != nil {
				return side, fmt.Errorf("%s: %w", method.At.Sub("request"), err)
			}
		}
		if method.Result, err = DecodeAt(m.Result, method.At.Sub("result")); err != nil {
			return side, fmt.Errorf("%s: %w", method.At.Sub("result"), err)
		}
		side.Methods = append(side.Methods, method)
	}
	sort.Slice(side.Methods, func(i, j int) bool { return side.Methods[i].Name < side.Methods[j].Name })
	for name, e := range w.Events {
		event := Event{Name: name, Description: e.Description, At: at.Sub("events", name)}
		var err error
		if event.Type, err = DecodeAt(e.Type, event.At.Sub("type")); err != nil {
			return side, fmt.Errorf("%s: %w", event.At.Sub("type"), err)
		}
		side.Events = append(side.Events, event)
	}
	sort.Slice(side.Events, func(i, j int) bool { return side.Events[i].Name < side.Events[j].Name })
	return side, nil
}

// DecodeSession decodes the session tier's own sections.
func DecodeSession(file string, raw json.RawMessage) (*Session, error) {
	var w sessionJSON
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &Session{Decides: w.Decides, Asks: w.Asks, Conversation: w.Conversation, Extensions: w.Extensions, At: diag.Location{File: file}}, nil
}

// DecodeOverrides decodes a target's override file; a key it does not know
// is refused, since an override file may only override.
func DecodeOverrides(file string, raw json.RawMessage) (Overrides, error) {
	var o Overrides
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&o); err != nil {
		return Overrides{}, fmt.Errorf("%s: %w", file, err)
	}
	return o, nil
}

// Method finds a method by name on either side; ok is false when neither
// side declares it.
func (p *Protocol) Method(name string) (m *Method, server bool, ok bool) {
	for i := range p.Server.Methods {
		if p.Server.Methods[i].Name == name {
			return &p.Server.Methods[i], true, true
		}
	}
	for i := range p.Client.Methods {
		if p.Client.Methods[i].Name == name {
			return &p.Client.Methods[i], false, true
		}
	}
	return nil, false, false
}

// Event finds an event by name on either side.
func (p *Protocol) Event(name string) (e *Event, server bool, ok bool) {
	for i := range p.Server.Events {
		if p.Server.Events[i].Name == name {
			return &p.Server.Events[i], true, true
		}
	}
	for i := range p.Client.Events {
		if p.Client.Events[i].Name == name {
			return &p.Client.Events[i], false, true
		}
	}
	return nil, false, false
}

// Error finds a public error by code.
func (p *Protocol) Error(code string) (*Error, bool) {
	for i := range p.Errors {
		if p.Errors[i].Code == code {
			return &p.Errors[i], true
		}
	}
	return nil, false
}

// Parameter finds a parameter by name.
func (p *Protocol) Parameter(name string) (*Parameter, bool) {
	for i := range p.Parameters {
		if p.Parameters[i].Name == name {
			return &p.Parameters[i], true
		}
	}
	return nil, false
}

// WalkExpressions visits every type expression the type declares at the
// top of its own structure — each field's, its alias, each variant's — with
// where it sits. It does not descend: Walk does that.
func (t *Type) WalkExpressions(visit func(TypeExpr, diag.Location)) {
	for _, base := range t.Extends {
		visit(base.Expression(), base.At)
	}
	for i := range t.Fields {
		visit(t.Fields[i].Type, t.At.Sub("fields", i, "type"))
	}
	if t.Alias != nil {
		visit(t.Alias, t.At.Sub("type"))
	}
	for i := range t.Variants {
		visit(t.Variants[i].Type, t.At.Sub("variants", t.Variants[i].Tag))
	}
}

// rewritten is the type with every expression in it rewritten.
func (t *Type) rewritten(f func(TypeExpr) TypeExpr) *Type {
	out := *t
	out.Extends = rewriteBases(t.Extends, f)
	out.Fields = append([]Field(nil), t.Fields...)
	for i := range out.Fields {
		out.Fields[i].Type = Rewrite(out.Fields[i].Type, f)
	}
	out.Alias = Rewrite(t.Alias, f)
	out.Variants = append([]Variant(nil), t.Variants...)
	for i := range out.Variants {
		out.Variants[i].Type = Rewrite(out.Variants[i].Type, f)
	}
	return &out
}

// Expressions visits every type expression a family declares, with where it
// sits: each own field of each type, each alias, each variant, each
// method's request and result, each event's type. Parents before children.
func (f *Family) Expressions(visit func(ExprAt)) {
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		t.WalkExpressions(func(e TypeExpr, at diag.Location) {
			walkAt(e, ExprAt{Owner: name, At: at}, visit)
		})
	}
	if f.Protocol == nil {
		return
	}
	for _, side := range []*Side{&f.Protocol.Server, &f.Protocol.Client} {
		for _, base := range side.Extends {
			for _, name := range sortedFillers(base.With) {
				walkAt(base.With[name].Type, ExprAt{At: base.At.Sub("with", name)}, visit)
			}
		}
		for i := range side.Methods {
			m := &side.Methods[i]
			walkAt(m.Request, ExprAt{At: m.At.Sub("request")}, visit)
			walkAt(m.Result, ExprAt{At: m.At.Sub("result")}, visit)
		}
		for i := range side.Events {
			e := &side.Events[i]
			walkAt(e.Type, ExprAt{At: e.At.Sub("type")}, visit)
		}
	}
}

// RewriteExpressions replaces every type expression the family declares,
// and everything beneath it, by what fn returns: what the loader turns a
// reference to a carried built-in into, in place, before anything resolves.
func (f *Family) RewriteExpressions(fn func(TypeExpr) TypeExpr) {
	for _, t := range f.Types {
		t.Extends = rewriteBases(t.Extends, fn)
		for i := range t.Fields {
			t.Fields[i].Type = Rewrite(t.Fields[i].Type, fn)
		}
		t.Alias = Rewrite(t.Alias, fn)
		for i := range t.Variants {
			t.Variants[i].Type = Rewrite(t.Variants[i].Type, fn)
		}
	}
	if f.Protocol == nil {
		return
	}
	for _, side := range []*Side{&f.Protocol.Server, &f.Protocol.Client} {
		side.Extends = rewriteBases(side.Extends, fn)
		for i := range side.Methods {
			side.Methods[i].Request = Rewrite(side.Methods[i].Request, fn)
			side.Methods[i].Result = Rewrite(side.Methods[i].Result, fn)
		}
		for i := range side.Events {
			side.Events[i].Type = Rewrite(side.Events[i].Type, fn)
		}
	}
}

// ExprAt is one type expression where it is declared: Owner is the type it
// is part of, empty for an operation's.
type ExprAt struct {
	Expr  TypeExpr
	Owner string
	At    diag.Location
}

func walkAt(e TypeExpr, site ExprAt, visit func(ExprAt)) {
	Walk(e, func(x TypeExpr) bool {
		visit(ExprAt{Expr: x, Owner: site.Owner, At: site.At})
		return true
	})
}

// The wire form of a type and of a field: what a validator reads and what
// an inline shape marshals to inside a type expression. A description is
// not on the wire; the keys a declaration may add come after the ones it
// always had, so that what a validator reads of a plain record is what it
// always read.

type wireTypeJSON struct {
	Kind       string          `json:"kind"`
	Key        string          `json:"key,omitempty"`
	Fields     []Field         `json:"fields,omitempty"`
	Extends    []Inheritance   `json:"extends,omitempty"`
	Open       bool            `json:"open,omitempty"`
	Values     []string        `json:"values,omitempty"`
	Type       TypeExpr        `json:"type,omitempty"`
	Parameters []wireParameter `json:"parameters,omitempty"`
	Tag        string          `json:"tag,omitempty"`
	Value      string          `json:"value,omitempty"`
	Variants   map[string]any  `json:"variants,omitempty"`
}

type wireParameter struct {
	Name string `json:"name"`
	Of   string `json:"of,omitempty"`
}

// MarshalJSON writes a parameter in the declaration's wire spelling.
func (p Parameter) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireParameter{Name: p.Name, Of: p.Of})
}

type wireFieldJSON struct {
	Name     string       `json:"name"`
	Type     TypeExpr     `json:"type"`
	Required bool         `json:"required"`
	Nullable bool         `json:"nullable,omitempty"`
	Unique   bool         `json:"unique,omitempty"`
	Min      *json.Number `json:"min,omitempty"`
	Max      *json.Number `json:"max,omitempty"`
	Length   *Length      `json:"length,omitempty"`
	Pattern  string       `json:"pattern,omitempty"`
}

// MarshalJSON writes the type as a validator reads it.
func (t *Type) MarshalJSON() ([]byte, error) {
	w := wireTypeJSON{Kind: t.Kind, Key: t.Key, Fields: t.Fields, Extends: t.Extends, Open: t.Open, Values: t.Values, Type: t.Alias, Tag: t.Tag, Value: t.Value}
	for _, p := range t.Parameters {
		w.Parameters = append(w.Parameters, wireParameter{Name: p.Name, Of: p.Of})
	}
	if len(t.Variants) > 0 {
		w.Variants = make(map[string]any, len(t.Variants))
		for _, variant := range t.Variants {
			w.Variants[variant.Tag] = variant.Type
			if variant.Type == nil {
				w.Variants[variant.Tag] = map[string]bool{"empty": true}
			}
		}
	}
	return json.Marshal(w)
}

// MarshalJSON writes the field as a validator reads it.
func (f Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireFieldJSON{Name: f.Name, Type: f.Type, Required: f.Required, Nullable: f.Nullable, Unique: f.Unique, Min: f.Min, Max: f.Max, Length: f.Length, Pattern: f.Pattern})
}
