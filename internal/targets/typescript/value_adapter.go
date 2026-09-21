package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

func familyValueSlots(family *render.Family) bool {
	for _, parameter := range family.Parameters {
		if parameter.Of == "" {
			return true
		}
	}
	return false
}

func typeAdapterSupported(t *render.Type) bool {
	if t.Carried {
		return false
	}
	return true
}

func valueAdapterName(name string) string { return "adapter" + upperFirst(name) }

func (f *file) emitValueAdapters() {
	for _, t := range f.family.Types {
		if !typeAdapterSupported(t) {
			continue
		}
		f.scope = t.Scope
		self := f.plan.types[t.Name] + apply(t.Uses)
		var parameters, bindings, live, exports, imports []string
		if t.IsLive {
			live = append(live, "true")
		}
		seen := map[string]bool{}
		for _, use := range t.Uses {
			name := "slot_" + use.Parameter
			parameter, _ := f.parameter(use.Parameter)
			if !seen[use.Parameter] {
				if parameter.IsFamily() {
					parameters = append(parameters, name+": FamilyBinding<"+use.Parameter+">")
					bindings = append(bindings, quote(use.Parameter)+": "+name)
				} else {
					parameters = append(parameters, name+": ValueAdapter<"+use.Parameter+">")
					bindings = append(bindings, quote(use.Parameter)+": "+name+".binding")
					live = append(live, name+".live")
				}
				seen[use.Parameter] = true
			}
			owner := ""
			if t.IsLive {
				owner = "owner: LiveOwner, "
			}
			if parameter.IsFamily() {
				typ := use.Parameter + "[" + quote(use.Type) + "]"
				exports = append(exports, "("+owner+"value: "+typ+") => value")
				imports = append(imports, "("+owner+"value: unknown) => value as "+typ)
			} else {
				exports = append(exports, "("+owner+"value: "+use.Parameter+") => "+name+".export(owner, value)")
				imports = append(imports, "("+owner+"value: unknown) => "+name+".import(owner, value)")
			}
		}
		if len(live) == 0 {
			live = append(live, "false")
		}
		f.w.Block(fmt.Sprintf("export function %s%s(%s): ValueAdapter<%s> {", valueAdapterName(f.plan.types[t.Name]), f.declare(t.Uses), strings.Join(parameters, ", "), self), "}", func() {
			f.linef("const slots: Slots = { %s };", strings.Join(bindings, ", "))
			f.linef("const binding: TypeBinding = { type: %s, validate: validateWire, slots };", quote(t.Name))
			f.linef("const live = %s;", strings.Join(live, " || "))
			f.w.Block("return { binding, live,", "};", func() {
				for _, export := range []bool{true, false} {
					method, source, returns, converters, helper := "export", "value", "unknown", exports, f.plan.exports[t.Name]
					input := "value: " + self
					if !export {
						method, source, returns, converters, helper, input = "import", "raw", self, imports, f.plan.imports_[t.Name], "raw: unknown"
					}
					f.w.Block(fmt.Sprintf("%s(owner: LiveOwner | undefined, %s): %s {", method, input, returns), "},", func() {
						f.line("if (live && !owner) throw new DuplexError('scope_closed', 'live conversion requires an owner');")
						if !export {
							f.line("binding.validate(binding.type, raw, '$', slots);")
						}
						f.w.Block(fmt.Sprintf("const convert = (owner: LiveOwner | undefined): %s => {", returns), "};", func() {
							converted := source
							if helper != "" {
								args := []string{source}
								if t.IsLive {
									args = append([]string{"owner!"}, args...)
								}
								args = append(args, converters...)
								converted = helper + apply(t.Uses) + "(" + strings.Join(args, ", ") + ")"
							} else if !export {
								converted += " as " + self
							}
							f.linef("const converted = %s;", converted)
							if export {
								f.line("binding.validate(binding.type, converted, '$', slots);")
							}
							f.line("return converted;")
						})
						f.linef("return live ? owner!.%sValue(convert) : convert(owner);", method)
					})
				}
			})
		})
	}
}

func (f *file) lifetimeType(base, helper string, parts ...model.TypeExpr) string {
	for _, part := range parts {
		if part != nil && f.family.IsLive(part) {
			if helper == "ValueContext" {
				return base + " & { owner: LiveOwner }"
			}
			return base + " & { owner?: LiveOwner }"
		}
	}
	if !f.liveNeeded(parts...) {
		return base
	}
	var values []string
	for _, part := range parts {
		if part != nil {
			values = append(values, f.spell(part))
		}
	}
	return helper + "<" + strings.Join(values, " | ") + ", " + base + ">"
}

func (f *file) familyLiveCondition() string {
	if f.family.Live {
		return "true"
	}
	var parts []string
	for _, name := range parameters(f.family.Uses) {
		if parameter, ok := f.parameter(name); ok && !parameter.IsFamily() {
			parts = append(parts, bindingName(name)+".live")
		}
	}
	if len(parts) == 0 {
		return "false"
	}
	return strings.Join(parts, " || ")
}

func (f *file) operationSlot(e model.TypeExpr) string {
	if !f.operationAdapters {
		return ""
	}
	if named, ok := e.(model.Named); ok {
		if parameter, ok := f.parameter(named.Name); ok && !parameter.IsFamily() {
			receiver := f.adapterReceiver
			if receiver == "" {
				receiver = "this."
			}
			return receiver + bindingName(named.Name)
		}
	}
	return ""
}

func (f *file) bindingValue(name string) string {
	if parameter, ok := f.parameter(name); ok && !parameter.IsFamily() {
		return bindingName(name) + ".binding"
	}
	return bindingName(name)
}

// Runtime liveness is supplied by a generic slot; conversion alone need not acquire an owner.
func (f *file) boundaryLive(e model.TypeExpr) string {
	if e == nil {
		return "false"
	}
	if f.family.IsLive(e) {
		return "true"
	}
	if slot := f.operationSlot(e); slot != "" {
		return slot + ".live"
	}
	switch x := e.(type) {
	case model.Array:
		return f.boundaryLive(x.Elem)
	case model.Map:
		return f.boundaryLive(x.Elem)
	case model.Nullable:
		return f.boundaryLive(x.Elem)
	}
	if t, args := f.family.Conversion(e); t != nil {
		parts := []string{}
		for _, argument := range args {
			if live := f.boundaryLive(argument.Expression()); live != "false" {
				parts = append(parts, live)
			}
		}
		if len(parts) > 0 {
			return "(" + strings.Join(parts, " || ") + ")"
		}
	}
	return "false"
}
