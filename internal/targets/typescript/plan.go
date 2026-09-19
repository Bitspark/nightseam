package typescript

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

// The identifiers the generated module declares of itself, or imports and
// so may not see shadowed, which a family may not name: the emitters write
// these constants and nothing else of their own.
const (
	identClient        = "Client"
	identCaller        = "Caller"
	identHandler       = "Handler"
	identEvents        = "Events"
	identFamily        = "Family"
	identAnyFamily     = "AnyFamily"
	identSessionFamily = "SessionFamily"
	identFamilyBinding = "FamilyBinding"
	identSlots         = "Slots"
	identErrorCode     = "ErrorCode"
	identErrors        = "errors"
	identDecides       = "decides"
	identAsks          = "asks"
	identConversation  = "conversation"
	identFamilyValue   = "family"
	identValidateWire  = "validateWire"
	identProtocol      = "Protocol"
	identPeer          = "peer"
	identSlotsField    = "slots"
	identClose         = "close"
	identConstructor   = "constructor"
	identThen          = "then"
	identEmit          = "emit"
	identOn            = "on"
)

// imported are the names the generated module imports from the runtime
// and the tunnel; globals are the ones of the language it uses. A type of
// either name would shadow them.
var imported = []string{"DuplexPeer", "DuplexError", "PeerOptions", "CallOptions", "RequestContext", "FrameConnection", "Tunnel", "createValidator", "TypeExpression", "WireType"}
var globals = []string{"Array", "Record", "Promise", "Set", "Error", "String"}

// Reserved is every identifier the generated module declares of itself,
// imports, or uses of the language.
func Reserved() []string {
	names := []string{identClient, identCaller, identHandler, identEvents, identFamily, identAnyFamily, identSessionFamily, identFamilyBinding, identSlots, identErrorCode, identErrors, identDecides, identAsks, identConversation, identFamilyValue, identValidateWire, identProtocol}
	names = append(names, imported...)
	names = append(names, globals...)
	names = append(names, identPeer, identSlotsField, identClose, identConstructor, identThen)
	return names
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// plan is every identifier the rendering of one family declares, resolved
// from the convention and the override file, held in its namespace.
type plan struct {
	family     *render.Family
	module     *emit.Namespace // what the module declares and imports
	client     *emit.Namespace // members of Client
	types      map[string]string
	operations map[string]string // method or event name → member
	errors     map[string]string // code → member of errors, quoted when not an identifier
	diag.List
}

func newPlan(f *render.Family) (*plan, []diag.Diagnostic) {
	p := &plan{family: f, module: emit.NewNamespace("module"), client: emit.NewNamespace("client"), types: map[string]string{}, operations: map[string]string{}, errors: map[string]string{}, List: diag.List{Family: f.Name}}
	p.module.Fix("generated declaration", identClient, identCaller, identHandler, identEvents, identFamily, identAnyFamily, identSessionFamily, identFamilyBinding, identSlots, identErrorCode, identErrors, identDecides, identAsks, identConversation, identFamilyValue, identValidateWire, identProtocol)
	p.module.Fix("generated import", imported...)
	p.module.Fix("generated use of a global", globals...)
	p.client.Fix("generated client field", identPeer, identSlotsField)
	p.client.Fix("generated client method", identClose, identConstructor)
	// A method named then would make the client a Promise-like value,
	// breaking the async dial factory through JavaScript's thenable
	// assimilation.
	p.client.Fix("generated client's promise", identThen)
	p.plan()
	diag.Sort(p.Diagnostics)
	return p, p.Diagnostics
}

func (p *plan) resolve(path, conventional string, declaredAt diag.Location) (string, diag.Location) {
	if name, ok := p.family.Override(Name, path); ok {
		return name, render.OverrideAt(Name, path)
	}
	return conventional, declaredAt
}

func (p *plan) declare(ns *emit.Namespace, ident string, at diag.Location, what string) {
	if d := ns.Declare(p.Family, ident, at, what); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
	}
}

func (p *plan) identifier(ident string, at diag.Location, what string) bool {
	if !identifierPattern.MatchString(ident) {
		p.Addf(at, "invalid_name", "%s %s must be a TypeScript identifier.", what, ident)
		return false
	}
	return true
}

func (p *plan) plan() {
	f := p.family
	for _, t := range f.Types {
		name, at := p.resolve(t.Name, t.Name, t.At)
		p.types[t.Name] = name
		if t.Injected {
			continue
		}
		if p.identifier(name, at, "Type name") {
			p.declare(p.module, name, at, "type")
		}
	}
	// A parameter becomes a type parameter of the generated declarations
	// and shadows anything of that name in the module; its binding is a
	// member of the client.
	for _, parameter := range f.Parameters {
		if what, taken := p.module.Reserved(parameter.Name); taken {
			p.Addf(parameter.At.Sub("name"), "generated_name_collision", "Generated type parameter %s collides with the %s.", parameter.Name, what)
		}
		p.client.Fix("binding of parameter "+parameter.Name, bindingName(parameter.Name))
	}
	// A public error becomes a member of the errors object; two codes that
	// spell the same member are refused.
	members := emit.NewNamespace("errors")
	for _, e := range f.Errors {
		name, at := p.resolve("errors."+e.Code, errorKey(e.Code), e.At)
		p.errors[e.Code] = name
		p.declare(members, name, at, "error member")
	}
	// The server's methods are the client's members; events add a receive
	// helper for the server's and an emit helper for the client's.
	operation := func(name string, at diag.Location, what string) (string, diag.Location) {
		member, at := p.resolve(name, naming.LowerCamel(name), at)
		p.operations[name] = member
		p.identifier(member, at, what)
		return member, at
	}
	for _, m := range f.Server.Methods {
		member, at := operation(m.Name, m.At, "Method name")
		p.declare(p.client, member, at, "method")
	}
	for _, m := range f.Client.Methods {
		operation(m.Name, m.At, "Method name")
	}
	for _, e := range f.Server.Events {
		member, at := operation(e.Name, e.At, "Event name")
		p.declare(p.client, identOn+upperFirst(member), at, "event handler")
	}
	for _, e := range f.Client.Events {
		member, at := operation(e.Name, e.At, "Event name")
		p.declare(p.client, identEmit+upperFirst(member), at, "event emitter")
	}
}

// references is every family the generated package depends on: the
// families it refers to and, when it is generic, the session families,
// whose Family types SessionFamily is the union of.
func (p *plan) references() []string {
	references := slices.Clone(p.family.References)
	if p.family.Generic {
		for _, family := range p.family.SessionFamilies {
			if !slices.Contains(references, family) {
				references = append(references, family)
			}
		}
		sort.Strings(references)
	}
	return references
}

// errorKey is the member of errors one public error becomes: the code's
// words in lower camel case, notFound for not_found, quoted when that is
// not an identifier.
func errorKey(code string) string {
	key := naming.LowerCamel(code)
	if key == "" || !identifierPattern.MatchString(key) {
		return quote(code)
	}
	return key
}

// bindingName is what a parameter's binding is called as an argument of
// the client and a field on it: the parameter in lower camel case, so that
// a parameter S is bound by an argument s.
func bindingName(parameter string) string {
	return strings.ToLower(parameter[:1]) + parameter[1:]
}

func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
