package typescript

import (
	"fmt"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/naming"
	"github.com/Bitspark/nightseam/internal/render"
)

// The live tier in TypeScript.
//
// A callable is a **function value** — the operator's verdict on #201 — so
// `ProgressSink` is an ordinary interface whose `report` member is a
// function, and each member is its own binding. The conversion is a pair of
// generated functions per live type, taking the owner the bindings belong
// to, because a live value has no meaning apart from it:
//
//	export function exportJobUnchecked(owner: LiveOwner, value: Job): unknown
//	export function importJobUnchecked(owner: LiveOwner, raw: unknown): Job
//
// Unlike Go, nothing here has to refuse an ordinary encoding: a TypeScript
// function in a value would simply be dropped by `JSON.stringify`, silently,
// which is worse than an error. The generated client and binding therefore
// never hand a live value to the peer unconverted — they call these — and
// the conversion is what the validator then sees.

// liveIdentifiers registers what the live tier declares, so that a family
// naming one of them is a diagnostic rather than a TypeScript error in the
// consumer's checkout.
func (p *plan) planLive() {
	f := p.family
	p.exports, p.imports_, p.contracts = map[string]string{}, map[string]string{}, map[string]string{}
	for _, t := range f.Types {
		if typeAdapterSupported(t) {
			p.declare(p.module, valueAdapterName(p.types[t.Name]), t.At, "value adapter factory")
		}
		if t.Carried || (!t.IsLive && len(t.Uses) == 0) {
			continue
		}
		name := p.types[t.Name]
		_, callable := f.CallableView(t)
		p.exports[t.Name] = "export" + name
		p.imports_[t.Name] = "import" + name
		if !callable {
			p.exports[t.Name] += "Unchecked"
			p.imports_[t.Name] += "Unchecked"
		}
		p.declare(p.module, p.exports[t.Name], t.At, "live export function")
		p.declare(p.module, p.imports_[t.Name], t.At, "live import function")
		if callable {
			p.contracts[t.Name] = "contract" + name
			p.declare(p.module, p.contracts[t.Name], t.At, "callable contract constant")
		}
	}
}

// liveImports is what types.ts needs for the live layer: the owner itself,
// and a **value** import of every referenced family that declares live types,
// since its conversion functions are called rather than only named. The
// ordinary import of a sibling family is type-only, which is right for a type
// and not enough for a function.
func (f *file) liveImports() {
	f.liveSiblings()
}

// liveSiblings imports the conversion functions of every referenced family
// that declares live types, as values. Both generated modules need them: the
// one that declares the conversion and the one that calls it.
func (f *file) liveSiblings() {
	arguments := f.completeFamilyImports()
	for _, family := range f.plan.references() {
		other := f.family.ReferencedFamily(family)
		if other == nil {
			continue
		}
		needed := other.Live || arguments[family]
		for _, t := range other.Types {
			needed = needed || len(t.Uses) > 0
		}
		if !needed {
			continue
		}
		f.linef("import * as %s from %s;", liveAlias(family), quote(f.config.pkg(family)))
	}
}

// Complete live helpers receive concrete family dictionaries as values even
// when that provider has no live declarations or generic conversion helpers.
// Inspect the expressions whose conversions are emitted, rather than promoting
// every ordinary type-only dependency to a family-value import.
func (f *file) completeFamilyImports() map[string]bool {
	imports := map[string]bool{}
	visit := func(expression model.TypeExpr) {
		model.Walk(expression, func(expression model.TypeExpr) bool {
			if t, arguments := f.family.Conversion(expression); t != nil && t.IsLive {
				for _, argument := range arguments {
					if argument.Family != "" {
						imports[argument.Family] = true
					}
				}
			}
			return true
		})
	}
	for _, t := range f.family.Types {
		for _, field := range t.Fields {
			visit(field.Type)
		}
		for _, variant := range t.Variants {
			visit(variant.Type)
		}
		if callable, ok := f.family.CallableView(t); ok {
			visit(callable.Request)
			visit(callable.Result)
		} else {
			visit(t.Alias)
		}
	}
	for _, side := range []render.Side{f.family.Server, f.family.Client} {
		for _, method := range side.Methods {
			visit(method.Request)
			visit(method.Result)
		}
		for _, event := range side.Events {
			visit(event.Type)
		}
	}
	return imports
}

// liveAlias is the namespace a referenced family's conversion functions are
// called through, apart from the type-only namespace its types are named in.
func liveAlias(family string) string { return "live_" + alias(family) }

// emitLive renders the live tier's types and their boundary conversion into
// types.ts, after the ordinary types it may refer to.
func (f *file) emitLive() {
	for _, t := range f.family.Types {
		if t.Carried || (!t.IsLive && len(t.Uses) == 0) {
			continue
		}
		f.scope = t.Scope
		f.codecs = t.Uses
		f.scopedCodecs = t.IsLive
		f.completeCodecs = t.IsLive && len(t.Uses) > 0
		if callable, ok := f.family.CallableView(t); ok {
			f.completeCodecs = len(t.Uses) > 0 || t.Kind == model.KindAlias
			f.emitCallable(callable)
			continue
		}
		f.emitLiveConversion(t)
	}
	f.codecs = nil
	f.scopedCodecs = false
	f.completeCodecs = false
}

// emitCallable renders one callable: the function type a consumer writes and
// calls, the identity a reference to it carries, and the two halves of the
// boundary.
func (f *file) emitCallableType(t *render.Type) {
	name := f.plan.types[t.Name]
	if t.Description != "" {
		f.linef("/** %s */", comment(t.Description))
	}
	f.line("/** A value of it is one implementation, called across the seam; each is its own binding, with its own lifetime. */")
	f.linef("export type %s%s = (%s) => Promise<%s>;", name, f.declare(t.Uses), f.callableParams(t), f.callableResult(t))
}

// emitCallable renders what the boundary needs beside the type: the identity
// a reference to it carries, and the two halves of the conversion. The type
// itself is emitted with the family's other types, in their order.
func (f *file) emitCallable(t *render.Type) {
	if len(t.Uses) > 0 || t.Declaration.Kind == model.KindAlias {
		f.emitGenericCallable(t)
		return
	}
	p := f.plan
	name := p.types[t.Name]
	f.linef("/** The declaration a reference to %s carries. It is nominal: a reference is usable exactly where this callable is expected. */", name)
	f.linef("export const %s = %s;", p.contracts[t.Name], quote(t.Contract))
	f.linef("/** Makes a binding of a local %s and answers the reference that names it. */", name)
	f.w.Block(fmt.Sprintf("export function %s(owner: LiveOwner, value: %s): unknown {", p.exports[t.Name], name), "}", func() {
		f.line("if (typeof value !== 'function') throw new TypeError('callable implementation must be a function');")
		f.w.Block("return owner.exportValue((owner) => {", "});", func() {
			f.line("const parent = owner;")
			f.w.Block(fmt.Sprintf("const reference = owner.export(%s, %s, async (request, options) => {", p.contracts[t.Name], identWireDigest), "});", func() {
				f.line("const owner = parent.child();")
				// The invoking context is inherited, never copied: what the
				// connection placed on it reaches the function, as in Go.
				f.line("const context = Object.assign(Object.create(options ?? null), { owner });")
				call := "context"
				if t.Request != nil {
					f.linef("%s(%s, request);", identValidateWire, expression(t.Request))
					// A callable's own request is converted like any other
					// position: a callable that takes a callable is handed a
					// native function, not a reference.
					f.linef("const argument = %s;", f.liveConversion(t.Request, "request", false))
					call = "argument, context"
				}
				if t.Result == nil {
					f.linef("await value(%s);", call)
					f.line("return undefined;")
					return
				}
				f.linef("const result = await value(%s);", call)
				f.linef("return %s;", f.liveExport(t.Result, "result", ""))
			})
			// The reference's wire form, not the Reference itself: what travels is
			// the identity members, and the validator that meets the converted value
			// then reads an ordinary object rather than an instance of a class.
			f.line("return reference.toJSON();")
		})
	})
	f.linef("/** A %s that calls the binding a reference names. */", name)
	f.w.Block(fmt.Sprintf("export function %s(owner: LiveOwner, raw: unknown): %s {", p.imports_[t.Name], name), "}", func() {
		f.linef("const invoke = owner.import(owner.scope.decode(raw), %s, %s);", p.contracts[t.Name], identWireDigest)
		f.line("const scope = owner.scope;")
		f.w.Block(fmt.Sprintf("return async (%s) => {", f.callableParams(t)), "};", func() {
			f.line("const owner = options?.owner?.scope === scope ? options.owner : scope.owner();")
			call := "invoke(undefined, options)"
			if t.Request != nil {
				// And what a caller sends: a callable it passes becomes a
				// binding of this owner, as it would in any other position.
				call = f.livePublish(t.Request, "request", "", "invoke(%s, options)")
			}
			if t.Result == nil {
				f.linef("await %s;", call)
				f.line("return;")
				return
			}
			f.linef("const result = await %s;", call)
			f.linef("%s(%s, result);", identValidateWire, expression(t.Result))
			f.linef("return %s;", f.liveConversion(t.Result, "result", false))
		})
	})
}

// callableParams is what a callable takes in TypeScript: its request where it
// has one, and the options a caller may pass and an implementation may read
// for the signal that says the invocation was cancelled.
func (f *file) callableParams(t *render.Type) string {
	options := "options?: { signal?: AbortSignal; owner?: LiveOwner }"
	if t.Request == nil {
		return options
	}
	return "request: " + f.spell(t.Request) + ", " + options
}

func (f *file) callableResult(t *render.Type) string {
	if t.Result == nil {
		return "void"
	}
	return f.spell(t.Result)
}

// emitLiveConversion renders the boundary for a live record, entity, union or
// alias.
func (f *file) emitLiveConversion(t *render.Type) {
	p := f.plan
	name := p.types[t.Name]
	self := name + apply(t.Uses)
	owner := ""
	if t.IsLive {
		owner = "owner: LiveOwner, "
	}
	f.line("/** Conversion only: use the value adapter or validate the entire wire value inside the same operation batch. */")
	if len(t.Uses) > 0 {
		f.linef("/** Writes %s using the supplied conversion for each type argument. */", name)
	} else {
		f.linef("/** Writes %s as it travels: each callable in it becomes a binding of the owner, and the reference that names it takes its place. */", name)
	}
	f.w.Block(fmt.Sprintf("export function %s%s(%svalue: %s%s): unknown {", p.exports[t.Name], f.declare(t.Uses), owner, self, f.converterParameters(t, true)), "}", func() {
		if f.completeCodecs {
			f.emitCompleteSlots(t.Uses)
		}
		if t.IsLive {
			f.w.Block("return owner.exportValue((owner) => {", "});", func() {
				f.liveBody(t, true)
			})
			return
		}
		f.liveBody(t, true)
	})
	if len(t.Uses) > 0 {
		f.linef("/** Reads %s using the supplied conversion for each type argument. */", name)
	} else {
		f.linef("/** Reads %s as it arrived: each reference in it becomes a typed proxy of the binding it names, so a handler is given native values. */", name)
	}
	f.line("/** Conversion only: validation belongs to the value adapter or its caller. */")
	f.w.Block(fmt.Sprintf("export function %s%s(%sraw: unknown%s): %s {", p.imports_[t.Name], f.declare(t.Uses), owner, f.converterParameters(t, false), self), "}", func() {
		if f.completeCodecs {
			f.emitCompleteSlots(t.Uses)
		}
		if t.IsLive {
			f.w.Block("return owner.importValue((owner) => {", "});", func() {
				f.liveBody(t, false)
			})
			return
		}
		f.liveBody(t, false)
	})
}

func (f *file) liveBody(t *render.Type, export bool) {
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		if export {
			if t.Open {
				f.line("const out: Record<string, unknown> = { ...value };")
			} else {
				f.line("const out: Record<string, unknown> = {};")
			}
			for _, field := range t.Fields {
				f.liveField(field, true)
			}
			f.line("return out;")
			return
		}
		f.line("const wire = raw as Record<string, unknown>;")
		if t.Open {
			f.line("const out: Record<string, unknown> = { ...wire };")
		} else {
			f.line("const out: Record<string, unknown> = {};")
		}
		for _, field := range t.Fields {
			f.liveField(field, false)
		}
		f.linef("return out as unknown as %s;", f.plan.types[t.Name]+apply(t.Uses))
	case model.KindAlias:
		f.linef("return %s;", f.liveExpr(t.Alias, source(export), export))
	case model.KindUnion:
		f.liveUnion(t, export)
	}
}

func source(export bool) string {
	if export {
		return "value"
	}
	return "raw"
}

// liveField converts one member of a live record. An absent optional member
// stays absent: presence is a fact of the member and is preserved on both
// sides of the boundary.
func (f *file) liveField(field render.Field, export bool) {
	key := quote(field.Name)
	from := "value[" + key + "]"
	if !export {
		from = "wire[" + key + "]"
	}
	convert := func(src string) string {
		if !field.Nullable {
			return f.liveExpr(field.Type, src, export)
		}
		return src + " === null ? null : " + f.liveExpr(field.Type, src, export)
	}
	if !field.Required {
		f.linef("if (%s !== undefined) out[%s] = %s;", from, key, convert(from))
		return
	}
	f.linef("out[%s] = %s;", key, convert(from))
}

// liveExpr is the expression that converts one value in the chosen
// direction. A position that carries no callable is carried across as it is:
// it is already the value the wire wants, and copying it would only risk
// changing it.
func (f *file) liveExpr(e model.TypeExpr, src string, export bool) string {
	if slot := f.valueSlot(e); slot != "" {
		method := "import"
		if export {
			method = "export"
			src = "(" + src + ") as " + f.spell(e)
		}
		return slot + "." + method + "(owner, " + src + ")"
	}
	if codec := f.parameterConverter(e); codec != "" {
		if export {
			src = "(" + src + ") as " + f.spell(e)
		}
		if f.scopedCodecs {
			src = "owner, " + src
		}
		return codec + "(" + src + ")"
	}
	if !f.needsConversion(e) {
		return src
	}
	switch x := e.(type) {
	case model.Array:
		return "(" + src + " as unknown[]).map((item) => " + f.liveExpr(x.Elem, "item", export) + ")"
	case model.Map:
		return "Object.fromEntries(Object.entries(" + src + " as Record<string, unknown>).map(([key, item]) => [key, " + f.liveExpr(x.Elem, "item", export) + "]))"
	case model.Nullable:
		return "(" + src + " === null ? null : " + f.liveExpr(x.Elem, src, export) + ")"
	default:
		return f.conversionCall(e, src, export)
	}
}

// liveCall is the generated conversion function of a named live type, in the
// module that declares it.
func (f *file) liveCall(e model.TypeExpr, export bool) string {
	family, name := "", ""
	switch x := e.(type) {
	case model.Named:
		name = x.Name
	case model.Imported:
		family, name = x.Family, x.Name
	case model.Apply:
		family, name = x.Family, x.Name
	case model.Inline:
		t := f.family.InlineType(x)
		family, name = t.Origin.Family, t.Name
	}
	if family == "" || family == f.family.Name {
		if export {
			return f.conversion + f.plan.exports[name]
		}
		return f.conversion + f.plan.imports_[name]
	}
	source := f.family.ReferencedFamily(family)
	if source != nil {
		if override, ok := source.Override(Name, name); ok {
			name = override
		}
	}
	prefix := "export"
	if !export {
		prefix = "import"
	}
	suffix := ""
	if t, _ := f.family.Conversion(e); t != nil {
		if _, callable := source.CallableView(t); !callable {
			suffix = "Unchecked"
		}
	}
	return liveAlias(family) + "." + prefix + naming.UpperCamel(name) + suffix
}

// liveUnion converts a union whose arms carry callables: the tag is written
// as it always is, and the payload by the arm it belongs to.
func (f *file) liveUnion(t *render.Type, export bool) {
	name := f.plan.types[t.Name]
	f.linef("const held = %s as Record<string, unknown>;", source(export))
	f.w.Block(fmt.Sprintf("switch (held[%s]) {", quote(t.Tag)), "}", func() {
		for _, variant := range t.Variants {
			if variant.Form == render.VariantEmpty {
				f.linef("case %s: return { %s: %s }%s;", quote(variant.Tag), quote(t.Tag), quote(variant.Tag), f.liveCast(t, export))
				continue
			}
			converted := f.liveExpr(variant.Type, "held["+quote(t.Value)+"]", export)
			f.linef("case %s: return { %s: %s, %s: %s }%s;", quote(variant.Tag), quote(t.Tag), quote(variant.Tag), quote(t.Value), converted, f.liveCast(t, export))
		}
	})
	f.linef("throw new Error(%s + String(held[%s]));", quote(name+": unknown variant "), quote(t.Tag))
}

// liveCast is the assertion an arm needs on the way in, where the union's
// TypeScript type is the destination; on the way out the value is unknown.
func (f *file) liveCast(t *render.Type, export bool) string {
	if export {
		return ""
	}
	return " as unknown as " + f.plan.types[t.Name] + apply(t.Uses)
}

// liveConversion is what the client wraps a value in when an operation
// carries callables, or the value itself when it does not. An operation may
// be live in one direction only — a request carrying a callback whose result
// is ordinary data — and on the way in the untouched side arrives as unknown,
// so it is named there.
func (f *file) liveConversion(e model.TypeExpr, src string, export bool) string {
	if e == nil {
		return src
	}
	if !f.needsConversion(e) {
		if export {
			return src
		}
		return src + " as " + f.spell(e)
	}
	if !export {
		converted := f.liveExpr(e, src, false)
		live := f.boundaryLive(e)
		if live == "false" {
			return converted
		}
		imported := "owner!.importValue((owner) => " + converted + ")"
		if f.operationAdapters {
			imported = "environment!.import(owner, (owner) => " + converted + ")"
		}
		if live != "true" {
			return "(" + live + " ? " + imported + " : " + converted + ")"
		}
		return imported
	}
	return f.liveExpr(e, src, export)
}

// liveNeeded reports whether an operation carries callables in either
// direction.
func (f *file) liveNeeded(parts ...model.TypeExpr) bool {
	for _, part := range parts {
		if part != nil && f.needsConversion(part) {
			return true
		}
	}
	return false
}
