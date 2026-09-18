// Package check holds the rules a family is held to before any target sees
// it: one function per tier over the facts analysis derived, and one over
// the override files. Each reports every problem it finds, located by tier
// file and pointer, and none renders anything. The Concerns table pairs
// each concern with its checker, so that the kernel runs them in tier order
// without naming one.
package check

import (
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// Concern pairs a tier with the checker that holds a family to its rules.
type Concern struct {
	Name  string
	File  string
	Check func(*analysis.Family) []diag.Diagnostic
}

// Concerns are the checkers of the tiers above the model, in tier order.
var Concerns = []Concern{
	{Name: "protocol", File: model.ProtocolFile, Check: Protocol},
	{Name: "session", File: model.SessionFile, Check: Session},
}

// Family runs every check the family's tiers call for, in tier order, and
// the override files' check, and sorts what they say.
func Family(f *analysis.Family) []diag.Diagnostic {
	diagnostics := Model(f)
	for _, concern := range Concerns {
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

// Model holds the family to the rules of its types, whatever tier declares
// them: every import resolves and none cycles; every name a type refers to
// resolves, within its tier or below; inheritance is of records and
// carries no field twice; no value contains itself; an entity's key is one
// of its primitive fields; a constraint fits its field's type.
func Model(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	c.imports()
	edges := map[string][]string{}
	for _, name := range f.TypeNames() {
		t := f.Types[name]
		if model.IsInjected(name) && t.At.Pointer == "" {
			continue
		}
		if model.IsInjected(name) {
			c.Addf(t.At, "reserved_name", "Type %s is carried by every family with a protocol and may not be declared.", name)
			continue
		}
		context := f.Rank(name)
		switch t.Kind {
		case model.KindRecord, model.KindEntity:
			for i, parent := range t.Extends {
				at := t.At.Sub("extends", i)
				inherited, ok := f.Types[parent]
				switch {
				case !ok:
					c.Addf(at, "unresolved_type", "Unknown inherited type %s.", parent)
				case inherited.Kind != model.KindRecord && inherited.Kind != model.KindEntity:
					c.Add(at, "invalid_inheritance", "An inherited type must be a record.")
				case f.Rank(parent) > context:
					c.tierViolation(at, context, parent, f.Types[parent].At.File)
				default:
					edges[name] = append(edges[name], parent)
				}
			}
			for i := range t.Fields {
				field := &t.Fields[i]
				c.expression(field.Type, field.At.Sub("type"), name, context, edges)
				c.constraints(field, t)
			}
			if t.Kind == model.KindEntity {
				c.key(t)
			}
		case model.KindAlias:
			c.expression(t.Alias, t.At.Sub("type"), name, context, edges)
		}
	}
	c.cycles(edges)
	c.fieldCollisions()
	return c.Diagnostics
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
// resolution and the direction rule: a declaration of one tier refers to
// its own tier or a lower one. context is the rank of the declaration the
// expression sits in, owner the type it is part of, for the value graph.
func (c *checker) expression(e model.TypeExpr, at diag.Location, owner string, context int, edges map[string][]string) {
	f := c.f
	switch x := e.(type) {
	case model.Primitive:
	case model.Named:
		t, ok := f.Types[x.Name]
		if !ok {
			c.Addf(at, "unresolved_type", "Unknown type %s.", x.Name)
			return
		}
		if f.Rank(x.Name) > context {
			c.tierViolation(at, context, x.Name, t.At.File)
		}
		if owner != "" && edges != nil {
			edges[owner] = append(edges[owner], x.Name)
		}
	case model.Imported:
		other, ok := f.Imported[x.Family]
		if !ok {
			c.Addf(at, "unresolved_type", "Type %s.%s names a family this family does not import.", x.Family, x.Name)
			return
		}
		t, ok := other.Types[x.Name]
		if !ok {
			c.Addf(at, "unresolved_type", "Unknown type %s in family %s.", x.Name, x.Family)
			return
		}
		if other.Rank(x.Name) > context {
			c.tierViolation(at, context, x.Family+"."+x.Name, t.At.File)
		}
		// A generic type of an imported family has parameters of its own,
		// which this family must fill. A family with one parameter fills
		// them all with it; with any other number the plain reference says
		// nothing about which fills which, so an application is required.
		if len(other.Generics().Types[x.Name]) > 0 && len(f.Parameters()) != 1 {
			c.Addf(at, "ambiguous_application", "Type %s.%s is generic and this family declares %d parameters; say what fills each with {\"apply\": \"%s.%s\", \"with\": {…}}.", x.Family, x.Name, len(f.Parameters()), x.Family, x.Name)
		}
	case model.Drawn:
		if context < model.Rank(model.ProtocolFile) {
			c.Addf(at, "tier_violation", "A %s declaration draws on parameter %s; a parameter is of the protocol tier, and a declaration refers to its own tier or a lower one.", model.TierName(context), x.Parameter)
		}
		if !f.HasParameter(x.Parameter) {
			c.Addf(at, "unresolved_parameter", "Unknown parameter %s: this family declares no parameter of that name.", x.Parameter)
			return
		}
		if model.IsInjected(x.Name) {
			return
		}
		// A type beyond the two every family carries must be one every
		// family that may bind the parameter declares, as a record or an
		// enum of its own — an alias has no identity for a language to hold
		// it to — and plainly.
		for _, member := range f.Sessions {
			other, ok := f.Members[member]
			if !ok {
				continue
			}
			t, declared := other.Types[x.Name]
			switch {
			case !declared:
				c.Addf(at, "unresolved_type", "Type %s of %s: the session family %s declares no type of that name, and every family that may bind %s must.", x.Name, x.Parameter, member, x.Parameter)
			case t.Kind == model.KindAlias:
				c.Addf(at, "unresolved_type", "Type %s of %s: in the session family %s it is an alias, and a slot draws a record or an enum.", x.Name, x.Parameter, member)
			case len(other.Generics().Types[x.Name]) > 0:
				c.Addf(at, "unresolved_type", "Type %s of %s: in the session family %s it is generic, and a slot draws a plain type.", x.Name, x.Parameter, member)
			}
		}
	case model.Array:
		c.expression(x.Elem, at, owner, context, edges)
	case model.Map:
		c.expression(x.Elem, at, owner, context, edges)
	case model.Ref:
		t, ok := f.Types[x.Entity]
		switch {
		case !ok:
			c.Addf(at, "unresolved_type", "Unknown entity %s.", x.Entity)
		case t.Kind != model.KindEntity:
			c.Addf(at, "invalid_ref", "A ref names an entity; %s is a %s.", x.Entity, t.Kind)
		case f.Rank(x.Entity) > context:
			c.tierViolation(at, context, x.Entity, t.At.File)
		}
	case model.Apply:
		other, ok := f.Imported[x.Family]
		if !ok {
			c.Addf(at.Sub("apply"), "unresolved_type", "Type %s.%s names a family this family does not import.", x.Family, x.Name)
			return
		}
		t, declared := other.Types[x.Name]
		if !declared {
			c.Addf(at.Sub("apply"), "unresolved_type", "Unknown type %s in family %s.", x.Name, x.Family)
			return
		}
		wanted := map[string]bool{}
		for _, use := range other.Generics().Types[x.Name] {
			wanted[use.Parameter] = true
		}
		if len(wanted) == 0 {
			c.Addf(at.Sub("apply"), "needless_application", "Type %s.%s is not generic; refer to it by name.", x.Family, x.Name)
		}
		for _, parameter := range sortedKeys(wanted) {
			if _, bound := x.With[parameter]; !bound {
				c.Addf(at.Sub("with"), "unbound_parameter", "The application leaves %s's parameter %s unbound.", x.Family, parameter)
			}
		}
		for _, parameter := range sortedFillers(x.With) {
			filler := x.With[parameter]
			here := at.Sub("with", parameter)
			if !wanted[parameter] {
				c.Addf(here, "unresolved_parameter", "Type %s.%s has no parameter %s.", x.Family, x.Name, parameter)
			}
			switch {
			case filler.Parameter != "":
				if !f.HasParameter(filler.Parameter) {
					c.Addf(here, "unresolved_parameter", "Unknown parameter %s: this family declares no parameter of that name.", filler.Parameter)
				}
			case filler.Family == f.Name:
				c.Add(here, "self_slot", "A parameter cannot be filled with the family that declares the application.")
			case f.Imported[filler.Family] == nil:
				c.Addf(here, "unresolved_type", "Family %s fills a parameter and is not imported.", filler.Family)
			}
		}
		if other.Rank(x.Name) > context {
			c.tierViolation(at, context, x.Family+"."+x.Name, t.At.File)
		}
	}
}

func (c *checker) tierViolation(at diag.Location, context int, name, file string) {
	c.Addf(at, "tier_violation", "A %s declaration refers to %s, declared in %s; a declaration refers to its own tier or a lower one.", model.TierName(context), name, file)
}

// constraints: each constraint fits the field's type; unique is of an
// entity's field.
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
	if field.Unique && owner.Kind != model.KindEntity {
		c.Addf(field.At.Sub("unique"), "invalid_constraint", "unique is of an entity's field; %s is a %s.", owner.Name, owner.Kind)
	}
}

// key: an entity's key is one of its own fields, primitive and required.
func (c *checker) key(t *model.Type) {
	for _, field := range t.Fields {
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
// of a role, bindable and used; every operation's expressions resolve;
// a request is an object on the wire; the wire names of what flows one way
// are distinct; a method's errors are declared; crud is not yet.
func Protocol(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	p := f.Protocol
	seen := map[string]bool{}
	for _, parameter := range p.Parameters {
		if seen[parameter.Name] {
			c.Addf(parameter.At.Sub("name"), "duplicate_parameter", "Parameter %s is declared twice.", parameter.Name)
		}
		seen[parameter.Name] = true
		if _, collides := f.Types[parameter.Name]; collides {
			c.Addf(parameter.At.Sub("name"), "reserved_name", "Parameter %s shares its name with a type of this family.", parameter.Name)
		}
		switch {
		case parameter.Of != model.SessionRole:
			c.Addf(parameter.At.Sub("of"), "unknown_role", "Unknown role %s: a parameter is of the session role.", parameter.Of)
		case len(f.Members) == 0:
			c.Add(parameter.At.Sub("of"), "unresolved_type", "No other family has a session tier, so a parameter of session has nothing to bind.")
		}
	}
	context := model.Rank(model.ProtocolFile)
	wire := map[string]diag.Location{}
	operation := func(direction, name string, at diag.Location) {
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
	}{
		{&p.Server, "client to server", "server to client", "server"},
		{&p.Client, "server to client", "client to server", "client"},
	} {
		for i := range side.side.Methods {
			m := &side.side.Methods[i]
			operation(side.calls, m.Name, m.At)
			if m.Request != nil {
				before := len(c.Diagnostics)
				c.expression(m.Request, m.At.Sub("request"), "", context, nil)
				if len(c.Diagnostics) == before && !f.IsObject(m.Request) {
					c.Add(m.At.Sub("request"), "invalid_request", "A method's request is a record, of this family or an imported one, or a type drawn from a parameter: params is an object on the wire.")
				}
			}
			c.expression(m.Result, m.At.Sub("result"), "", context, nil)
			for j, code := range m.Errors {
				if _, declared := p.Error(code); !declared {
					c.Addf(m.At.Sub("errors", j), "unknown_error", "Method %s may return %s, which the family does not declare among its errors.", m.Name, code)
				}
			}
		}
		for i := range side.side.Events {
			e := &side.side.Events[i]
			operation(side.notifies, e.Name, e.At)
			c.expression(e.Type, e.At.Sub("type"), "", context, nil)
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
		case model.Apply:
			for _, filler := range x.With {
				if filler.Parameter != "" {
					used[filler.Parameter] = true
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

// Session holds the session tier to its rules: decides names methods of
// the family; asks names methods the server sends, the client side's; the
// conversation arrives in an event of the family.
func Session(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	s, p := f.Session, f.Protocol
	if p == nil {
		return nil
	}
	for i, name := range s.Decides {
		if _, _, ok := p.Method(name); !ok {
			c.Addf(s.At.Sub("decides", i), "unknown_operation", "Decides names %s, which is not a method of this family.", name)
		}
	}
	for i, name := range s.Asks {
		_, server, ok := p.Method(name)
		switch {
		case !ok:
			c.Addf(s.At.Sub("asks", i), "unknown_operation", "Asks names %s, which is not a method of this family.", name)
		case server:
			c.Addf(s.At.Sub("asks", i), "invalid_side", "Asks names %s, which the client sends; an asking method is one the server sends, on the client side.", name)
		}
	}
	if conversation := s.Conversation; conversation != nil {
		if _, _, ok := p.Event(conversation.Event); !ok {
			c.Addf(s.At.Sub("conversation", "event"), "unknown_operation", "The conversation arrives in %s, which is not an event of this family.", conversation.Event)
		}
	}
	return c.Diagnostics
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
			if _, ok := Locate(f, key); !ok {
				c.Addf(diag.Location{File: file, Pointer: "/names/" + diag.Escape(key)}, "unknown_override", "%s names nothing this family declares: a type, Type.field, Enum.value, a method or event, or errors.code.", key)
			}
		}
	}
	return c.Diagnostics
}

// Locate answers an override's path key with the declaration it names:
// Type, Type.field, Enum.value, a method or an event of either side, or
// errors.code. A name both sides declare is ambiguous and not found.
func Locate(f *analysis.Family, key string) (diag.Location, bool) {
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
