package typescript

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// file is one TypeScript file being emitted, and the prefix a type of the
// family is spelled with: none in types.ts, Protocol. in index.ts.
type file struct {
	plan              *plan
	family            *render.Family
	config            Config
	w                 *emit.Writer
	prefix            string
	scope             []model.Parameter
	codecs            []render.Use
	scopedCodecs      bool
	operationAdapters bool
	adapterReceiver   string
	// conversion is the namespace a live type's generated export/import is
	// called through: none in types.ts, which declares them, and the
	// re-exported module in index.ts, which only calls them.
	conversion string
}

func (f *file) line(text string)                 { f.w.Line(text) }
func (f *file) linef(format string, args ...any) { f.w.Linef(format, args...) }

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// expression is a type expression as the validator reads it.
func expression(e model.TypeExpr) string { return model.String(e) }

// alias is the namespace a generated file refers to an imported family's
// types by: the family's name without its dashes.
func alias(family string) string { return strings.ReplaceAll(family, "-", "") }

// TypeScript has associated types, so one contract parameter is one type
// parameter whatever types it is drawn at: a drawn type is one of its
// associated types, S["Envelope"] or S["Payload"]. Go, which has none, takes
// one type parameter per drawn type instead.
//
// parameters is the contract parameters a set of uses names, in the order
// the uses are in, each named once.
func parameters(uses []render.Use) []string {
	var names []string
	for _, use := range uses {
		if !slices.Contains(names, use.Parameter) {
			names = append(names, use.Parameter)
		}
	}
	return names
}

// apply renders the type arguments a reference passes on, or nothing.
func apply(uses []render.Use) string {
	names := parameters(uses)
	if len(names) == 0 {
		return ""
	}
	return "<" + strings.Join(names, ", ") + ">"
}

// imports emits the import lines for the families the file depends on:
// their types as a namespace and, where asked, each referred family's
// validator under a name no type can collide with.
func (f *file) imports(validators bool) {
	for _, family := range f.plan.references() {
		f.linef("import type * as %s from %s;", alias(family), quote(f.config.pkg(family)))
		if validators && slices.Contains(f.family.References, family) {
			f.linef("import { %s as validate_%s } from %s;", identValidateWire, alias(family), quote(f.config.pkg(family)))
		}
	}
}

// emitTypes renders src/types.ts: the wire types, the family's descriptor
// and the bindings a slot is filled with, and the validator, made of the
// family's wire description by the runtime.
func emitTypes(f *file) {
	p, fam := f.plan, f.family
	f.linef("import { createValidator, DuplexError, type %s, type %s, type %s, type %s, type TypeExpression, type WireFamily } from %s;", identAnyFamily, identFamilyBinding, identTypeBinding, identSlots, quote(f.config.Runtime))
	f.linef("export type { %s, %s, %s, %s, TypeExpression };", identAnyFamily, identFamilyBinding, identTypeBinding, identSlots)
	f.imports(true)
	f.liveImports()
	f.linef("import type { LiveOwner, ValueAdapter, ValueContext } from %s;", quote(f.config.Live))
	if fam.HasProtocol() {
		f.linef("import type { WireModelContext } from %s;", quote(f.config.Runtime))
	}
	for _, t := range fam.Types {
		f.emitType(t)
	}
	// The family as a slot of another family sees it: its name and the wire
	// types a slot of it draws on.
	var drawn []string
	for _, t := range fam.Types {
		if len(t.Uses) == 0 {
			drawn = append(drawn, t.Name+": "+p.types[t.Name])
		}
	}
	f.line("/** The family: its name and the wire types a slot of it draws on. */")
	f.linef("export interface %s { readonly name: %s; %s }", identFamily, quote(fam.Name), strings.Join(drawn, "; "))
	f.emitLive()
	f.emitValueAdapters()
	if fam.HasProtocol() {
		f.emitWireModelTypes()
	}
	f.line("")
	f.linef("const contractTypes = %s as unknown as WireFamily;", fam.Wire)
	var validators []string
	for _, family := range fam.References {
		validators = append(validators, quote(family)+": validate_"+alias(family))
	}
	f.line("/** Runtime validation applies equally to calls, replies, reverse calls and events; what fills a slot of a parameter is validated by the binding of the family that fills it. */")
	f.linef("export const %s = createValidator(contractTypes, { %s });", identValidateWire, strings.Join(validators, ", "))
	f.line("/** This family bound: its name and its validator, to fill a family slot in another family's client. */")
	f.linef("export const %s = { name: %s, validate: %s } as const;", identFamilyValue, quote(fam.Name), identValidateWire)
}

// emitType is the declaration shared by package generation and documents.
func (f *file) emitType(t *render.Type) {
	p := f.plan
	name := p.types[t.Name]
	f.scope = t.Scope
	switch t.Kind {
	case "record", "entity":
		if t.Description != "" {
			f.linef("/** %s */", comment(t.Description))
		}
		if len(t.Fields) == 0 && !t.Open {
			f.linef("export type %s%s = Record<string, never>;", name, f.declare(t.Uses))
			return
		}
		f.w.Block(fmt.Sprintf("export interface %s%s {", name, f.declare(t.Uses)), "}", func() {
			for _, field := range t.Fields {
				optional, null := "", ""
				if !field.Required {
					optional = "?"
				}
				if field.Nullable {
					null = " | null"
				}
				if field.Description != "" {
					f.linef("/** %s */", comment(field.Description))
				}
				f.linef("%s%s: %s%s;", quote(field.Name), optional, f.spell(field.Type), null)
			}
			if t.Open {
				f.line("[key: string]: unknown;")
			}
		})
	case "enum":
		values := make([]string, len(t.Values))
		for i, v := range t.Values {
			values[i] = quote(v)
		}
		if t.Description != "" {
			f.linef("/** %s */", comment(t.Description))
		}
		f.linef("export type %s = %s;", name, strings.Join(values, " | "))
	case "union":
		if t.Description != "" {
			f.linef("/** %s */", comment(t.Description))
		}
		var variants []string
		for _, variant := range t.Variants {
			members := quote(t.Tag) + ": " + quote(variant.Tag)
			if variant.Form != render.VariantEmpty {
				members += "; " + quote(t.Value) + ": " + f.spell(variant.Type)
			}
			variants = append(variants, "{ "+members+" }")
		}
		f.linef("export type %s%s = %s;", name, f.declare(t.Uses), strings.Join(variants, " | "))
	case "alias":
		if t.Description != "" {
			f.linef("/** %s */", comment(t.Description))
		}
		f.linef("export type %s%s = %s;", name, f.declare(t.Uses), f.spell(t.Alias))
	case "callable":
		f.emitCallableType(t)
	}
}

func comment(text string) string { return strings.ReplaceAll(text, "*/", "* /") }

// request is the type of a method's params, or a record of nothing.
func (f *file) request(m render.Method) string {
	if m.Request == nil {
		return "Record<string, never>"
	}
	return f.spell(m.Request)
}

// requestExpression is what the validator checks a method's params against.
func requestExpression(m render.Method) string {
	if m.Request == nil {
		return "{ empty: true }"
	}
	return expression(m.Request)
}

// emitClient renders the adapter of the client model, sharing the side types.
func emitClient(f *file) { emitWireAdapter(f, "Client", "'./types.ts'") }

// operations is every method and event name a family declares, each side's
// methods before each side's events, in the order the sides hold them.
func operations(fam *render.Family) []string {
	names := make([]string, 0, len(fam.Server.Methods)+len(fam.Client.Methods)+len(fam.Server.Events)+len(fam.Client.Events))
	appendName := func(name string) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for _, m := range fam.Server.Methods {
		appendName(m.Name)
	}
	for _, m := range fam.Client.Methods {
		appendName(m.Name)
	}
	for _, e := range fam.Server.Events {
		appendName(e.Name)
	}
	for _, e := range fam.Client.Events {
		appendName(e.Name)
	}
	return names
}

// list renders names as a JSON array, an absent list as an empty one.
func list(names []string) string {
	if names == nil {
		names = []string{}
	}
	data, _ := json.Marshal(names)
	return string(data)
}
