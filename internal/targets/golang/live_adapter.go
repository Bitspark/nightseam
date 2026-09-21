package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// emitValueAdapters composes declaration bindings and both conversion directions
// once. The owner is supplied only when a value crosses an operation boundary.
func (f *file) emitValueAdapters() {
	for _, t := range f.family.Types {
		if t.Carried {
			continue
		}
		f.uses, f.codecs = t.Uses, t.Uses
		name := f.plan.types[t.Name]
		self := name + apply(t.Uses)
		var parameters, live []string
		if t.IsLive {
			live = append(live, "true")
		}
		for _, use := range t.Uses {
			n := parameterName(use)
			parameters = append(parameters, "adapter"+n+" "+f.live()+".ValueAdapter["+n+"]")
			live = append(live, "adapter"+n+".Live")
		}
		if len(live) == 0 {
			live = append(live, "false")
		}
		factory := identAdapter + name
		f.linef("// %s composes the declaration and both conversions, receiving an owner at each use.", factory)
		f.w.Block(fmt.Sprintf("func %s%s(%s) %s.ValueAdapter[%s] {", factory, declare(t.Uses), strings.Join(parameters, ", "), f.live(), self), "}", func() {
			for _, use := range t.Uses {
				n := parameterName(use)
				f.linef("type%s := adapter%s.Binding", n, n)
			}
			f.linef("binding := %s.TypeBinding{Schema: %s, Type: %s}", f.runtime(), f.boundSchema(t.Uses), f.typeExpression(t))
			f.linef("isLive := %s", liveOr(live...))
			f.w.Block(fmt.Sprintf("return %s.ValueAdapter[%s]{", f.live(), self), "}", func() {
				f.line("Binding: binding,")
				f.line("Live: isLive,")
				f.w.Block(fmt.Sprintf("Export: func(owner *%s.Owner, value %s) (%s.RawMessage, error) {", f.live(), self, f.std("json")), "},", func() {
					f.w.Block(fmt.Sprintf("convert := func(owner *%s.Owner) (%s.RawMessage, error) {", f.live(), f.std("json")), "}", func() {
						f.linef("raw, err := %s", f.adapterHelper(t, true))
						f.line("if err == nil { err = binding.Schema.ValidateExpressionRaw(binding.Type, raw) }")
						f.line("return raw, err")
					})
					f.linef("if isLive { if owner == nil { return nil, %s.Errorf(%q) }; return owner.ExportValue(convert) }", f.std("fmt"), name+": a live value is exported into an owner")
					f.line("return convert(owner)")
				})
				f.w.Block(fmt.Sprintf("Import: func(owner *%s.Owner, raw %s.RawMessage) (%s, error) {", f.live(), f.std("json"), self), "},", func() {
					f.linef("var value %s", self)
					f.w.Block(fmt.Sprintf("convert := func(owner *%s.Owner) error {", f.live()), "}", func() {
						f.line("if err := binding.Schema.ValidateExpressionRaw(binding.Type, raw); err != nil { return err }")
						f.linef("converted, err := %s", f.adapterHelper(t, false))
						f.line("if err == nil { value = converted }; return err")
					})
					f.linef("if isLive { if owner == nil { return value, %s.Errorf(%q) }; err := owner.ImportValue(convert); if err != nil { var zero %s; return zero, err }; return value, nil }", f.std("fmt"), name+": a live value is imported into an owner", self)
					f.line("err := convert(owner); return value, err")
				})
			})
		})
	}
	f.uses, f.codecs = f.family.Uses, nil
}

func (f *file) slotUses() []render.Use {
	var uses []render.Use
	for _, use := range f.family.Uses {
		if use.Type == "" {
			uses = append(uses, use)
		}
	}
	return uses
}

func (f *file) operationAdapters() {
	f.codecs = f.slotUses()
	f.adapters = len(f.codecs) > 0
}

func (f *file) slotParameters() string {
	var out string
	for _, use := range f.slotUses() {
		n := parameterName(use)
		out += ", adapter" + n + " " + f.live() + ".ValueAdapter[" + n + "]"
	}
	return out
}

func (f *file) slotArguments() string {
	var out string
	for _, use := range f.slotUses() {
		out += ", adapter" + parameterName(use)
	}
	return out
}

func (f *file) slotFields() string {
	var out string
	for _, use := range f.slotUses() {
		n := parameterName(use)
		out += "; adapter" + n + " " + f.live() + ".ValueAdapter[" + n + "]"
	}
	return out
}

func (f *file) slotValues() string {
	var out string
	for _, use := range f.slotUses() {
		n := parameterName(use)
		out += ", adapter" + n + ": adapter" + n
	}
	return out
}

func (f *file) scopeLive() string {
	if f.family.Live {
		return "true"
	}
	var values []string
	for _, use := range f.slotUses() {
		values = append(values, f.adapterPrefix+"adapter"+parameterName(use)+".Live")
	}
	if len(values) == 0 {
		return "false"
	}
	return liveOr(values...)
}

func (f *file) expressionLive(e model.TypeExpr) string {
	if f.family.IsLive(e) {
		return "true"
	}
	if codec := f.parameterConverter(e); codec != "" {
		return f.adapterPrefix + "adapter" + strings.TrimPrefix(codec, "convert") + ".Live"
	}
	switch x := e.(type) {
	case model.Array:
		return f.expressionLive(x.Elem)
	case model.Map:
		return f.expressionLive(x.Elem)
	case model.Nullable:
		return f.expressionLive(x.Elem)
	}
	if t, arguments := f.family.Conversion(e); t != nil {
		var parts []string
		for _, argument := range arguments {
			if v := f.expressionLive(argument.Expression()); v != "false" {
				parts = append(parts, v)
			}
		}
		if len(parts) > 0 {
			return liveOr(parts...)
		}
	}
	return "false"
}

func liveOr(values ...string) string {
	var parts []string
	seen := map[string]bool{}
	for _, value := range values {
		if value == "true" {
			return "true"
		}
		if value != "false" && !seen[value] {
			seen[value] = true
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "false"
	}
	return strings.Join(parts, " || ")
}

func (f *file) adapterBoundary(e model.TypeExpr, src, dst string, export bool) {
	result := f.spell(e)
	if export {
		result = f.std("json") + ".RawMessage"
	}
	f.w.Block(fmt.Sprintf("%s, err := func() (%s, error) {", dst, result), "}()", func() {
		f.linef("var value %s", result)
		f.w.Block(fmt.Sprintf("convert := func(owner *%s.Owner) (%s, error) {", f.live(), result), "}", func() {
			if !export {
				f.linef("if err := %s; err != nil { return value, err }", f.validateExpression(e, src))
			}
			f.liveExpr(e, src, "converted", export, "value")
			if export {
				f.linef("if err := %s; err != nil { return value, err }", f.validateExpression(e, "converted"))
			}
			f.line("return converted, nil")
		})
		if export {
			f.linef("if %s { return owner.ExportValue(convert) }", f.expressionLive(e))
		} else {
			f.w.Block("if "+f.expressionLive(e)+" {", "}", func() {
				f.linef("err := owner.ImportValue(func(owner *%s.Owner) error { converted, err := convert(owner); if err == nil { value = converted }; return err })", f.live())
				f.linef("if err != nil { var zero %s; return zero, err }; return value, nil", result)
			})
		}
		f.line("return convert(owner)")
	})
}

func (f *file) adapterHelper(t *render.Type, export bool) string {
	if !t.IsLive && len(t.Uses) == 0 {
		if export {
			return f.runtime() + ".MarshalJSON(value)"
		}
		return fmt.Sprintf("func() (%s, error) { var value %s; err := %s.Unmarshal(raw, &value); return value, err }()", f.plan.types[t.Name], f.plan.types[t.Name], f.std("json"))
	}
	var arguments []string
	if t.IsLive {
		arguments = append(arguments, "owner")
	}
	prefix, value := f.plan.exports[t.Name], "value"
	if !export {
		prefix, value = f.plan.imports_[t.Name], "raw"
	}
	arguments = append(arguments, value)
	for _, use := range t.Uses {
		name := parameterName(use)
		method, from, to := "Export", name, f.std("json")+".RawMessage"
		if !export {
			method, from, to = "Import", to, from
		}
		conversion := "adapter" + name + "." + method
		if !t.IsLive {
			conversion = fmt.Sprintf("func(value %s) (%s, error) { return adapter%s.%s(owner, value) }", from, to, name, method)
		}
		arguments = append(arguments, conversion, "type"+name)
	}
	return prefix + apply(t.Uses) + "(" + strings.Join(arguments, ", ") + ")"
}
