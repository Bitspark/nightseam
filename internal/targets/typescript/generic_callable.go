package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

func (f *file) completeParameters(uses []render.Use) string {
	var out []string
	for _, name := range parameters(uses) {
		typ := "ValueAdapter<" + name + ">"
		if parameter, _ := f.parameter(name); parameter.IsFamily() {
			typ = familyBindingType(name, uses)
		}
		out = append(out, "slot_"+name+": "+typ)
	}
	return strings.Join(out, ", ")
}

func (f *file) completeBindings(uses []render.Use) string {
	var out []string
	for _, name := range parameters(uses) {
		value := "slot_" + name
		if parameter, _ := f.parameter(name); !parameter.IsFamily() {
			value += ".binding"
		}
		out = append(out, quote(name)+": "+value)
	}
	return "{ " + strings.Join(out, ", ") + " }"
}

func (f *file) emitCompleteSlots(uses []render.Use) {
	for _, use := range uses {
		if use.Type != "" {
			f.linef("familyTypeAdapter(slot_%s, %s);", use.Parameter, quote(use.Type))
		} else {
			f.linef("if (typeof slot_%s.export !== 'function' || typeof slot_%s.import !== 'function') throw new Error('missing complete value interpretation for %s');", use.Parameter, use.Parameter, use.Parameter)
		}
	}
	f.linef("const slots: Slots = %s;", f.completeBindings(uses))
}

func (f *file) completeArguments(arguments []render.Argument) []string {
	var out []string
	seen := map[string]bool{}
	for _, argument := range arguments {
		if seen[argument.Use.Parameter] {
			continue
		}
		seen[argument.Use.Parameter] = true
		if argument.Type != nil {
			out = append(out, f.expressionAdapter(argument.Type))
		} else if argument.Parameter != "" {
			value := "slot_" + argument.Parameter
			if f.operationAdapters {
				value = f.adapterReceiver + bindingName(argument.Parameter)
			}
			out = append(out, value)
		} else if argument.Family == f.family.Name {
			out = append(out, f.conversion+identFamilyValue)
		} else {
			out = append(out, liveAlias(argument.Family)+"."+identFamilyValue)
		}
	}
	return out
}

// A retained expression recipe closes over bindings only. Each invocation
// supplies its current owner, including when a callable contains a callable.
func (f *file) expressionAdapter(e model.TypeExpr) string {
	if slot := f.valueSlot(e); slot != "" {
		return slot
	}
	validation := expression(e)
	slots := "undefined"
	if f.completeCodecs || f.operationAdapters && f.family.Generic {
		slots = "slots"
	}
	binding := "{ type: " + validation + ", validate: validateWire, slots: " + slots + " }"
	if !f.needsConversion(e) {
		return "jsonAdapter<" + f.spell(e) + ">(" + binding + ")"
	}
	export := f.liveExpr(e, "input", true)
	imported := f.liveExpr(e, "input", false)
	ownerType := "unknown"
	if f.family.Live {
		ownerType = "LiveOwner"
	}
	return fmt.Sprintf("({ binding: %s, needsContext: %s, export(context: unknown, input: %s): unknown { const owner = context as %s; const converted = %s; validateWire(%s, converted, '$', %s); return converted; }, import(context: unknown, input: unknown): %s { const owner = context as %s; validateWire(%s, input, '$', %s); return (%s) as %s; } })", binding, f.boundaryLive(e), f.spell(e), ownerType, export, validation, slots, f.spell(e), ownerType, validation, slots, imported, f.spell(e))
}

func (f *file) emitGenericCallable(t *render.Type) {
	name := f.plan.types[t.Name]
	self, declaration := name+apply(t.Uses), f.declare(t.Uses)
	parameters := f.completeParameters(t.Uses)
	more := ""
	if parameters != "" {
		more = ", " + parameters
	}
	f.line("/** Canonical identity of this closed nominal callable application. */")
	f.w.Block(fmt.Sprintf("export function %s%s(%s): DeclarationIdentity {", f.plan.contracts[t.Name], declaration, parameters), "}", func() {
		f.emitCompleteSlots(t.Uses)
		f.linef("return callableIdentity({ type: %s, validate: validateWire, slots });", quote(t.Name))
	})
	f.line("/** Exports an implementation with complete recipes retained for its later invocations. */")
	f.w.Block(fmt.Sprintf("export function %s%s(owner: LiveOwner, value: %s%s): unknown {", f.plan.exports[t.Name], declaration, self, more), "}", func() {
		f.emitCompleteSlots(t.Uses)
		f.linef("const identity = callableIdentity({ type: %s, validate: validateWire, slots });", quote(t.Name))
		f.line("if (typeof value !== 'function') throw new TypeError('callable implementation must be a function');")
		f.w.Block("return owner.exportValue((owner) => {", "});", func() {
			f.line("const parent = owner;")
			f.w.Block("const reference = owner.export(identity.path, identity.digest ?? '', async (request, options) => {", "});", func() {
				f.line("const owner = parent.child();")
				f.line("const context = { ...options, owner };")
				call := "context"
				if t.Request != nil {
					f.linef("validateWire(%s, request, '$', slots);", expression(t.Request))
					f.linef("const argument = %s;", f.liveConversion(t.Request, "request", false))
					call = "argument, context"
				}
				if t.Result == nil {
					f.linef("await value(%s);", call)
					f.line("return undefined;")
				} else {
					f.linef("const result = await value(%s);", call)
					f.linef("return %s;", f.liveExport(t.Result, "result", ", '$', slots"))
				}
			})
			f.line("return reference.toJSON();")
		})
	})
	f.line("/** Imports a callable whose conversions use the owner of each invocation. */")
	f.w.Block(fmt.Sprintf("export function %s%s(owner: LiveOwner, raw: unknown%s): %s {", f.plan.imports_[t.Name], declaration, more, self), "}", func() {
		f.emitCompleteSlots(t.Uses)
		f.linef("const identity = callableIdentity({ type: %s, validate: validateWire, slots });", quote(t.Name))
		f.line("const invoke = owner.import(owner.scope.decode(raw), identity.path, identity.digest ?? '');")
		f.line("const scope = owner.scope;")
		f.w.Block(fmt.Sprintf("return async (%s) => {", f.callableParams(t)), "};", func() {
			f.line("const owner = options?.owner?.scope === scope ? options.owner : scope.owner();")
			call := "invoke(undefined, options)"
			if t.Request != nil {
				call = f.livePublish(t.Request, "request", ", '$', slots", "invoke(%s, options)")
			}
			if t.Result == nil {
				f.linef("await %s;", call)
				f.line("return;")
			} else {
				f.linef("const result = await %s;", call)
				f.linef("validateWire(%s, result, '$', slots);", expression(t.Result))
				f.linef("return %s;", f.liveConversion(t.Result, "result", false))
			}
		})
	})
}
