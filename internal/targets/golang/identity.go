package golang

import (
	"fmt"
	"strings"
)

func identityArguments(f *file) string { return strings.TrimPrefix(f.slotArguments(), ", ") }

// The identity is closed over every supplied type and family binding before a
// carrier or model factory is touched. Receivers use a late lookup because the
// interpretation is registered before its factory can be bound.
func (f *file) emitWireIdentity(side, opposite string) {
	decl, args, open := declare(f.family.Uses), apply(f.family.Uses), f.entry(f.family.Uses)
	rt, proto := f.runtime(), f.proto()
	contextType := f.adapterContext()
	f.w.Block(fmt.Sprintf("func declarationIdentity%s(%s) (%s.DeclarationIdentity, error) {", decl, strings.TrimPrefix(f.slotParameters(), ", "), rt), "}", func() {
		f.linef("digest, err := %s.DeclarationDigest()", f.boundSchema(f.family.Uses))
		f.linef("if err != nil { return %s.DeclarationIdentity{}, err }", rt)
		f.linef("return %s.DeclarationIdentity{Path:%q,Digest:digest}, nil", rt, f.family.Name)
	})
	f.w.Block(fmt.Sprintf("func registerIdentity(wire %s.HandlerRegistry, identity %s.DeclarationIdentity) (func(), error) {", rt, rt), "}", func() {
		f.linef("handler, err := %s.IdentityHandler(identity)", rt)
		f.line("if err != nil { return nil, err }")
		f.linef("return %s.HandleWire(wire, []string{%s.IdentityMethod}, func(ctx %s.Context, raw %s.RawMessage) (any,error) { return handler(ctx,nil,raw) })", rt, rt, f.std("context"), f.std("json"))
	})
	f.line("// PrepareFromWire registers receivers synchronously, before the wire is attached.")
	f.line("// Complete checks identity and returns a factory that may be bound once. Both steps")
	f.line("// must finish within environment.Options.RequestTimeout. Cleanup detaches this")
	f.line("// interpretation's registrations, including after success, and never closes the wire.")
	f.w.Block(fmt.Sprintf("func PrepareFromWire%s(wire %s.Endpoint, environment %s%s) (complete func(%s.Context) (%s%sModel%s,error), cleanup func(), err error) {", open, f.bitwire(), contextType, f.slotParameters(), f.std("context"), proto, side, args), "}", func() {
		f.linef("if wire == nil { return nil, nil, %s.Errorf(\"wire is required\") }", f.std("fmt"))
		f.linef("environment, err = normalizeContext%s(environment%s)", args, f.slotArguments())
		f.line("if err != nil { return nil, nil, err }")
		f.linef("identity, err := declarationIdentity%s(%s)", args, identityArguments(f))
		f.line("if err != nil { return nil, nil, err }")
		f.linef("preparation, err := %s.PrepareIdentity(wire, identity, environment.Options)", rt)
		f.line("if err != nil { return nil, nil, err }")
		f.line("cleanup = preparation.Close")
		f.linef("var implementation %s.Pointer[%s%s%s]", f.use("atomic", "sync/atomic"), proto, opposite, args)
		f.linef("lookup := func() %s%s%s { if current := implementation.Load(); current != nil { return *current }; return %s%s%s{} }", proto, opposite, args, proto, opposite, args)
		f.linef("if err = bind%s%s(preparation.Wire(),lookup,environment%s); err != nil { cleanup(); return nil, nil, err }", opposite, args, f.slotArguments())
		f.linef("var checking, bound %s.Bool", f.use("atomic", "sync/atomic"))
		f.w.Block(fmt.Sprintf("complete = func(ctx %s.Context) (%s%sModel%s,error) {", f.std("context"), proto, side, args), "}", func() {
			f.linef("if !checking.CompareAndSwap(false,true) { return nil, %s.Errorf(\"identity completion is already started\") }", f.std("fmt"))
			f.line("if err := preparation.Check(ctx); err != nil { cleanup(); return nil, err }")
			f.w.Block(fmt.Sprintf("return func(value %s%s%s) (%s%s%s,error) {", proto, opposite, args, proto, side, args), "}, nil", func() {
				f.linef("if !bound.CompareAndSwap(false,true) { return %s%s%s{}, %s.Errorf(\"model factory is already bound\") }", proto, side, args, f.std("fmt"))
				methods, _ := f.sideOperations(opposite)
				if len(methods) > 0 {
					f.linef("if value.Methods == nil { cleanup(); return %s%s%s{}, %s.Errorf(\"%s methods are required\") }", proto, side, args, f.std("fmt"), opposite)
				}
				f.line("implementation.Store(&value)")
				f.linef("if err := preparation.Ready(); err != nil { cleanup(); return %s%s%s{}, err }", proto, side, args)
				f.linef("return access%s%s(preparation.Wire(),environment%s), nil", side, args, f.slotArguments())
			})
		})
		f.line("return complete, cleanup, nil")
	})
	f.line("// FromWire checks identity and returns a factory that may be bound once.")
	f.line("// Use PrepareFromWire before attachment when incoming delivery can begin immediately.")
	f.w.Block(fmt.Sprintf("func FromWire%s(ctx %s.Context, wire %s.Endpoint, environment %s%s) (%s%sModel%s,error) {", open, f.std("context"), f.bitwire(), contextType, f.slotParameters(), proto, side, args), "}", func() {
		f.linef("complete, cleanup, err := PrepareFromWire%s(wire,environment%s)", args, f.slotArguments())
		f.line("if err != nil { return nil, err }")
		f.line("model, err := complete(ctx)")
		f.line("if err != nil { cleanup(); return nil, err }")
		f.line("return model, nil")
	})
}

func (f *file) wireValidateImplementation(side, failure string) {
	methods, _ := f.sideOperations(side)
	if len(methods) > 0 {
		f.linef("if implementation.Methods == nil { %s%s.Errorf(\"%s methods are required\") }", failure, f.std("fmt"), side)
	}
}
