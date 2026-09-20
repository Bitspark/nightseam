package typescript

import (
	"fmt"
	"strings"
)

// emitBinding supplies the served role over an already accepted connection.
// Endpoint authentication and HTTP upgrade belong to the host. Protocol types
// and live conversions are shared with the client, not generated a second time.
func emitBinding(f *file) {
	p, fam := f.plan, f.family
	f.scope = familyScope(fam)
	f.conversion = "conversion."
	decl, args := f.declare(fam.Uses), apply(fam.Uses)
	names := parameters(fam.Uses)
	var bind, give, made []string
	for _, name := range names {
		bind = append(bind, bindingName(name)+": "+f.bindingType(name))
		give = append(give, bindingName(name))
		made = append(made, quote(name)+": "+bindingName(name))
	}
	binding, pass, slots := "", "", ""
	if fam.Generic {
		binding = strings.Join(bind, ", ") + ", "
		pass = strings.Join(give, ", ") + ", "
		slots = ", '$', this." + identSlotsField
	}
	protocol := quote(f.config.pkg(fam.Name) + "/types")
	f.linef("import { DuplexPeer, DuplexError, type PeerOptions, type CallOptions, type EmitOptions, type RequestContext, type EventContext, type FrameConnection, type WebSocketLike } from %s;", quote(f.config.Runtime))
	f.linef("import { %s } from %s;", identValidateWire, protocol)
	f.linef("import type * as %s from %s;", identProtocol, protocol)
	if fam.Generic {
		f.linef("import type { AnyFamily, FamilyBinding, TypeBinding, Slots } from %s;", protocol)
	}
	if fam.Live {
		f.linef("import { liveOver, scopeOf, type LiveScope } from %s;", quote(f.config.Live))
		f.linef("import * as conversion from %s;", protocol)
		f.liveSiblings()
	}
	f.imports(false)
	f.linef("export * from %s;", protocol)
	f.line("export { DuplexError };")
	f.line("/** Typed handlers for the declaration's server side. */")
	f.w.Block(fmt.Sprintf("export interface Handler%s {", decl), "}", func() {
		for _, m := range fam.Server.Methods {
			f.linef("%s(params: %s, remote: Remote%s, context: RequestContext): %s | Promise<%s>;", p.operations[m.Name], f.request(m), args, f.spell(m.Result), f.spell(m.Result))
		}
	})
	f.line("/** Client-originated event listeners installed before the connection reads its first frame. */")
	f.w.Block(fmt.Sprintf("export interface Events%s {", decl), "}", func() {
		for _, e := range fam.Client.Events {
			f.linef("%s?: (data: %s, context: EventContext) => void | Promise<void>;", p.operations[e.Name], f.spell(e.Type))
		}
	})
	f.line("/** Typed reverse calls and events for one connected client. */")
	f.w.Block(fmt.Sprintf("export class Remote%s {", decl), "}", func() {
		f.line("readonly peer: DuplexPeer;")
		if fam.Generic {
			for _, name := range names {
				f.linef("readonly %s: %s;", bindingName(name), f.bindingType(name))
			}
			f.line("readonly slots: Slots;")
		}
		constructorBinding := strings.TrimSuffix(binding, ", ")
		if constructorBinding != "" {
			constructorBinding = ", " + constructorBinding
		}
		f.w.Block(fmt.Sprintf("constructor(peer: DuplexPeer%s) {", constructorBinding), "}", func() {
			f.line("this.peer = peer;")
			if fam.Generic {
				for _, name := range names {
					f.linef("this.%s = %s;", bindingName(name), bindingName(name))
				}
				f.linef("this.slots = { %s };", strings.Join(made, ", "))
			}
		})
		f.line("close(): void { this.peer.close(); }")
		for _, m := range fam.Client.Methods {
			initial := ""
			if m.Request == nil {
				initial = "const params = {}; "
			}
			if f.liveNeeded(m.Request, m.Result) {
				f.linef("async %s(%s): Promise<%s> { %s%s const sent = %s; const result = await this.peer.call<unknown>(%s, sent, options); validateWire(%s, result%s); return %s; }", p.operations[m.Name], f.parameters(m), f.spell(m.Result), initial, f.liveScope(), f.liveExport(m.Request, "params", slots), quote(m.Name), expression(m.Result), slots, f.liveConversion(m.Result, "result", false))
			} else {
				f.linef("async %s(%s): Promise<%s> { %svalidateWire(%s, params%s); const result = await this.peer.call<%s>(%s, params, options); validateWire(%s, result%s); return result; }", p.operations[m.Name], f.parameters(m), f.spell(m.Result), initial, requestExpression(m), slots, f.spell(m.Result), quote(m.Name), expression(m.Result), slots)
			}
		}
		for _, e := range fam.Server.Events {
			if f.liveNeeded(e.Type) {
				f.linef("async emit%s(data: %s, options?: EmitOptions): Promise<void> { %s const sent = %s; await this.peer.emit(%s, sent, options); }", upperFirst(p.operations[e.Name]), f.spell(e.Type), f.liveScope(), f.liveExport(e.Type, "data", slots), quote(e.Name))
			} else {
				f.linef("async emit%s(data: %s, options?: EmitOptions): Promise<void> { validateWire(%s, data%s); await this.peer.emit(%s, data, options); }", upperFirst(p.operations[e.Name]), f.spell(e.Type), expression(e.Type), slots, quote(e.Name))
			}
		}
		for _, e := range fam.Client.Events {
			if f.liveNeeded(e.Type) {
				f.linef("on%s(handler: (data: %s, context: EventContext) => void | Promise<void>): () => void { return this.peer.onEvent(%s, (raw, context) => { %s try { validateWire(%s, raw%s); } catch(error) { this.peer.close(); throw error; } return handler(%s, context); }); }", upperFirst(p.operations[e.Name]), f.spell(e.Type), quote(e.Name), f.liveScope(), expression(e.Type), slots, f.liveConversion(e.Type, "raw", false))
			} else {
				f.linef("on%s(handler: (data: %s, context: EventContext) => void | Promise<void>): () => void { return this.peer.onEvent(%s, (data, context) => { try { validateWire(%s, data%s); } catch(error) { this.peer.close(); throw error; } return handler(data as %s, context); }); }", upperFirst(p.operations[e.Name]), f.spell(e.Type), quote(e.Name), expression(e.Type), slots, f.spell(e.Type))
			}
		}
	})
	f.line("/** Installs the typed binding before a host-configured peer starts reading. An existing live scope is preserved. */")
	f.w.Block(fmt.Sprintf("export function install%s(peer: DuplexPeer, %shandler: Handler%s, events: Events%s = {}): Remote%s {", decl, binding, args, args, args), "}", func() {
		f.line("if (!handler) throw new Error('handler is required');")
		for _, m := range fam.Server.Methods {
			f.linef("if (typeof handler.%s !== 'function') throw new Error(%s);", p.operations[m.Name], quote("handler for "+m.Name+" is required"))
		}
		remoteArgs := strings.TrimSuffix(pass, ", ")
		if remoteArgs != "" {
			remoteArgs = ", " + remoteArgs
		}
		f.linef("const remote = new Remote%s(peer%s);", args, remoteArgs)
		if fam.Live {
			f.line("if (!scopeOf(peer)) liveOver(peer, {});")
		}
		serveSlots := strings.ReplaceAll(slots, "this.", "remote.")
		serveScope := strings.ReplaceAll(f.liveScope(), "this.", "remote.")
		for _, m := range fam.Server.Methods {
			if f.liveNeeded(m.Request, m.Result) {
				f.linef("peer.handle(%s, async (raw, context) => { %s try { validateWire(%s, raw%s); } catch(error) { throw new DuplexError('invalid_params', String(error)); } const params = %s; const result = await handler.%s(params as %s, remote, context); return %s; });", quote(m.Name), serveScope, requestExpression(m), serveSlots, f.liveConversion(m.Request, "raw", false), p.operations[m.Name], f.request(m), f.liveExport(m.Result, "result", serveSlots))
			} else {
				f.linef("peer.handle(%s, async (params, context) => { try { validateWire(%s, params%s); } catch(error) { throw new DuplexError('invalid_params', String(error)); } const result = await handler.%s(params as %s, remote, context); validateWire(%s, result%s); return result; });", quote(m.Name), requestExpression(m), serveSlots, p.operations[m.Name], f.request(m), expression(m.Result), serveSlots)
			}
		}
		for _, e := range fam.Client.Events {
			f.linef("if (events.%s) remote.on%s(events.%s);", p.operations[e.Name], upperFirst(p.operations[e.Name]), p.operations[e.Name])
		}
		f.line("return remote;")
	})
	f.line("/** Serves an externally authenticated accepted connection. The returned peer is the caller's to close. */")
	f.w.Block(fmt.Sprintf("export async function serve%s(connection: FrameConnection | WebSocketLike, %soptions: PeerOptions, handler: Handler%s, events: Events%s = {}): Promise<DuplexPeer> {", decl, binding, args, args), "}", func() {
		f.linef("const peer = new DuplexPeer({ ...%s, role: 'server' });", labelled(fam))
		f.linef("try { install%s(peer, %shandler, events); await peer.attach(connection); return peer; } catch (error) { peer.close(); throw error; }", args, pass)
	})
}
