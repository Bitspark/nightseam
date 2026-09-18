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
)

// Type is one declared type.
type Type struct {
	Name        string
	Kind        string
	Description string
	Key         string   // entity: the field that identifies it
	Extends     []string // record, entity: the records whose fields come first
	Open        bool     // record, entity: fields beyond the declared ones are kept
	Fields      []Field  // record, entity: in wire order, own fields only
	Values      []string // enum
	Alias       TypeExpr // alias
	At          diag.Location
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

// Parameter is a hole in a family: a slot draws a type from it, and a
// consumer binds it to a family that declares its role.
type Parameter struct {
	Name        string
	Of          string // the role a bound family must declare
	Description string
	At          diag.Location
}

// Side is one peer's interface: the methods it implements and the events
// it emits. The server side is implemented by the server and called by the
// client; the client side is the reverse.
type Side struct {
	Methods []Method            // by name
	Events  []Event             // by name
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
	Kind        string          `json:"kind"`
	Description string          `json:"description"`
	Key         string          `json:"key"`
	Extends     []string        `json:"extends"`
	Open        bool            `json:"open"`
	Fields      []fieldJSON     `json:"fields"`
	Values      []string        `json:"values"`
	Type        json.RawMessage `json:"type"`
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
		t := &Type{Name: name, Kind: w.Kind, Description: w.Description, Key: w.Key, Extends: w.Extends, Open: w.Open, Values: w.Values, At: at}
		for i, f := range w.Fields {
			field, err := decodeField(f, at.Sub("fields", i))
			if err != nil {
				return nil, err
			}
			t.Fields = append(t.Fields, field)
		}
		if w.Type != nil {
			alias, err := Decode(w.Type)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", at.Sub("type"), err)
			}
			t.Alias = alias
		}
		types[name] = t
	}
	return types, nil
}

func decodeField(w fieldJSON, at diag.Location) (Field, error) {
	expr, err := Decode(w.Type)
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
	for name, m := range w.Methods {
		method := Method{Name: name, Description: m.Description, Errors: m.Errors, At: at.Sub("methods", name)}
		var err error
		if m.Request != nil {
			if method.Request, err = Decode(m.Request); err != nil {
				return side, fmt.Errorf("%s: %w", method.At.Sub("request"), err)
			}
		}
		if method.Result, err = Decode(m.Result); err != nil {
			return side, fmt.Errorf("%s: %w", method.At.Sub("result"), err)
		}
		side.Methods = append(side.Methods, method)
	}
	sort.Slice(side.Methods, func(i, j int) bool { return side.Methods[i].Name < side.Methods[j].Name })
	for name, e := range w.Events {
		event := Event{Name: name, Description: e.Description, At: at.Sub("events", name)}
		var err error
		if event.Type, err = Decode(e.Type); err != nil {
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

// ParameterNames is the parameters' names in declaration order: the order
// every rendering declares its type parameters in.
func (p *Protocol) ParameterNames() []string {
	names := make([]string, len(p.Parameters))
	for i, parameter := range p.Parameters {
		names[i] = parameter.Name
	}
	return names
}

// Expressions visits every type expression a family declares, with where it
// sits: each own field of each type, each alias, each method's request and
// result, each event's type. Parents before children.
func (f *Family) Expressions(visit func(ExprAt)) {
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		for i := range t.Fields {
			field := &t.Fields[i]
			walkAt(field.Type, ExprAt{Owner: name, At: field.At.Sub("type")}, visit)
		}
		if t.Alias != nil {
			walkAt(t.Alias, ExprAt{Owner: name, At: t.At.Sub("type")}, visit)
		}
	}
	if f.Protocol == nil {
		return
	}
	for _, side := range []*Side{&f.Protocol.Server, &f.Protocol.Client} {
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
