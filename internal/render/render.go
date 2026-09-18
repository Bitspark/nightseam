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
	Fields                  []Field // in wire order, inherited first
	Own                     []Field // declared by this type alone
	Open                    bool
	Values                  []string
	Alias                   model.TypeExpr
	Uses                    []Use // the type parameters the type takes
	Injected                bool
	At                      diag.Location
}

// Field is one wire field.
type Field struct {
	Name, Description string
	Type              model.TypeExpr
	Required          bool
	Nullable          bool
	Owner             string // the type that declares it
	At                diag.Location
}

// Side is one peer's interface.
type Side struct {
	Methods []Method
	Events  []Event
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
		rt := &Type{Name: name, Kind: t.Kind, Description: t.Description, Key: t.Key, Open: t.Open, Values: t.Values, Alias: t.Alias, Uses: g.Types[name], Injected: model.IsInjected(name) && t.At.Pointer == "", At: t.At}
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
	return Field{Name: m.Name, Description: m.Description, Type: m.Type, Required: m.Required, Nullable: m.Nullable, Owner: owner, At: m.At}
}

func side(f *analysis.Family, s model.Side) Side {
	var out Side
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

// HasSession reports whether the family has a session tier.
func (r *Family) HasSession() bool { return r.Session != nil }

// HasProtocol reports whether the family has a protocol tier.
func (r *Family) HasProtocol() bool { return r.f.Protocol != nil }

// UsesOf is what an expression is generic in.
func (r *Family) UsesOf(e model.TypeExpr) []Use { return r.f.UsesOf(e) }

// ImportedUses is what a type of an imported family is generic in, in that
// family's own parameters.
func (r *Family) ImportedUses(family, typeName string) []Use {
	return r.f.Generics().Imported[family][typeName]
}

// Argument is what fills one type parameter of an applied type.
type Argument struct {
	Use    Use          // the imported type's parameter, at the type drawn
	Filler model.Filler // a parameter of this family, or a named family
}

// Arguments resolves an application: for each type parameter the applied
// type takes, what fills it — a parameter of this family when the
// application says so or when the family has one parameter, a named family
// otherwise.
func (r *Family) Arguments(a model.Apply) []Argument {
	var out []Argument
	for _, use := range r.ImportedUses(a.Family, a.Name) {
		filler, bound := a.With[use.Parameter]
		if !bound {
			if len(r.Parameters) == 1 {
				filler = model.Filler{Parameter: r.Parameters[0].Name}
			} else {
				filler = model.Filler{Parameter: use.Parameter}
			}
		}
		out = append(out, Argument{Use: use, Filler: filler})
	}
	return out
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

// The wire description a validator reads: every type with its own fields,
// its parents, and each field's expression as written, so that the
// validator flattens inheritance itself and reads the one reference form.

type wireType struct {
	Kind    string         `json:"kind"`
	Key     string         `json:"key,omitempty"`
	Fields  []wireField    `json:"fields,omitempty"`
	Extends []string       `json:"extends,omitempty"`
	Open    bool           `json:"open,omitempty"`
	Values  []string       `json:"values,omitempty"`
	Type    model.TypeExpr `json:"type,omitempty"`
}

type wireField struct {
	Name     string         `json:"name"`
	Type     model.TypeExpr `json:"type"`
	Required bool           `json:"required"`
	Nullable bool           `json:"nullable,omitempty"`
	Unique   bool           `json:"unique,omitempty"`
	Min      *json.Number   `json:"min,omitempty"`
	Max      *json.Number   `json:"max,omitempty"`
	Length   *model.Length  `json:"length,omitempty"`
	Pattern  string         `json:"pattern,omitempty"`
}

func wire(f *analysis.Family) string {
	types := map[string]wireType{}
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		w := wireType{Kind: t.Kind, Key: t.Key, Extends: t.Extends, Open: t.Open, Values: t.Values, Type: t.Alias}
		for _, field := range t.Fields {
			w.Fields = append(w.Fields, wireField{Name: field.Name, Type: field.Type, Required: field.Required, Nullable: field.Nullable, Unique: field.Unique, Min: field.Min, Max: field.Max, Length: field.Length, Pattern: field.Pattern})
		}
		types[name] = w
	}
	data, _ := json.Marshal(types)
	return string(data)
}
