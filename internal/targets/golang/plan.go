package golang

import (
	"go/token"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

// The identifiers the generated packages declare of themselves, which a
// family may not: the emitters write these constants and nothing else of
// their own, so that what is reserved is what is emitted. The ones a
// generated body declares of itself — the label map install merges — are
// named here too and reserve nothing, since nothing outside that body can
// see them.
const (
	identTag                   = "Tag"
	identOf                    = "Of"
	identWireType              = "WireType"
	identMarshalJSON           = "MarshalJSON"
	identUnmarshalJSON         = "UnmarshalJSON"
	identAdditionalFields      = "AdditionalFields"
	identValidateRaw           = "ValidateRaw"
	identWireSchema            = "WireSchema"
	identValidateExpressionRaw = "ValidateExpressionRaw"
	identValidateValue         = "ValidateValue"
	identMustTypeExpression    = "MustTypeExpression"
	identErrors                = "Errors"
	identIsError               = "IsError"
	identRemote                = "Remote"
	identHandler               = "Handler"
	identEvents                = "Events"
	identInstall               = "install"
	identFamilies              = "families"
	identNewHandler            = "NewHandler"
	identServe                 = "Serve"
	identClient                = "Client"
	identCaller                = "Caller"
	identDial                  = "Dial"
	identAttach                = "Attach"
	identOpen                  = "Open"
	identPeer                  = "Peer"
	identClose                 = "Close"
	identEmit                  = "Emit"
	identOn                    = "On"
)

// plan is every identifier the rendering of one family declares, resolved
// from the convention and the override file and held in the namespace it
// lands in: the protocol package's declarations, the client and remote
// members, and each record's fields. A protocol type may share a name
// with an entry point in another package, as the built-in tunnel's Open
// request does with the client's Open helper.
type plan struct {
	family     *render.Family
	packages   *emit.Namespace // declarations in the protocol package
	client     *emit.Namespace // members of Client
	remote     *emit.Namespace // members of Remote
	types      map[string]string
	unions     map[string]unionPlan
	literals   map[string]literalPlan // literal wire value → its Go type and constant
	fields     map[string]string      // "Type.field" → Go field
	constants  map[string]string      // "Enum.value" → constant
	operations map[string]string      // method or event name → Go name
	errors     map[string]string      // code → constant
	typeParams map[render.Use]string
	diag.List
}

// Reserved is every identifier the generated packages declare of
// themselves: what a family may not name, and what a reviewer sees change
// when an emitter declares something new.
func Reserved() []string {
	return []string{
		identTag, identOf, identWireType, identMarshalJSON, identUnmarshalJSON, identAdditionalFields,
		identValidateRaw, identValidateExpressionRaw, identValidateValue, identMustTypeExpression, identWireSchema, identErrors, identIsError,
		identRemote, identHandler, identEvents, identInstall, identNewHandler, identServe,
		identClient, identCaller, identDial, identAttach, identOpen,
		identPeer, identClose,
	}
}

func newPlan(f *render.Family) (*plan, []diag.Diagnostic) {
	return planFamily(f, map[*render.Family]bool{})
}

func planFamily(f *render.Family, seen map[*render.Family]bool) (*plan, []diag.Diagnostic) {
	seen[f] = true
	p := &plan{
		family:   f,
		packages: emit.NewNamespace("protocol package"),
		client:   emit.NewNamespace("client"),
		remote:   emit.NewNamespace("remote"),
		types:    map[string]string{}, fields: map[string]string{}, constants: map[string]string{},
		operations: map[string]string{}, errors: map[string]string{}, typeParams: map[render.Use]string{},
		List: diag.List{Family: f.Name},
	}
	p.packages.Fix("generated declaration", identTag, identValidateRaw, identValidateExpressionRaw, identValidateValue, identMustTypeExpression, identWireSchema, identErrors, identIsError)
	p.client.Fix("generated client field", identPeer)
	p.client.Fix("generated client method", identClose)
	p.remote.Fix("generated remote field", identPeer)
	p.plan()
	// Imported names retain their source overrides. Check that source's
	// declarations too, once even when several inheritance paths reach it.
	for _, name := range f.References {
		source := f.ReferencedFamily(name)
		if source != nil && !seen[source] {
			_, diagnostics := planFamily(source, seen)
			p.Diagnostics = append(p.Diagnostics, diagnostics...)
		}
	}
	diag.Sort(p.Diagnostics)
	return p, p.Diagnostics
}

// resolve is a name for a path key: the override file's, or the
// convention's; where the name is declared, for a diagnostic about it.
func (p *plan) resolve(path, conventional string, declaredAt diag.Location) (string, diag.Location) {
	if name, ok := p.family.Override(Name, path); ok {
		return name, render.OverrideAt(Name, path)
	}
	return conventional, declaredAt
}

func (p *plan) resolveOrigin(origin render.Origin, path, conventional string, at diag.Location) (string, diag.Location) {
	if origin.Family != "" && origin.Family != p.family.Name {
		if source := p.family.ReferencedFamily(origin.Family); source != nil {
			if name, ok := source.Override(Name, path); ok {
				return name, render.OverrideAt(Name, path)
			}
			return conventional, at
		}
	}
	return p.resolve(path, conventional, at)
}

func (p *plan) fieldName(field render.Field) (string, diag.Location) {
	return p.resolveOrigin(field.Origin, field.Owner+"."+field.Name, naming.UpperCamel(field.Name), field.At)
}

func (p *plan) declare(ns *emit.Namespace, ident string, at diag.Location, what string) {
	if d := ns.Declare(p.Family, ident, at, what); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
	}
}

func (p *plan) identifier(ident string, at diag.Location, what string, exported bool) bool {
	if ident == "" || !token.IsIdentifier(ident) || token.Lookup(ident).IsKeyword() || (exported && !token.IsExported(ident)) {
		which := "a Go identifier"
		if exported {
			which = "an exported Go identifier"
		}
		p.Addf(at, "invalid_name", "%s %s must be %s.", what, ident, which)
		return false
	}
	return true
}

func (p *plan) plan() {
	f := p.family
	// Types first, so that what a constant or a type parameter collides
	// with is the type.
	for _, t := range f.Types {
		name, at := p.resolve(t.Name, t.Name, t.At)
		p.types[t.Name] = name
		if t.Carried {
			continue
		}
		if p.identifier(name, at, "Type name", true) {
			p.declare(p.packages, name, at, "type")
		}
	}
	p.planUnions()
	p.planLiterals()
	for _, t := range f.Types {
		switch t.Kind {
		case "record", "entity":
			for _, field := range t.Own {
				name, at := p.resolve(t.Name+"."+field.Name, naming.UpperCamel(field.Name), field.At)
				p.fields[t.Name+"."+field.Name] = name
				if t.Carried {
					continue
				}
				if !p.identifier(name, at, "Field name", true) {
					continue
				}
				if name == identMarshalJSON || name == identUnmarshalJSON || name == identOf || name == identWireType {
					p.Addf(at, "reserved_name", "Field name %s collides with a generated method.", name)
				}
				if t.Open && name == identAdditionalFields {
					p.Addf(at, "reserved_name", "Field name %s collides with the open record's storage for its additional fields.", name)
				}
			}
		case "enum":
			if t.Carried {
				continue
			}
			for i, value := range t.Values {
				name, at := p.resolve(t.Name+"."+value, p.types[t.Name]+naming.UpperCamel(value), t.At.Sub("values", i))
				p.constants[t.Name+"."+value] = name
				if p.identifier(name, at, "Enum constant", true) {
					p.declare(p.packages, name, at, "enum constant")
				}
			}
		}
	}
	// Each record's Go fields, along every inheritance path: a diamond
	// carries a field twice on the wire and twice in Go.
	for _, t := range f.Types {
		if t.Carried || (t.Kind != "record" && t.Kind != "entity") {
			continue
		}
		fields := emit.NewNamespace("record " + t.Name)
		if t.Open {
			fields.Fix("generated field", identAdditionalFields)
		}
		for _, field := range t.Fields {
			name, at := p.fieldName(field)
			p.declare(fields, name, at, "field")
		}
	}
	// A generic type takes its parameters as Go type parameters, which
	// shadow any type of the same name in the generated package; so does
	// the tag an entry point takes for each.
	generated := map[string]string{}
	entries := emit.NewNamespace("entry-point packages")
	entries.Fix("generated declaration", identRemote, identHandler, identEvents, identInstall, identNewHandler, identServe, identClient, identCaller, identDial, identAttach, identOpen)
	for _, use := range f.Uses {
		p.typeParams[use] = parameterName(use)
		at := diag.Location{}
		for _, parameter := range f.Parameters {
			if parameter.Name == use.Parameter {
				at = parameter.At.Sub("name")
			}
		}
		names := []string{parameterName(use)}
		if use.Type != "" {
			names = append(names, tagName(use.Parameter))
		}
		for _, name := range names {
			if what, taken := p.packages.Reserved(name); taken {
				p.Addf(at, "generated_name_collision", "Generated Go type parameter %s collides with the %s.", name, what)
			} else if what, taken := entries.Reserved(name); taken {
				p.Addf(at, "generated_name_collision", "Generated Go type parameter %s collides with the %s in an entry-point package.", name, what)
			} else if previous, exists := generated[name]; exists && previous != use.Parameter {
				p.Addf(at, "generated_name_collision", "Generated Go type parameter %s is also generated for parameter %s.", name, previous)
			}
			generated[name] = use.Parameter
		}
	}
	for _, t := range f.Types {
		generated := map[string]string{}
		for _, use := range t.Uses {
			name := parameterName(use)
			at, own := t.At, false
			for _, parameter := range t.Parameters {
				if parameter.Name == use.Parameter {
					at, own = parameter.At.Sub("name"), true
				}
			}
			if own {
				if what, taken := p.packages.Reserved(name); taken {
					p.Addf(at, "generated_name_collision", "Generated Go type parameter %s collides with the %s.", name, what)
				}
			}
			if previous, exists := generated[name]; exists && previous != use.Parameter {
				p.Addf(at, "generated_name_collision", "Generated Go type parameter %s is also generated for parameter %s.", name, previous)
			}
			generated[name] = use.Parameter
		}
	}
	// A public error becomes a constant of the protocol package, beside the
	// types and the enum constants.
	for _, e := range f.Errors {
		name, at := p.resolveOrigin(e.Origin, "errors."+e.Code, "Error"+naming.UpperCamel(e.Code), e.At)
		p.errors[e.Code] = name
		if name == "Error" {
			p.Addf(at, "invalid_name", "Public error code %s yields no Go identifier.", e.Code)
			continue
		}
		if p.identifier(name, at, "Error constant", true) {
			p.declare(p.packages, name, at, "error constant")
		}
	}
	// Operations: the server's methods are the client's members and the
	// remote's methods to implement; the client's methods the reverse.
	// Events add receive helpers as well as emit helpers, so their receiver
	// namespace crosses the sides.
	operation := func(name string, origin render.Origin, at diag.Location, what string) (string, diag.Location) {
		goName, at := p.resolveOrigin(origin, name, naming.UpperCamel(name), at)
		p.operations[name] = goName
		p.identifier(goName, at, what, true)
		return goName, at
	}
	for _, m := range f.Server.Methods {
		name, at := operation(m.Name, m.Origin, m.At, "Method name")
		p.declare(p.client, name, at, "method")
	}
	for _, m := range f.Client.Methods {
		name, at := operation(m.Name, m.Origin, m.At, "Method name")
		p.declare(p.remote, name, at, "method")
	}
	for _, e := range f.Server.Events {
		name, at := operation(e.Name, e.Origin, e.At, "Event name")
		p.declare(p.client, identOn+name, at, "event handler")
		p.declare(p.remote, identEmit+name, at, "event emitter")
	}
	for _, e := range f.Client.Events {
		name, at := operation(e.Name, e.Origin, e.At, "Event name")
		p.declare(p.client, identEmit+name, at, "event emitter")
		p.declare(p.remote, identOn+name, at, "event handler")
	}
	// An override that names nothing is the model's to refuse; one whose
	// value is not an identifier is refused above, where it is resolved.
}

// parameterName is the Go type parameter one use becomes: the contract's
// parameter followed by the type drawn from it, SEnvelope and SHandle for
// a parameter S drawn at its Envelope and its Handle. Go has no associated
// types, so a parameter drawn at two types becomes two Go parameters; the
// tag pairs them again, see tagName.
func parameterName(use render.Use) string { return use.Parameter + use.Type }

// tagName is the type parameter an entry point takes for a parameter's
// family itself: STag for a parameter S. Every type a family's protocol
// package declares carries an Of method returning the package's Tag, and
// an entry point constrains every type parameter drawn from S to
// runtime.Of of STag, so that all of them come from one family or the call
// does not compile. STag is inferred from any of them and is never spelled.
func tagName(parameter string) string { return parameter + "Tag" }
