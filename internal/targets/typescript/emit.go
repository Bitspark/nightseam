package typescript

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// file is one TypeScript file being emitted, and the prefix a type of the
// family is spelled with: none in types.ts, Protocol. in index.ts.
type file struct {
	plan   *plan
	family *render.Family
	config Config
	w      *emit.Writer
	prefix string
}

func (f *file) line(text string)                 { f.w.Line(text) }
func (f *file) linef(format string, args ...any) { f.w.Linef(format, args...) }

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// expression is a type expression as the validator reads it.
func expression(e model.TypeExpr) string { return model.String(e) }

// alias is the namespace a generated file refers to an imported family's
// types by: the family's name without its dashes.
func alias(family string) string { return strings.ReplaceAll(family, "-", "") }

// TypeScript has associated types, so one contract parameter is one type
// parameter whatever types it is drawn at: a drawn type is one of its
// associated types, S["Envelope"] or S["Payload"]. Go, which has none, takes
// one type parameter per drawn type instead.
//
// parameters is the contract parameters a set of uses names, in the order
// the uses are in, each named once.
func parameters(uses []render.Use) []string {
	var names []string
	for _, use := range uses {
		if !slices.Contains(names, use.Parameter) {
			names = append(names, use.Parameter)
		}
	}
	return names
}

// declare renders the type parameters of a declaration that makes the
// uses, each bound to any family that has the types drawn from it beyond
// the two every family carries, and defaulting to the session families'
// union, or nothing for a plain one.
func declare(uses []render.Use) string {
	names := parameters(uses)
	if len(names) == 0 {
		return ""
	}
	bound := make([]string, len(names))
	for i, name := range names {
		var drawn []string
		for _, use := range uses {
			if use.Parameter == name && !model.Carried(use.Type) {
				drawn = append(drawn, quote(use.Type)+": unknown")
			}
		}
		constraint := identAnyFamily
		if len(drawn) > 0 {
			constraint += " & { " + strings.Join(drawn, "; ") + " }"
		}
		bound[i] = name + " extends " + constraint + " = " + identSessionFamily
	}
	return "<" + strings.Join(bound, ", ") + ">"
}

// apply renders the type arguments a reference passes on, or nothing.
func apply(uses []render.Use) string {
	names := parameters(uses)
	if len(names) == 0 {
		return ""
	}
	return "<" + strings.Join(names, ", ") + ">"
}

// spell is the TypeScript type of a type expression.
func (f *file) spell(e model.TypeExpr) string {
	switch x := e.(type) {
	case model.Primitive:
		switch x {
		case "string", "boolean":
			return string(x)
		case "number", "integer":
			return "number"
		case "timestamp":
			return "string"
		default:
			return "unknown"
		}
	case model.Named:
		return f.prefix + f.plan.types[x.Name] + apply(f.family.Type(x.Name).Uses)
	case model.Imported:
		return alias(x.Family) + "." + x.Name + apply(f.family.ImportedUses(x.Family, x.Name))
	case model.Drawn:
		return x.Parameter + "[" + quote(x.Name) + "]"
	case model.Array:
		return "Array<" + f.spell(x.Elem) + ">"
	case model.Map:
		return "Record<string, " + f.spell(x.Elem) + ">"
	case model.Ref:
		t := f.family.Type(x.Entity)
		for _, field := range t.Fields {
			if field.Name == t.Key {
				return f.spell(field.Type)
			}
		}
		return "string"
	case model.Apply:
		var args []string
		for _, argument := range f.family.Arguments(x) {
			if argument.Parameter != "" {
				args = append(args, argument.Parameter)
			} else {
				args = append(args, alias(argument.Family)+"."+identFamily)
			}
		}
		rendered := alias(x.Family) + "." + x.Name
		if len(args) > 0 {
			rendered += "<" + strings.Join(args, ", ") + ">"
		}
		return rendered
	}
	return "unknown"
}

// imports emits the import lines for the families the file depends on:
// their types as a namespace and, where asked, each referred family's
// validator under a name no type can collide with.
func (f *file) imports(validators bool) {
	for _, family := range f.plan.references() {
		f.linef("import type * as %s from %s;", alias(family), quote(f.config.pkg(family)))
		if validators && slices.Contains(f.family.References, family) {
			f.linef("import { %s as validate_%s } from %s;", identValidateWire, alias(family), quote(f.config.pkg(family)))
		}
	}
}

// emitTypes renders src/types.ts: the wire types, the family's descriptor
// and the bindings a slot is filled with, and the validator, made of the
// family's wire description by the runtime.
func emitTypes(f *file) {
	p, fam := f.plan, f.family
	f.linef("import { createValidator, type %s, type %s, type %s, type TypeExpression, type WireType } from %s;", identAnyFamily, identFamilyBinding, identSlots, quote(f.config.Runtime))
	f.linef("export type { %s, %s, %s, TypeExpression };", identAnyFamily, identFamilyBinding, identSlots)
	f.imports(true)
	for _, t := range fam.Types {
		name := p.types[t.Name]
		switch t.Kind {
		case "record", "entity":
			if t.Description != "" {
				f.linef("/** %s */", comment(t.Description))
			}
			f.w.Block(fmt.Sprintf("export interface %s%s {", name, declare(t.Uses)), "}", func() {
				for _, field := range t.Fields {
					optional, null := "", ""
					if !field.Required {
						optional = "?"
					}
					if field.Nullable {
						null = " | null"
					}
					if field.Description != "" {
						f.linef("/** %s */", comment(field.Description))
					}
					f.linef("%s%s: %s%s;", quote(field.Name), optional, f.spell(field.Type), null)
				}
				if t.Open {
					f.line("[key: string]: unknown;")
				}
			})
		case "enum":
			values := make([]string, len(t.Values))
			for i, v := range t.Values {
				values[i] = quote(v)
			}
			if t.Description != "" {
				f.linef("/** %s */", comment(t.Description))
			}
			f.linef("export type %s = %s;", name, strings.Join(values, " | "))
		case "alias":
			if t.Description != "" {
				f.linef("/** %s */", comment(t.Description))
			}
			f.linef("export type %s%s = %s;", name, declare(t.Uses), f.spell(t.Alias))
		}
	}
	// The family as a slot of another family sees it: its descriptor and,
	// for a generic family, the session families' union, which a parameter
	// defaults to.
	var drawn []string
	for _, t := range fam.Types {
		if len(t.Uses) == 0 {
			drawn = append(drawn, p.types[t.Name]+": "+p.types[t.Name])
		}
	}
	f.line("/** The family: its name and the wire types a slot of it draws on. */")
	f.linef("export interface %s { readonly name: %s; %s }", identFamily, quote(fam.Name), strings.Join(drawn, "; "))
	if fam.Generic {
		union := "never"
		if len(fam.SessionFamilies) > 0 {
			names := make([]string, len(fam.SessionFamilies))
			for i, family := range fam.SessionFamilies {
				names[i] = alias(family) + "." + identFamily
			}
			union = strings.Join(names, " | ")
		}
		f.line("/** The session role: every family of the world that has a session tier. */")
		f.linef("export type %s = %s;", identSessionFamily, union)
	}
	f.line("")
	f.linef("const contractTypes = %s as unknown as Record<string, WireType>;", fam.Wire)
	var validators []string
	for _, family := range fam.References {
		validators = append(validators, quote(family)+": validate_"+alias(family))
	}
	f.line("/** Runtime validation applies equally to calls, replies, reverse calls and events; what fills a slot of a parameter is validated by the binding of the family that fills it. */")
	f.linef("export const %s = createValidator(contractTypes, { %s });", identValidateWire, strings.Join(validators, ", "))
	f.line("/** This family bound: its name and its validator, to fill a slot of the session role in another family's client. */")
	f.linef("export const %s = { name: %s, validate: %s } as const;", identFamilyValue, quote(fam.Name), identValidateWire)
}

func comment(text string) string { return strings.ReplaceAll(text, "*/", "* /") }

// request is the type of a method's params, or a record of nothing.
func (f *file) request(m render.Method) string {
	if m.Request == nil {
		return "Record<string, never>"
	}
	return f.spell(m.Request)
}

// requestExpression is what the validator checks a method's params against.
func requestExpression(m render.Method) string {
	if m.Request == nil {
		return "{ empty: true }"
	}
	return expression(m.Request)
}

// emitClient renders src/index.ts: the handler of what the server sends,
// the caller side as an interface, the session's governance and the public
// errors as data, and the client class with dial, attach and open.
func emitClient(f *file) {
	p, fam := f.plan, f.family
	decl, args := declare(fam.Uses), apply(fam.Uses)
	names := parameters(fam.Uses)
	// slots is the argument every validation of a generic client passes:
	// the families bound to the parameters, which validate what fills a slot.
	slots, binding, pass := "", "", ""
	if fam.Generic {
		bind, give := make([]string, len(names)), make([]string, len(names))
		for i, name := range names {
			bind[i] = bindingName(name) + ": " + identFamilyBinding + "<" + name + ">"
			give[i] = bindingName(name)
		}
		slots = ", '$', this." + identSlotsField
		binding = strings.Join(bind, ", ") + ", "
		pass = strings.Join(give, ", ") + ", "
	}
	f.linef("import { DuplexPeer, DuplexError, type PeerOptions, type CallOptions, type EmitOptions, type RequestContext, type EventContext, type FrameConnection } from %s;", quote(f.config.Runtime))
	f.linef("import type { Tunnel } from %s;", quote(f.config.Tunnel))
	f.linef("import { %s } from './types.ts';", identValidateWire)
	if fam.Generic {
		f.linef("import type { %s, %s, %s, %s } from './types.ts';", identAnyFamily, identFamilyBinding, identSessionFamily, identSlots)
	}
	f.linef("import type * as %s from './types.ts';", identProtocol)
	f.imports(false)
	f.line("export * from './types.ts';")
	f.line("export { DuplexError };")
	f.w.Block(fmt.Sprintf("export interface %s%s {", identHandler, decl), "}", func() {
		for _, m := range fam.Client.Methods {
			if m.Description != "" {
				f.linef("/** %s */", comment(m.Description))
			}
			f.linef("%s(params: %s, context: RequestContext): %s | Promise<%s>;", p.operations[m.Name], f.request(m), f.spell(m.Result), f.spell(m.Result))
		}
	})
	// The protocol's caller side as an interface, which the client class
	// implements; a consumer may stand another implementation in its place.
	f.w.Block(fmt.Sprintf("export interface %s%s {", identCaller, decl), "}", func() {
		for _, m := range fam.Server.Methods {
			f.linef("%s(%s): Promise<%s>;", p.operations[m.Name], f.parameters(m), f.spell(m.Result))
		}
	})
	if s := fam.Session; s != nil {
		// The session tier's governance, as data both halves read.
		f.line("/** The methods that need control to send. */")
		f.linef("export const %s: ReadonlySet<string> = new Set(%s);", identDecides, list(s.Decides))
		f.line("/** The methods the server sends that raise a request the holder of control must answer. */")
		f.linef("export const %s: ReadonlySet<string> = new Set(%s);", identAsks, list(s.Asks))
		if s.Conversation != nil {
			f.line("/** Where the agent's own conversation id arrives: the event, and the path to the id in its data. */")
			f.linef("export const %s = { event: %s, path: %s } as const;", identConversation, quote(s.Conversation.Event), quote(s.Conversation.Path))
		}
	}
	// The public errors the family declares: what a DuplexError's code may
	// be, by name.
	if len(fam.Errors) > 0 {
		var members []string
		for _, e := range fam.Errors {
			member := ""
			if e.Description != "" {
				member = "/** " + comment(e.Description) + " */ "
			}
			members = append(members, member+p.errors[e.Code]+": "+quote(e.Code))
		}
		f.line("/** The public errors of the family: what the code of a DuplexError a call rejects with may be. */")
		f.linef("export const %s = { %s } as const;", identErrors, strings.Join(members, ", "))
		f.line("/** One of the family's public error codes. */")
		f.linef("export type %s = (typeof %s)[keyof typeof %s];", identErrorCode, identErrors, identErrors)
	}
	f.w.Block(fmt.Sprintf("export class %s%s implements %s%s {", identClient, decl, identCaller, args), "}", func() {
		f.linef("readonly %s: DuplexPeer;", identPeer)
		var made []string
		if fam.Generic {
			for _, name := range names {
				f.linef("/** The family bound to %s: what fills a slot of it is validated by it. */", name)
				f.linef("readonly %s: %s<%s>;", bindingName(name), identFamilyBinding, name)
				made = append(made, quote(name)+": "+bindingName(name))
			}
			f.linef("readonly %s: %s;", identSlotsField, identSlots)
		}
		f.w.Block(fmt.Sprintf("%s(peer: DuplexPeer, %shandler?: %s%s) {", identConstructor, binding, identHandler, args), "}", func() {
			f.linef("this.%s = peer;", identPeer)
			if fam.Generic {
				for _, name := range names {
					f.linef("this.%s = %s;", bindingName(name), bindingName(name))
				}
				f.linef("this.%s = { %s };", identSlotsField, strings.Join(made, ", "))
			}
			for _, m := range fam.Client.Methods {
				f.line("if (!handler) throw new Error('reverse-call handler is required');")
				f.linef("peer.handle(%s, async (params, context) => { try { %s(%s, params%s); } catch(error) { throw new DuplexError('invalid_params', String(error)); } const result = await handler.%s(params as %s, context); %s(%s, result%s); return result; });", quote(m.Name), identValidateWire, requestExpression(m), slots, p.operations[m.Name], f.request(m), identValidateWire, expression(m.Result), slots)
			}
		})
		f.line("/** Connects to a WebSocket endpoint and speaks the family over it. */")
		f.linef("static async dial%s(url: string, %soptions: PeerOptions = {}, handler?: %s%s): Promise<%s%s> { const peer = new DuplexPeer(%s); const client = new %s%s(peer, %shandler); await peer.connect(url); return client; }", decl, binding, identHandler, args, identClient, args, labelled(fam), identClient, args, pass)
		f.line("/** Speaks the family over a connection of the seam — a tunnel channel, a pipe, an open socket — as the client side of it. */")
		f.linef("static async attach%s(connection: FrameConnection, %soptions: PeerOptions = {}, handler?: %s%s): Promise<%s%s> { const peer = new DuplexPeer(%s); const client = new %s%s(peer, %shandler); await peer.attach(connection); return client; }", decl, binding, identHandler, args, identClient, args, labelled(fam), identClient, args, pass)
		f.line("/** Resolves a handle to the channel it names on a tunnel and speaks the family over it. */")
		f.linef("static async open%s(tunnel: Tunnel, handle: %sHandle, %soptions: PeerOptions = {}, handler?: %s%s): Promise<%s%s> { const channel = tunnel.channel(handle.channel); if (!channel) throw new Error('no channel ' + handle.channel + ' on the connection'); return %s.attach%s(channel, %soptions, handler); }", decl, f.prefix, binding, identHandler, args, identClient, args, identClient, args, pass)
		f.linef("%s(): void { this.%s.close(); }", identClose, identPeer)
		for _, m := range fam.Server.Methods {
			initial := ""
			if m.Request == nil {
				initial = "const params = {}; "
			}
			if m.Description != "" {
				f.linef("/** %s */", comment(m.Description))
			}
			f.linef("async %s(%s): Promise<%s> { %s%s(%s, params%s); const result = await this.%s.call<%s>(%s, params, options); %s(%s, result%s); return result; }", p.operations[m.Name], f.parameters(m), f.spell(m.Result), initial, identValidateWire, requestExpression(m), slots, identPeer, f.spell(m.Result), quote(m.Name), identValidateWire, expression(m.Result), slots)
		}
		for _, e := range fam.Client.Events {
			f.linef("async %s%s(data: %s, options?: EmitOptions): Promise<void> { %s(%s, data%s); await this.%s.emit(%s, data, options); }", identEmit, upperFirst(p.operations[e.Name]), f.spell(e.Type), identValidateWire, expression(e.Type), slots, identPeer, quote(e.Name))
		}
		for _, e := range fam.Server.Events {
			data := f.spell(e.Type)
			f.linef("%s%s(handler: (data: %s, context: EventContext) => void | Promise<void>): () => void { return this.%s.onEvent(%s, (data, context) => { try { %s(%s, data%s); } catch(error) { this.%s.close(); throw error; } return handler(data as %s, context); }); }", identOn, upperFirst(p.operations[e.Name]), data, identPeer, quote(e.Name), identValidateWire, expression(e.Type), slots, identPeer, data)
		}
	})
}

// labelled is the options a peer of the family is made with: the caller's,
// carrying the family label of every method and event of the family — both
// sides, whichever side this peer is — beside whatever the caller labelled.
// An observer then says which family a name belongs to without parsing it.
// It is spelled where the peer is made, so that nothing of it is declared
// at the module and a family may name what it likes. open makes no peer of
// its own; it resolves the handle and attaches.
func labelled(fam *render.Family) string {
	labels := []string{"...options.families"}
	for _, name := range operations(fam) {
		labels = append(labels, quote(name)+": "+quote(fam.Name))
	}
	return "{ ...options, families: { " + strings.Join(labels, ", ") + " } }"
}

// operations is every method and event name a family declares, each side's
// methods before each side's events, in the order the sides hold them.
func operations(fam *render.Family) []string {
	names := make([]string, 0, len(fam.Server.Methods)+len(fam.Client.Methods)+len(fam.Server.Events)+len(fam.Client.Events))
	for _, m := range fam.Server.Methods {
		names = append(names, m.Name)
	}
	for _, m := range fam.Client.Methods {
		names = append(names, m.Name)
	}
	for _, e := range fam.Server.Events {
		names = append(names, e.Name)
	}
	for _, e := range fam.Client.Events {
		names = append(names, e.Name)
	}
	return names
}

// parameters is a method's signature: its params, when it takes any, and
// the call's options.
func (f *file) parameters(m render.Method) string {
	if m.Request == nil {
		return "options?: CallOptions"
	}
	return "params: " + f.request(m) + ", options?: CallOptions"
}

// list renders names as a JSON array, an absent list as an empty one.
func list(names []string) string {
	if names == nil {
		names = []string{}
	}
	data, _ := json.Marshal(names)
	return string(data)
}
