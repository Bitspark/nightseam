// Package check holds the rules a family is held to before any target sees
// it: one function per tier over the facts analysis derived, and one over
// the override files. Each reports every problem it finds, located by tier
// file and pointer, and none renders anything. The concerns table pairs
// each concern with its checker, so that the kernel runs them in tier order
// without naming one.
package check

import (
	"reflect"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// concern pairs a tier with the checker that holds a family to its rules.
type concern struct {
	Name  string
	File  string
	Check func(*analysis.Family) []diag.Diagnostic
}

// concerns are the checkers of the tiers above the model, in tier order.
var concerns = []concern{
	{Name: "protocol", File: model.ProtocolFile, Check: Protocol},
	{Name: "live", File: model.LiveFile, Check: Live},
}

// Family runs every check the family's tiers call for, in tier order, and
// the override files' check, and sorts what they say.
func Family(f *analysis.Family) []diag.Diagnostic {
	diagnostics := Model(f)
	for _, concern := range concerns {
		if f.Has(concern.File) {
			diagnostics = append(diagnostics, concern.Check(f)...)
		}
	}
	diagnostics = append(diagnostics, Overrides(f)...)
	diag.Sort(diagnostics)
	return diagnostics
}

// checker carries what every check shares.
type checker struct {
	f *analysis.Family
	diag.List
}

func newChecker(f *analysis.Family) *checker {
	return &checker{f: f, List: diag.List{Family: f.Name}}
}

// site is where one type expression sits: the tier it must not reach above,
// the type it is part of for the value graph, the parameters in scope
// there, and whether a shape may be written inline at it.
type site struct {
	owner   string
	context int
	scope   []model.Parameter
	edges   map[string][]string
	inline  bool
}

// Model holds the family to the rules of its types, whatever tier declares
// them: every import resolves and none cycles; every name a type refers to
// resolves, within its tier or below; inheritance is of records and carries
// no field twice; a union declares a discriminator and its variants agree
// about it; no value contains itself; an entity's key is one of its
// primitive fields; a constraint fits its field's type, a pattern the
// dialect.
func Model(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	c.imports()
	edges := map[string][]string{}
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		if f.IsCarried(name) {
			continue
		}
		if from := f.DeclaredByBuiltin(name); from != "" {
			c.Addf(t.At, "reserved_name", "Type %s is carried from the built-in %s family by every family with that tier, and may not be declared; name it %s.%s.", name, from, from, name)
			continue
		}
		context := f.Rank(name)
		scope := c.parameters(t.Parameters, name, context)
		c.type_(t, name, context, scope, edges)
	}
	c.cycles(edges)
	c.fieldCollisions()
	c.derivedNames()
	return c.Diagnostics
}

// derivedNames: a shape written inline is generated under a name derived
// from the path to it, and that name is a type of the family like any
// other — so it may not be one the family declares, and two shapes may not
// derive the same one. The path is in the diagnostic, since a shape with no
// name has nothing else to be pointed at by.
func (c *checker) derivedNames() {
	seen := map[string][]string{}
	for _, inline := range c.f.Inlines() {
		path := strings.Join(inline.Path, "/")
		switch {
		case c.f.Types[inline.Name] != nil:
			c.Addf(inline.At, "derived_collision", "The shape at %s is generated under the name %s, which this family declares as a type of its own; name the shape, or rename the type.", path, inline.Name)
		case seen[inline.Name] != nil:
			c.Addf(inline.At, "derived_collision", "The shape at %s is generated under the name %s, which the shape at %s already derives; name one of them.", path, inline.Name, strings.Join(seen[inline.Name], "/"))
		default:
			seen[inline.Name] = inline.Path
		}
	}
}

// type_ holds one type — declared or written inline — to the rules of its
// kind.
func (c *checker) type_(t *model.Type, name string, context int, scope []model.Parameter, edges map[string][]string) {
	where := site{owner: name, context: context, scope: scope, edges: edges, inline: true}
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		for i, parent := range t.Extends {
			c.inherits(t, i, parent, where, model.KindRecord)
		}
		for i := range t.Fields {
			field := &t.Fields[i]
			c.expression(field.Type, field.At.Sub("type"), where)
			c.constraints(field, t)
		}
		if t.Kind == model.KindEntity {
			c.key(t)
		}
	case model.KindUnion:
		for i, parent := range t.Extends {
			c.inherits(t, i, parent, where, model.KindUnion)
		}
		c.union(t, where)
	case model.KindAlias:
		c.expression(t.Alias, t.At.Sub("type"), site{owner: name, context: context, scope: scope, edges: edges})
	case model.KindCallable:
		c.callable(t, where)
	}
}

// callable holds the live tier's own kind: it is declared in the live tier
// and nowhere else, it takes no parameters of its own, and its request and
// result are ordinary type expressions of its tier or below.
//
// The tier rule then does the rest of the work with no machinery of its
// own. A callable is declared in live.json, so its rank is the live tier's,
// so a record of model.json or an operation of protocol.json that names one
// is already a tier_violation — which is why ordinary data and RPC stay
// usable with no live registry anywhere, and why that independence is a
// consequence of the language rather than a promise about it.
func (c *checker) callable(t *model.Type, where site) {
	if t.At.File != model.LiveFile {
		c.Addf(t.At.Sub("kind"), "callable_tier", "Callable %s is declared in %s; a callable is the live tier's kind and is declared in %s, which is what keeps a value of a lower tier self-contained data.", t.Name, t.At.File, model.LiveFile)
	}
	if len(t.Parameters) > 0 {
		c.Addf(t.At.Sub("parameters", 0), "callable_parameters", "Callable %s declares parameters. A reference to a callable carries the identity of the declaration it implements, and a generic callable has one identity per application rather than one declaration; declare a callable for each filled shape until that is settled.", t.Name)
	}
	if t.Request != nil {
		c.expression(t.Request, t.At.Sub("request"), where)
	}
	if t.Result != nil {
		c.expression(t.Result, t.At.Sub("result"), where)
	}
	for i, code := range t.Errors {
		if c.f.Protocol == nil {
			continue
		}
		if _, declared := c.f.Protocol.Error(code); !declared {
			c.Addf(t.At.Sub("errors", i), "unknown_error", "Callable %s may return %s, which the family does not declare among its errors.", t.Name, code)
		}
	}
}

// union holds the adjacent envelope; a payload's own members cannot
// collide with it, including a literal with a different value or nullness.
func (c *checker) union(t *model.Type, where site) {
	c.inheritedVariantCollisions(t)
	if t.Tag == "" {
		c.Addf(t.At, "invalid_union", "Union %s declares no tag: the member that says which variant a value is.", t.Name)
	}
	if t.Tag != "" && t.Tag == t.ValueMember() {
		c.Addf(t.At.Sub("tag"), "invalid_union", "Union %s uses the same member for its discriminator and complete payload.", t.Name)
	}
	if len(t.Variants) == 0 && len(t.Extends) == 0 {
		c.Addf(t.At, "invalid_union", "Union %s declares no variants and extends nothing.", t.Name)
	}
	for _, variant := range t.Variants {
		if variant.Type != nil {
			c.expression(variant.Type, variant.At, where)
		}
	}
}

// parameters holds a declaration's parameters to their rules: distinct,
// not the name of a type, of a tier the tier model has or of no tier at
// all, and bindable.
func (c *checker) parameters(parameters []model.Parameter, owner string, context int) []model.Parameter {
	f := c.f
	seen := map[string]bool{}
	for _, parameter := range parameters {
		if seen[parameter.Name] {
			c.Addf(parameter.At.Sub("name"), "duplicate_parameter", "Parameter %s is declared twice.", parameter.Name)
		}
		seen[parameter.Name] = true
		if _, collides := f.Types[parameter.Name]; collides {
			c.Addf(parameter.At.Sub("name"), "reserved_name", "Parameter %s shares its name with a type of this family.", parameter.Name)
		}
		if owner != "" && f.HasParameter(parameter.Name) {
			c.Addf(parameter.At.Sub("name"), "duplicate_parameter", "Parameter %s of %s shares its name with a parameter of the family.", parameter.Name, owner)
		}
		switch {
		case parameter.Of == "":
			// A type parameter: filled by a type expression, no bound.
		case !model.IsTierRole(parameter.Of):
			c.Addf(parameter.At.Sub("of"), "unknown_role", "Unknown tier %s: a parameter is of a tier a family carries — %s — or of none, which makes it a type parameter.", parameter.Of, strings.Join(model.TierRoles(), ", "))
		case context >= 0 && context < model.Rank(model.ProtocolFile):
			c.Addf(parameter.At.Sub("of"), "tier_violation", "A %s declaration is generic in a family of the %s tier; a declaration refers to its own tier or a lower one.", model.TierName(context), parameter.Of)
		case len(f.Carriers(parameter.Of)) == 0:
			c.Addf(parameter.At.Sub("of"), "unresolved_type", "No other family carries the %s tier, so a parameter of %s has nothing to bind.", parameter.Of, parameter.Of)
		}
	}
	return parameters
}

// imports: a family imports neither itself nor a family the world lacks,
// and no chain of imports returns to it.
func (c *checker) imports() {
	root := diag.Location{File: model.ModelFile}
	for i, name := range c.f.Imports {
		at := root.Sub("imports", i)
		switch {
		case name == c.f.Name:
			c.Add(at, "self_import", "A family cannot import itself.")
		case c.f.Imported[name] == nil:
			c.Addf(at, "unresolved_import", "Unknown family %s: it is not among the families of this checkout.", name)
		case reaches(c.f.Imported[name], c.f.Name, map[string]bool{}):
			c.Addf(at, "import_cycle", "Family %s imports this one, directly or through what it imports; the generated packages would import each other.", name)
		}
	}
}

func reaches(from *analysis.Family, target string, seen map[string]bool) bool {
	if from == nil || seen[from.Name] {
		return false
	}
	seen[from.Name] = true
	for name, imported := range from.Imported {
		if name == target || reaches(imported, target, seen) {
			return true
		}
	}
	return false
}

// expression holds one type expression, and every one beneath it, to
// resolution, the direction rule — a declaration of one tier refers to its
// own tier or a lower one — and the one reference form.
func (c *checker) expression(e model.TypeExpr, at diag.Location, where site) {
	f := c.f
	switch x := e.(type) {
	case model.Primitive:
	case model.Literal:
	case model.Named:
		if parameter, ok := lookup(where.scope, x.Name); ok {
			if parameter.IsFamily() {
				c.Addf(at, "invalid_parameter_use", "Parameter %s is filled by a family, and a family is not a type; draw a type through it, %s.Type.", x.Name, x.Name)
			}
			return
		}
		if parameter, ok := f.Parameter(x.Name); ok {
			if parameter.IsFamily() {
				c.Addf(at, "invalid_parameter_use", "Parameter %s is filled by a family, and a family is not a type; draw a type through it, %s.Type.", x.Name, x.Name)
			}
			return
		}
		t, ok := f.Types[x.Name]
		if !ok {
			c.Addf(at, "unresolved_type", "Unknown type %s.", x.Name)
			return
		}
		if f.Rank(x.Name) > where.context {
			c.tierViolation(at, where.context, f.Spell(x.Name), t.At.File)
		}
		if len(t.Parameters) > 0 {
			c.Addf(at, "unbound_parameter", "Type %s is generic; say what fills each of its parameters with {\"apply\": \"%s\", \"with\": {…}}.", x.Name, x.Name)
		}
		if where.owner != "" && where.edges != nil {
			where.edges[where.owner] = append(where.edges[where.owner], x.Name)
		}
	case model.Imported:
		c.imported(x, at, where)
	case model.Drawn:
		c.drawn(x, at, where)
	case model.Array:
		c.expression(x.Elem, at, where)
	case model.Map:
		c.expression(x.Elem, at, where)
	case model.Nullable:
		c.expression(x.Elem, at, where)
	case model.Ref:
		t, ok := f.Types[x.Entity]
		switch {
		case !ok:
			c.Addf(at, "unresolved_type", "Unknown entity %s.", x.Entity)
		case t.Kind != model.KindEntity:
			c.Addf(at, "invalid_ref", "A ref names an entity; %s is a %s.", x.Entity, t.Kind)
		case f.Rank(x.Entity) > where.context:
			c.tierViolation(at, where.context, x.Entity, t.At.File)
		}
	case model.Apply:
		c.apply(x, at, where)
	case model.Inline:
		if !where.inline {
			c.Addf(at, "inline_not_admissible", "A shape is written inline where a value's type is declared — a field, a variant, an operation's request, result or event — and not here; declare it under a name of its own.")
			return
		}
		if len(x.Type.Parameters) > 0 {
			c.Add(at, "inline_not_admissible", "A shape written inline declares no parameters: it has no name for an application to fill them through.")
		}
		inner := where
		inner.scope = append(append([]model.Parameter{}, x.Type.Parameters...), where.scope...)
		inner.edges = nil // an inline shape is a value of its owner, not a type of the family
		c.inlineType(x.Type, at, inner)
	}
}

// inlineType holds a shape written inline to the rules of its kind, with
// every diagnostic pointing at the path the shape sits on.
func (c *checker) inlineType(t *model.Type, at diag.Location, where site) {
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		if len(t.Extends) > 0 {
			c.Add(at, "inline_not_admissible", "A shape written inline extends nothing: it has no name for another to inherit, and none to inherit from without one.")
		}
		for i := range t.Fields {
			c.expression(t.Fields[i].Type, at.Sub("fields", i, "type"), where)
			c.constraints(&t.Fields[i], t)
		}
		if t.Kind == model.KindEntity {
			c.Add(at, "inline_not_admissible", "An entity is identified across a family and is declared under a name of its own, never inline.")
		}
	case model.KindUnion:
		if len(t.Extends) > 0 {
			c.Add(at, "inline_not_admissible", "A shape written inline extends nothing: it has no name for another to inherit, and none to inherit from without one.")
		}
		c.union(t, where)
	case model.KindEnum:
		if len(t.Values) == 0 {
			c.Add(at, "invalid_shape", "An enum has values.")
		}
	case model.KindAlias:
		c.Add(at, "inline_not_admissible", "An alias gives a shape a second name; written inline it gives it none.")
	case model.KindCallable:
		c.Add(at, "callable_inline", "A callable is declared under a name of its own and referred to by it: a reference to one carries the identity of the declaration it implements, and a callable written inline has no declaration to name. Lift it into the live tier's types and name it here.")
	default:
		c.Addf(at, "invalid_shape", "A shape written inline is a record, an enum or a union, not a %s.", t.Kind)
	}
}

// imported holds a reference to a type of another family: the family is
// imported, the type is declared there, of its tier or a lower one, and
// what is generic there is filled here.
func (c *checker) imported(x model.Imported, at diag.Location, where site) {
	f := c.f
	other, ok := f.Imported[x.Family]
	if !ok {
		if _, builtin := f.Builtin(x.Family); builtin {
			c.Addf(at, "implicit_import", "Family %s is built in: a family that has the tier bringing it carries its types under their own names, and one that does not cannot name them.", x.Family)
			return
		}
		c.Addf(at, "unresolved_type", "Type %s.%s names a family this family does not import.", x.Family, x.Name)
		return
	}
	t, ok := other.Types[x.Name]
	if !ok {
		c.Addf(at, "unresolved_type", "Unknown type %s in family %s.", x.Name, x.Family)
		return
	}
	if other.Rank(x.Name) > where.context {
		c.tierViolation(at, where.context, x.Family+"."+x.Name, t.At.File)
	}
	// Plain imports can infer family bindings from the caller's one family
	// parameter. Type arguments still need an application, and a family
	// bound must guarantee the tier each inferred slot requires.
	parameters := append([]model.Parameter{}, t.Parameters...)
	for _, use := range other.Generics().Types[x.Name] {
		if _, own := lookup(parameters, use.Parameter); !own {
			parameter, _ := other.Parameter(use.Parameter)
			parameter.Name = use.Parameter
			parameters = append(parameters, parameter)
		}
	}
	if len(parameters) == 0 {
		return
	}
	fillers := f.FamilyParameters()
	if len(fillers) != 1 {
		c.Addf(at, "ambiguous_application", "Type %s.%s is generic and this family declares %d family parameters; say what fills each with {\"apply\": \"%s.%s\", \"with\": {…}}.", x.Family, x.Name, len(fillers), x.Family, x.Name)
		return
	}
	for _, parameter := range parameters {
		if !parameter.IsFamily() {
			c.Addf(at, "ambiguous_application", "Type %s.%s needs type parameter %s, which a family parameter cannot fill; supply it with {\"apply\": \"%s.%s\", \"with\": {…}}.", x.Family, x.Name, parameter.Name, x.Family, x.Name)
			return
		}
		if !familyBoundFills(fillers[0].Of, parameter.Of) {
			c.Addf(at, "ambiguous_application", "Type %s.%s needs parameter %s of the %s tier, but %s guarantees only %s; supply a suitable family with {\"apply\": \"%s.%s\", \"with\": {…}}.", x.Family, x.Name, parameter.Name, parameter.Of, fillers[0].Name, fillers[0].Of, x.Family, x.Name)
			return
		}
	}
}

// Carrying a tier requires its lower tiers too. Unknown roles are refused
// by the parameter checker and cannot establish an inferred binding.
func familyBoundFills(from, to string) bool {
	fromRank, toRank := -1, -1
	for _, tier := range model.Tiers {
		if tier.Name == from {
			fromRank = tier.Rank
		}
		if tier.Name == to {
			toRank = tier.Rank
		}
	}
	return fromRank > 0 && toRank > 0 && fromRank >= toRank
}

// drawn holds a draw through a parameter: the parameter is one this
// declaration has in scope, it is filled by a family — a type has no types
// of its own to draw — and every family that may fill it declares the type
// plainly.
func (c *checker) drawn(x model.Drawn, at diag.Location, where site) {
	f := c.f
	parameter, ok := lookup(where.scope, x.Parameter)
	if !ok {
		parameter, ok = f.Parameter(x.Parameter)
	}
	if ok && !parameter.IsFamily() {
		c.Addf(at, "invalid_draw", "Parameter %s is filled by a type, and a type has no types of its own to draw; %s.%s draws through a parameter of a tier, the one declared with \"of\".", x.Parameter, x.Parameter, x.Name)
		return
	}
	if where.context < model.Rank(model.ProtocolFile) {
		c.Addf(at, "tier_violation", "A %s declaration draws on parameter %s; a parameter of a tier is of the protocol tier, and a declaration refers to its own tier or a lower one.", model.TierName(where.context), x.Parameter)
	}
	if !ok {
		c.Addf(at, "unresolved_parameter", "Unknown parameter %s: nothing in scope declares a parameter of that name.", x.Parameter)
		return
	}
	if model.Carried(x.Name) {
		return
	}
	// A type beyond the ones every family carries must be one every family
	// that may bind the parameter declares, as a record or an enum of its
	// own — an alias has no identity for a language to hold it to — and
	// plainly.
	for _, name := range sortedKeys(toNames(f.Carriers(parameter.Of))) {
		other := f.Carriers(parameter.Of)[name]
		t, declared := other.Types[x.Name]
		switch {
		case !declared:
			c.Addf(at, "unresolved_type", "Type %s of %s: the %s family %s declares no type of that name, and every family that may bind %s must.", x.Name, x.Parameter, parameter.Of, name, x.Parameter)
		case t.Kind == model.KindAlias:
			c.Addf(at, "unresolved_type", "Type %s of %s: in the %s family %s it is an alias, and a slot draws a record or an enum.", x.Name, x.Parameter, parameter.Of, name)
		case len(other.Generics().Types[x.Name]) > 0 || len(t.Parameters) > 0:
			c.Addf(at, "unresolved_type", "Type %s of %s: in the %s family %s it is generic, and a slot draws a plain type.", x.Name, x.Parameter, parameter.Of, name)
		}
	}
}

// apply fills the parameters of a generic type of this family or of an
// imported one: every parameter it declares is filled, by a type where it
// is a type parameter and by a family that carries the tier where it is a
// family parameter.
func (c *checker) apply(x model.Apply, at diag.Location, where site) {
	f := c.f
	target, declared := f.Applied(x)
	if !declared {
		if x.Family == "" {
			c.Addf(at.Sub("apply"), "unresolved_type", "Unknown type %s.", x.Name)
			return
		}
		if _, ok := f.Imported[x.Family]; !ok {
			c.Addf(at.Sub("apply"), "unresolved_type", "Type %s.%s names a family this family does not import.", x.Family, x.Name)
			return
		}
		c.Addf(at.Sub("apply"), "unresolved_type", "Unknown type %s in family %s.", x.Name, x.Family)
		return
	}
	wanted := map[string]model.Parameter{}
	for _, parameter := range target.Parameters {
		wanted[parameter.Name] = parameter
	}
	if x.Family != "" {
		for _, use := range f.Imported[x.Family].Generics().Types[x.Name] {
			if _, already := wanted[use.Parameter]; !already {
				p, _ := f.Imported[x.Family].Parameter(use.Parameter)
				wanted[use.Parameter] = p
			}
		}
	}
	c.arguments(applied(x), wanted, x.With, at, where)
	// The applied type is a declaration like any other, so it is of this
	// declaration's tier or a lower one.
	rank, in := where.context, f
	if x.Family != "" {
		in = f.Imported[x.Family]
	}
	if in.Rank(x.Name) > rank {
		c.tierViolation(at, rank, applied(x), target.At.File)
	}
}

func (c *checker) arguments(target string, wanted map[string]model.Parameter, with map[string]model.Filler, at diag.Location, where site) {
	f := c.f
	if len(wanted) == 0 {
		c.Addf(at.Sub("apply"), "needless_application", "Type %s is not generic; refer to it by name.", target)
	}
	for _, name := range sortedParameters(wanted) {
		if _, bound := with[name]; !bound {
			c.Addf(at.Sub("with"), "unbound_parameter", "The application leaves %s's parameter %s unbound.", target, name)
		}
	}
	for _, name := range sortedFillers(with) {
		filler := with[name]
		here := at.Sub("with", name)
		parameter, ok := wanted[name]
		if !ok {
			c.Addf(here, "unresolved_parameter", "Type %s has no parameter %s.", target, name)
			continue
		}
		if !parameter.IsFamily() {
			if filler.Family != "" {
				c.Addf(here, "invalid_filler", "Parameter %s of %s is filled by a type; %s names a family.", name, target, filler.Family)
				continue
			}
			c.expression(filler.Type, here, site{owner: where.owner, context: where.context, scope: where.scope, edges: where.edges})
			continue
		}
		family := filler.Family
		if family == "" {
			named := filler.Name()
			forwarded, declared := lookup(where.scope, named)
			if !declared {
				forwarded, declared = f.Parameter(named)
			}
			switch {
			case declared && forwarded.IsFamily():
				if !familyBoundFills(forwarded.Of, parameter.Of) {
					c.Addf(here, "invalid_filler", "Parameter %s guarantees the %s tier, but parameter %s of %s requires %s.", named, forwarded.Of, name, target, parameter.Of)
				}
			case named != "" && model.IsParameter(named) && !declared && f.Types[named] == nil:
				c.Addf(here, "unresolved_parameter", "Unknown parameter %s: no parameter of that name is in scope.", named)
			default:
				c.Addf(here, "invalid_filler", "Parameter %s of %s is filled by a family that carries the %s tier, or by an in-scope family parameter that guarantees it; %s is neither.", name, target, parameter.Of, filler.String())
			}
			continue
		}
		switch {
		case family == f.Name:
			c.Add(here, "self_slot", "A parameter cannot be filled with the family that declares the application.")
		case f.Imported[family] == nil:
			c.Addf(here, "unresolved_type", "Family %s fills a parameter and is not imported.", family)
		case f.Carriers(parameter.Of)[family] == nil:
			c.Addf(here, "invalid_filler", "Family %s fills parameter %s of %s, which is of the %s tier, and %s does not carry it.", family, name, target, parameter.Of, family)
		}
	}
}

func applied(x model.Apply) string {
	if x.Family == "" {
		return x.Name
	}
	return x.Family + "." + x.Name
}

func lookup(parameters []model.Parameter, name string) (model.Parameter, bool) {
	for _, parameter := range parameters {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return model.Parameter{}, false
}

func (c *checker) tierViolation(at diag.Location, context int, name, file string) {
	c.Addf(at, "tier_violation", "A %s declaration refers to %s, declared in %s; a declaration refers to its own tier or a lower one.", model.TierName(context), name, file)
}

// constraints: each constraint fits the field's type; a pattern is in the
// dialect; unique is of an entity's field.
func (c *checker) constraints(field *model.Field, owner *model.Type) {
	primitive, _ := field.Type.(model.Primitive)
	_, isArray := field.Type.(model.Array)
	numeric := primitive == "integer" || primitive == "number" || primitive == "timestamp"
	if (field.Min != nil || field.Max != nil) && !numeric {
		c.Addf(field.At, "invalid_constraint", "min and max bound an integer, a number or a timestamp, not %s.", model.String(field.Type))
	}
	if field.Length != nil && primitive != "string" && !isArray {
		c.Addf(field.At.Sub("length"), "invalid_constraint", "length bounds a string or an array, not %s.", model.String(field.Type))
	}
	if field.Pattern != "" && primitive != "string" {
		c.Addf(field.At.Sub("pattern"), "invalid_constraint", "pattern holds a string, not %s.", model.String(field.Type))
	}
	if field.Pattern != "" {
		if err := Pattern(field.Pattern); err != nil {
			c.Addf(field.At.Sub("pattern"), "invalid_pattern", "%s: %v. %s", field.Pattern, err, Dialect)
		}
	}
	if field.Unique && owner.Kind != model.KindEntity {
		c.Addf(field.At.Sub("unique"), "invalid_constraint", "unique is of an entity's field; %s is a %s.", owner.Name, owner.Kind)
	}
}

// key: an entity's key is one of its fields, its own or inherited,
// primitive and required.
func (c *checker) key(t *model.Type) {
	for _, field := range c.f.FlattenedFields(t.Name) {
		if field.Name != t.Key {
			continue
		}
		if _, primitive := field.Type.(model.Primitive); !primitive || !field.Required || field.Nullable {
			c.Addf(t.At.Sub("key"), "invalid_key", "The key of %s is %s, which must be a required, non-null primitive.", t.Name, t.Key)
		}
		return
	}
	c.Addf(t.At.Sub("key"), "invalid_key", "The key of %s names no field of it: %s.", t.Name, t.Key)
}

// cycles: no value contains itself, through fields, aliases, arrays or
// maps; inheritance edges count, since a record carries its parent's fields.
func (c *checker) cycles(edges map[string][]string) {
	state := map[string]int{}
	var visit func(string)
	visit = func(name string) {
		if state[name] == 2 {
			return
		}
		state[name] = 1
		for _, next := range edges[name] {
			if state[next] == 1 {
				c.Addf(c.f.Types[name].At, "cyclic_type", "Value schema cycle reaches %s.", next)
			} else if state[next] == 0 {
				visit(next)
			}
		}
		state[name] = 2
	}
	for _, name := range c.f.TypeNames() {
		if state[name] == 0 {
			visit(name)
		}
	}
}

// fieldCollisions: a record carries no wire field twice, its own or
// inherited; each inheritance path is walked apart, since a diamond
// carries its shared fields twice on the wire.
func (c *checker) fieldCollisions() {
	for _, name := range c.f.TypeNames() {
		t := c.f.Types[name]
		if t.Kind != model.KindRecord && t.Kind != model.KindEntity {
			continue
		}
		seen := map[string]diag.Location{}
		c.f.WalkFields(name, func(at analysis.FieldAt) {
			if previous, ok := seen[at.Field.Name]; ok {
				c.Addf(at.Field.At.Sub("name"), "field_collision", "Record %s repeats field %s already provided at %s.", name, at.Field.Name, previous)
			} else {
				seen[at.Field.Name] = at.Field.At
			}
		})
	}
}

// Protocol holds the protocol tier to its rules: parameters are distinct,
// of a tier or of none, bindable and used; a side that extends another
// family's names one it imports and collides with none of its operations;
// every operation's expressions resolve; a request is an object on the
// wire; the wire names of what flows one way are distinct; a method's
// errors are declared; crud is not yet.
func Protocol(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	p := f.Protocol
	c.parameters(p.Parameters, "", model.Rank(model.ProtocolFile))
	context := model.Rank(model.ProtocolFile)
	where := site{context: context, inline: true}
	wire := map[string]diag.Location{}
	operation := func(direction, name string, at diag.Location) {
		c.reservedOperation(name, at)
		key := direction + ":" + name
		if previous, ok := wire[key]; ok {
			c.Addf(at, "operation_collision", "Operation %s collides with the one at %s: both flow %s under one name.", name, previous, direction)
		} else {
			wire[key] = at
		}
	}
	for _, side := range []struct {
		side      *model.Side
		calls     string // the direction its methods flow
		notifies  string // the direction its events flow
		sideLabel string
		server    bool
	}{
		{&p.Server, "client to server", "server to client", "server", true},
		{&p.Client, "server to client", "client to server", "client", false},
	} {
		c.extendedSide(side.side, side.server, side.sideLabel, operation, side.calls, side.notifies)
		for i := range side.side.Methods {
			m := &side.side.Methods[i]
			operation(side.calls, m.Name, m.At)
			if m.Request != nil {
				before := len(c.Diagnostics)
				c.expression(m.Request, m.At.Sub("request"), where)
				if len(c.Diagnostics) == before && !f.IsObject(m.Request) {
					c.Add(m.At.Sub("request"), "invalid_request", "A method's request is a record, of this family or an imported one, written inline, or a type drawn from a parameter: params is an object on the wire.")
				}
			}
			c.expression(m.Result, m.At.Sub("result"), where)
			for j, code := range m.Errors {
				if _, declared := p.Error(code); !declared {
					c.Addf(m.At.Sub("errors", j), "unknown_error", "Method %s may return %s, which the family does not declare among its errors.", m.Name, code)
				}
			}
		}
		for i := range side.side.Events {
			e := &side.side.Events[i]
			operation(side.notifies, e.Name, e.At)
			c.expression(e.Type, e.At.Sub("type"), where)
		}
		if len(side.side.CRUD) > 0 {
			c.Addf(side.side.At.Sub("crud"), "unsupported", "crud is not expanded yet; write the %s side's methods out.", side.sideLabel)
		}
	}
	used := map[string]bool{}
	f.Expressions(func(site model.ExprAt) {
		switch x := site.Expr.(type) {
		case model.Drawn:
			used[x.Parameter] = true
		case model.Named:
			used[x.Name] = true
		case model.Apply:
			for _, filler := range x.With {
				if name := filler.Name(); name != "" {
					used[name] = true
				}
			}
		}
	})
	for _, parameter := range p.Parameters {
		if !used[parameter.Name] {
			c.Addf(parameter.At.Sub("name"), "unused_parameter", "Parameter %s is declared and nothing names it: no slot, and no application of an imported type.", parameter.Name)
		}
	}
	return c.Diagnostics
}

// extendedSide holds a side that extends another family's: it names a
// family this one imports, which has a protocol, and whose operations do
// not collide with this side's own or with another base's. What the
// extending family offers is the union — a consumer of the base may speak
// to it — so a name may not mean two things across the join.
func (c *checker) extendedSide(side *model.Side, server bool, label string, operation func(string, string, diag.Location), calls, notifies string) {
	f := c.f
	seenMethods := map[*model.Method][2]model.TypeExpr{}
	seenEvents := map[*model.Event]model.TypeExpr{}
	var walk func(*analysis.Family, map[string]model.Filler, diag.Location, map[*analysis.Family]bool)
	walk = func(source *analysis.Family, bindings map[string]model.Filler, at diag.Location, stack map[*analysis.Family]bool) {
		if source == nil || source.Protocol == nil || stack[source] {
			return
		}
		stack[source] = true
		defer delete(stack, source)
		inherited := &source.Protocol.Server
		if !server {
			inherited = &source.Protocol.Client
		}
		for _, edge := range inherited.Extends {
			walk(source.Imported[edge.Name], f.BindArguments(edge.With, source, bindings), at, stack)
		}
		for i := range inherited.Methods {
			m := &inherited.Methods[i]
			signature := [2]model.TypeExpr{f.BindExpression(m.Request, source, bindings), f.BindExpression(m.Result, source, bindings)}
			if previous, seen := seenMethods[m]; !seen || !reflect.DeepEqual(previous, signature) {
				operation(calls, m.Name, at)
				seenMethods[m] = signature
			}
		}
		for i := range inherited.Events {
			e := &inherited.Events[i]
			signature := f.BindExpression(e.Type, source, bindings)
			if previous, seen := seenEvents[e]; !seen || !reflect.DeepEqual(previous, signature) {
				operation(notifies, e.Name, at)
				seenEvents[e] = signature
			}
		}
	}
	for i, edge := range side.Extends {
		name := edge.Name
		at := side.At.Sub("extends", i)
		switch {
		case name == f.Name:
			c.Add(at, "invalid_extends", "A side cannot extend its own family's.")
			continue
		case f.Imported[name] == nil:
			c.Addf(at, "invalid_extends", "The %s side extends %s's, which this family does not import.", label, name)
			continue
		case f.Imported[name].Protocol == nil:
			c.Addf(at, "invalid_extends", "The %s side extends %s's, and %s has no protocol tier.", label, name, name)
			continue
		}
		base := f.Imported[name]
		wanted := map[string]model.Parameter{}
		for _, parameter := range base.Parameters() {
			wanted[parameter.Name] = parameter
		}
		c.inheritanceArguments(edge, wanted, at, site{context: model.Rank(side.At.File), scope: f.Parameters()})
		if extendsBack(base, f.Name, server, map[string]bool{}) {
			c.Addf(at, "extends_cycle", "The %s side extends %s's, which extends this one, directly or through what it extends.", label, name)
			continue
		}
		walk(base, edge.With, at, map[*analysis.Family]bool{})
	}
}

func extendsBack(from *analysis.Family, target string, server bool, seen map[string]bool) bool {
	if from == nil || from.Protocol == nil || seen[from.Name] {
		return false
	}
	seen[from.Name] = true
	side := from.Protocol.Server
	if !server {
		side = from.Protocol.Client
	}
	for _, edge := range side.Extends {
		name := edge.Name
		if name == target || extendsBack(from.Imported[name], target, server, seen) {
			return true
		}
	}
	return false
}

// Overrides holds every target's override file to the one rule the model
// has for it: each key names something the family declares. What a target
// makes of the value is the target's to check.
func Overrides(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	for _, target := range sortedTargets(f.Overrides) {
		file := model.OverrideFile(target)
		raw := f.Overrides[target]
		if raw == nil {
			continue
		}
		o, err := model.DecodeOverrides(file, raw)
		if err != nil {
			c.Add(diag.Location{File: file}, "invalid_override", err.Error())
			continue
		}
		for _, key := range sortedKeys(toSet(o.Names)) {
			if _, ok := f.Locate(key); !ok {
				c.Addf(diag.Location{File: file, Pointer: "/names/" + diag.Escape(key)}, "unknown_override", "%s names nothing this family declares: a type, Type.field, Enum.value, a method or event, or errors.code.", key)
			}
		}
	}
	return c.Diagnostics
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedFillers(with map[string]model.Filler) []string {
	keys := make([]string, 0, len(with))
	for key := range with {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedParameters(m map[string]model.Parameter) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedTargets[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toSet(m map[string]string) map[string]bool {
	set := make(map[string]bool, len(m))
	for key := range m {
		set[key] = true
	}
	return set
}

func toNames(m map[string]*analysis.Family) map[string]bool {
	set := make(map[string]bool, len(m))
	for key := range m {
		set[key] = true
	}
	return set
}
