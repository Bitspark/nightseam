package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
)

// The union stays native; the existing outgoing event adapter owns validation
// and conversion before the raw recorder admits its opaque Wire message.
func emitRecordedEvents(f *file, side, opposite, decl, args, binding, passing string) {
	_, events := f.wireSide(opposite)
	f.line("import { record as recordWire, type WireLog, type RecordOptions, type RecordedWire } from '@nightseam/duplex';")
	f.linef("import { checkIdentity } from %s;", quote(f.config.Runtime))
	var variants []string
	var payloads []model.TypeExpr
	for _, e := range events {
		variants = append(variants, fmt.Sprintf("{ readonly name: %s; readonly data: %s }", quote(e.Name), f.spell(e.Type)))
		payloads = append(payloads, e.Type)
	}
	union := "never"
	if len(variants) > 0 {
		union = strings.Join(variants, " | ")
	}
	f.line("/** The closed union of this side's outgoing native event payloads. */")
	f.linef("export type RecordedEvent%s = %s;", decl, union)
	context := f.lifetimeType("WireModelContext", "ValueContext", payloads...)
	f.linef("export interface Recorder%s extends RecordedWire { append(event: RecordedEvent%s, context?: %s): Promise<void>; }", decl, args, context)
	f.line("/** Checks a prepared origin before typed append; failed setup leaves its carrier usable. */")
	f.w.Block(fmt.Sprintf("export async function record%s(target: Wire, log: WireLog, options: RecordOptions, context: AdapterContext%s, setup?: WireCallOptions): Promise<Recorder%s> {", decl, binding, args), "}", func() {
		f.linef("const adapter = makeAdapter%s(context%s);", args, passing)
		f.line("const preparation = prepareIdentity(target, adapter.identity, adapter.options);")
		f.w.Block("try {", "} catch (error) { preparation.close(); throw error; }", func() {
			f.line("await preparation.check(setup); preparation.ready();")
			f.line("const wire = await recordWire(preparation.wire, log, { ...options, onClose(error) { preparation.close(); options.onClose?.(error); } }, setup?.signal);")
			f.linef("const events = adapter.proxy%s(wire).events;", opposite)
			f.w.Block("return {", "};", func() {
				f.line("...wire,")
				f.w.Block("async append(event, context) {", "},", func() {
					if len(events) == 0 {
						f.line("throw new Error('This side declares no outgoing events.');")
					} else {
						f.w.Block("switch (event.name) {", "}", func() {
							for _, e := range events {
								f.linef("case %s: await events.%s(event.data, context); return;", quote(e.Name), f.plan.operations[e.Name])
							}
							f.line("default: throw new Error('Unknown recorded event.');")
						})
					}
				})
				f.w.Block("async follow(after, subscriber, signal) {", "},", func() {
					f.line("await checkIdentity((method, params, options) => callWire(subscriber, [method], params, { ...options, observer: adapter.options.observer, propagator: adapter.options.propagator }), adapter.identity, { signal, timeoutMs: adapter.options.requestTimeoutMs });")
					f.line("return wire.follow(after, subscriber, signal);")
				})
			})
		})
	})
}
