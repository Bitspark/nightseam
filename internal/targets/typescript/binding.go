package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

func emitBinding(f *file) { emitWireAdapter(f, "Server", quote(f.config.pkg(f.family.Name)+"/types")) }

// Both directions use one session-factory model. Methods and notifications
// occupy separate facets, even when their declaration names are identical.
func (f *file) emitWireModelTypes() {
	f.scope = familyScope(f.family)
	f.operationAdapters = true
	decl, args := f.declare(f.family.Uses), apply(f.family.Uses)
	for _, side := range []string{"Server", "Client"} {
		methods, events := f.wireSide(side)
		f.w.Block(fmt.Sprintf("export interface %sMethods%s {", side, decl), "}", func() {
			for _, m := range methods {
				f.linef("%s(params: %s, context?: %s): %s | Promise<%s>;", f.plan.operations[m.Name], f.request(m), f.lifetimeType("WireModelContext", "ValueContext", m.Request, m.Result), f.spell(m.Result), f.spell(m.Result))
			}
		})
		f.w.Block(fmt.Sprintf("export interface %sEvents%s {", side, decl), "}", func() {
			for _, e := range events {
				f.linef("%s(data: %s, context?: %s): void | Promise<void>;", f.plan.operations[e.Name], f.spell(e.Type), f.lifetimeType("WireModelContext", "ValueContext", e.Type))
			}
		})
		f.linef("export interface %s%s { methods: %sMethods%s; events: %sEvents%s; }", side, decl, side, args, side, args)
	}
	f.linef("export type ServerModel%s = (remote: Client%s) => Server%s;", decl, args, args)
	f.linef("export type ClientModel%s = (remote: Server%s) => Client%s;", decl, args, args)
	f.operationAdapters = false
}

// A side implements its own requests and receives the opposite side's events.
func (f *file) wireSide(side string) ([]render.Method, []render.Event) {
	if side == "Server" {
		return f.family.Server.Methods, f.family.Client.Events
	}
	return f.family.Client.Methods, f.family.Server.Events
}

func emitWireAdapter(f *file, side, protocol string) {
	fam := f.family
	f.scope = familyScope(fam)
	f.operationAdapters = true
	f.adapterReceiver = "bindings."
	f.conversion = "conversion."
	decl, args := f.declare(fam.Uses), apply(fam.Uses)
	opposite := "Client"
	if side == "Client" {
		opposite = "Server"
	}
	live := fam.Live || familyValueSlots(fam)
	f.linef("import { DuplexError, callWire, emitWire, registerWire, wirePair, createDispatcher, declarationDigest, familyTypeAdapter, validateDrawnType, jsonAdapter, identityHandler, IDENTITY_METHOD, prepareIdentity, type HandlerRegistry, type WireModelContext, type WireCallOptions } from %s;", quote(f.config.Runtime))
	f.line("import { encodePath } from '@nightseam/duplex';\nimport type { Wire, Endpoint } from '@bitspark/bitwire';")
	f.linef("import type { AdapterContext, ValueAdapter, ValueContext } from %s;", quote(f.config.Runtime))
	if fam.Live {
		f.linef("import type { LiveOwner } from %s;", quote(f.config.Live))
	}
	f.linef("import { validateWire } from %s;", protocol)
	f.linef("import type * as Protocol from %s;", protocol)
	f.linef("import type { AnyFamily, FamilyBinding, Slots } from %s;", protocol)
	f.linef("import * as conversion from %s;", protocol)
	f.liveSiblings()
	f.imports(false)
	f.linef("export * from %s;", protocol)
	f.line("export { DuplexError };")
	f.line("export type { AdapterContext };")
	var bind, pass, values []string
	for _, name := range parameters(fam.Uses) {
		bind = append(bind, bindingName(name)+": "+f.bindingType(name))
		pass = append(pass, bindingName(name))
		values = append(values, quote(name)+": "+f.bindingValue(name))
	}
	binding, passing := "", ""
	if len(bind) > 0 {
		binding = ", " + strings.Join(bind, ", ")
		passing = ", " + strings.Join(pass, ", ")
	}
	f.w.Block(fmt.Sprintf("function makeAdapter%s(context: AdapterContext%s) {", decl, binding), "}", func() {
		f.line("const options = { ...context.options };")
		f.line("const observer = options.observer;")
		f.line("const propagator = options.propagator;")
		f.line("const requestTimeoutMs = options.requestTimeoutMs;")
		f.linef("const bindings = { %s };", strings.Join(pass, ", "))
		f.linef("const slots: Slots = { %s };", strings.Join(values, ", "))
		for _, use := range fam.Uses {
			if use.Type != "" {
				f.linef("familyTypeAdapter(%s, %s);", bindingName(use.Parameter), quote(use.Type))
			} else {
				name := bindingName(use.Parameter)
				f.linef("if (typeof %s.export !== 'function' || typeof %s.import !== 'function') throw new Error('missing complete value interpretation for %s');", name, name, use.Parameter)
			}
		}
		for _, use := range fam.ObjectDraws {
			if !model.Carried(use.Type) {
				f.linef("validateDrawnType(familyTypeAdapter(%s, %s).binding, %s, true);", bindingName(use.Parameter), quote(use.Type), quote(use.Type))
			}
		}
		f.linef("const identity = { path: %s, digest: declarationDigest(validateWire, slots) };", quote(fam.Name))
		if live {
			f.line("const environment = context.valueEnvironment;")
			f.linef("if ((%s) && !environment) throw new DuplexError('scope_closed', 'model adaptation requires an explicit value environment');", f.familyLiveCondition())
		}
		f.w.Block("function hasModelHandler(facet: object, name: string): boolean {", "}", func() {
			f.w.Block("for (let current = facet; current !== null && current !== Object.prototype; current = Object.getPrototypeOf(current)) {", "}", func() {
				f.line("if (Object.prototype.hasOwnProperty.call(current, name)) return typeof (facet as Record<string, unknown>)[name] === 'function';")
			})
			f.line("return false;")
		})
		for _, name := range []string{"Server", "Client"} {
			f.emitWireProxy(name, args)
			f.emitWireRegistration(name, args)
		}
		f.line("return { options, identity, proxyServer, proxyClient, validateServer, validateClient, bindServer, bindClient };")
	})
	emitWireIdentity(f, side, opposite, decl, args, binding, passing)
	emitRecordedEvents(f, side, opposite, decl, args)
	if len(fam.Errors) > 0 {
		var members []string
		for _, e := range fam.Errors {
			members = append(members, f.plan.errors[e.Code]+": "+quote(e.Code))
		}
		f.linef("export const errors = { %s } as const;", strings.Join(members, ", "))
		f.line("export type ErrorCode = (typeof errors)[keyof typeof errors];")
	}
}

func (f *file) wireSlots() string {
	if f.family.Generic {
		return ", '$', slots"
	}
	return ""
}

func (f *file) emitWireProxy(side, args string) {
	methods, events := f.wireSide(side)
	slots := f.wireSlots()
	f.w.Block(fmt.Sprintf("function proxy%s(wire: Wire): Protocol.%s%s {", side, side, args), "}", func() {
		f.w.Block("return {", "};", func() {
			f.w.Block("methods: {", "},", func() {
				for _, m := range methods {
					f.w.Block(fmt.Sprintf("async %s(params, context) {", f.plan.operations[m.Name]), "},", func() {
						f.linef("const options = { context, signal: context?.signal, timeoutMs: context?.timeoutMs ?? requestTimeoutMs, meta: context?.outgoingMeta, observer, propagator, family: %s };", quote(f.family.Name))
						if f.liveNeeded(m.Request, m.Result) {
							f.line(f.wireOwner(false, m.Request, m.Result))
							f.linef("const result = await %s;", f.livePublish(m.Request, "params", slots, fmt.Sprintf("callWire(wire, [%s], %%s, options)", quote(m.Name))))
							f.linef("validateWire(%s, result%s);", expression(m.Result), slots)
							f.linef("return %s;", f.liveConversion(m.Result, "result", false))
						} else {
							f.linef("validateWire(%s, params%s);", requestExpression(m), slots)
							f.linef("const result = await callWire<%s>(wire, [%s], params, options);", f.spell(m.Result), quote(m.Name))
							f.linef("validateWire(%s, result%s);", expression(m.Result), slots)
							f.line("return result;")
						}
					})
				}
			})
			f.w.Block("events: {", "},", func() {
				for _, e := range events {
					f.w.Block(fmt.Sprintf("async %s(data, context) {", f.plan.operations[e.Name]), "},", func() {
						f.linef("const options = { context, meta: context?.outgoingMeta, observer, propagator, family: %s };", quote(f.family.Name))
						if f.liveNeeded(e.Type) {
							f.line(f.wireOwner(false, e.Type))
							f.linef("await %s;", f.livePublish(e.Type, "data", slots, fmt.Sprintf("(async () => { emitWire(wire, [%s], %%s, options); })()", quote(e.Name))))
						} else {
							f.linef("validateWire(%s, data%s);", expression(e.Type), slots)
							f.linef("emitWire(wire, [%s], data, options);", quote(e.Name))
						}
					})
				}
			})
		})
	})
}

func (f *file) emitWireRegistration(side, args string) {
	methods, events := f.wireSide(side)
	slots := f.wireSlots()
	var names []string
	byMethod, byEvent := map[string]render.Method{}, map[string]render.Event{}
	for _, m := range methods {
		names = append(names, m.Name)
		byMethod[m.Name] = m
	}
	for _, e := range events {
		if _, ok := byMethod[e.Name]; !ok {
			names = append(names, e.Name)
		}
		byEvent[e.Name] = e
	}
	f.w.Block(fmt.Sprintf("function validate%s(implementation: Protocol.%s%s): void {", side, side, args), "}", func() {
		f.line("if (!implementation?.methods || !implementation.events) throw new Error('model methods and events are required');")
		for _, m := range methods {
			f.linef("if (!hasModelHandler(implementation.methods, %s)) throw new Error(%s);", quote(f.plan.operations[m.Name]), quote("handler for "+m.Name+" is required"))
		}
		for _, e := range events {
			f.linef("if (!hasModelHandler(implementation.events, %s)) throw new Error(%s);", quote(f.plan.operations[e.Name]), quote("event handler for "+e.Name+" is required"))
		}
	})
	f.w.Block(fmt.Sprintf("function bind%s(wire: HandlerRegistry, implementation: () => Protocol.%s%s): () => void {", side, side, args), "}", func() {
		f.line("const detach: Array<() => void> = [];")
		f.w.Block("try {", "} catch (error) { for (const remove of detach.reverse()) remove(); throw error; }", func() {
			for _, name := range names {
				m, hasMethod := byMethod[name]
				e, hasEvent := byEvent[name]
				f.w.Block(fmt.Sprintf("detach.push(registerWire(wire, [%s], {", quote(name)), "}));", func() {
					f.linef("observer, family: %s,", quote(f.family.Name))
					if hasMethod {
						f.w.Block("request: async (raw, context) => {", "},", func() {
							f.line("const target = implementation();")
							f.linef("try { validateWire(%s, raw%s); } catch (error) { if (error instanceof DuplexError && error.code === 'contract_mismatch') throw error; throw new DuplexError('invalid_params', String(error)); }", requestExpression(m), slots)
							ctx := "context"
							if f.liveNeeded(m.Request, m.Result) {
								f.line(f.wireOwner(true, m.Request, m.Result))
								ctx = "ownedContext"
							}
							f.linef("const params = %s;", f.liveConversion(m.Request, "raw", false))
							f.linef("const result = await target.methods.%s(params as %s, %s);", f.plan.operations[m.Name], f.request(m), ctx)
							f.linef("return %s;", f.liveExport(m.Result, "result", slots))
						})
					}
					if hasEvent {
						f.w.Block("event: async (raw, context) => {", "},", func() {
							f.line("const target = implementation();")
							f.linef("try { validateWire(%s, raw%s); } catch (error) { wire.close(); throw error; }", expression(e.Type), slots)
							ctx := "context"
							if f.liveNeeded(e.Type) {
								f.line(f.wireOwner(true, e.Type))
								ctx = "ownedContext"
							}
							f.linef("await target.events.%s(%s, %s);", f.plan.operations[e.Name], f.liveConversion(e.Type, "raw", false), ctx)
						})
					}
				})
			}
		})
		f.line("return () => { for (const remove of detach.reverse()) remove(); };")
	})
}

// The environment supplies lifetime explicitly. Context inheritance preserves
// consumer-owned, non-enumerable verified values beside incoming dispatch.
func (f *file) wireOwner(incoming bool, parts ...model.TypeExpr) string {
	var lives []string
	for _, part := range parts {
		if value := f.boundaryLive(part); value != "false" {
			lives = append(lives, value)
		}
	}
	condition := "false"
	if len(lives) > 0 {
		condition = "(" + strings.Join(lives, " || ") + ")"
	}
	if incoming {
		typ := f.lifetimeType("WireModelContext", "ValueContext", parts...)
		return "const owner = " + condition + " ? environment!.child(undefined) : undefined; const ownedContext = Object.create(context) as " + typ + "; if (owner !== undefined) Object.defineProperty(ownedContext, 'valueContext', { value: owner, enumerable: true });"
	}
	return "const owner = " + condition + " ? environment!.select((context as {valueContext?: unknown} | undefined)?.valueContext) : undefined;"
}
