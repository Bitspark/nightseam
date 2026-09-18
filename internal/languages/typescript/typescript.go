// Package typescript renders an API family as a TypeScript client package:
// the wire types with their validator, the client, a manifest and a compiler
// configuration. It implements spi.Language and is named nowhere but where
// the tool is composed.
//
// It descends from Nightshift's dev/go/repo/generate/typescript.go and the
// TypeScript checks of its contract.go (D-001).
package typescript

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/contract"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Options places the generated package. Scope is the npm scope the package
// and the packages of the families it imports are published under, and is
// required; Runtime and Tunnel name the runtime package the client depends
// on and the tunnel package it opens channels of, RuntimeVersion the
// version of both, Nightseam's own when left empty. A path left empty is
// derived from the family's name when a contract is rendered.
type Options struct {
	Scope          string
	Runtime        string
	Tunnel         string
	RuntimeVersion string
	ClientPath     string
}

// The packages the generated client depends on unless the options name
// others: the TypeScript peer of the nightseam.duplex/1 profile and the
// tunnel over it, at the version DefaultRuntimeVersion.
const (
	DefaultRuntime        = "@nightseam/runtime"
	DefaultTunnel         = "@nightseam/tunnel"
	DefaultRuntimeVersion = "0.1.0"
)

// scopePattern is an npm scope: an @ and one package-name segment.
var scopePattern = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)

// New returns the TypeScript language with its options.
func New(options Options) spi.Language { return &language{options} }

type language struct{ options Options }

func (*language) Name() string { return "typescript" }

// settings are the options resolved for one family.
type settings struct{ scope, runtime, tunnel, runtimeVersion, dir string }

func (l *language) resolve(api contract.API) (settings, error) {
	s := settings{l.options.Scope, l.options.Runtime, l.options.Tunnel, l.options.RuntimeVersion, l.options.ClientPath}
	if !scopePattern.MatchString(s.scope) {
		return settings{}, fmt.Errorf("an npm scope such as @example is required to name the generated packages; got %q", s.scope)
	}
	if s.runtime == "" {
		s.runtime = DefaultRuntime
	}
	if s.tunnel == "" {
		s.tunnel = DefaultTunnel
	}
	if s.runtimeVersion == "" {
		s.runtimeVersion = DefaultRuntimeVersion
	}
	if s.dir == "" {
		s.dir = "api/ts/" + api.Name + "-client"
	}
	if p := s.dir; p == "." || p == ".." || strings.Contains(p, "\\") || strings.Contains(p, ":") || strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.HasPrefix(p, "../") {
		return settings{}, fmt.Errorf("invalid output path %q", p)
	}
	return s, nil
}

// Render emits the four files of the client package.
func (l *language) Render(api contract.API) ([]spi.File, error) {
	s, err := l.resolve(api)
	if err != nil {
		return nil, err
	}
	return generateTS(api, api.Generics(), s), nil
}

// reservedTypes are the identifiers the generated client declares or would
// shadow in the module that declares the contract's types — among them the
// family's own descriptor, Family, the bound and the binding of a generic
// family's parameter, and the parameter, F; a contract type of that name is
// refused.
var reservedTypes = strings.Fields("Client Caller Handler TypeExpression WireType WireField Family AnyFamily SessionFamily FamilyBinding Slots F Array Record Promise AbortSignal Date Number Object Set Error")

// A method named then would also make the client a Promise-like value, breaking
// the async dial factory through JavaScript's thenable assimilation.
var reservedMethods = strings.Fields("constructor close call notify handle connect then")
var reservedWords = strings.Fields("break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with as implements interface let package private protected public static yield any boolean number string symbol type from of namespace unknown never object")

// Check reports the names the contract would make TypeScript generate that
// it cannot: names the client reserves or the language does, and collisions
// among the client's members.
func (*language) Check(api contract.API) []contract.Diagnostic {
	diagnostics := []contract.Diagnostic{}
	add := func(code, pointer, message string) {
		diagnostics = append(diagnostics, contract.Diagnostic{Code: code, Pointer: pointer, Message: message})
	}
	for _, name := range api.TypeNames() {
		if slices.Contains(reservedTypes, name) {
			add("reserved_name", "/types/"+contract.EscapePointer(name), "Type name is reserved by the generated TypeScript client: "+name+".")
		}
	}
	// The TypeScript namespace is shared across methods and events in each direction.
	operations := map[string]string{}
	operation := func(kind string, index int, tsName, direction string) {
		p := fmt.Sprintf("/%s/%d", kind, index)
		key := direction + ":" + tsName
		if previous, ok := operations[key]; ok {
			add("operation_collision", p+"/ts_name", "Operation name collides with "+previous+".")
		} else {
			operations[key] = p
		}
		if slices.Contains(reservedMethods, tsName) {
			add("reserved_name", p+"/ts_name", "Operation name is reserved by the generated TypeScript client.")
		}
		if slices.Contains(reservedWords, tsName) {
			add("reserved_name", p+"/ts_name", "Operation name is a reserved TypeScript identifier.")
		}
	}
	for i, method := range api.Methods {
		operation("methods", i, method.TSName, method.Direction)
	}
	for i, event := range api.Events {
		operation("events", i, event.TSName, event.Direction)
	}
	// Events add receive helpers as well as emit helpers, so their receiver
	// namespace crosses the protocol's direction boundary.
	client := map[string]string{"peer": "generated client field", "family": "generated client field", "slots": "generated client field", "close": "generated client method", "constructor": "generated client constructor"}
	register := func(name, pointer string) {
		if previous, exists := client[name]; exists {
			add("generated_name_collision", pointer, "Generated member "+name+" collides with "+previous+".")
		} else {
			client[name] = pointer
		}
	}
	for i, method := range api.Methods {
		if method.Direction == "client_to_server" {
			register(method.TSName, fmt.Sprintf("/methods/%d/ts_name", i))
		}
	}
	for i, event := range api.Events {
		p := fmt.Sprintf("/events/%d/ts_name", i)
		if event.Direction == "client_to_server" {
			register("emit"+upperFirst(event.TSName), p)
		} else {
			register("on"+upperFirst(event.TSName), p)
		}
	}
	return diagnostics
}

// parameter is the one type parameter a generic family has: the family that
// fills its slots of the session role. TypeScript has associated types, so a
// slot is one of the parameter's, F["Envelope"] or F["Handle"], and a family
// is one parameter whatever slots it holds.
const parameter = "F"

// declare renders the type parameter of a declaration that uses the kinds,
// bound to any family and defaulting to the session role's union, or
// nothing for a plain one.
func declare(kinds []string) string {
	if len(kinds) == 0 {
		return ""
	}
	return "<" + parameter + " extends AnyFamily = SessionFamily>"
}

// apply renders the type argument a reference passes on, or nothing.
func apply(kinds []string) string {
	if len(kinds) == 0 {
		return ""
	}
	return "<" + parameter + ">"
}

// tsAlias is the namespace a generated file refers to an imported family's
// types by: the family's name without its dashes.
func tsAlias(family string) string { return strings.ReplaceAll(family, "-", "") }

// tsPackage is the package a family's client is published as: its name
// under the scope, as -client.
func tsPackage(scope, family string) string { return scope + "/" + family + "-client" }

// sessionMembers is every family of the world that declares the session
// role, other than this one: what SessionFamily is the union of.
func sessionMembers(api contract.API) []string {
	var members []string
	for _, family := range api.Sessions {
		if family != api.Name {
			members = append(members, family)
		}
	}
	return members
}

// tsReferences is every family the generated package depends on: the
// families it refers to and, when it is generic, the session role's
// members, whose Family types SessionFamily is the union of.
func tsReferences(api contract.API, g contract.Generics) []string {
	references := api.References()
	if g.Generic() {
		for _, family := range sessionMembers(api) {
			if !slices.Contains(references, family) {
				references = append(references, family)
			}
		}
		sort.Strings(references)
	}
	return references
}

// tsImports is the import lines a generated file needs for the families it
// depends on: their types as a namespace and, where asked, the validator of
// each family it refers to, under a name no type can collide with.
func tsImports(api contract.API, g contract.Generics, scope string, validators bool) string {
	var b strings.Builder
	validated := api.References()
	for _, family := range tsReferences(api, g) {
		fmt.Fprintf(&b, "import type * as %s from %s;\n", tsAlias(family), quote(tsPackage(scope, family)))
		if validators && slices.Contains(validated, family) {
			fmt.Fprintf(&b, "import { validateWire as validate_%s } from %s;\n", tsAlias(family), quote(tsPackage(scope, family)))
		}
	}
	return b.String()
}

func quote(value string) string      { encoded, _ := json.Marshal(value); return string(encoded) }
func expression(value any) string    { encoded, _ := json.Marshal(value); return string(encoded) }
func canonicalJSON(value any) []byte { data, _ := json.Marshal(value); return data }

// slotType is what fills a slot: an associated type of the parameter for
// the session role, the named family's own type otherwise.
func slotType(kind, family string) string {
	if family == contract.SessionRole {
		return parameter + "[" + quote(contract.SlotType(kind)) + "]"
	}
	return tsAlias(family) + "." + contract.SlotType(kind)
}

func tsType(g contract.Generics, expression any) string {
	switch t := expression.(type) {
	case string:
		switch t {
		case "string", "boolean":
			return t
		case "number", "integer":
			return "number"
		case "timestamp":
			return "string"
		case "json":
			return "unknown"
		default:
			if family, name, ok := contract.Reference(t); ok {
				return tsAlias(family) + "." + name + apply(g.Imported[family][name])
			}
			return t + apply(g.Types[t])
		}
	case map[string]any:
		if kind, family, ok := contract.Slot(t); ok {
			return slotType(kind, family)
		}
		if item, ok := t["array"]; ok {
			return "Array<" + tsType(g, item) + ">"
		}
		if item, ok := t["map"]; ok {
			return "Record<string, " + tsType(g, item) + ">"
		}
	}
	return "unknown"
}

// generateTS renders the four files of the client package into s.dir.
func generateTS(api contract.API, g contract.Generics, s settings) []spi.File {
	var files []spi.File
	var types strings.Builder
	types.WriteString(spi.Header)
	types.WriteString(tsImports(api, g, s.scope, true))
	for _, name := range api.TypeNames() {
		t := api.Types[name]
		kinds := g.Types[name]
		switch t.Kind {
		case "record":
			fmt.Fprintf(&types, "export interface %s%s", name, declare(kinds))
			types.WriteString(" {\n")
			fields := api.FlattenedFields(name)
			for _, field := range fields {
				optional := ""
				if !field.Required {
					optional = "?"
				}
				null := ""
				if field.Nullable {
					null = " | null"
				}
				fmt.Fprintf(&types, "  %s%s: %s%s;\n", quote(field.Name), optional, tsType(g, field.Type), null)
			}
			if t.Open {
				types.WriteString("  [key: string]: unknown;\n")
			}
			types.WriteString("}\n")
		case "enum":
			values := make([]string, len(t.Values))
			for i, v := range t.Values {
				values[i] = quote(v)
			}
			fmt.Fprintf(&types, "export type %s = %s;\n", name, strings.Join(values, " | "))
		case "alias":
			fmt.Fprintf(&types, "export type %s%s = %s;\n", name, declare(kinds), tsType(g, t.Type))
		}
	}
	// The family as a slot of another family sees it: its descriptor, the
	// bound and the binding of a parameter, and, for a generic family, the
	// session role's union, which the parameter defaults to.
	fmt.Fprintf(&types, "/** The family: its name and the wire types a slot of it draws on. */\nexport interface Family { readonly name: %s; Envelope: Envelope; Handle: Handle }\n", quote(api.Name))
	types.WriteString("/** What a slot of the session role is filled with: any family. */\nexport interface AnyFamily { readonly name: string; Envelope: unknown; Handle: unknown }\n")
	types.WriteString("/** A family bound at runtime: its name and its validator, which validates what fills a slot of it. */\nexport interface FamilyBinding<F extends AnyFamily> { readonly name: F[\"name\"]; validate(type: TypeExpression, value: unknown, location?: string): void }\n")
	types.WriteString("/** The families bound to the roles a value's slots name. */\nexport type Slots = { readonly [role: string]: FamilyBinding<AnyFamily> };\n")
	if g.Generic() {
		union := "never"
		if members := sessionMembers(api); len(members) > 0 {
			names := make([]string, len(members))
			for i, family := range members {
				names[i] = tsAlias(family) + ".Family"
			}
			union = strings.Join(names, " | ")
		}
		fmt.Fprintf(&types, "/** The session role: every family of the world that declares it. */\nexport type SessionFamily = %s;\n", union)
	}
	types.WriteString("\nconst contractTypes = ")
	types.Write(canonicalJSON(api.Types))
	types.WriteString(" as unknown as Record<string, WireType>;\n")
	types.WriteString("const importedValidators: Record<string, (type: TypeExpression, value: unknown, location?: string, slots?: Slots) => void> = {")
	for i, family := range api.References() {
		if i > 0 {
			types.WriteString(", ")
		}
		fmt.Fprintf(&types, "%s: validate_%s", quote(family), tsAlias(family))
	}
	types.WriteString("};\n")
	types.WriteString(strings.NewReplacer(
		"SESSION_ROLE", quote(contract.SessionRole),
		"ENVELOPE_TYPE", quote(contract.EnvelopeType),
		"HANDLE_TYPE", quote(contract.HandleType),
	).Replace(tsValidationTemplate))
	fmt.Fprintf(&types, "/** This family bound: its name and its validator, to fill a slot of the session role in another family's client. */\nexport const family = { name: %s, validate: validateWire } as const;\n", quote(api.Name))
	files = append(files, spi.File{Path: path.Join(s.dir, "src/types.ts"), Data: []byte(types.String())})

	decl, args := declare(g.Family), apply(g.Family)
	// slots is the argument every validation of a generic client passes: the
	// family bound to the session role, which validates what fills a slot.
	slots, binding, pass := "", "", ""
	if g.Generic() {
		slots, binding, pass = ", '$', this.slots", "family: FamilyBinding<F>, ", "family, "
	}
	var client strings.Builder
	client.WriteString(spi.Header)
	client.WriteString("import { DuplexPeer, DuplexError, type PeerOptions, type CallOptions, type RequestContext, type FrameConnection } from " + quote(s.runtime) + ";\nimport type { Tunnel } from " + quote(s.tunnel) + ";\nimport { validateWire } from './types.ts';\n")
	if g.Generic() {
		client.WriteString("import type { AnyFamily, FamilyBinding, SessionFamily, Slots } from './types.ts';\n")
	}
	client.WriteString("import type * as Protocol from './types.ts';\n" + tsImports(api, g, s.scope, false) + "export * from './types.ts';\nexport { DuplexError };\n")
	fmt.Fprintf(&client, "export interface Handler%s {\n", decl)
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			fmt.Fprintf(&client, "  %s(params: %s, context: RequestContext): %s | Promise<%s>;\n", m.TSName, tsRequest(g, m), tsQualified(g, m.Result), tsQualified(g, m.Result))
		}
	}
	client.WriteString("}\n")
	// The RPC layer's caller side as an interface, which the session class
	// implements; a consumer may stand another implementation in its place.
	fmt.Fprintf(&client, "export interface Caller%s {\n", decl)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			parameters := "params: " + tsRequest(g, m) + ", options?: CallOptions"
			if m.Request == "" {
				parameters = "options?: CallOptions"
			}
			fmt.Fprintf(&client, "  %s(%s): Promise<%s>;\n", m.TSName, parameters, tsQualified(g, m.Result))
		}
	}
	client.WriteString("}\n")
	if s := api.Session; s != nil {
		// The sess layer's governance, as data both halves read.
		fmt.Fprintf(&client, "/** The methods that need control to send. */\nexport const decides: ReadonlySet<string> = new Set(%s);\n", expression(orEmpty(s.Decides)))
		fmt.Fprintf(&client, "/** The server-to-client methods that raise a request the holder of control must answer. */\nexport const asks: ReadonlySet<string> = new Set(%s);\n", expression(orEmpty(s.Asks)))
		if s.Conversation != nil {
			fmt.Fprintf(&client, "/** Where the agent's own conversation id arrives: the event, and the path to the id in its data. */\nexport const conversation = { event: %s, path: %s } as const;\n", quote(s.Conversation.Event), quote(s.Conversation.Path))
		}
	}
	fmt.Fprintf(&client, "export class Client%s implements Caller%s {\n  readonly peer: DuplexPeer;\n", decl, args)
	if g.Generic() {
		client.WriteString("  /** The family bound to the session role: what fills a slot of it is validated by it. */\n  readonly family: FamilyBinding<F>;\n  readonly slots: Slots;\n")
	}
	fmt.Fprintf(&client, "  constructor(peer: DuplexPeer, %shandler?: Handler%s) {\n    this.peer = peer;\n", binding, args)
	if g.Generic() {
		fmt.Fprintf(&client, "    this.family = family;\n    this.slots = { %s: family };\n", quote(contract.SessionRole))
	}
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			fmt.Fprintf(&client, "    if (!handler) throw new Error('reverse-call handler is required');\n    peer.handle(%q, async (params, context) => { try { validateWire(%s, params%s); } catch(error) { throw new DuplexError('invalid_params', String(error)); } const result = await handler.%s(params as %s, context); validateWire(%s, result%s); return result; });\n", m.Name, tsRequestExpr(m), slots, m.TSName, tsRequest(g, m), expression(m.Result), slots)
		}
	}
	fmt.Fprintf(&client, "  }\n  /** Connects to a WebSocket endpoint and speaks the family over it. */\n  static async dial%s(url: string, %soptions: PeerOptions = {}, handler?: Handler%s): Promise<Client%s> { const peer = new DuplexPeer(options); const client = new Client%s(peer, %shandler); await peer.connect(url); return client; }\n", decl, binding, args, args, args, pass)
	fmt.Fprintf(&client, "  /** Speaks the family over a connection of the seam — a tunnel channel, a pipe, an open socket — as the client side of it. */\n  static async attach%s(connection: FrameConnection, %soptions: PeerOptions = {}, handler?: Handler%s): Promise<Client%s> { const peer = new DuplexPeer(options); const client = new Client%s(peer, %shandler); await peer.attach(connection); return client; }\n", decl, binding, args, args, args, pass)
	fmt.Fprintf(&client, "  /** Resolves a handle to the channel it names on a tunnel and speaks the family over it. */\n  static async open%s(tunnel: Tunnel, handle: Protocol.Handle, %soptions: PeerOptions = {}, handler?: Handler%s): Promise<Client%s> { const channel = tunnel.channel(handle.channel); if (!channel) throw new Error('no channel ' + handle.channel + ' on the connection'); return Client.attach%s(channel, %soptions, handler); }\n  close(): void { this.peer.close(); }\n", decl, binding, args, args, args, pass)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			parameters := "params: " + tsRequest(g, m) + ", options?: CallOptions"
			initial := ""
			if m.Request == "" {
				parameters = "options?: CallOptions"
				initial = "const params = {}; "
			}
			fmt.Fprintf(&client, "  async %s(%s): Promise<%s> { %svalidateWire(%s, params%s); const result = await this.peer.call<%s>(%q, params, options); validateWire(%s, result%s); return result; }\n", m.TSName, parameters, tsQualified(g, m.Result), initial, tsRequestExpr(m), slots, tsQualified(g, m.Result), m.Name, expression(m.Result), slots)
		}
	}
	for _, event := range api.Events {
		data := tsQualified(g, event.Type)
		if event.Direction == "client_to_server" {
			fmt.Fprintf(&client, "  async emit%s(data: %s): Promise<void> { validateWire(%s, data%s); await this.peer.emit(%q, data); }\n", upperFirst(event.TSName), data, expression(event.Type), slots, event.Name)
		} else {
			fmt.Fprintf(&client, "  on%s(handler: (data: %s) => void | Promise<void>): () => void { return this.peer.onEvent(%q, (data) => { try { validateWire(%s, data%s); } catch(error) { this.peer.close(); throw error; } return handler(data as %s); }); }\n", upperFirst(event.TSName), data, event.Name, expression(event.Type), slots, data)
		}
	}
	client.WriteString("}\n")
	files = append(files, spi.File{Path: path.Join(s.dir, "src/index.ts"), Data: []byte(client.String())})
	dependencies := map[string]any{s.runtime: s.runtimeVersion, s.tunnel: s.runtimeVersion}
	for _, family := range tsReferences(api, g) {
		dependencies[tsPackage(s.scope, family)] = "0.0.0"
	}
	manifest := map[string]any{"name": tsPackage(s.scope, api.Name), "version": "0.0.0", "private": true, "type": "module", "exports": "./src/index.ts", "scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": dependencies}
	files = append(files, spi.File{Path: path.Join(s.dir, "package.json"), Data: append(canonicalJSON(manifest), '\n')})
	files = append(files, spi.File{Path: path.Join(s.dir, "tsconfig.json"), Data: []byte("{\"compilerOptions\":{\"target\":\"ES2022\",\"module\":\"NodeNext\",\"moduleResolution\":\"NodeNext\",\"strict\":true,\"skipLibCheck\":true,\"noEmit\":true,\"allowImportingTsExtensions\":true,\"lib\":[\"ES2022\",\"DOM\"]},\"include\":[\"src/**/*.ts\"]}\n")})
	return files
}

// orEmpty renders an absent list as an empty one.
func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
func tsRequest(g contract.Generics, m contract.Method) string {
	if m.Request == "" {
		return "Record<string, never>"
	}
	return "Protocol." + m.Request + apply(g.Types[m.Request])
}
func tsRequestExpr(m contract.Method) string {
	if m.Request == "" {
		return "{ empty: true }"
	}
	return quote(m.Request)
}
func tsQualified(g contract.Generics, expr any) string {
	switch t := expr.(type) {
	case string:
		switch t {
		case "string", "boolean", "number", "integer", "timestamp", "json":
			return tsType(g, t)
		default:
			if family, name, ok := contract.Reference(t); ok {
				return tsAlias(family) + "." + name + apply(g.Imported[family][name])
			}
			return "Protocol." + t + apply(g.Types[t])
		}
	case map[string]any:
		if kind, family, ok := contract.Slot(t); ok {
			return slotType(kind, family)
		}
		if item, ok := t["array"]; ok {
			return "Array<" + tsQualified(g, item) + ">"
		}
		if item, ok := t["map"]; ok {
			return "Record<string, " + tsQualified(g, item) + ">"
		}
	}
	return "unknown"
}

const tsValidationTemplate = `
type TypeExpression = string | {array: TypeExpression} | {map: TypeExpression} | {empty: true} | {envelope: string} | {connection: string};
interface WireField {name: string;type: TypeExpression;required?: boolean;nullable?: boolean}
interface WireType {kind: string;fields?: WireField[];extends?: string[];open?: boolean;values?: string[];type?: TypeExpression}
function fields(name: string): WireField[] {const type = contractTypes[name]!; return [...(type.extends ?? []).flatMap(fields), ...(type.fields ?? [])];}
function timestamp(value: unknown): boolean {if(typeof value!=='string')return false;const m=/^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|([+-])(\d\d):(\d\d))$/.exec(value);if(!m)return false;const year=Number(m[1]),month=Number(m[2]),day=Number(m[3]);const leap=year%4===0&&(year%100!==0||year%400===0);const days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31];return month>=1&&month<=12&&day>=1&&day<=days[month-1]!&&Number(m[4])<=23&&Number(m[5])<=59&&Number(m[6])<=59&&(!m[7]||(Number(m[8])<=23&&Number(m[9])<=59))&&!Number.isNaN(Date.parse(value));}
function jsonValue(value: unknown, seen = new Set<object>()): void {if (value === null || typeof value === 'string' || typeof value === 'boolean') return; if (typeof value === 'number' && Number.isFinite(value)) return; if (typeof value !== 'object' || seen.has(value)) throw new Error('expected finite acyclic JSON'); seen.add(value); if (Array.isArray(value)) for (const child of value) jsonValue(child,seen); else {if(Object.getPrototypeOf(value)!==Object.prototype && Object.getPrototypeOf(value)!==null)throw new Error('expected plain JSON object'); for(const child of Object.values(value))jsonValue(child,seen);} seen.delete(value);}
/** What fills a slot is validated by the family that fills it: a named family's validator, or the binding of the session role passed in, since the family that fills a slot of the role is chosen where the client is instantiated. */
function slot(type: string, family: string, value: unknown, location: string, slots?: Slots): void {
 if(family===SESSION_ROLE){const binding=slots?.[family];if(!binding)throw new Error(location+': expected a binding of the '+family+' role');binding.validate(type,value,location);return;}
 const validate=importedValidators[family];if(!validate)throw new Error(location+': expected known family');validate(type,value,location,slots);
}
/** Runtime validation applies equally to calls, replies, reverse calls and events. */
export function validateWire(type: TypeExpression, value: unknown, location = '$', slots?: Slots): void {
 const bad = (expected: string): never => {throw new Error(location + ': expected ' + expected);};
 if(typeof type === 'object') {
  if('envelope' in type) {slot(ENVELOPE_TYPE,type.envelope,value,location,slots);return;}
  if('connection' in type) {slot(HANDLE_TYPE,type.connection,value,location,slots);return;}
  if('array' in type) {if(!Array.isArray(value))bad('array'); let index=0;for(const item of value as unknown[])validateWire(type.array,item,location+'['+(index++)+']',slots);return;}
  if(value===null||typeof value!=='object'||Array.isArray(value)||(Object.getPrototypeOf(value)!==Object.prototype&&Object.getPrototypeOf(value)!==null))bad('plain object');
  if('map' in type){for(const [key,item]of Object.entries(value as Record<string,unknown>))validateWire(type.map,item,location+'.'+key,slots);return;}
  if(Object.keys(value as object).length!==0)bad('empty object');return;
 }
 if(typeof type==='string'&&type.includes('.')){const at=type.indexOf('.');const validate=importedValidators[type.slice(0,at)];if(!validate)bad('known family');validate!(type.slice(at+1),value,location,slots);return;}
 switch(type){
 case 'json':jsonValue(value);return;
 case 'string':if(typeof value!=='string')bad('string');return;
 case 'boolean':if(typeof value!=='boolean')bad('boolean');return;
 case 'number':if(typeof value!=='number'||!Number.isFinite(value))bad('finite number');return;
 case 'integer':if(typeof value!=='number'||!Number.isSafeInteger(value))bad('JavaScript-safe integer');return;
 case 'timestamp':if(!timestamp(value))bad('RFC3339 timestamp');return;
 }
 const definition = contractTypes[type];if(!definition)bad('known type');
 if(definition.kind==='alias'){validateWire(definition.type!,value,location,slots);return;}
 if(definition.kind==='enum'){if(typeof value!=='string'||!definition.values!.includes(value))bad(type);return;}
 if(definition.kind!=='record')bad('supported type');
 if(value===null||typeof value!=='object'||Array.isArray(value)||(Object.getPrototypeOf(value)!==Object.prototype&&Object.getPrototypeOf(value)!==null))bad(type+' plain object');
 const object = value as Record<string,unknown>, allowed = new Set<string>();
 for(const field of fields(type)){allowed.add(field.name);if(!Object.hasOwn(object,field.name)){if(field.required!==false)bad('required field '+field.name);continue;} const child=object[field.name];if(child===null&&field.nullable)continue;if(child===null)bad('non-null field '+field.name);validateWire(field.type,child,location+'.'+field.name,slots);}
 for(const key of Object.keys(object)){if(!allowed.has(key)){if(!definition.open)bad('known field '+key);jsonValue(object[key]);}}
}
`
