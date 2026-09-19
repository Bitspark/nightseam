// Package analysis derives the facts about a family that checks and
// renderings read, within the world it is rendered in: the families it
// imports, resolved; the session families a parameter may bind; the two
// types every family with a protocol carries; each declaration's tier; the
// fields a record carries through inheritance; and what is generic in the
// family — which parameter each type draws on, at which of the bound
// family's types. It reports nothing: a fact that does not hold is left for
// check to say.
package analysis

import (
	"slices"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// World is every family of a checkout, by name.
type World map[string]*model.Family

// Family is one family with the facts of its world.
type Family struct {
	*model.Family
	Types    map[string]*model.Type // the declared types with the two injected ones, when there is a protocol
	Families []string               // every family of the world, sorted
	Sessions []string               // the families with a session tier, sorted, this one among them if it has one
	Imported map[string]*Family     // the imported families the world has, resolved in turn
	Members  map[string]*Family     // the session families other than this one: what a parameter may bind
	world    World
	generics *Generics
}

// Resolve gives a family its world. An import the world lacks is absent
// from Imported and reported by check; a family importing itself likewise.
func Resolve(world World, name string) *Family {
	return resolve(world, name, map[string]*Family{})
}

func resolve(world World, name string, resolved map[string]*Family) *Family {
	if f, done := resolved[name]; done {
		return f
	}
	m := world[name]
	if m == nil {
		return nil
	}
	f := &Family{Family: m, Types: map[string]*model.Type{}, Imported: map[string]*Family{}, Members: map[string]*Family{}, world: world}
	resolved[name] = f
	for typeName, t := range m.Types {
		f.Types[typeName] = t
	}
	if m.Protocol != nil {
		for typeName, t := range model.Injected() {
			if _, declared := f.Types[typeName]; !declared {
				f.Types[typeName] = t
			}
		}
	}
	for other, om := range world {
		f.Families = append(f.Families, other)
		if om.Session != nil {
			f.Sessions = append(f.Sessions, other)
		}
	}
	sort.Strings(f.Families)
	sort.Strings(f.Sessions)
	for _, other := range f.Sessions {
		if other != name {
			f.Members[other] = resolve(world, other, resolved)
		}
	}
	for _, imported := range m.Imports {
		if imported != name && world[imported] != nil {
			f.Imported[imported] = resolve(world, imported, resolved)
		}
	}
	return f
}

// TypeNames is every type name, injected ones included, in byte order.
func (f *Family) TypeNames() []string {
	names := make([]string, 0, len(f.Types))
	for name := range f.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Rank is the tier a type is declared in: the rank of its file, the
// protocol's for an injected type.
func (f *Family) Rank(typeName string) int {
	if t, ok := f.Types[typeName]; ok {
		return model.Rank(t.At.File)
	}
	return -1
}

// Parameters is the family's parameters, none without a protocol.
func (f *Family) Parameters() []model.Parameter {
	if f.Protocol == nil {
		return nil
	}
	return f.Protocol.Parameters
}

// HasParameter reports whether the family declares a parameter.
func (f *Family) HasParameter(name string) bool {
	for _, p := range f.Parameters() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// FieldAt is one field of a record as inheritance reaches it: the record
// that declares it and its index there.
type FieldAt struct {
	Owner string
	Index int
	Field model.Field
}

// WalkFields visits a record's fields in wire order: each inherited
// record's fields first, in extends order and depth first, then its own. It
// walks each inheritance path separately, so a diamond visits its shared
// fields twice, as the wire would carry them twice; a cycle is cut where it
// closes, and a parent that is not a record is skipped, so the walk is safe
// on any family the loader accepts.
func (f *Family) WalkFields(name string, visit func(FieldAt)) {
	var walk func(string, map[string]bool)
	walk = func(current string, stack map[string]bool) {
		if stack[current] {
			return
		}
		stack[current] = true
		defer delete(stack, current)
		t, ok := f.Types[current]
		if !ok {
			return
		}
		for _, parent := range t.Extends {
			if p, ok := f.Types[parent]; ok && isRecord(p) {
				walk(parent, stack)
			}
		}
		for i, field := range t.Fields {
			visit(FieldAt{current, i, field})
		}
	}
	walk(name, map[string]bool{})
}

// FlattenedFields is a record's fields in wire order, inherited first.
func (f *Family) FlattenedFields(name string) []model.Field {
	var fields []model.Field
	f.WalkFields(name, func(at FieldAt) { fields = append(fields, at.Field) })
	return fields
}

func isRecord(t *model.Type) bool { return t.Kind == model.KindRecord || t.Kind == model.KindEntity }

// IsObject reports whether an expression is an object on the wire, which a
// method's request must be: a record or entity of this family or of an
// imported one, a type drawn from a parameter that every member declares
// as a record, or an application of an imported record.
func (f *Family) IsObject(e model.TypeExpr) bool {
	switch x := e.(type) {
	case model.Named:
		t, ok := f.Types[x.Name]
		return ok && isRecord(t)
	case model.Imported:
		other, ok := f.Imported[x.Family]
		if !ok {
			return false
		}
		t, ok := other.Types[x.Name]
		return ok && isRecord(t)
	case model.Drawn:
		if model.IsInjected(x.Name) {
			return true
		}
		for _, member := range f.Members {
			if t, ok := member.Types[x.Name]; !ok || !isRecord(t) {
				return false
			}
		}
		return true
	case model.Apply:
		other, ok := f.Imported[x.Family]
		if !ok {
			return false
		}
		t, ok := other.Types[x.Name]
		return ok && isRecord(t)
	}
	return false
}

// References names every family whose generated package this one's refers
// to: the families it imports, since every family a declaration names —
// as a type's family, as what fills a parameter — is imported.
func (f *Family) References() []string {
	return slices.Clone(f.Imports)
}

// Use is one parameter drawn at one type: the pair a language turns into
// a type parameter. A family generic in S and T, where S is drawn at its
// Envelope and its Handle and T at its Envelope, has the uses {S,Envelope},
// {S,Handle}, {T,Envelope} — in that order, by the parameter's declaration
// and then by drawnBefore.
type Use struct {
	Parameter string
	Type      string
}

// drawnBefore orders the types a parameter is drawn at, which is the order
// a language declares the type parameters they become: Envelope, Handle,
// then the rest by name.
func drawnBefore(a, b string) bool {
	rank := func(name string) int {
		switch name {
		case model.EnvelopeType:
			return 0
		case model.HandleType:
			return 1
		}
		return 2
	}
	if rank(a) != rank(b) {
		return rank(a) < rank(b)
	}
	return a < b
}

// Generics is what makes a family generic. A type drawn from a parameter
// is filled where the generated code is instantiated, so a type that holds
// one — directly, or through the types it refers to, its own family's or
// an imported one's — is generic in that parameter, and so is a family
// with such a type. Types maps each generic type to the uses it makes, in
// parameter order; Family is the union over every type and operation;
// Imported is Types of each imported family, with the imported family's
// parameters renamed to the parameter of this one that fills them.
type Generics struct {
	Types    map[string][]Use
	Family   []Use
	Imported map[string]map[string][]Use
}

// Generic reports whether the family is generic at all.
func (g Generics) Generic() bool { return len(g.Family) > 0 }

// Generics computes what is generic in the family, once.
func (f *Family) Generics() Generics {
	if f.generics != nil {
		return *f.generics
	}
	g := Generics{Types: map[string][]Use{}, Imported: map[string]map[string][]Use{}}
	parameters := f.Parameters()
	order := map[string]int{}
	for i, parameter := range parameters {
		order[parameter.Name] = i
	}
	// An imported family's parameters are not this family's, so an
	// application says what fills each: through a parameter of this family,
	// which is a use of it, or through a named family, which is not. A
	// family with one parameter may refer to a generic imported type plainly
	// and fill every parameter of it with that one; check refuses the plain
	// reference from a family with any other number.
	rename := func(uses []Use, with map[string]model.Filler) []Use {
		var out []Use
		for _, use := range uses {
			filler, bound := with[use.Parameter]
			if !bound {
				if len(parameters) != 1 {
					continue
				}
				filler = model.Filler{Parameter: parameters[0].Name}
			}
			if filler.Parameter == "" {
				continue
			}
			if renamed := (Use{filler.Parameter, use.Type}); !slices.Contains(out, renamed) {
				out = append(out, renamed)
			}
		}
		return out
	}
	for name, other := range f.Imported {
		g.Imported[name] = other.Generics().Types
	}
	union := func(sets ...[]Use) []Use {
		var out []Use
		for _, set := range sets {
			for _, use := range set {
				if !slices.Contains(out, use) {
					out = append(out, use)
				}
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Parameter != out[j].Parameter {
				return order[out[i].Parameter] < order[out[j].Parameter]
			}
			return drawnBefore(out[i].Type, out[j].Type)
		})
		return out
	}
	// usesOf is the uses an expression makes, given the uses known of every
	// type so far; the fixpoint below reaches the types through references.
	var usesOf func(model.TypeExpr) []Use
	usesOf = func(e model.TypeExpr) []Use {
		switch x := e.(type) {
		case model.Drawn:
			if _, declared := order[x.Parameter]; declared {
				return []Use{{x.Parameter, x.Name}}
			}
		case model.Apply:
			return rename(g.Imported[x.Family][x.Name], x.With)
		case model.Imported:
			return rename(g.Imported[x.Family][x.Name], nil)
		case model.Named:
			return g.Types[x.Name]
		case model.Array:
			return usesOf(x.Elem)
		case model.Map:
			return usesOf(x.Elem)
		}
		return nil
	}
	for changed := true; changed; {
		changed = false
		for _, name := range f.TypeNames() {
			t := f.Types[name]
			var sets [][]Use
			if isRecord(t) {
				for _, field := range f.FlattenedFields(name) {
					sets = append(sets, usesOf(field.Type))
				}
			}
			if t.Alias != nil {
				sets = append(sets, usesOf(t.Alias))
			}
			if uses := union(sets...); len(uses) > len(g.Types[name]) {
				g.Types[name] = uses
				changed = true
			}
		}
	}
	var sets [][]Use
	for _, uses := range g.Types {
		sets = append(sets, uses)
	}
	if f.Protocol != nil {
		for _, side := range []*model.Side{&f.Protocol.Server, &f.Protocol.Client} {
			for _, m := range side.Methods {
				sets = append(sets, usesOf(m.Request), usesOf(m.Result))
			}
			for _, e := range side.Events {
				sets = append(sets, usesOf(e.Type))
			}
		}
	}
	g.Family = union(sets...)
	f.generics = &g
	return g
}

// UsesOf is the uses one expression makes, once the family's generics are
// known: what a rendering declares a field or an operation generic in.
func (f *Family) UsesOf(e model.TypeExpr) []Use {
	g := f.Generics()
	switch x := e.(type) {
	case model.Drawn:
		if f.HasParameter(x.Parameter) {
			return []Use{{x.Parameter, x.Name}}
		}
	case model.Apply:
		return f.renamed(g.Imported[x.Family][x.Name], x.With)
	case model.Imported:
		return f.renamed(g.Imported[x.Family][x.Name], nil)
	case model.Named:
		return g.Types[x.Name]
	case model.Array:
		return f.UsesOf(x.Elem)
	case model.Map:
		return f.UsesOf(x.Elem)
	}
	return nil
}

func (f *Family) renamed(uses []Use, with map[string]model.Filler) []Use {
	parameters := f.Parameters()
	var out []Use
	for _, use := range uses {
		filler, bound := with[use.Parameter]
		if !bound {
			if len(parameters) != 1 {
				continue
			}
			filler = model.Filler{Parameter: parameters[0].Name}
		}
		if filler.Parameter != "" {
			if renamed := (Use{filler.Parameter, use.Type}); !slices.Contains(out, renamed) {
				out = append(out, renamed)
			}
		}
	}
	return out
}

// Locate answers an override's path key with the declaration it names:
// Type, Type.field, Enum.value, a method or an event of either side, or
// errors.code. A name both sides declare is ambiguous and not found.
func (f *Family) Locate(key string) (diag.Location, bool) {
	if strings.HasPrefix(key, "errors.") {
		if f.Protocol != nil {
			if e, ok := f.Protocol.Error(strings.TrimPrefix(key, "errors.")); ok {
				return e.At, true
			}
		}
		return diag.Location{}, false
	}
	if typeName, member, ok := strings.Cut(key, "."); ok && model.IsParameter(typeName) {
		t, declared := f.Types[typeName]
		if !declared || model.IsInjected(typeName) {
			return diag.Location{}, false
		}
		for _, field := range t.Fields {
			if field.Name == member {
				return field.At, true
			}
		}
		for i, value := range t.Values {
			if value == member {
				return t.At.Sub("values", i), true
			}
		}
		return diag.Location{}, false
	}
	if t, ok := f.Types[key]; ok && !model.IsInjected(key) {
		return t.At, true
	}
	if f.Protocol == nil {
		return diag.Location{}, false
	}
	var found []diag.Location
	for _, side := range []*model.Side{&f.Protocol.Server, &f.Protocol.Client} {
		for _, m := range side.Methods {
			if m.Name == key {
				found = append(found, m.At)
			}
		}
		for _, e := range side.Events {
			if e.Name == key {
				found = append(found, e.At)
			}
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return diag.Location{}, false
}
