package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// emitValueAdapters composes neutral conversions. A caller supplies the active
// batch context; reusable factories never retain an owner or environment.
func (f *file) emitValueAdapters() {
	for _, t := range f.family.Types {
		if t.Carried {
			continue
		}
		f.uses, f.codecs = t.Uses, t.Uses
		name := f.plan.types[t.Name]
		self := f.spell(model.Named{Name: t.Name})
		var parameters, needs []string
		if t.IsLive {
			needs = append(needs, "true")
		}
		for _, use := range t.Uses {
			n := parameterName(use)
			parameters = append(parameters, "adapter"+n+" "+f.runtime()+".ValueAdapter["+n+"]")
			needs = append(needs, "adapter"+n+".NeedsContext")
		}
		factory := identAdapter + name
		f.linef("// %s composes declaration validation and conversion within the supplied invocation context.", factory)
		f.w.Block(fmt.Sprintf("func %s%s(%s) %s.ValueAdapter[%s] {", factory, declare(t.Uses), strings.Join(parameters, ", "), f.runtime(), self), "}", func() {
			for _, use := range t.Uses {
				n := parameterName(use)
				f.linef("type%s := adapter%s.Binding", n, n)
			}
			f.linef("binding := %s.TypeBinding{Schema: %s, Type: %s}", f.runtime(), f.boundSchema(t.Uses), f.typeExpression(t))
			f.w.Block(fmt.Sprintf("return %s.ValueAdapter[%s]{", f.runtime(), self), "}", func() {
				f.line("Binding: binding,")
				f.linef("NeedsContext: %s,", liveOr(needs...))
				f.w.Block(fmt.Sprintf("Export: func(ctx %s.Context, value %s) (%s.RawMessage,error) {", f.std("context"), self, f.std("json")), "},", func() {
					f.adapterInterpretation(t.Uses, "nil, ")
					if t.IsLive {
						f.nativeOwner("nil, ")
					}
					f.linef("raw,err := %s", f.adapterHelper(t, true))
					f.line("if err == nil { err = binding.Schema.ValidateExpressionRaw(binding.Type,raw) };return raw,err")
				})
				f.w.Block(fmt.Sprintf("Import: func(ctx %s.Context, raw %s.RawMessage) (%s,error) {", f.std("context"), f.std("json"), self), "},", func() {
					f.linef("var zero %s", self)
					f.adapterInterpretation(t.Uses, "zero, ")
					if t.IsLive {
						f.nativeOwner("zero, ")
					}
					f.line("if err := binding.Schema.ValidateExpressionRaw(binding.Type,raw); err != nil { return zero,err }")
					f.linef("return %s", f.adapterHelper(t, false))
				})
			})
		})
	}
	f.uses, f.codecs = f.family.Uses, nil
}

// A composed interpretation checks the whole associated family before any
// supplied recipe can acquire a value. Declaration uses the same canonical
// drawn-family association as the model boundary, including bound revisions.
func (f *file) adapterInterpretation(uses []render.Use, failure string) {
	f.adapterRecipes(uses, failure)
	for _, use := range uses {
		if use.Type != "" {
			f.linef("if _, err := binding.Declaration(); err != nil { return %serr }", failure)
			return
		}
	}
}

func (f *file) adapterRecipes(uses []render.Use, failure string) {
	for _, use := range uses {
		name := "adapter" + parameterName(use)
		f.linef("if %s.Export == nil || %s.Import == nil { return %s%s.Errorf(%q) }", name, name, failure, f.std("fmt"), parameterName(use)+": both conversion recipes are required")
	}
}

// Native live declarations alone interpret the invocation context as an owner.
func (f *file) nativeOwner(failure string) {
	f.linef("if ctx == nil { return %s%s.Errorf(\"a live conversion requires an active owner\") }", failure, f.std("fmt"))
	f.linef("owner, ok := %s.OwnerOf(ctx)", f.live())
	f.linef("if !ok || owner == nil { return %s%s.Errorf(\"a live conversion requires an active owner\") }", failure, f.std("fmt"))
}

func (f *file) slotUses() []render.Use {
	return f.family.Uses
}

func (f *file) operationAdapters() {
	f.codecs = f.slotUses()
	f.adapters = true
}

func (f *file) slotParameters() string {
	var out string
	for _, use := range f.slotUses() {
		n := parameterName(use)
		out += ", adapter" + n + " " + f.runtime() + ".ValueAdapter[" + n + "]"
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
		out += "; adapter" + n + " " + f.runtime() + ".ValueAdapter[" + n + "]"
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
		values = append(values, f.adapterPrefix+"adapter"+parameterName(use)+".NeedsContext")
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
		return f.adapterPrefix + "adapter" + strings.TrimPrefix(codec, "convert") + ".NeedsContext"
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
	f.w.Block(fmt.Sprintf("%s,err:=func()(%s,error){", dst, result), "}()", func() {
		f.linef("var value %s", result)
		f.w.Block(fmt.Sprintf("convert:=func(ctx %s.Context)(%s,error){", f.std("context"), result), "}", func() {
			if f.family.IsLive(e) {
				f.nativeOwner("value, ")
			}
			if !export {
				f.linef("if err:=%s;err!=nil{return value,err}", f.validateExpression(e, src))
			}
			f.liveExpr(e, src, "converted", export, "value")
			if export {
				f.linef("if err:=%s;err!=nil{return value,err}", f.validateExpression(e, "converted"))
			}
			f.line("return converted,nil")
		})
		environment := f.adapterPrefix + "environment.ValueEnvironment"
		if export {
			f.linef("if %s { return %s.Export(ctx,convert) }", f.expressionLive(e), environment)
		} else {
			f.w.Block("if "+f.expressionLive(e)+" {", "}", func() {
				f.linef("err:=%s.Import(ctx,func(ctx %s.Context)error{converted,err:=convert(ctx);if err==nil{value=converted};return err})", environment, f.std("context"))
				f.linef("if err!=nil{var zero %s;return zero,err};return value,nil", result)
			})
		}
		f.line("return convert(ctx)")
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
		conversion := fmt.Sprintf("func(value %s) (%s,error) { return adapter%s.%s(ctx,value) }", from, to, name, method)
		if t.IsLive {
			conversion = fmt.Sprintf("func(owner *%s.Owner,value %s)(%s,error){return adapter%s.%s(%s.WithOwner(ctx,owner),value)}", f.live(), from, to, name, method, f.live())
		}
		arguments = append(arguments, conversion, "type"+name)
	}
	return prefix + apply(t.Uses) + "(" + strings.Join(arguments, ", ") + ")"
}
