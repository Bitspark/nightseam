package typescript

import (
	"fmt"
	"strings"
)

// Identity preparation installs receivers synchronously, while their model
// implementation is supplied only after the peer's declaration is checked.
func emitWireIdentity(f *file, side, opposite, decl, args, binding, passing string) {
	f.line("/** Exposes one model session at a new local wire origin. */")
	f.w.Block(fmt.Sprintf("export function toWire%s(model: Protocol.%sModel%s, context: AdapterContext%s): Wire {", decl, side, args, binding), "}", func() {
		f.linef("const adapter = makeAdapter%s(context%s);", args, passing)
		labels := []string{"...adapter.options.families"}
		for _, name := range operations(f.family) {
			labels = append(labels, "[encodePath(["+quote(name)+"])]: "+quote(f.family.Name))
		}
		f.linef("const [access, binding] = wirePair({ ...adapter.options, families: { %s } });", strings.Join(labels, ", "))
		f.w.Block("try {", "} catch (error) { access.close(); throw error; }", func() {
			f.line("registerWire(binding, [IDENTITY_METHOD], { request: identityHandler(adapter.identity) });")
			f.linef("const implementation = model(adapter.proxy%s(binding));", opposite)
			f.linef("adapter.validate%s(implementation);", side)
			f.linef("adapter.bind%s(binding, () => implementation);", side)
			f.line("return access;")
		})
	})
	f.line("/** Registers receivers before reading begins; complete checks identity before returning the one-use model factory. Close detaches this interpretation without closing its carrier. */")
	f.w.Block(fmt.Sprintf("export function prepareFromWire%s(wire: Wire, context: AdapterContext%s): { complete(options?: WireCallOptions): Promise<Protocol.%sModel%s>; close(): void } {", decl, binding, side, args), "}", func() {
		f.linef("const adapter = makeAdapter%s(context%s);", args, passing)
		f.line("const gate = prepareIdentity(wire, adapter.identity, adapter.options);")
		f.linef("let implementation: Protocol.%s%s | undefined;", opposite, args)
		f.line("let completed = false, bound = false, closed = false;")
		f.line("const detach: Array<() => void> = [];")
		f.line("const cleanup = () => { if (closed) return; closed = true; implementation = undefined; gate.close(); for (const remove of detach.reverse()) remove(); };")
		f.w.Block("try {", "} catch (error) { cleanup(); throw error; }", func() {
			f.linef("detach.push(adapter.bind%s(gate.wire, () => implementation!));", opposite)
		})
		f.w.Block("return {", "};", func() {
			f.w.Block(fmt.Sprintf("async complete(options?: WireCallOptions): Promise<Protocol.%sModel%s> {", side, args), "},", func() {
				f.line("if (closed) throw new DuplexError('scope_closed', 'model interpretation is closed');")
				f.line("if (completed) throw new DuplexError('already_bound', 'model interpretation already completed');")
				f.line("completed = true;")
				f.line("try { await gate.check(options); } catch (error) { cleanup(); throw error; }")
				f.w.Block("return remote => {", "};", func() {
					f.line("if (closed) throw new DuplexError('scope_closed', 'model interpretation is closed');")
					f.line("if (bound) throw new DuplexError('already_bound', 'model already bound');")
					f.line("bound = true;")
					f.w.Block("try {", "} catch (error) { cleanup(); throw error; }", func() {
						f.linef("adapter.validate%s(remote);", opposite)
						f.line("implementation = remote;")
						f.line("gate.ready();")
						f.linef("return adapter.proxy%s(gate.wire);", side)
					})
				})
			})
			f.line("close: cleanup,")
		})
	})
	f.line("/** Checks and interprets a wire as the same model factory; the resulting session binds once. */")
	f.w.Block(fmt.Sprintf("export async function fromWire%s(wire: Wire, context: AdapterContext%s): Promise<Protocol.%sModel%s> {", decl, binding, side, args), "}", func() {
		f.linef("const preparation = prepareFromWire%s(wire, context%s);", args, passing)
		f.line("try { return await preparation.complete(); } catch (error) { preparation.close(); throw error; }")
	})
}
