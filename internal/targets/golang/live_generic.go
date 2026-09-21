package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// publishBoundary keeps one completed conversion batch until the send outcome
// establishes whether the payload could have reached the other side.
func (f *file) publishBoundary(e model.TypeExpr, src, dst string, publish func()) {
	json := f.std("json")
	if f.adapters {
		f.w.Block(fmt.Sprintf("%s, err := func() (%s.RawMessage, error) {", dst, json), "}()", func() {
			f.w.Block(fmt.Sprintf("build := func(ctx %s.Context) (%s.RawMessage, error) {", f.std("context"), json), "}", func() {
				f.liveBoundary(e, src, "sent", true)
				f.line("return sent, err")
			})
			f.w.Block(fmt.Sprintf("publish := func(sent %s.RawMessage) (%s.RawMessage, error) {", json, json), "}", publish)
			f.linef("if %s { return %senvironment.ValueEnvironment.Publish(ctx, build, publish) }", f.expressionLive(e), f.adapterPrefix)
			f.line("sent, err := build(ctx); if err != nil { return nil, err }; return publish(sent)")
		})
		return
	}
	f.w.Block(fmt.Sprintf("%s, err := owner.PublishValue(", dst), ")", func() {
		f.w.Block(fmt.Sprintf("func(owner *%s.Owner) (%s.RawMessage, error) {", f.live(), json), "},", func() {
			f.liveBoundary(e, src, "sent", true)
			f.line("return sent, err")
		})
		f.w.Block(fmt.Sprintf("func(sent %s.RawMessage) (%s.RawMessage, error) {", json, json), "},", publish)
	})
}

// liveBoundary keeps the caller's error mapping while expressions (including
// anonymous containers and applications) use the same recursive conversion.
func (f *file) liveBoundary(e model.TypeExpr, src, dst string, export bool) {
	result := f.spell(e)
	if f.adapters && f.needsConversion(e) {
		f.adapterBoundary(e, src, dst, export)
		return
	}
	// Ordinary data needs no lifetime. In particular, releasing an owner is
	// a barrier to new acquisitions, not a cancellation of a dispatched call
	// that is returning a scalar or record without live positions.
	if !f.needsConversion(e) {
		if export {
			f.linef("%s, err := %s.MarshalJSON(%s)", dst, f.runtime(), src)
			f.linef("if err == nil { err = %s }", f.validateExpression(e, dst))
		} else {
			f.w.Block(fmt.Sprintf("%s, err := func() (%s, error) {", dst, result), "}()", func() {
				f.linef("var value %s", result)
				f.linef("if err := %s; err != nil { return value, err }", f.validateExpression(e, src))
				f.linef("err := %s.Unmarshal(%s, &value)", f.std("json"), src)
				f.line("return value, err")
			})
		}
		return
	}
	if !export {
		f.w.Block(fmt.Sprintf("%s, err := func() (%s, error) {", dst, result), "}()", func() {
			f.linef("var value %s", result)
			f.w.Block(fmt.Sprintf("err := owner.ImportValue(func(owner *%s.Owner) error {", f.live()), "})", func() {
				f.w.Block(fmt.Sprintf("converted, err := func() (%s, error) {", result), "}()", func() {
					f.linef("var zero %s", result)
					f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(e, src))
					f.liveExpr(e, src, "converted", false, "zero")
					f.line("return converted, nil")
				})
				f.line("value = converted")
				f.line("return err")
			})
			f.linef("if err != nil { var zero %s; return zero, err }", result)
			f.line("return value, nil")
		})
		return
	}
	if export {
		result = f.std("json") + ".RawMessage"
	}
	open, close := fmt.Sprintf("%s, err := func() (%s, error) {", dst, result), "}()"
	if export {
		open, close = fmt.Sprintf("%s, err := owner.ExportValue(func(owner *%s.Owner) (%s, error) {", dst, f.live(), result), "})"
	}
	f.w.Block(open, close, func() {
		f.linef("var zero %s", result)
		if !export {
			f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(e, src))
		}
		f.liveExpr(e, src, "converted", export, "zero")
		if export {
			f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(e, "converted"))
		}
		f.line("return converted, nil")
	})
}

// A generic data declaration has no live dependency. Its conversion helpers
// take per-argument codecs; a live caller closes those codecs over its owner.
func (f *file) converterParameters(t *render.Type, export bool) string {
	var out strings.Builder
	for _, use := range t.Uses {
		name := parameterName(use)
		from, to := name, f.std("json")+".RawMessage"
		if !export {
			from, to = to, from
		}
		if t.IsLive {
			from = "*" + f.live() + ".Owner, " + from
		}
		fmt.Fprintf(&out, ", convert%s func(%s) (%s, error), type%s %s.TypeBinding", name, from, to, name, f.runtime())
	}
	return out.String()
}

func (f *file) parameterConverter(e model.TypeExpr) string {
	for _, use := range f.codecs {
		switch x := e.(type) {
		case model.Named:
			if use.Type == "" && x.Name == use.Parameter {
				return "convert" + parameterName(use)
			}
		case model.Drawn:
			if x.Parameter == use.Parameter && x.Name == use.Type {
				return "convert" + parameterName(use)
			}
		}
	}
	return ""
}

func (f *file) needsConversion(e model.TypeExpr) bool {
	if f.parameterConverter(e) != "" {
		return true
	}
	switch x := e.(type) {
	case model.Array:
		return f.needsConversion(x.Elem)
	case model.Map:
		return f.needsConversion(x.Elem)
	case model.Nullable:
		return f.needsConversion(x.Elem)
	}
	if t, args := f.family.Conversion(e); t != nil && len(f.codecs) > 0 && len(args) > 0 {
		return true
	}
	return f.family.IsLive(e)
}

func (f *file) conversionCall(e model.TypeExpr, src, dst string, export bool) (string, string) {
	t, arguments := f.family.Conversion(e)
	call := f.liveCall(e, export) + f.arguments(arguments)
	passed := []string{}
	if t.IsLive {
		passed = append(passed, "owner")
	}
	passed = append(passed, src)
	for i, argument := range arguments {
		expr := argument.Expression()
		name := fmt.Sprintf("%sConvert%d", dst, i)
		from, to := f.spell(expr), f.std("json")+".RawMessage"
		if !export {
			from, to = to, from
		}
		parameters := "input " + from
		if t.IsLive {
			parameters = "owner *" + f.live() + ".Owner, " + parameters
		}
		f.w.Block(fmt.Sprintf("%s := func(%s) (%s, error) {", name, parameters, to), "}", func() {
			if t.IsLive && f.adapters {
				f.linef("ctx := %s.WithOwner(ctx, owner)", f.live())
			}
			fail := "nil"
			if !export {
				f.linef("var zero %s", to)
				fail = "zero"
			}
			f.liveExpr(expr, "input", "converted", export, fail)
			f.line("return converted, nil")
		})
		binding := f.runtime() + ".TypeBinding{Schema: " + f.boundSchema(f.uses) + ", Type: " + f.runtime() + ".MustTypeExpression(" + expression(expr) + ")}"
		passed = append(passed, name, binding)
	}
	return call, strings.Join(passed, ", ")
}
