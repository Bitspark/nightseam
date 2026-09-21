package golang

import (
	"fmt"
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// The generated access surface is a model factory on either side of one Wire.
// Carrier construction and live-scope construction belong to the host.
func emitBinding(f *file) { f.emitWireAdapter("Server", "Client") }
func emitClient(f *file)  { f.emitWireAdapter("Client", "Server") }

func (f *file) adapterContext() string { return f.runtime() + ".AdapterContext" }

func (f *file) emitWireModels() {
	if !f.family.HasModel() {
		return
	}
	decl, args := declare(f.family.Uses), apply(f.family.Uses)
	for _, name := range []string{"Server", "Client"} {
		methods, events := f.sideOperations(name)
		f.w.Block(fmt.Sprintf("type %sMethods%s interface {", name, decl), "}", func() {
			for _, m := range methods {
				f.linef("%s(ctx %s.Context%s) (%s, error)", f.plan.operations[m.Name], f.std("context"), f.request(m), f.spell(m.Result))
			}
		})
		f.w.Block(fmt.Sprintf("type %sEvents%s interface {", name, decl), "}", func() {
			for _, e := range events {
				f.linef("%s(ctx %s.Context, data %s) error", f.plan.operations[e.Name], f.std("context"), f.spell(e.Type))
			}
		})
		f.linef("type %s%s struct { Methods %sMethods%s; Events %sEvents%s }", name, decl, name, args, name, args)
	}
	f.linef("type ServerModel%s func(Client%s) (Server%s, error)", decl, args, args)
	f.linef("type ClientModel%s func(Server%s) (Client%s, error)", decl, args, args)
}

// A delivery facet contains what this side receives, including events declared
// by the opposite side. Keeping the facets separate avoids Go member collisions.
func (f *file) sideOperations(name string) ([]render.Method, []render.Event) {
	if name == "Server" {
		return f.family.Server.Methods, f.family.Client.Events
	}
	return f.family.Client.Methods, f.family.Server.Events
}

func (f *file) emitWireAdapter(side, opposite string) {
	f.operationAdapters()
	decl, args, open := declare(f.family.Uses), apply(f.family.Uses), f.entry(f.family.Uses)
	rt, seam, proto := f.runtime(), f.seam(), f.proto()
	contextType := f.adapterContext()
	for _, name := range []string{"Server", "Client"} {
		methods, events := f.sideOperations(name)
		lower := "server"
		if name == "Client" {
			lower = "client"
		}
		for _, facet := range []string{"Methods", "Events"} {
			f.linef("type %s%s%s struct { wire %s.Wire; environment %s%s }", lower, facet, decl, seam, contextType, f.slotFields())
		}
		f.linef("func access%s%s(wire %s.Wire, environment %s%s) %s%s%s { return %s%s%s{Methods: &%sMethods%s{wire:wire,environment:environment%s}, Events: &%sEvents%s{wire:wire,environment:environment%s}} }", name, decl, seam, contextType, f.slotParameters(), proto, name, args, proto, name, args, lower, args, f.slotValues(), lower, args, f.slotValues())
		for _, m := range methods {
			f.wireCaller(m, lower+"Methods"+args)
		}
		for _, e := range events {
			f.wireEmitter(e, lower+"Events"+args)
		}
		f.wireRegistration(name, methods, events)
	}
	f.w.Block(fmt.Sprintf("func normalizeContext%s(environment %s%s) (%s, error) {", decl, contextType, f.slotParameters(), contextType), "}", func() {
		f.adapterRecipes(f.slotUses(), "environment, ")
		for _, use := range f.family.ObjectDraws {
			if !model.Carried(use.Type) {
				f.linef("if err := %s.ValidateDrawnType(adapter%s.Binding, %q, true); err != nil { return environment, err }", rt, parameterName(use), use.Type)
			}
		}
		f.linef("if (%s) && environment.ValueEnvironment == nil { return environment, %s.Errorf(\"a context-dependent adapter requires a value environment\") }", f.scopeLive(), f.std("fmt"))

		f.line("return environment, nil")
	})
	f.linef("// ToWire binds one model factory and returns its access wire.")
	f.w.Block(fmt.Sprintf("func ToWire%s(model %s%sModel%s, environment %s%s) (%s.Wire, error) {", open, proto, side, args, contextType, f.slotParameters(), seam), "}", func() {
		f.linef("if model == nil { return nil, %s.Errorf(\"model factory is required\") }", f.std("fmt"))
		f.linef("environment, err := normalizeContext%s(environment%s)", args, f.slotArguments())
		f.line("if err != nil { return nil, err }")
		f.linef("identity, err := declarationIdentity%s(%s)", args, identityArguments(f))
		f.line("if err != nil { return nil, err }")
		f.line("options := environment.Options")
		f.labels()
		f.linef("access, binding, err := %s.NewWirePair(options)", rt)
		f.line("if err != nil { return nil, err }")
		f.line("complete := false")
		f.linef("defer func() { if !complete { _ = access.Close(%s.CodeInternalError, \"model construction failed\") } }()", seam)
		f.line("if _, err := registerIdentity(binding, identity); err != nil { return nil, err }")
		f.linef("implementation, err := model(access%s%s(binding,environment%s))", opposite, args, f.slotArguments())
		f.line("if err != nil { return nil, err }")
		f.wireValidateImplementation(side, "return nil, ")
		f.linef("if err := bind%s%s(binding,func() %s%s%s { return implementation },environment%s); err != nil { return nil, err }", side, args, proto, side, args, f.slotArguments())
		f.line("complete = true; return access, nil")
	})
	f.emitWireIdentity(side, opposite)
	f.emitRecordedEvents(side, opposite)
}

// Declaration identity failures retain their public refusal through validation.
func (f *file) invalidParams() string {
	return fmt.Sprintf("var public *%s.PublicError; if %s.As(err, &public) && public.Code == \"contract_mismatch\" { return nil, err }; return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()}", f.runtime(), f.std("errors"), f.runtime())
}

func (f *file) labels() {
	f.line("families := map[string]string{}")
	f.line("for name, existing := range options.Families { families[name] = existing }")
	for _, name := range operations(f.family) {
		f.linef("families[%q] = %q", fmt.Sprintf("%d:%s", len(name), name), f.family.Name)
	}
	f.line("options.Families = families")
}
func operations(fam *render.Family) []string {
	var names []string
	for _, side := range []render.Side{fam.Server, fam.Client} {
		for _, m := range side.Methods {
			names = append(names, m.Name)
		}
		for _, e := range side.Events {
			names = append(names, e.Name)
		}
	}
	return names
}

// Each request gets a child of the explicitly selected owner. Outgoing work
// uses a same-scope owner from its context, preserving publication batches.
func (f *file) wireOwner(expressions []model.TypeExpr, incoming bool, prefix, failure string) {
	needed := false
	var parts []string
	for _, e := range expressions {
		if f.needsConversion(e) {
			needed = true
			parts = append(parts, f.expressionLive(e))
		}
	}
	if !needed {
		return
	}
	f.w.Block("if "+liveOr(parts...)+" {", "}", func() {
		f.line("var err error")
		action := "Select"
		if incoming {
			action = "Child"
		}
		f.linef("ctx, err = %senvironment.ValueEnvironment.%s(ctx)", prefix, action)
		f.linef("if err != nil { return %serr }", failure)
	})
}

func (f *file) wireCaller(m render.Method, receiver string) {
	previous := f.adapterPrefix
	f.adapterPrefix = "c."
	defer func() { f.adapterPrefix = previous }()
	json := f.std("json")
	f.w.Block(fmt.Sprintf("func(c *%s) %s(ctx %s.Context%s) (%s,error) {", receiver, f.plan.operations[m.Name], f.std("context"), f.request(m), f.spell(m.Result)), "}", func() {
		f.linef("var result %s", f.spell(m.Result))
		f.wireOwner([]model.TypeExpr{m.Request, m.Result}, false, "c.", "result, ")
		if m.Request != nil && f.needsConversion(m.Request) {
			f.publishBoundary(m.Request, "params", "raw", func() {
				f.linef("var raw %s.RawMessage", json)
				f.linef("err := %s.CallWire(ctx,c.wire,[]string{%q},sent,&raw,%s.WireCallOptions{Observer:c.environment.Options.Observer,Family:%q,Propagator:c.environment.Options.Propagator,RequestTimeout:c.environment.Options.RequestTimeout})", f.runtime(), m.Name, f.runtime(), f.family.Name)
				f.line("return raw,err")
			})
			f.line("if err != nil { return result,err }")
		} else {
			if m.Request != nil {
				f.linef("if err := %s.ValidateValue(%sMustTypeExpression(%s),params); err != nil {return result,err}", f.boundSchema(f.uses), f.proto(), expression(m.Request))
			}
			f.linef("var raw %s.RawMessage", json)
			f.linef("if err := %s.CallWire(ctx,c.wire,[]string{%q},%s,&raw,%s.WireCallOptions{Observer:c.environment.Options.Observer,Family:%q,Propagator:c.environment.Options.Propagator,RequestTimeout:c.environment.Options.RequestTimeout}); err != nil {return result,err}", f.runtime(), m.Name, argument(m), f.runtime(), f.family.Name)
		}
		if f.needsConversion(m.Result) {
			f.liveBoundary(m.Result, "raw", "received", false)
			f.line("return received,err")
			return
		}
		f.linef("if err := %s.ValidateExpressionRaw(%sMustTypeExpression(%s),raw);err!=nil{return result,err}", f.boundSchema(f.uses), f.proto(), expression(m.Result))
		f.linef("if err := %s.Unmarshal(raw,&result);err!=nil{return result,err}", json)
		f.line("return result,nil")
	})
}
func (f *file) wireEmitter(e render.Event, receiver string) {
	previous := f.adapterPrefix
	f.adapterPrefix = "c."
	defer func() { f.adapterPrefix = previous }()
	f.w.Block(fmt.Sprintf("func(c *%s) %s(ctx %s.Context,data %s) error {", receiver, f.plan.operations[e.Name], f.std("context"), f.spell(e.Type)), "}", func() {
		f.wireOwner([]model.TypeExpr{e.Type}, false, "c.", "")
		if f.needsConversion(e.Type) {
			f.publishBoundary(e.Type, "data", "_", func() {
				f.linef("return nil,%s.EmitWire(ctx,c.wire,[]string{%q},sent,%s.WireEmitOptions{Observer:c.environment.Options.Observer,Family:%q,Propagator:c.environment.Options.Propagator})", f.runtime(), e.Name, f.runtime(), f.family.Name)
			})
			f.line("return err")
			return
		}
		f.linef("if err := %s.ValidateValue(%sMustTypeExpression(%s),data);err!=nil{return err}", f.boundSchema(f.uses), f.proto(), expression(e.Type))
		f.linef("return %s.EmitWire(ctx,c.wire,[]string{%q},data,%s.WireEmitOptions{Observer:c.environment.Options.Observer,Family:%q,Propagator:c.environment.Options.Propagator})", f.runtime(), e.Name, f.runtime(), f.family.Name)
	})
}

func (f *file) wireRegistration(side string, methods []render.Method, events []render.Event) {
	rt := f.runtime()
	decl, args := declare(f.family.Uses), apply(f.family.Uses)
	f.w.Block(fmt.Sprintf("func bind%s%s(wire %s.Wire, lookup func() %s%s%s, environment %s%s) error {", side, decl, f.seam(), f.proto(), side, args, f.adapterContext(), f.slotParameters()), "}", func() {
		f.line("var detach []func(); complete := false")
		f.line("defer func(){if !complete{for _,off:=range detach{off()}}}()")
		byName := map[string]bool{}
		for _, m := range methods {
			byName[m.Name] = true
		}
		for _, e := range events {
			byName[e.Name] = true
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			f.w.Block("{", "}", func() {
				f.linef("handlers := %s.WireHandlers{Observer:environment.Options.Observer,Family:%q}", rt, f.family.Name)
				for _, m := range methods {
					if m.Name == name {
						f.wireRequest(m)
					}
				}
				for _, e := range events {
					if e.Name == name {
						f.wireEvent(e)
					}
				}
				f.w.Block("if handlers.Request != nil || handlers.Event != nil {", "}", func() {
					f.linef("off,err:=%s.RegisterWire(wire,[]string{%q},handlers)", rt, name)
					f.line("if err != nil{return err};detach=append(detach,off)")
				})
			})
		}
		f.line("complete=true;return nil")
	})
}
func (f *file) wireRequest(m render.Method) {
	json := f.std("json")
	f.w.Block(fmt.Sprintf("handlers.Request = func(ctx %s.Context,raw %s.RawMessage)(any,error){", f.std("context"), json), "}", func() {
		f.line("implementation := lookup()")
		f.linef("if implementation.Methods == nil { return nil, %s.Errorf(\"model methods are required\") }", f.std("fmt"))
		f.wireOwner([]model.TypeExpr{m.Request, m.Result}, true, "", "nil, ")
		params := ""
		if m.Request != nil {
			if f.needsConversion(m.Request) {
				f.liveBoundary(m.Request, "raw", "params", false)
				f.linef("if err!=nil{%s}", f.invalidParams())
			} else {
				f.linef("if err:=%s.ValidateExpressionRaw(%sMustTypeExpression(%s),raw);err!=nil{%s}", f.boundSchema(f.uses), f.proto(), expression(m.Request), f.invalidParams())
				f.linef("var params %s", f.spell(m.Request))
				f.linef("if err:=%s.Unmarshal(raw,&params);err!=nil{%s}", json, f.invalidParams())
			}
			params = ",params"
		} else {
			f.linef("if err:=%s.ValidateExpressionRaw(map[string]any{\"empty\":true},raw);err!=nil{%s}", f.boundSchema(f.uses), f.invalidParams())
		}
		f.linef("result,err:=implementation.Methods.%s(ctx%s)", f.plan.operations[m.Name], params)
		f.line("if err!=nil{return nil,err}")
		if f.needsConversion(m.Result) {
			f.liveBoundary(m.Result, "result", "sent", true)
			f.line("return sent,err")
			return
		}
		f.linef("if err=%s.ValidateValue(%sMustTypeExpression(%s),result);err!=nil{return nil,err}", f.boundSchema(f.uses), f.proto(), expression(m.Result))
		f.line("return result,nil")
	})
}
func (f *file) wireEvent(e render.Event) {
	f.w.Block(fmt.Sprintf("handlers.Event=func(ctx %s.Context,raw %s.RawMessage)error{", f.std("context"), f.std("json")), "}", func() {
		f.line("implementation := lookup()")
		f.line("if implementation.Events == nil { return nil }")
		f.wireOwner([]model.TypeExpr{e.Type}, true, "", "")
		if f.needsConversion(e.Type) {
			f.liveBoundary(e.Type, "raw", "data", false)
			f.line("if err!=nil{return err}")
		} else {
			f.linef("if err:=%s.ValidateExpressionRaw(%sMustTypeExpression(%s),raw);err!=nil{return err}", f.boundSchema(f.uses), f.proto(), expression(e.Type))
			f.linef("var data %s", f.spell(e.Type))
			f.linef("if err:=%s.Unmarshal(raw,&data);err!=nil{return err}", f.std("json"))
		}
		f.linef("return implementation.Events.%s(ctx,data)", f.plan.operations[e.Name])
	})
}
