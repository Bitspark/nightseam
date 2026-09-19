// Package doc is the specification as a document: every fact of a family a
// reader wants, computed once from render and held in one structure that
// writers render and never derive again — the types with their fields and
// what inherits them, the two sides with their operations and errors, the
// governance of a session, and the families and built-ins a family draws
// on. A writer (Writer) renders the document into pages of one format and
// is a target of the generator by the adapter Target, so that every format
// reads the one document and the kernel holds every format the same way.
//
// The document is data: it names nothing of the pipeline that built it, and
// it marshals as JSON for a writer that embeds it in a page. A type
// expression stays what the declaration wrote, in the one reference form
// the model marshals, so that a writer reading the document reads the
// declaration.
package doc

import (
	"encoding/json"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// Checkout is every family of a checkout, documented, by name.
type Checkout struct {
	Families []*Family
}

// Family is one family, documented.
type Family struct {
	Name       string
	Files      []string    // the tier files present, lowest tier first
	Imports    []string    // the families whose types this one's refer to, sorted
	Carries    []string    // the built-in families the tiers bring, sorted
	Generic    bool        // whether any type or operation draws on a parameter
	Parameters []Parameter // the family's, in declaration order
	Types      []*Type     // the family's own, in byte order
	Carried    []*Type     // carried from the built-in families, in byte order
	Protocol   bool        // whether the family has a protocol tier
	Server     Side
	Client     Side
	Errors     []Error // by code
	Session    *Session
}

// Parameter is one parameter of a generic family.
type Parameter struct {
	Name, Of, Description string
	Drawn                 []string // the types drawn through it, in order
}

// Type is one type, with its wire fields flattened.
type Type struct {
	Name, Kind, Description string
	Key                     string   // entity: the field that identifies it
	Open                    bool     // record, entity: fields beyond the declared ones are kept
	Carried                 bool     // carried from a built-in family
	From                    string   // the built-in family a carried type is declared by
	Uses                    []Use    // what the type is generic in
	Fields                  []Field  // record, entity: in wire order, inherited first
	Values                  []string // enum
	Alias                   model.TypeExpr
	Tag                     string          // union: the member that discriminates
	Value                   string          // union: the member a non-object payload rides under
	Variants                []Variant       // union: by tag
	Example                 json.RawMessage // an example value of the type, canonical JSON
	UsedBy                  []Reference     // where the type is used, in the order the declaration reaches it
}

// Use is one draw of a parameter at a type: "S.Envelope".
type Use struct{ Parameter, Type string }

// Field is one wire field of a record.
type Field struct {
	Name, Description string
	Type              model.TypeExpr
	Required          bool
	Nullable          bool
	Unique            bool
	Min, Max          *json.Number
	Length            *model.Length
	Pattern           string
	Owner             string // the type that declares it; another than the field's type where inherited
}

// Variant is one arm of a union.
type Variant struct {
	Tag  string
	Type model.TypeExpr
}

// Side is one peer's interface.
type Side struct {
	Extends []string // the families whose same side this one extends
	Methods []Method
	Events  []Event
}

// Method is one operation of a side.
type Method struct {
	Name, Description string
	Request           model.TypeExpr // nil: takes nothing
	Result            model.TypeExpr
	Errors            []string
	Frames            Frames // the exchange on the wire
	Weight            Weight // how much each side says
}

// Event is one notification of a side.
type Event struct {
	Name, Description string
	Type              model.TypeExpr
	Frame             json.RawMessage // the event on the wire
	Weight            int             // how much it says
}

// Error is one public error of the family.
type Error struct{ Code, Description string }

// Session is how a session of the family is governed.
type Session struct {
	Decides      []string
	Asks         []string
	Conversation *Conversation
}

// Conversation is where a session's conversation id arrives.
type Conversation struct{ Event, Path string }

// BuildCheckout documents every family of a checkout.
func BuildCheckout(w *render.World) *Checkout {
	c := &Checkout{}
	for _, f := range w.Families {
		c.Families = append(c.Families, Build(f))
	}
	return c
}

// Build documents one family as render presents it.
func Build(f *render.Family) *Family {
	d := &Family{Name: f.Name, Files: f.Files, Imports: f.References, Carries: f.Carries, Generic: f.Generic, Protocol: f.HasProtocol()}
	for _, p := range f.Parameters {
		parameter := Parameter{Name: p.Name, Of: p.Of, Description: p.Description}
		for _, use := range p.Uses {
			parameter.Drawn = append(parameter.Drawn, use.Type)
		}
		d.Parameters = append(d.Parameters, parameter)
	}
	x := newExampler(f)
	used := references(f)
	for _, t := range f.Types {
		dt := &Type{Name: t.Name, Kind: t.Kind, Description: t.Description, Key: t.Key, Open: t.Open, Carried: t.Carried, From: t.From, Values: t.Values, Alias: t.Alias, Tag: t.Tag, Value: t.Value, Example: x.example(t).raw(), UsedBy: used[t.Name]}
		for _, use := range t.Uses {
			dt.Uses = append(dt.Uses, Use{Parameter: use.Parameter, Type: use.Type})
		}
		for _, field := range t.Fields {
			dt.Fields = append(dt.Fields, Field{Name: field.Name, Description: field.Description, Type: field.Type, Required: field.Required, Nullable: field.Nullable, Unique: field.Unique, Min: field.Min, Max: field.Max, Length: field.Length, Pattern: field.Pattern, Owner: field.Owner})
		}
		for _, variant := range t.Variants {
			dt.Variants = append(dt.Variants, Variant{Tag: variant.Tag, Type: variant.Type})
		}
		if t.Carried {
			d.Carried = append(d.Carried, dt)
		} else {
			d.Types = append(d.Types, dt)
		}
	}
	if f.HasProtocol() {
		d.Server = side(x, "server", f.Server, f.Errors)
		d.Client = side(x, "client", f.Client, f.Errors)
		for _, e := range f.Errors {
			d.Errors = append(d.Errors, Error{Code: e.Code, Description: e.Description})
		}
	}
	if s := f.Session; s != nil {
		d.Session = &Session{Decides: s.Decides, Asks: s.Asks}
		if s.Conversation != nil {
			d.Session.Conversation = &Conversation{Event: s.Conversation.Event, Path: s.Conversation.Path}
		}
	}
	return d
}

func side(x *exampler, name string, s render.Side, errors []render.Error) Side {
	out := Side{Extends: s.Extends}
	for _, m := range s.Methods {
		frames := x.frames(name, m, errors)
		w := Weight{}
		if m.Request != nil {
			w.Request = x.value(m.Request, "params", constraints{}).weight()
		}
		if m.Result != nil {
			w.Result = x.value(m.Result, "result", constraints{}).weight()
		}
		out.Methods = append(out.Methods, Method{Name: m.Name, Description: m.Description, Request: m.Request, Result: m.Result, Errors: m.Errors, Frames: frames, Weight: w})
	}
	for _, e := range s.Events {
		w := 0
		if e.Type != nil {
			w = x.value(e.Type, "data", constraints{}).weight()
		}
		out.Events = append(out.Events, Event{Name: e.Name, Description: e.Description, Type: e.Type, Frame: x.event(e), Weight: w})
	}
	return out
}
