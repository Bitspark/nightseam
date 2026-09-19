package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// The live tier in Go.
//
// A callable is a **function value** — the operator's verdict on #201 — so
// `ProgressSink` is an ordinary struct whose `Report` field is a `func`, and
// a consumer writes one where it means to be called and receives one where it
// means to call. Each field is its own binding: a record of callables
// acquires no shared identity and no shared lifetime.
//
// Go will not marshal a `func`, and that is the semantics being honest rather
// than a wart: **a live value has no scope-free encoding**, because a
// reference only means anything inside the scope that minted it. So a live
// type's `MarshalJSON` refuses, and the conversion is a pair of generated
// functions that take the scope:
//
//	func ExportJob(scope *live.Scope, v Job) (json.RawMessage, error)
//	func ImportJob(scope *live.Scope, raw json.RawMessage) (Job, error)
//
// Export walks the value, exports each local function as a binding and writes
// the reference in its place; Import validates, and replaces each reference
// with a typed proxy closed over the attachment. The generated client and
// binding call these instead of marshalling, so a handler is handed native
// functions and never sees a reference.

// liveNames registers the identifiers the live tier generates, so that a
// declaration colliding with one is a diagnostic rather than a Go compile
// error in the consumer's checkout.
func (p *plan) planLive() {
	f := p.family
	if !f.Live {
		return
	}
	for _, t := range f.Types {
		if t.Carried || !t.IsLive {
			continue
		}
		name := p.types[t.Name]
		p.exports[t.Name] = identExport + name
		p.imports_[t.Name] = identImport + name
		p.declare(p.packages, p.exports[t.Name], t.At, "live export function")
		p.declare(p.packages, p.imports_[t.Name], t.At, "live import function")
		if t.Kind == model.KindCallable {
			p.contracts[t.Name] = identContract + name
			p.declare(p.packages, p.contracts[t.Name], t.At, "callable contract constant")
		}
	}
}

// emitLive renders the live tier's types and their boundary conversion.
func (f *file) emitLive() {
	if !f.family.Live {
		return
	}
	for _, t := range f.family.Types {
		if t.Carried || !t.IsLive {
			continue
		}
		f.uses = t.Uses
		switch t.Kind {
		case model.KindCallable:
			f.emitCallable(t)
		default:
			f.emitLiveConversion(t)
		}
	}
	f.uses = f.family.Uses
}

// emitCallable renders one callable: the function type a consumer writes and
// calls, the identity a reference to it carries, and the two halves of the
// boundary.
func (f *file) emitCallable(t *render.Type) {
	name := f.plan.types[t.Name]
	json := f.std("json")
	scope := f.live() + ".Scope"
	f.line("")
	if t.Description != "" {
		f.linef("// %s: %s", name, t.Description)
	}
	f.linef("// A value of it is one implementation, called across the seam; each is its own binding, with its own lifetime.")
	f.linef("type %s = func(ctx %s.Context, %s) %s", name, f.std("context"), f.callableParam(t), f.callableResult(t))
	f.line("")
	f.linef("// %s is the declaration a reference to %s carries. It is nominal: a reference is usable exactly where this callable is expected.", f.plan.contracts[t.Name], name)
	f.linef("const %s = %s", f.plan.contracts[t.Name], quote(t.Contract))
	f.line("")
	f.linef("// %s makes a binding of a local %s and writes the reference that names it.", f.plan.exports[t.Name], name)
	f.w.Block(fmt.Sprintf("func %s(scope *%s, v %s) (%s.RawMessage, error) {", f.plan.exports[t.Name], scope, name, json), "}", func() {
		f.linef("if scope == nil { return nil, %s.Errorf(\"%s: a live value is exported into a scope\") }", f.std("fmt"), name)
		f.linef("if v == nil { return nil, %s.Errorf(\"%s: no implementation to export\") }", f.std("fmt"), name)
		f.w.Block(fmt.Sprintf("reference, err := scope.Export(%s, func(ctx %s.Context, request %s.RawMessage) (%s.RawMessage, error) {", f.plan.contracts[t.Name], f.std("context"), json, json), "})", func() {
			if t.Request != nil {
				f.linef("if err := %s; err != nil { return nil, err }", f.validateExpression(t.Request, "request"))
				// A callable's own request is converted like any other
				// position: a callable that takes a callable is handed a
				// native function, not a reference.
				f.liveExpr(t.Request, "request", "argument", false, "nil")
			}
			if t.Result == nil {
				f.linef("return nil, v(ctx%s)", callArgument(t))
			} else {
				f.linef("result, err := v(ctx%s)", callArgument(t))
				f.line("if err != nil { return nil, err }")
				f.liveExpr(t.Result, "result", "data", true, "nil")
				f.linef("if err := %s; err != nil { return nil, err }", f.validateExpression(t.Result, "data"))
				f.line("return data, nil")
			}
		})
		f.line("if err != nil { return nil, err }")
		f.linef("return %s.MarshalJSON(reference)", f.runtime())
	})
	f.line("")
	f.linef("// %s is a %s that calls the binding a reference names.", f.plan.imports_[t.Name], name)
	f.w.Block(fmt.Sprintf("func %s(scope *%s, raw %s.RawMessage) (%s, error) {", f.plan.imports_[t.Name], scope, json, name), "}", func() {
		f.linef("if scope == nil { return nil, %s.Errorf(\"%s: a live value is imported into a scope\") }", f.std("fmt"), name)
		f.line("reference, err := scope.Decode(raw)")
		f.line("if err != nil { return nil, err }")
		f.linef("invoke, err := scope.Import(reference, %s)", f.plan.contracts[t.Name])
		f.line("if err != nil { return nil, err }")
		f.w.Block(fmt.Sprintf("return func(ctx %s.Context, %s) %s {", f.std("context"), f.callableParam(t), f.callableResult(t)), "}, nil", func() {
			zero, fail := "", ""
			if t.Result != nil {
				f.linef("var zero %s", f.spell(t.Result))
				zero, fail = "zero, ", "zero"
			}
			if t.Request == nil {
				f.linef("result, err := invoke(ctx, nil)")
			} else {
				// And what a caller sends: a callable it passes becomes a
				// binding of this scope, as it would in any other position.
				f.liveExpr(t.Request, "params", "request", true, fail)
				f.linef("if err := %s; err != nil { return %serr }", f.validateExpression(t.Request, "request"), zero)
				f.line("result, err := invoke(ctx, request)")
			}
			f.linef("if err != nil { return %serr }", zero)
			if t.Result == nil {
				f.line("_ = result")
				f.line("return nil")
				return
			}
			f.linef("if err := %s; err != nil { return zero, err }", f.validateExpression(t.Result, "result"))
			f.liveExpr(t.Result, "result", "answer", false, "zero")
			f.line("return answer, nil")
		})
	})
}

// callableParam is the request a callable takes, as a Go parameter, or none.
func (f *file) callableParam(t *render.Type) string {
	if t.Request == nil {
		return ""
	}
	return "params " + f.spell(t.Request)
}

// callableResult is what a callable answers in Go: an error alone, or its
// result beside one.
func (f *file) callableResult(t *render.Type) string {
	if t.Result == nil {
		return "error"
	}
	return "(" + f.spell(t.Result) + ", error)"
}

func callArgument(t *render.Type) string {
	if t.Request == nil {
		return ""
	}
	return ", argument"
}

// emitLiveConversion renders the boundary for a live record, entity, union or
// alias: the pair that converts it, and the refusal that says a live value has
// no encoding of its own.
func (f *file) emitLiveConversion(t *render.Type) {
	name := f.plan.types[t.Name]
	json := f.std("json")
	scope := f.live() + ".Scope"
	self := name + apply(t.Uses)
	f.line("")
	f.linef("// %s writes %s as it travels: each callable in it becomes a binding of the scope, and the reference that names it takes its place.", f.plan.exports[t.Name], name)
	f.w.Block(fmt.Sprintf("func %s%s(scope *%s, v %s) (%s.RawMessage, error) {", f.plan.exports[t.Name], declare(t.Uses), scope, self, json), "}", func() {
		f.linef("if scope == nil { return nil, %s.Errorf(\"%s: a live value is exported into a scope\") }", f.std("fmt"), name)
		f.liveBody(t, true)
	})
	f.line("")
	f.linef("// %s reads %s as it arrived: each reference in it becomes a typed proxy of the binding it names, so a handler is given native values.", f.plan.imports_[t.Name], name)
	f.w.Block(fmt.Sprintf("func %s%s(scope *%s, raw %s.RawMessage) (%s, error) {", f.plan.imports_[t.Name], declare(t.Uses), scope, json, self), "}", func() {
		f.linef("var value %s", self)
		f.linef("if scope == nil { return value, %s.Errorf(\"%s: a live value is imported into a scope\") }", f.std("fmt"), name)
		f.liveBody(t, false)
	})
}

// liveBody writes one direction of a live type's conversion.
func (f *file) liveBody(t *render.Type, export bool) {
	json := f.std("json")
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		if export {
			f.linef("wire := map[string]%s.RawMessage{}", json)
			for _, field := range t.Fields {
				f.liveField(field, true)
			}
			f.linef("data, err := %s.MarshalObject(%s, wire)", f.runtime(), f.wireOrder(t))
			f.line("if err != nil { return nil, err }")
			f.linef("if err := %s; err != nil { return nil, err }", f.validateType(t, "data"))
			f.line("return data, nil")
			return
		}
		f.linef("if err := %s; err != nil { return value, err }", f.validateType(t, "raw"))
		f.linef("var wire map[string]%s.RawMessage", json)
		f.linef("if err := %s.Unmarshal(raw, &wire); err != nil { return value, err }", json)
		for _, field := range t.Fields {
			f.liveField(field, false)
		}
		f.line("return value, nil")
	case model.KindAlias:
		if export {
			// Every export answers a json.RawMessage already; marshalling it
			// again would encode an encoded value.
			f.liveExpr(t.Alias, "v", "converted", true, "nil")
			f.line("return converted, nil")
			return
		}
		f.liveExpr(t.Alias, "raw", "converted", false, "value")
		f.line("value = converted")
		f.line("return value, nil")
	case model.KindUnion:
		f.liveUnion(t, export)
	}
}

// wireOrder is the field order a record writes, so that conversion does not
// reorder what an ordinary record's struct would have written.
func (f *file) wireOrder(t *render.Type) string {
	var names []string
	for _, field := range t.Fields {
		names = append(names, quote(field.Name))
	}
	return "[]string{" + strings.Join(names, ", ") + "}"
}

// liveField converts one field of a live record in the chosen direction.
func (f *file) liveField(field render.Field, export bool) {
	name, _ := f.plan.fieldName(field)
	key := quote(field.Name)
	json := f.std("json")
	inner := func(src, dst string, nullable bool) {
		if nullable {
			if export {
				f.w.Block(fmt.Sprintf("if %s.Null {", src), "} else {", func() {
					f.linef("%s = %s.RawMessage(\"null\")", dst, json)
				})
				f.liveExpr(field.Type, src+".Value", dst+"Inner", true, "nil")
				f.linef("%s = %sInner", dst, dst)
				f.line("}")
				return
			}
			f.w.Block(fmt.Sprintf("if string(%s) == \"null\" {", src), "} else {", func() {
				f.linef("%s = %s.Null[%s]()", dst, f.runtime(), f.spell(field.Type))
			})
			f.liveExpr(field.Type, src, dst+"Inner", false, "value")
			f.linef("%s = %s.NonNull(%sInner)", dst, f.runtime(), dst)
			f.line("}")
			return
		}
		if export {
			f.liveExpr(field.Type, src, dst+"Converted", true, "nil")
			f.linef("%s = %sConverted", dst, dst)
			return
		}
		f.liveExpr(field.Type, src, dst+"Converted", false, "value")
		f.linef("%s = %sConverted", dst, dst)
	}
	switch {
	case export && !field.Required:
		f.w.Block(fmt.Sprintf("if v.%s.Present {", name), "}", func() {
			f.linef("var member %s.RawMessage", json)
			inner("v."+name+".Value", "member", field.Nullable)
			f.linef("wire[%s] = member", key)
		})
	case export:
		f.linef("var %sMember %s.RawMessage", lower(name), json)
		inner("v."+name, lower(name)+"Member", field.Nullable)
		f.linef("wire[%s] = %sMember", key, lower(name))
	case !field.Required:
		f.w.Block(fmt.Sprintf("if member, present := wire[%s]; present && string(member) != \"undefined\" {", key), "}", func() {
			f.linef("var held %s", f.memberType(field))
			inner("member", "held", field.Nullable)
			f.linef("value.%s = %s.Some(held)", name, f.runtime())
		})
	default:
		f.w.Block(fmt.Sprintf("if member, present := wire[%s]; present {", key), "}", func() {
			f.linef("var held %s", f.memberType(field))
			inner("member", "held", field.Nullable)
			f.linef("value.%s = held", name)
		})
	}
}

// memberType is a field's Go type inside its presence wrapper: what the
// conversion builds before it is stored.
func (f *file) memberType(field render.Field) string {
	t := f.spell(field.Type)
	if field.Nullable {
		t = f.runtime() + ".Nullable[" + t + "]"
	}
	return t
}

// liveExpr converts one expression in the chosen direction, declaring dst.
// fail is what a failing conversion returns beside the error.
func (f *file) liveExpr(e model.TypeExpr, src, dst string, export bool, fail string) {
	json := f.std("json")
	failure := func() string {
		switch fail {
		case "nil":
			return "nil, err"
		case "":
			// A proxy of a callable that answers nothing returns error alone.
			return "err"
		}
		return fail + ", err"
	}
	if !f.family.IsLive(e) {
		if export {
			f.linef("%s, err := %s.MarshalJSON(%s)", dst, f.runtime(), src)
			f.linef("if err != nil { return %s }", failure())
			return
		}
		f.linef("var %s %s", dst, f.spell(e))
		f.linef("if err := %s.Unmarshal(%s, &%s); err != nil { return %s }", json, src, dst, failure())
		return
	}
	switch x := e.(type) {
	case model.Array:
		if export {
			// Every export answers one json.RawMessage, so a container is
			// built as a list of them and then written as one value.
			f.linef("%sItems := make([]%s.RawMessage, 0, len(%s))", dst, json, src)
			f.w.Block(fmt.Sprintf("for _, item := range %s {", src), "}", func() {
				f.liveExpr(x.Elem, "item", "element", true, fail)
				f.linef("%sItems = append(%sItems, element)", dst, dst)
			})
			f.linef("%s, err := %s.MarshalJSON(%sItems)", dst, f.runtime(), dst)
			f.linef("if err != nil { return %s }", failure())
			return
		}
		f.linef("var %sRaw []%s.RawMessage", dst, json)
		f.linef("if err := %s.Unmarshal(%s, &%sRaw); err != nil { return %s }", json, src, dst, failure())
		f.linef("%s := make(%s, 0, len(%sRaw))", dst, f.spell(e), dst)
		f.w.Block(fmt.Sprintf("for _, item := range %sRaw {", dst), "}", func() {
			f.liveExpr(x.Elem, "item", "element", false, fail)
			f.linef("%s = append(%s, element)", dst, dst)
		})
	case model.Map:
		if export {
			f.linef("%sMembers := make(map[string]%s.RawMessage, len(%s))", dst, json, src)
			f.w.Block(fmt.Sprintf("for key, item := range %s {", src), "}", func() {
				f.liveExpr(x.Elem, "item", "element", true, fail)
				f.linef("%sMembers[key] = element", dst)
			})
			f.linef("%s, err := %s.MarshalJSON(%sMembers)", dst, f.runtime(), dst)
			f.linef("if err != nil { return %s }", failure())
			return
		}
		f.linef("var %sRaw map[string]%s.RawMessage", dst, json)
		f.linef("if err := %s.Unmarshal(%s, &%sRaw); err != nil { return %s }", json, src, dst, failure())
		f.linef("%s := make(%s, len(%sRaw))", dst, f.spell(e), dst)
		f.w.Block(fmt.Sprintf("for key, item := range %sRaw {", dst), "}", func() {
			f.liveExpr(x.Elem, "item", "element", false, fail)
			f.linef("%s[key] = element", dst)
		})
	case model.Nullable:
		if export {
			f.linef("var %s %s.RawMessage", dst, json)
			f.w.Block(fmt.Sprintf("if %s.Null {", src), "} else {", func() {
				f.linef("%s = %s.RawMessage(\"null\")", dst, json)
			})
			f.liveExpr(x.Elem, src+".Value", dst+"Held", true, fail)
			f.linef("%s = %sHeld", dst, dst)
			f.line("}")
			return
		}
		f.linef("var %s %s", dst, f.spell(e))
		f.w.Block(fmt.Sprintf("if string(%s) == \"null\" {", src), "} else {", func() {
			f.linef("%s = %s.Null[%s]()", dst, f.runtime(), f.spell(x.Elem))
		})
		f.liveExpr(x.Elem, src, dst+"Held", false, fail)
		f.linef("%s = %s.NonNull(%sHeld)", dst, f.runtime(), dst)
		f.line("}")
	default:
		call := f.liveCall(e, export)
		if export {
			f.linef("%s, err := %s(scope, %s)", dst, call, src)
			f.linef("if err != nil { return %s }", failure())
			return
		}
		f.linef("%s, err := %s(scope, %s)", dst, call, src)
		f.linef("if err != nil { return %s }", failure())
	}
}

// liveCall is the generated conversion function of a named live type, in the
// package that declares it.
func (f *file) liveCall(e model.TypeExpr, export bool) string {
	family, name := "", ""
	switch x := e.(type) {
	case model.Named:
		name = x.Name
	case model.Imported:
		family, name = x.Family, x.Name
	case model.Inline:
		t := f.family.InlineType(x)
		family, name = t.Origin.Family, t.Name
	}
	prefix := identExport
	if !export {
		prefix = identImport
	}
	if family == "" || family == f.family.Name {
		if export {
			return f.proto() + f.plan.exports[name]
		}
		return f.proto() + f.plan.imports_[name]
	}
	source := f.family.ReferencedFamily(family)
	if source != nil {
		if override, ok := source.Override(Name, name); ok {
			name = override
		}
	}
	return f.peer(family) + "." + prefix + name
}

// liveUnion converts a union whose arms carry callables: the tag is written
// as it always is, and the payload is converted by the arm it belongs to.
func (f *file) liveUnion(t *render.Type, export bool) {
	json := f.std("json")
	name := f.plan.types[t.Name]
	u := f.plan.unions[t.Name]
	planned := map[string]unionVariant{}
	for _, variant := range f.unionVariants(t) {
		planned[variant.tag] = variant
	}
	// The payload sits behind the selected pointer, and behind its Value
	// when the arm carries something that is not a record of its own.
	held := func(variant unionVariant) string {
		if variant.wrapped {
			return "v." + variant.field + ".Value"
		}
		return "*v." + variant.field
	}
	if export {
		f.linef("wire := map[string]%s.RawMessage{}", json)
		f.linef("tag, err := %s.MarshalJSON(string(v.Kind()))", f.runtime())
		f.line("if err != nil { return nil, err }")
		f.linef("wire[%s] = tag", quote(t.Tag))
		f.w.Block("switch v.Kind() {", "}", func() {
			for _, variant := range t.Variants {
				p := planned[variant.Tag]
				f.linef("case %s:", p.kind)
				if p.empty {
					continue
				}
				f.liveExpr(variant.Type, held(p), "payload", true, "nil")
				f.linef("wire[%s] = payload", quote(t.Value))
			}
			f.line("default:")
			f.linef("return nil, %s.Errorf(\"%s: no variant is selected\")", f.std("fmt"), name)
		})
		f.linef("data, err := %s.MarshalObject([]string{%s, %s}, wire)", f.runtime(), quote(t.Tag), quote(t.Value))
		f.line("if err != nil { return nil, err }")
		f.linef("if err := %s; err != nil { return nil, err }", f.validateType(t, "data"))
		f.line("return data, nil")
		return
	}
	f.linef("if err := %s; err != nil { return value, err }", f.validateType(t, "raw"))
	f.linef("var wire map[string]%s.RawMessage", json)
	f.linef("if err := %s.Unmarshal(raw, &wire); err != nil { return value, err }", json)
	f.line("var tag string")
	f.linef("if err := %s.Unmarshal(wire[%s], &tag); err != nil { return value, err }", json, quote(t.Tag))
	f.w.Block(fmt.Sprintf("switch %s(tag) {", u.kind), "}", func() {
		for _, variant := range t.Variants {
			p := planned[variant.Tag]
			f.linef("case %s:", p.kind)
			if p.empty {
				f.linef("value.%s = &struct{}{}", p.field)
				continue
			}
			f.liveExpr(variant.Type, "wire["+quote(t.Value)+"]", "payload", false, "value")
			if p.wrapped {
				f.linef("value.%s = &%s{Value: payload}", p.field, p.wrapper+apply(t.Uses))
				continue
			}
			f.linef("value.%s = &payload", p.field)
		}
		f.line("default:")
		f.linef("return value, %s.Errorf(\"%s: unknown variant %%q\", tag)", f.std("fmt"), name)
	})
	f.line("return value, nil")
}

// emitLiveRefusal replaces the ordinary codecs of a live type. A live value
// has no scope-free encoding — a reference means nothing outside the scope
// that minted its binding — so the refusal is the contract, not a gap, and it
// names the pair that does have a scope.
func (f *file) emitLiveRefusal(self string, t *render.Type) {
	name := f.plan.types[t.Name]
	f.linef("// %s refuses: %s carries a callable, and a live value has no encoding apart from the scope its bindings belong to.", identMarshalJSON, name)
	f.linef("func (v %s) %s() ([]byte, error) { return nil, %s.Errorf(%s) }", self, identMarshalJSON, f.std("fmt"),
		quote(name+" carries a callable; write it with "+f.plan.exports[t.Name]+", which takes the live scope its bindings are made in"))
	f.linef("// %s refuses for the same reason: a reference resolves in a scope or nowhere.", identUnmarshalJSON)
	f.linef("func (v *%s) %s(data []byte) error { return %s.Errorf(%s) }", self, identUnmarshalJSON, f.std("fmt"),
		quote(name+" carries a callable; read it with "+f.plan.imports_[t.Name]+", which takes the live scope its references resolve in"))
}

// validateExpression holds a value to one declared expression, which is what
// a callable's request and result are validated against: they are positions
// in a declaration rather than named types of their own.
func (f *file) validateExpression(e model.TypeExpr, data string) string {
	return f.boundSchema(f.uses) + "." + identValidateExpressionRaw + "(" + f.proto() + identMustTypeExpression + "(" + expression(e) + "), " + data + ")"
}

func (f *file) live() string { return f.use("live", f.config.Live) }

func lower(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}
