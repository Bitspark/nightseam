package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// Conversion and validation both finish before the value can be published.
func (f *file) liveExport(e model.TypeExpr, src, slots string) string {
	validation := expression(e)
	if e == nil {
		// Only an ordinary method's omitted request reaches this boundary:
		// it sends an empty record even when the result carries live values.
		validation = "{ empty: true }"
	}
	plain := fmt.Sprintf("(() => { const converted = %s; %s(%s, converted%s); return converted; })()", f.liveConversion(e, src, true), identValidateWire, validation, slots)
	live := f.boundaryLive(e)
	if live == "false" {
		return plain
	}
	owned := fmt.Sprintf("owner!.exportValue((owner) => { const converted = %s; %s(%s, converted%s); return converted; })", f.liveConversion(e, src, true), identValidateWire, validation, slots)
	if f.operationAdapters {
		owned = fmt.Sprintf("environment!.export(owner, (owner) => { const converted = %s; %s(%s, converted%s); return converted; })", f.liveConversion(e, src, true), identValidateWire, validation, slots)
	}
	if live != "true" {
		return "(" + live + " ? " + owned + " : " + plain + ")"
	}
	return owned
}

func (f *file) livePublish(e model.TypeExpr, src, slots, send string) string {
	value := f.liveExport(e, src, slots)
	live := f.boundaryLive(e)
	if live == "false" {
		return fmt.Sprintf(send, value)
	}
	owned := fmt.Sprintf("owner!.publishValue(owner => %s, sent => %s)", value, fmt.Sprintf(send, "sent"))
	if f.operationAdapters {
		owned = fmt.Sprintf("environment!.publish(owner, owner => %s, sent => %s)", value, fmt.Sprintf(send, "sent"))
	}
	if live != "true" {
		return "(" + live + " ? " + owned + " : " + fmt.Sprintf(send, value) + ")"
	}
	return owned
}

func converterName(use render.Use) string { return "convert_" + use.Parameter + "_" + use.Type }
func useExpression(use render.Use) model.TypeExpr {
	if use.Type == "" {
		return model.Named{Name: use.Parameter}
	}
	return model.Drawn{Parameter: use.Parameter, Name: use.Type}
}

func (f *file) converterParameters(t *render.Type, export bool) string {
	var out strings.Builder
	for _, use := range t.Uses {
		from, to := f.spell(useExpression(use)), "unknown"
		if !export {
			from, to = to, from
		}
		parameters := "value: " + from
		if t.IsLive {
			parameters = "owner: LiveOwner, " + parameters
		}
		fmt.Fprintf(&out, ", %s: (%s) => %s", converterName(use), parameters, to)
	}
	return out.String()
}

func (f *file) parameterConverter(e model.TypeExpr) string {
	for _, use := range f.codecs {
		switch x := e.(type) {
		case model.Named:
			if use.Type == "" && x.Name == use.Parameter {
				return converterName(use)
			}
		case model.Drawn:
			if x.Parameter == use.Parameter && x.Name == use.Type {
				return converterName(use)
			}
		}
	}
	return ""
}

func (f *file) needsConversion(e model.TypeExpr) bool {
	if f.operationSlot(e) != "" {
		return true
	}
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
	if t, args := f.family.Conversion(e); t != nil && len(args) > 0 {
		if len(f.codecs) > 0 {
			return true
		}
		for _, argument := range args {
			if f.needsConversion(argument.Expression()) {
				return true
			}
		}
	}
	return f.family.IsLive(e)
}

func (f *file) conversionCall(e model.TypeExpr, src string, export bool) string {
	t, arguments := f.family.Conversion(e)
	passed := []string{}
	if t.IsLive {
		owner := "owner"
		if f.operationAdapters {
			owner = "owner as LiveOwner"
		}
		passed = append(passed, owner)
	}
	if export {
		src = "(" + src + ") as " + f.spell(e)
	}
	passed = append(passed, src)
	for _, argument := range arguments {
		expr := argument.Expression()
		from, to := f.spell(expr), "unknown"
		if !export {
			from, to = to, from
		}
		converted := f.liveExpr(expr, "input", export)
		if !export {
			converted = "(" + converted + ") as " + to
		}
		parameters := "input: " + from
		if t.IsLive {
			parameters = "owner: LiveOwner, " + parameters
		}
		passed = append(passed, "("+parameters+"): "+to+" => "+converted)
	}
	return f.liveCall(e, export) + f.renderArguments(arguments) + "(" + strings.Join(passed, ", ") + ")"
}
