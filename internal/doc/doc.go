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
// declaration — with one resolution made here for every writer: a shape
// written inline is named by the declaration the generator derives for it,
// so that a writer spells it as it spells any other type.
package doc

import (
	"encoding/json"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Checkout is every family of a checkout, documented, by name.
type Checkout struct {
	Families []*Family
}

// Family is one family, documented.
type Family struct {
	Name       string
	Source     string      // the declaration directory, relative to the checkout; a built-in's own namespace
	Builtin    bool        // one of the families Nightseam declares of itself: the profile's own
	Files      []string    // the tier files present, lowest tier first
	Imports    []string    // the families whose types this one's refer to, sorted
	Carries    []string    // the built-in families the tiers bring, sorted
	Generic    bool        // whether any type or operation draws on a parameter
	Parameters []Parameter // the family's, in declaration order
	Types      []*Type     // the family's own, in byte order, the shapes written inline among them
	Carried    []*Type     // carried from the built-in families, in byte order
	Protocol   bool        // whether the family has a protocol tier
	Server     Side
	Client     Side
	Errors     []Error // by code
	Session    *Session
}

// Origin is where a declaration comes from: the family and the declaration
// that made it, which differ from the family documented where a member is
// inherited or carried.
type Origin struct{ Family, Declaration string }

// Parameter is one parameter, of a family or of a type: a type parameter
// where Of is empty, a family parameter of the named tier otherwise.
type Parameter struct {
	Name, Of, Description string
	Drawn                 []string // the types drawn through it, in order
}

// Type is one type, with its wire fields flattened.
type Type struct {
	Languages               map[string]Language // target names, declarations and invocation code
	Name, Kind, Description string
	Key                     string           // entity: the field that identifies it
	Open                    bool             // record, entity: fields beyond the declared ones are kept
	Carried                 bool             // carried from a built-in family
	From                    string           // the built-in family a carried type is declared by
	Inline                  bool             // a shape written inline, under the name derived for it
	Origin                  Origin           // the family and declaration it comes from
	Extends                 []model.TypeExpr // the bases, in extends order, as declared
	Parameters              []Parameter      // the type's own
	Scope                   []string         // the parameters in scope, by name: what a plain name may be
	Uses                    []Use            // what the type is generic in
	Fields                  []Field          // record, entity: in wire order, inherited first
	Values                  []string         // enum
	Alias                   model.TypeExpr
	Tag                     string          // union: the member that discriminates
	Value                   string          // union: the member the payload rides under
	Variants                []Variant       // union: inherited first, then own
	Example                 json.RawMessage // an example value of the type, canonical JSON
	UsedBy                  []Reference     // where the type is used, in the order the declaration reaches it
}

// Use is one draw of a parameter at a type: "S.Envelope", or a type
// parameter alone.
type Use struct{ Parameter, Type string }

// Field is one wire field of a record. Type is the field's type as
// resolved, what an example is made of; Declared is what the declaration
// wrote, bound where inherited through an application, with a reference
// still a reference — what a page spells.
type Field struct {
	Name, Description string
	Type              model.TypeExpr
	Declared          model.TypeExpr
	Required          bool
	Nullable          bool
	Unique            bool
	Min, Max          *json.Number
	Length            *model.Length
	Pattern           string
	Owner             string // the type that declares it; another than the field's type where inherited
	Origin            Origin
}

// Variant is one arm of a union: a payload under the union's value member,
// or, where Empty, the tag alone.
type Variant struct {
	Tag      string
	Type     model.TypeExpr // the payload as resolved; nil where Empty
	Declared model.TypeExpr // the payload as declared
	Empty    bool
	Origin   Origin
	Scope    []string
}

// Side is one peer's interface.
type Side struct {
	Extends []model.Inheritance // the families whose same side this one extends, applied where generic
	Methods []Method
	Events  []Event
}

// Method is one operation of a side, its own or inherited.
type Method struct {
	Languages         map[string]Language
	Name, Description string
	Request           model.TypeExpr // resolved; nil: takes nothing
	Result            model.TypeExpr
	DeclaredRequest   model.TypeExpr // as declared, bound where inherited
	DeclaredResult    model.TypeExpr
	Errors            []string
	Origin            Origin
	Scope             []string
	Frames            Frames // the exchange on the wire
	Weight            Weight // how much each side says
}

// Event is one notification of a side, its own or inherited.
type Event struct {
	Languages         map[string]Language
	Name, Description string
	Type              model.TypeExpr // resolved
	Declared          model.TypeExpr // as declared, bound where inherited
	Origin            Origin
	Scope             []string
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
	Inherited    []Inherited // the sides whose governance this one inherits
}

// Inherited names a side of another family whose governance a session
// inherits.
type Inherited struct{ Family, Side string }

// Conversation is where a session's conversation id arrives.
type Conversation struct{ Event, Path string }

// BuildCheckout documents every family of a checkout.
func BuildCheckout(w *render.World, spellers map[string]spi.Speller) *Checkout {
	c := &Checkout{}
	for _, f := range w.Families {
		c.Families = append(c.Families, Build(f, spellers))
	}
	return c
}

// Build documents one family as render presents it.
func Build(f *render.Family, spellers map[string]spi.Speller) *Family {
	d := &Family{Name: f.Name, Source: f.Source, Builtin: f.Builtin, Files: f.Files, Imports: f.References, Carries: f.Carries, Generic: f.Generic, Protocol: f.HasProtocol()}
	for _, p := range f.Parameters {
		d.Parameters = append(d.Parameters, Parameter{Name: p.Name, Of: p.Of, Description: p.Description, Drawn: drawn(p.Uses, p.Name)})
	}
	x := newExampler(f)
	used := references(f)
	named := namer(f)
	for _, t := range f.Types {
		dt := &Type{Name: t.Name, Kind: t.Kind, Description: t.Description, Key: t.Key, Open: t.Open, Carried: t.Carried, From: t.From, Inline: t.Inline, Origin: origin(t.Origin), Scope: names(t.Scope), Values: t.Values, Alias: named(t.Alias), Tag: t.Tag, Value: t.Value, Example: x.example(t).raw(), UsedBy: used[t.Name]}
		for _, edge := range t.Extends {
			dt.Extends = append(dt.Extends, named(edge.Expression()))
		}
		for _, p := range t.Parameters {
			dt.Parameters = append(dt.Parameters, Parameter{Name: p.Name, Of: p.Of, Description: p.Description, Drawn: drawn(t.Uses, p.Name)})
		}
		for _, use := range t.Uses {
			dt.Uses = append(dt.Uses, Use{Parameter: use.Parameter, Type: use.Type})
		}
		for _, field := range t.Fields {
			dt.Fields = append(dt.Fields, Field{Name: field.Name, Description: field.Description, Type: field.Type, Declared: named(declaredOr(field.DeclaredType, field.Type)), Required: field.Required, Nullable: field.Nullable, Unique: field.Unique, Min: field.Min, Max: field.Max, Length: field.Length, Pattern: field.Pattern, Owner: field.Owner, Origin: origin(field.Origin)})
		}
		for _, variant := range t.Variants {
			dt.Variants = append(dt.Variants, Variant{Tag: variant.Tag, Type: variant.Type, Declared: named(declaredOr(variant.DeclaredType, variant.Type)), Empty: variant.Form == render.VariantEmpty, Origin: origin(variant.Origin), Scope: names(variant.Scope)})
		}
		if t.Carried {
			d.Carried = append(d.Carried, dt)
		} else {
			d.Types = append(d.Types, dt)
		}
	}
	if f.HasProtocol() {
		d.Server = side(x, named, "server", f.Server, f.Errors)
		d.Client = side(x, named, "client", f.Client, f.Errors)
		for _, e := range f.Errors {
			d.Errors = append(d.Errors, Error{Code: e.Code, Description: e.Description})
		}
	}
	if s := f.Session; s != nil {
		d.Session = &Session{Decides: s.Decides, Asks: s.Asks}
		if s.Conversation != nil {
			d.Session.Conversation = &Conversation{Event: s.Conversation.Event, Path: s.Conversation.Path}
		}
		for _, inherited := range s.Inherited {
			d.Session.Inherited = append(d.Session.Inherited, Inherited{Family: inherited.Family, Side: inherited.Side})
		}
	}
	addLanguages(d, f, spellers)
	return d
}

func side(x *exampler, named func(model.TypeExpr) model.TypeExpr, name string, s render.Side, errors []render.Error) Side {
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
		request, result := m.Request, m.Result
		if m.BoundDeclaration != nil {
			request, result = m.BoundDeclaration.Request, m.BoundDeclaration.Result
		}
		out.Methods = append(out.Methods, Method{Name: m.Name, Description: m.Description, Request: m.Request, Result: m.Result, DeclaredRequest: named(request), DeclaredResult: named(result), Errors: m.Errors, Origin: origin(m.Origin), Scope: names(m.Scope), Frames: frames, Weight: w})
	}
	for _, e := range s.Events {
		w := 0
		if e.Type != nil {
			w = x.value(e.Type, "data", constraints{}).weight()
		}
		declared := e.Type
		if e.BoundDeclaration != nil {
			declared = e.BoundDeclaration.Type
		}
		out.Events = append(out.Events, Event{Name: e.Name, Description: e.Description, Type: e.Type, Declared: named(declared), Origin: origin(e.Origin), Scope: names(e.Scope), Frame: x.event(e), Weight: w})
	}
	return out
}

func origin(o render.Origin) Origin { return Origin{Family: o.Family, Declaration: o.Declaration} }

func names(parameters []model.Parameter) []string {
	var out []string
	for _, p := range parameters {
		out = append(out, p.Name)
	}
	return out
}

func drawn(uses []render.Use, parameter string) []string {
	var out []string
	for _, use := range uses {
		if use.Parameter == parameter && use.Type != "" {
			out = append(out, use.Type)
		}
	}
	return out
}

func declaredOr(declared, resolved model.TypeExpr) model.TypeExpr {
	if declared != nil {
		return declared
	}
	return resolved
}

// namer resolves every shape written inline in an expression to the name
// the generator derives for it, qualified by the family that declares it
// where that is another, so that a writer spells an inline shape as it
// spells any type. Every other form is copied as it is.
func namer(f *render.Family) func(model.TypeExpr) model.TypeExpr {
	var rewrite func(model.TypeExpr) model.TypeExpr
	rewrite = func(e model.TypeExpr) model.TypeExpr {
		switch v := e.(type) {
		case model.Array:
			return model.Array{Elem: rewrite(v.Elem)}
		case model.Map:
			return model.Map{Elem: rewrite(v.Elem)}
		case model.Nullable:
			return model.Nullable{Elem: rewrite(v.Elem)}
		case model.Apply:
			with := map[string]model.Filler{}
			for name, filler := range v.With {
				with[name] = model.Filler{Type: rewrite(filler.Type), Family: filler.Family}
			}
			return model.Apply{Family: v.Family, Name: v.Name, With: with}
		case model.Inline:
			if t := f.InlineType(v); t != nil {
				if t.Origin.Family != "" && t.Origin.Family != f.Name {
					return model.Imported{Family: t.Origin.Family, Name: t.Name}
				}
				return model.Named{Name: t.Name}
			}
		}
		return e
	}
	return rewrite
}
