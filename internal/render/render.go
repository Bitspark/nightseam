// Package render is the family as a target sees it: every fact a rendering
// needs, computed once from analysis after the neutral checks passed, and
// nothing target-specific. Types in the order every rendering lists them,
// each with its wire fields flattened and the type parameters it takes; the
// two sides with their operations; the errors; the session; the families
// the rendering refers to; the wire description the validators embed; the
// names a convention derives and a target's override file replaces.
package render

import (
	"encoding/json"
	"sort"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// Use is one parameter drawn at one type.
type Use = analysis.Use

// Family is one family, ready to render.
type Family struct {
	Name            string
	Files           []string    // the tier files present
	Generic         bool        // whether any type or operation draws on a parameter
	Parameters      []Parameter // in declaration order
	Uses            []Use       // the family's, in order: what every entry point is generic in
	Types           []*Type     // in byte order, the injected ones among them
	Server, Client  Side        // the two sides
	Errors          []Error     // by code
	Session         *Session    // nil without a session tier
	References      []string    // families whose generated packages this one's refer to, sorted
	Carries         []string    // the built-in families the tiers bring, sorted
	SessionFamilies []string    // the other families with a session tier, sorted: what a parameter may bind
	Wire            string      // the wire description of every type, canonical JSON, what a validator reads
	overrides       map[string]model.Overrides
	f               *analysis.Family
	types           map[string]*Type
}

// Parameter is one parameter of a generic family with the types it is
// drawn at.
type Parameter struct {
	Name, Of, Description string
	Uses                  []Use
	At                    diag.Location
}

// Type is one type with its wire fields flattened.
type Type struct {
	Name, Kind, Description string
	Key                     string
	Parameters              []model.Parameter // the holes in the type itself
	Fields                  []Field           // in wire order, inherited first
	Own                     []Field           // declared by this type alone
	Open                    bool
	Values                  []string
	Alias                   model.TypeExpr
	Tag                     string          // union: the member that discriminates
	Value                   string          // union: the member a non-object payload rides under
	Variants                []model.Variant // union: own variants, by tag
	Uses                    []Use           // the type parameters the type takes
	Carried                 bool            // carried from a built-in family, not declared here
	From                    string          // the built-in family that declares a carried type
	At                      diag.Location
}

// Field is one wire field, with its constraints.
type Field struct {
	Name, Description string
	Type              model.TypeExpr
	Required          bool
	Nullable          bool
	Unique            bool
	Min, Max          *json.Number
	Length            *model.Length
	Pattern           string
	Owner             string // the type that declares it
	At                diag.Location
}

// Side is one peer's interface.
type Side struct {
	Extends []string // the families whose same side this one extends
	Methods []Method
	Events  []Event
	At      diag.Location
}

// Method is one operation, with what it is generic in.
type Method struct {
	Name, Description string
	Request           model.TypeExpr // nil: takes nothing
	Result            model.TypeExpr
	Errors            []string
	Uses              []Use
	At                diag.Location
}

// Event is one notification.
type Event struct {
	Name, Description string
	Type              model.TypeExpr
	Uses              []Use
	At                diag.Location
}

// Error is one public error.
type Error struct {
	Code, Description string
	At                diag.Location
}

// Session is the governance of a session family.
type Session struct {
	Decides      []string
	Asks         []string
	Conversation *model.Conversation
}

// Build renders the facts of a family that passed every neutral check.
func Build(f *analysis.Family) *Family {
	g := f.Generics()
	r := &Family{Name: f.Name, Files: f.Files, Generic: g.Generic(), Uses: g.Family, overrides: map[string]model.Overrides{}, f: f, types: map[string]*Type{}}
	for _, p := range f.Parameters() {
		var uses []Use
		for _, use := range g.Family {
			if use.Parameter == p.Name {
				uses = append(uses, use)
			}
		}
		r.Parameters = append(r.Parameters, Parameter{Name: p.Name, Of: p.Of, Description: p.Description, Uses: uses, At: p.At})
	}
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		rt := &Type{Name: name, Kind: t.Kind, Description: t.Description, Key: t.Key, Parameters: t.Parameters, Open: t.Open, Values: t.Values, Alias: t.Alias, Tag: t.Tag, Value: t.ValueMember(), Variants: t.Variants, Uses: g.Types[name], Carried: f.IsCarried(name), From: f.CarriedFrom(name), At: t.At}
		f.WalkFields(name, func(at analysis.FieldAt) {
			rt.Fields = append(rt.Fields, field(at.Owner, at.Field))
		})
		for _, own := range t.Fields {
			rt.Own = append(rt.Own, field(name, own))
		}
		r.Types = append(r.Types, rt)
		r.types[name] = rt
	}
	if p := f.Protocol; p != nil {
		r.Server = side(f, p.Server)
		r.Client = side(f, p.Client)
		for _, e := range p.Errors {
			r.Errors = append(r.Errors, Error{Code: e.Code, Description: e.Description, At: e.At})
		}
	}
	if s := f.Session; s != nil {
		r.Session = &Session{Decides: s.Decides, Asks: s.Asks, Conversation: s.Conversation}
	}
	r.References = f.References()
	r.Carries = f.Carries
	for name := range f.Members {
		r.SessionFamilies = append(r.SessionFamilies, name)
	}
	sort.Strings(r.SessionFamilies)
	r.Wire = wire(f)
	for target, raw := range f.Overrides {
		if raw == nil {
			continue
		}
		if o, err := model.DecodeOverrides(model.OverrideFile(target), raw); err == nil {
			r.overrides[target] = o
		}
	}
	return r
}

func field(owner string, m model.Field) Field {
	return Field{Name: m.Name, Description: m.Description, Type: m.Type, Required: m.Required, Nullable: m.Nullable, Unique: m.Unique, Min: m.Min, Max: m.Max, Length: m.Length, Pattern: m.Pattern, Owner: owner, At: m.At}
}

func side(f *analysis.Family, s model.Side) Side {
	out := Side{Extends: s.Extends, At: s.At}
	for _, m := range s.Methods {
		uses := union(f.UsesOf(m.Request), f.UsesOf(m.Result))
		out.Methods = append(out.Methods, Method{Name: m.Name, Description: m.Description, Request: m.Request, Result: m.Result, Errors: m.Errors, Uses: uses, At: m.At})
	}
	for _, e := range s.Events {
		out.Events = append(out.Events, Event{Name: e.Name, Description: e.Description, Type: e.Type, Uses: f.UsesOf(e.Type), At: e.At})
	}
	return out
}

func union(sets ...[]Use) []Use {
	var out []Use
	for _, set := range sets {
		for _, use := range set {
			found := false
			for _, have := range out {
				if have == use {
					found = true
				}
			}
			if !found {
				out = append(out, use)
			}
		}
	}
	return out
}

// Type finds a type by name.
func (r *Family) Type(name string) *Type { return r.types[name] }

// HasProtocol reports whether the family has a protocol tier.
func (r *Family) HasProtocol() bool { return r.f.Protocol != nil }

// UsesOf is what an expression is generic in.
func (r *Family) UsesOf(e model.TypeExpr) []Use { return r.f.UsesOf(e) }

// ImportedUses is what a type of an imported family is generic in, in that
// family's own parameters.
func (r *Family) ImportedUses(family, typeName string) []Use {
	return r.f.Generics().Imported[family][typeName]
}

// Argument is what fills one type parameter of an applied type: a family
// parameter of this family, or a named family. Exactly one is set.
type Argument struct {
	Use       Use    // the imported type's parameter, at the type drawn
	Parameter string // a family parameter of this family
	Family    string // a named family
}

// Arguments resolves an application: for each type parameter the applied
// type takes, what fills it — a family parameter of this family when the
// application says so or when the family has one, a named family otherwise.
func (r *Family) Arguments(a model.Apply) []Argument {
	var out []Argument
	for _, use := range r.ImportedUses(a.Family, a.Name) {
		argument := Argument{Use: use}
		filler, bound := a.With[use.Parameter]
		switch {
		case !bound && len(r.familyParameters()) == 1:
			argument.Parameter = r.familyParameters()[0]
		case !bound:
			argument.Parameter = use.Parameter
		case r.f.HasFamilyParameter(filler.Name()):
			argument.Parameter = filler.Name()
		case filler.Family != "":
			argument.Family = filler.Family
		default:
			argument.Parameter = filler.Name()
		}
		out = append(out, argument)
	}
	return out
}

func (r *Family) familyParameters() []string {
	var names []string
	for _, p := range r.Parameters {
		if p.Of != "" {
			names = append(names, p.Name)
		}
	}
	return names
}

// Override is a target's name for a path key, if its override file names
// one. Path keys: Type, Type.field, Enum.value, a method or event, and
// errors.code.
func (r *Family) Override(target, path string) (string, bool) {
	name, ok := r.overrides[target].Names[path]
	return name, ok
}

// Overrides is every path key a target's override file names, sorted.
func (r *Family) Overrides(target string) []string {
	keys := make([]string, 0, len(r.overrides[target].Names))
	for key := range r.overrides[target].Names {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Locate answers a path key with the declaration it names.
func (r *Family) Locate(path string) (diag.Location, bool) { return r.f.Locate(path) }

// OverrideAt is where a target's override of a path key is declared, for a
// diagnostic about the name it gives.
func OverrideAt(target, path string) diag.Location {
	return diag.Location{File: model.OverrideFile(target), Pointer: "/names/" + diag.Escape(path)}
}

// The wire description a validator reads: every type as the model marshals
// it — its own fields, its parents, each field's expression as written, and
// what a union discriminates on — so that the validator flattens
// inheritance itself and reads the one reference form. It is the
// declaration, not a second spelling of it beside it.

func wire(f *analysis.Family) string {
	types := map[string]*model.Type{}
	for _, name := range f.TypeNames() {
		types[name] = f.Types[name]
	}
	data, _ := json.Marshal(types)
	return string(data)
}

// Form is one form of the declaration language, named as a diagnostic names
// it. A target that does not render a form yet refuses a family that uses
// one, saying which form and which target, rather than emitting something
// that is not what was declared — never a panic, and never silence.
type Form struct {
	Name string
	At   diag.Location
}

// FormsUsed is every form of the declaration language the family uses that
// a target may not render, each named once, in the order they are met. A
// target whose renderer has learned a form drops it from what it refuses;
// the list is what the targets of a language still owe the language.
func FormsUsed(f *Family) []Form {
	var forms []Form
	seen := map[string]bool{}
	add := func(name string, at diag.Location) {
		if !seen[name] {
			seen[name] = true
			forms = append(forms, Form{Name: name, At: at})
		}
	}
	expression := func(e model.TypeExpr, at diag.Location) {
		model.Walk(e, func(x model.TypeExpr) bool {
			switch v := x.(type) {
			case model.Nullable:
				add("a nullable type expression, {\"nullable\": T}", at)
			case model.Literal:
				add("a literal type", at)
			case model.Inline:
				add("a shape written inline, named by where it sits", at)
			case model.Apply:
				if v.Family == "" {
					add("an application of a generic type of this family", at)
				}
			}
			return true
		})
	}
	for _, p := range f.Parameters {
		switch {
		case p.Of == "":
			add("a type parameter of the family", p.At)
		case p.Of != model.SessionRole:
			add("a family parameter of the "+p.Of+" tier", p.At)
		}
	}
	for _, t := range f.Types {
		if t.Carried {
			continue
		}
		if t.Kind == model.KindUnion {
			add("a union", t.At)
		}
		if len(t.Parameters) > 0 {
			add("a type parameter of a type", t.At)
		}
		for _, field := range t.Own {
			expression(field.Type, field.At.Sub("type"))
		}
		if t.Alias != nil {
			expression(t.Alias, t.At.Sub("type"))
		}
		for _, variant := range t.Variants {
			expression(variant.Type, variant.At)
		}
	}
	for _, side := range []Side{f.Server, f.Client} {
		if len(side.Extends) > 0 {
			add("a side that extends another family's", side.At.Sub("extends"))
		}
		for _, m := range side.Methods {
			expression(m.Request, m.At.Sub("request"))
			expression(m.Result, m.At.Sub("result"))
		}
		for _, e := range side.Events {
			expression(e.Type, e.At.Sub("type"))
		}
	}
	return forms
}

// Unrendered is the diagnostics a target reports for the forms it does not
// render yet: one per form, naming the form and the target.
func Unrendered(f *Family, target string) []diag.Diagnostic {
	var diagnostics []diag.Diagnostic
	for _, form := range FormsUsed(f) {
		diagnostics = append(diagnostics, diag.New(f.Name, form.At, "unrendered_form", "The "+target+" target does not render "+form.Name+" yet."))
	}
	return diagnostics
}
