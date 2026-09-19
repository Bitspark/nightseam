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
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/naming"
)

// World is every family of a checkout, by name.
type World map[string]*model.Family

// Family is one family with the facts of its world.
type Family struct {
	*model.Family
	Types    map[string]*model.Type // the declared types with the carried ones of every built-in the tiers bring
	Families []string               // every family of the world, sorted
	Sessions []string               // the families with a session tier, sorted, this one among them if it has one
	Imported map[string]*Family     // the imported families the world has, resolved in turn
	Members  map[string]*Family     // the session families other than this one: what a parameter may bind
	Carries  []string               // the built-in families the tiers bring, sorted
	carried  map[string]int         // a carried type's name, at the rank of the tier that carries it
	from     map[string]string      // a carried type's name, at the built-in family that declares it
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
	f := &Family{Family: m, Types: map[string]*model.Type{}, Imported: map[string]*Family{}, Members: map[string]*Family{}, carried: map[string]int{}, from: map[string]string{}, world: world}
	resolved[name] = f
	for typeName, t := range m.Types {
		f.Types[typeName] = t
	}
	// What a tier brings: the built-in family it names, imported with no
	// imports line. Where the built-in is carried, its types are this
	// family's own — a family's envelope is a message of that family — and
	// they rank at the tier that carries them, not at the tier the built-in
	// declares them in.
	for _, tier := range model.Tiers {
		if tier.Builtin == "" || !m.Has(tier.File) {
			continue
		}
		b, ok := builtin.Family(tier.Builtin)
		if !ok {
			continue
		}
		f.Carries = append(f.Carries, tier.Builtin)
		if !tier.Carries {
			continue
		}
		for typeName, t := range b.Types {
			if _, declared := f.Types[typeName]; !declared {
				f.Types[typeName] = t
				f.carried[typeName] = tier.Rank
				f.from[typeName] = tier.Builtin
			}
		}
	}
	sort.Strings(f.Carries)
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

// IsSession reports whether the family carries the session role.
func (f *Family) IsSession() bool { return f.Session != nil }

// TypeNames is every type name, injected ones included, in byte order.
func (f *Family) TypeNames() []string {
	names := make([]string, 0, len(f.Types))
	for name := range f.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Rank is the tier a type is declared in: the rank of its file, and for a
// type carried from a built-in the rank of the tier that carries it.
func (f *Family) Rank(typeName string) int {
	if rank, carried := f.carried[typeName]; carried {
		return rank
	}
	if t, ok := f.Types[typeName]; ok {
		return model.Rank(t.At.File)
	}
	return -1
}

// IsCarried reports whether a type of this family is one it carries from a
// built-in rather than one it declares.
func (f *Family) IsCarried(typeName string) bool {
	_, carried := f.carried[typeName]
	return carried
}

// CarriedFrom is the built-in family a carried type is declared by, empty
// for a type this family declares.
func (f *Family) CarriedFrom(typeName string) string { return f.from[typeName] }

// Parameters is the family's parameters, none without a protocol.
func (f *Family) Parameters() []model.Parameter {
	if f.Protocol == nil {
		return nil
	}
	return f.Protocol.Parameters
}

// HasParameter reports whether the family declares a parameter.
func (f *Family) HasParameter(name string) bool {
	_, ok := f.Parameter(name)
	return ok
}

// Parameter finds a parameter of the family by name.
func (f *Family) Parameter(name string) (model.Parameter, bool) {
	for _, p := range f.Parameters() {
		if p.Name == name {
			return p, true
		}
	}
	return model.Parameter{}, false
}

// FamilyParameters are the family's parameters filled by a family, in
// declaration order: the ones a type is drawn through, and the only ones a
// use names.
func (f *Family) FamilyParameters() []model.Parameter {
	var out []model.Parameter
	for _, p := range f.Parameters() {
		if p.IsFamily() {
			out = append(out, p)
		}
	}
	return out
}

// HasFamilyParameter reports whether the family declares a parameter of
// that name filled by a family.
func (f *Family) HasFamilyParameter(name string) bool {
	p, ok := f.Parameter(name)
	return ok && p.IsFamily()
}

// fills is the family parameter of this family a filler names, empty when
// it fills the slot with anything else.
func (f *Family) fills(filler model.Filler) string {
	if name := filler.Name(); name != "" && f.HasFamilyParameter(name) {
		return name
	}
	return ""
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
		return ok && (isRecord(t) || t.Kind == model.KindUnion)
	case model.Imported:
		other, ok := f.Imported[x.Family]
		if !ok {
			return false
		}
		t, ok := other.Types[x.Name]
		return ok && (isRecord(t) || t.Kind == model.KindUnion)
	case model.Drawn:
		if model.Carried(x.Name) {
			return true
		}
		for _, member := range f.Members {
			if t, ok := member.Types[x.Name]; !ok || !isRecord(t) {
				return false
			}
		}
		return true
	case model.Apply:
		t, ok := f.Applied(x)
		return ok && (isRecord(t) || t.Kind == model.KindUnion)
	case model.Inline:
		return isRecord(x.Type) || x.Type.Kind == model.KindUnion
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
// and then by DrawnBefore.
type Use struct {
	Parameter string
	Type      string
}

// DrawnBefore orders the types a parameter is drawn at, which is the order
// a language declares the type parameters they become: Envelope, Handle,
// then the rest by name.
func DrawnBefore(a, b string) bool {
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
	parameters := f.FamilyParameters()
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
			fills := f.fills(filler)
			if !bound {
				if len(parameters) != 1 {
					continue
				}
				fills = parameters[0].Name
			}
			if fills == "" {
				continue
			}
			if renamed := (Use{fills, use.Type}); !slices.Contains(out, renamed) {
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
			return DrawnBefore(out[i].Type, out[j].Type)
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
			if x.Family == "" {
				return rename(g.Types[x.Name], x.With)
			}
			return rename(g.Imported[x.Family][x.Name], x.With)
		case model.Imported:
			return rename(g.Imported[x.Family][x.Name], nil)
		case model.Named:
			return g.Types[x.Name]
		case model.Array:
			return usesOf(x.Elem)
		case model.Map:
			return usesOf(x.Elem)
		case model.Nullable:
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
			for _, variant := range t.Variants {
				sets = append(sets, usesOf(variant.Type))
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
		if x.Family == "" {
			return f.renamed(g.Types[x.Name], x.With)
		}
		return f.renamed(g.Imported[x.Family][x.Name], x.With)
	case model.Imported:
		return f.renamed(g.Imported[x.Family][x.Name], nil)
	case model.Named:
		return g.Types[x.Name]
	case model.Array:
		return f.UsesOf(x.Elem)
	case model.Map:
		return f.UsesOf(x.Elem)
	case model.Nullable:
		return f.UsesOf(x.Elem)
	}
	return nil
}

func (f *Family) renamed(uses []Use, with map[string]model.Filler) []Use {
	parameters := f.FamilyParameters()
	var out []Use
	for _, use := range uses {
		filler, bound := with[use.Parameter]
		fills := f.fills(filler)
		if !bound {
			if len(parameters) != 1 {
				continue
			}
			fills = parameters[0].Name
		}
		if fills != "" {
			if renamed := (Use{fills, use.Type}); !slices.Contains(out, renamed) {
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
		if !declared || model.Carried(typeName) {
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
	if t, ok := f.Types[key]; ok && !model.Carried(key) {
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

// Builtin is a built-in family by name: a family the world always holds,
// which a tier brings to a family that has it.
func (f *Family) Builtin(name string) (*model.Family, bool) { return builtin.Family(name) }

// Carriers are the families other than this one that carry a tier — what a
// family parameter of that tier may bind — by name.
func (f *Family) Carriers(role string) map[string]*Family {
	tier, ok := tierOfRole(role)
	if !ok {
		return nil
	}
	out := map[string]*Family{}
	for name, m := range f.world {
		if name == f.Name || !m.Has(tier.File) {
			continue
		}
		out[name] = resolve(f.world, name, map[string]*Family{})
	}
	return out
}

func tierOfRole(role string) (model.Tier, bool) {
	for _, tier := range model.Tiers {
		if tier.Name == role {
			return tier, true
		}
	}
	return model.Tier{}, false
}

// Shape is the record, entity or union a type expression denotes, with the
// family it belongs to: what a union's variant carries, once resolved.
// Anything that is not one of the three is not a shape.
func (f *Family) Shape(e model.TypeExpr) (*model.Type, bool) {
	switch x := e.(type) {
	case model.Named:
		t, ok := f.Types[x.Name]
		return t, ok && isShape(t)
	case model.Imported:
		other, ok := f.Imported[x.Family]
		if !ok {
			return nil, false
		}
		t, ok := other.Types[x.Name]
		return t, ok && isShape(t)
	case model.Inline:
		return x.Type, isShape(x.Type)
	case model.Apply:
		t, ok := f.Applied(x)
		return t, ok && isShape(t)
	}
	return nil, false
}

func isShape(t *model.Type) bool {
	return t != nil && (t.Kind == model.KindRecord || t.Kind == model.KindEntity || t.Kind == model.KindUnion)
}

// Applied is the type an application names, of this family or of an
// imported one.
func (f *Family) Applied(x model.Apply) (*model.Type, bool) {
	if x.Family == "" {
		t, ok := f.Types[x.Name]
		return t, ok
	}
	other, ok := f.Imported[x.Family]
	if !ok {
		return nil, false
	}
	t, ok := other.Types[x.Name]
	return t, ok
}

// ShapeFields is a shape's fields in wire order, inherited first, for a
// type that may be written inline and so have no name in the family.
func (f *Family) ShapeFields(t *model.Type) []model.Field {
	if t.Name != "" {
		if _, declared := f.Types[t.Name]; declared {
			return f.FlattenedFields(t.Name)
		}
	}
	var fields []model.Field
	for _, parent := range t.Extends {
		fields = append(fields, f.FlattenedFields(parent)...)
	}
	return append(fields, t.Fields...)
}

// VariantTags is every tag a union carries, its own and every one it
// extends, sorted.
func (f *Family) VariantTags(name string) []string {
	seen := map[string]bool{}
	var walk func(string, map[string]bool)
	walk = func(current string, stack map[string]bool) {
		if stack[current] {
			return
		}
		stack[current] = true
		defer delete(stack, current)
		t, ok := f.Types[current]
		if !ok || t.Kind != model.KindUnion {
			return
		}
		for _, parent := range t.Extends {
			walk(parent, stack)
		}
		for _, variant := range t.Variants {
			seen[variant.Tag] = true
		}
	}
	walk(name, map[string]bool{})
	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// Inline is one shape written inline: the type as it was declared, the name
// the generator derives from the path to it, the path itself for a
// diagnostic, and where it sits.
type Inline struct {
	Type *model.Type
	Name string
	Path []string
	At   diag.Location
}

// Inlines is every shape the family writes inline, in the order the
// declaration reaches them — its types in byte order, then the server side
// and the client side, methods before events — each with the name derived
// from the path to it. A target renders these as types of the family; check
// holds their names apart from the declared ones.
func (f *Family) Inlines() []Inline {
	var out []Inline
	var walk func(model.TypeExpr, []string, diag.Location)
	walk = func(e model.TypeExpr, path []string, at diag.Location) {
		switch x := e.(type) {
		case model.Array:
			walk(x.Elem, path, at)
		case model.Map:
			walk(x.Elem, path, at)
		case model.Nullable:
			walk(x.Elem, path, at)
		case model.Inline:
			out = append(out, Inline{Type: x.Type, Name: naming.Derived(path...), Path: path, At: at})
			for i := range x.Type.Fields {
				walk(x.Type.Fields[i].Type, append(append([]string{}, path...), x.Type.Fields[i].Name), at.Sub("fields", i, "type"))
			}
			for i := range x.Type.Variants {
				walk(x.Type.Variants[i].Type, append(append([]string{}, path...), x.Type.Variants[i].Tag), at.Sub("variants", x.Type.Variants[i].Tag))
			}
		}
	}
	for _, name := range f.TypeNames() {
		if f.IsCarried(name) {
			continue
		}
		t := f.Types[name]
		for i := range t.Fields {
			walk(t.Fields[i].Type, []string{name, t.Fields[i].Name}, t.At.Sub("fields", i, "type"))
		}
		for i := range t.Variants {
			walk(t.Variants[i].Type, []string{name, t.Variants[i].Tag}, t.At.Sub("variants", t.Variants[i].Tag))
		}
	}
	if f.Protocol == nil {
		return out
	}
	for _, side := range []*model.Side{&f.Protocol.Server, &f.Protocol.Client} {
		for i := range side.Methods {
			m := &side.Methods[i]
			walk(m.Request, []string{m.Name, "request"}, m.At.Sub("request"))
			walk(m.Result, []string{m.Name, "result"}, m.At.Sub("result"))
		}
		for i := range side.Events {
			e := &side.Events[i]
			walk(e.Type, []string{e.Name, "event"}, e.At.Sub("type"))
		}
	}
	return out
}
