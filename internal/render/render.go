// Package render is the family as a target sees it: every fact a rendering
// needs, computed once from analysis after the neutral checks passed, and
// nothing target-specific. Types in the order every rendering lists them,
// each with its wire fields flattened and the type parameters it takes; the
// two sides with their operations; the errors; the families
// the rendering refers to; the wire description the validators embed; the
// names a convention derives and a target's override file replaces.
package render

import (
	"crypto/sha256"
	"encoding/hex"
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
	Name           string
	Source         string      // original declaration directory, including a built-in's distinct namespace
	Builtin        bool        // the source belongs to the embedded built-in namespace
	Files          []string    // the tier files present
	Generic        bool        // whether any type or operation draws on a parameter
	Parameters     []Parameter // in declaration order
	Uses           []Use       // the family's, in order: what every entry point is generic in
	Types          []*Type     // in byte order, the injected ones among them
	Server, Client Side        // the two sides
	Errors         []Error     // by code
	Live           bool        // whether the family has a live tier
	Callables      []string    // the callables it declares, in byte order
	LiveTypes      []string    // every declared type whose values carry a callable, in byte order
	References     []string    // families whose generated packages this one's refer to, sorted
	Carries        []string    // the built-in families the tiers bring, sorted
	Wire           string      // the wire description of every type, canonical JSON, what a validator reads
	Declaration    string      // canonical declaration graph, independent of backend presentation
	WireDigest     string      // lower-case hex SHA-256 of Declaration's exact UTF-8 bytes
	overrides      map[string]model.Overrides
	f              *analysis.Family
	types          map[string]*Type
	builder        *builder
	inlines        map[*model.Type]*Type
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
	Tag                     string    // union: the member that discriminates
	Value                   string    // union: the member the complete payload rides under
	Variants                []Variant // union: inherited variants, then own
	OwnVariants             []Variant
	Extends                 []model.Inheritance
	Bases                   []Base            // resolved, applied bases in extends order
	Scope                   []model.Parameter // lexical parameters, including captured parameters of an inline shape
	Declaration             *model.Type       // original declaration; never rewritten by rendering
	Origin                  Origin
	Inline                  bool
	Uses                    []Use          // the type parameters the type takes
	Arguments               []Argument     // arguments of an applied view; declaration identity is unchanged
	Request                 model.TypeExpr // callable: what it takes; nil takes nothing
	Result                  model.TypeExpr // callable: what it answers; nil answers nothing
	Contract                string         // callable: the identity a reference to it carries
	IsLive                  bool           // whether values of the type carry a callable
	Carried                 bool           // carried from a built-in family, not declared here
	From                    string         // the built-in family that declares a carried type
	At                      diag.Location
	resolved                bool
}

// Base retains the written edge beside its applied view in this scope.
type Base struct {
	Edge model.Inheritance
	Type *Type
}

// Field is one wire field, with its constraints.
type Field struct {
	Name, Description string
	Type              model.TypeExpr
	DeclaredType      model.TypeExpr // bound expression retaining entity-reference meaning
	Scope             []model.Parameter
	Required          bool
	Nullable          bool
	Unique            bool
	Min, Max          *json.Number
	Length            *model.Length
	Pattern           string
	Owner             string // the type that declares it
	At                diag.Location
	Origin            Origin
}

// Origin identifies the declaration a fact came from, independently of the
// family or derived name under which a target is rendering it.
type Origin struct {
	Family, Declaration string
	At                  diag.Location
}

// Variant is a union arm, with its declaration and resolved wire shape.
type Variant struct {
	model.Variant
	Origin       Origin
	Form         VariantForm
	Fields       []Field // fields of a resolved, non-null record/entity payload, inside value
	Payload      *Type   // resolved record/entity payload, nil for other expressions
	DeclaredType model.TypeExpr
	Scope        []model.Parameter
	Arguments    []Argument // the declaring union's effective arguments for this inherited variant
}

// VariantForm says how a payload sits beside the discriminator.
type VariantForm string

const (
	VariantValue VariantForm = "value" // complete payload under the union's value member
	VariantEmpty VariantForm = "empty" // tag alone, with no value member
)

// Side is one peer's interface.
type Side struct {
	Extends    []model.Inheritance // the families whose same side this one extends
	Methods    []Method
	Events     []Event
	OwnMethods []Method
	OwnEvents  []Event
	At         diag.Location
}

// Method is one operation, with what it is generic in.
type Method struct {
	Name, Description string
	Request           model.TypeExpr // nil: takes nothing
	Result            model.TypeExpr
	Errors            []string
	Uses              []Use
	At                diag.Location
	Origin            Origin
	Declaration       *model.Method // original expressions in Origin.Family's scope
	BoundDeclaration  *model.Method // substituted expressions, preserving references
	Scope             []model.Parameter
}

// Event is one notification.
type Event struct {
	Name, Description string
	Type              model.TypeExpr
	Uses              []Use
	At                diag.Location
	Origin            Origin
	Declaration       *model.Event // original expression in Origin.Family's scope
	BoundDeclaration  *model.Event // substituted expression, preserving references
	Scope             []model.Parameter
}

// Error is one public error.
type Error struct {
	Code, Description string
	At                diag.Location
	Origin            Origin
}

// World is every family of a checkout, ready to render, by name — what a
// target that renders the checkout as a whole sees.
type World struct {
	Families []*Family
}

// Build renders the facts of a family that passed every neutral check.
func Build(f *analysis.Family) *Family {
	return (&builder{families: map[*analysis.Family]*Family{}}).build(f)
}

func (b *builder) build(f *analysis.Family) *Family {
	if r := b.families[f]; r != nil {
		return r
	}
	r := &Family{Name: f.Name, Source: f.Source, Builtin: f.IsBuiltin(), Files: f.Files, overrides: map[string]model.Overrides{}, f: f, types: map[string]*Type{}, builder: b, inlines: map[*model.Type]*Type{}}
	b.families[f] = r
	for _, p := range f.Parameters() {
		r.Parameters = append(r.Parameters, Parameter{Name: p.Name, Of: p.Of, Description: p.Description, At: p.At})
	}
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		rt := &Type{Name: name, Kind: t.Kind, Description: t.Description, Key: t.Key, Parameters: t.Parameters, Open: t.Open, Values: t.Values, Alias: t.Alias, Tag: t.Tag, Value: t.ValueMember(), Carried: f.IsCarried(name), From: f.CarriedFrom(name), At: t.At}
		rt.Declaration, rt.Extends, rt.Scope = t, t.Extends, append(append([]model.Parameter{}, f.Parameters()...), t.Parameters...)
		rt.Origin = Origin{Family: f.Name, Declaration: name, At: t.At}
		if rt.Carried {
			rt.Origin.Family = rt.From
		}
		f.WalkFields(name, func(at analysis.FieldAt) {
			rt.Fields = append(rt.Fields, field(at.Owner, at.Field))
		})
		for _, own := range t.Fields {
			rt.Own = append(rt.Own, field(name, own))
		}
		r.Types = append(r.Types, rt)
		r.types[name] = rt
	}
	r.References = f.References()
	r.Carries = f.Carries
	r.Wire = wire(f)
	r.Declaration = CanonicalDeclaration(f)
	digest := sha256.Sum256([]byte(r.Declaration))
	r.WireDigest = hex.EncodeToString(digest[:])
	for target, raw := range f.Overrides {
		if raw == nil {
			continue
		}
		if o, err := model.DecodeOverrides(model.OverrideFile(target), raw); err == nil {
			r.overrides[target] = o
		}
	}
	r.complete()
	return r
}

func field(owner string, m model.Field) Field {
	return Field{Name: m.Name, Description: m.Description, Type: m.Type, Required: m.Required, Nullable: m.Nullable, Unique: m.Unique, Min: m.Min, Max: m.Max, Length: m.Length, Pattern: m.Pattern, Owner: owner, At: m.At}
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

// IsParameter reports whether a plain name in an expression names a
// parameter of the family rather than a type of it.
func (r *Family) IsParameter(name string) bool { return r.f.HasParameter(name) }

// HasProtocol reports whether the family has a protocol tier.
func (r *Family) HasProtocol() bool { return r.f.Protocol != nil }

// UsesOf is what an expression is generic in.
func (r *Family) UsesOf(e model.TypeExpr) []Use { return r.uses(e, r.f.Parameters()) }

// ImportedUses is what a type of an imported family is generic in, in that
// family's own parameters.
func (r *Family) ImportedUses(family, typeName string) []Use {
	if other := r.other(family); other != nil {
		if t := other.Type(typeName); t != nil {
			return t.Uses
		}
	}
	return nil
}

// Argument fills one use of an applied declaration's parameter. Type fills
// a type slot; Parameter or Family fills a family slot. Exactly one is set.
type Argument struct {
	Use       Use            // the imported type's parameter, at the type drawn
	Parameter string         // a family parameter of this family
	Family    string         // a named family
	Type      model.TypeExpr // the expression filling a type parameter
	Slot      model.Parameter
}

// Arguments resolves the expressions and families filling an applied
// type's parameter uses, in declaration order.
func (r *Family) Arguments(a model.Apply) []Argument {
	return r.arguments(a)
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
		t := f.Types[name]
		if t.IsCallable() {
			// The identity a reference to this callable carries. It is
			// stamped here, once, so that a validator in any language reads
			// it rather than deriving it — the one place the nominal
			// identity is computed, and the reason no second spelling of it
			// has to be kept in step across languages.
			stamped := *t
			stamped.Contract = Contract(f.CarriedFrom(name), f.Name, name)
			t = &stamped
		}
		types[name] = t
	}
	data, _ := json.Marshal(struct {
		Types      map[string]*model.Type `json:"types"`
		Parameters []model.Parameter      `json:"parameters,omitempty"`
	}{types, f.Parameters()})
	return string(data)
}

// Form is one form of the declaration language a family uses, named in the
// words a diagnostic once named it in and located where the family writes it.
type Form struct {
	Name string
	At   diag.Location
}

// FormsUsed is every form of the declaration language the family uses, each
// named once, in the order they are met. Every target renders every form, so
// this is no longer what a target owes: it is the inventory the proof family
// is held to, so that dropping a form from that declaration cannot silently
// leave the form unproved.
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
		default:
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
		if t.Kind == model.KindCallable {
			add("a callable, whose values are exported and imported at the boundary", t.At)
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
		expression(t.Request, t.At.Sub("request"))
		expression(t.Result, t.At.Sub("result"))
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

// IsLive reports whether values of an expression carry a callable, in this
// family's scope: what a target asks to know whether a position needs
// conversion at the boundary.
func (r *Family) IsLive(e model.TypeExpr) bool { return r.f.IsLive(e) }

// Contract is the identity a reference to a callable carries: the
// declaration that a value of the type implements, as `family/Type`.
//
// It is **nominal** — the operator's verdict on #201. A reference is
// usable exactly where the callable it was declared as is expected, so a
// `setVolume` reference cannot arrive where a `report` is expected even
// though both take an integer and answer nothing. Every callable has a
// declaration site, which is why its nominal name needs no structural
// fingerprint. WireDigest identifies the separately carried revision.
func Contract(declaringFamily, family, name string) string {
	if declaringFamily != "" {
		family = declaringFamily
	}
	return family + "/" + name
}
