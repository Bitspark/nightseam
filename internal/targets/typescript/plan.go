package typescript

import (
	"regexp"
	"slices"
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
	identRemote        = "Remote"
	identServe         = "serve"
	identInstall       = "install"
	identCaller        = "Caller"
	identHandler       = "Handler"
	identEvents        = "Events"
	identFamily        = "Family"
	identAnyFamily     = "AnyFamily"
	identFamilyBinding = "FamilyBinding"
	identTypeBinding   = "TypeBinding"
	identSlots         = "Slots"
	identErrorCode     = "ErrorCode"
	identErrors        = "errors"
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
var imported = []string{"DuplexPeer", "DuplexError", "PeerOptions", "CallOptions", "EmitOptions", "RequestContext", "EventContext", "FrameConnection", "WebSocketLike", "Tunnel", "LiveOwner", "ValueAdapter", "ValueContext", "ValueOptions", "liveOver", "scopeOf", "conversion", "createValidator", "TypeExpression", "WireFamily"}
var globals = []string{"Array", "Record", "Promise", "Set", "Error", "String"}

// An omitted Events field must be absent on an ordinary {}. Inherited
// Object members would register built-in methods as callbacks, and their
// signatures can make {} fail to satisfy the optional Events interface.
var eventObjectMembers = []string{
	"constructor", "__defineGetter__", "__defineSetter__", "hasOwnProperty",
	"__lookupGetter__", "__lookupSetter__", "isPrototypeOf", "propertyIsEnumerable",
	"toString", "valueOf", "__proto__", "toLocaleString",
}

// Reserved is every identifier the generated module declares of itself,
// imports, or uses of the language.
func Reserved() []string {
	names := []string{identClient, identRemote, identServe, identInstall, identCaller, identHandler, identEvents, identFamily, identAnyFamily, identFamilyBinding, identTypeBinding, identSlots, identErrorCode, identErrors, identFamilyValue, identValidateWire, identProtocol}
	names = append(names, imported...)
	names = append(names, globals...)
	names = append(names, identPeer, identSlotsField, identClose, identConstructor, identThen)
	for _, name := range eventObjectMembers {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// plan is every identifier the rendering of one family declares, resolved
// from the convention and the override file, held in its namespace.
type plan struct {
	family     *render.Family
	module     *emit.Namespace // what the module declares and imports
	client     *emit.Namespace // members of Client
	remote     *emit.Namespace // members of Remote
	types      map[string]string
	operations map[string]string // method or event name → member
	errors     map[string]string // code → member of errors, quoted when not an identifier
	exports    map[string]string // live type → its export function
	imports_   map[string]string // live type → its import function
	contracts  map[string]string // callable → its contract constant
	diag.List
}

func newPlan(f *render.Family) (*plan, []diag.Diagnostic) {
	p := &plan{family: f, module: emit.NewNamespace("module"), client: emit.NewNamespace("client"), remote: emit.NewNamespace("remote"), types: map[string]string{}, operations: map[string]string{}, errors: map[string]string{}, List: diag.List{Family: f.Name}}
	p.module.Fix("generated declaration", identClient, identRemote, identServe, identInstall, identCaller, identHandler, identEvents, identFamily, identAnyFamily, identFamilyBinding, identTypeBinding, identSlots, identErrorCode, identErrors, identFamilyValue, identValidateWire, identProtocol)
	p.module.Fix("generated import", imported...)
	p.module.Fix("generated use of a global", globals...)
	p.client.Fix("generated client field", identPeer, identSlotsField)
	p.client.Fix("generated client method", identClose, identConstructor)
	// A method named then would make the client a Promise-like value,
	// breaking the async dial factory through JavaScript's thenable
	// assimilation.
	p.client.Fix("generated client's promise", identThen)
	p.remote.Fix("generated remote field", identPeer, identSlotsField)
	p.remote.Fix("generated remote method", identClose, identConstructor)
	p.remote.Fix("generated remote's promise", identThen)
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

func (p *plan) inheritedName(origin render.Origin, path, conventional string, declaredAt diag.Location) (string, diag.Location) {
	if origin.Family != "" && origin.Family != p.family.Name {
		if source := p.family.ReferencedFamily(origin.Family); source != nil {
			if name, ok := source.Override(Name, path); ok {
				return name, render.OverrideAt(Name, path)
			}
		}
		return conventional, declaredAt
	}
	return p.resolve(path, conventional, declaredAt)
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
		if t.Carried {
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
		binding := bindingName(parameter.Name)
		if what, taken := p.client.Reserved(binding); taken {
			p.Addf(parameter.At.Sub("name"), "generated_name_collision", "Generated parameter binding %s collides with the %s.", binding, what)
		} else {
			p.client.Fix("binding of parameter "+parameter.Name, binding)
			p.remote.Fix("binding of parameter "+parameter.Name, binding)
		}
	}
	p.planLive()
	for _, t := range f.Types {
		for _, parameter := range t.Parameters {
			if what, taken := p.module.Reserved(parameter.Name); taken {
				p.Addf(parameter.At.Sub("name"), "generated_name_collision", "Generated type parameter %s collides with the %s.", parameter.Name, what)
			}
		}
	}
	// A public error becomes a member of the errors object; two codes that
	// spell the same member are refused.
	members := emit.NewNamespace("errors")
	for _, e := range f.Errors {
		name, at := p.inheritedName(e.Origin, "errors."+e.Code, errorKey(e.Code), e.At)
		p.errors[e.Code] = name
		p.declare(members, name, at, "error member")
	}
	// The server's methods are the client's members; events add a receive
	// helper for the server's and an emit helper for the client's.
	operation := func(name string, at diag.Location, origin render.Origin, what string) (string, diag.Location) {
		member, at := p.inheritedName(origin, name, naming.LowerCamel(name), at)
		p.operations[name] = member
		p.identifier(member, at, what)
		return member, at
	}
	for _, m := range f.Server.Methods {
		member, at := operation(m.Name, m.At, m.Origin, "Method name")
		p.declare(p.client, member, at, "method")
	}
	for _, m := range f.Client.Methods {
		member, at := operation(m.Name, m.At, m.Origin, "Method name")
		p.declare(p.remote, member, at, "reverse method")
	}
	events := emit.NewNamespace("Events interface")
	events.Fix("generated event object's inherited member", eventObjectMembers...)
	remoteEvents := emit.NewNamespace("binding Events interface")
	remoteEvents.Fix("generated event object's inherited member", eventObjectMembers...)
	for _, e := range f.Server.Events {
		member, at := operation(e.Name, e.At, e.Origin, "Event name")
		p.declare(events, member, at, "typed event field")
		p.declare(p.client, identOn+upperFirst(member), at, "event handler")
		p.declare(p.remote, identEmit+upperFirst(member), at, "event emitter")
	}
	for _, e := range f.Client.Events {
		member, at := operation(e.Name, e.At, e.Origin, "Event name")
		p.declare(p.client, identEmit+upperFirst(member), at, "event emitter")
		p.declare(remoteEvents, member, at, "typed event field")
		p.declare(p.remote, identOn+upperFirst(member), at, "event handler")
	}
}

// references is every family the generated package depends on: the
// families it refers to.
func (p *plan) references() []string { return slices.Clone(p.family.References) }

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
