package check

import (
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// Live holds the live tier to its rules. Its two sides have the protocol's
// shape and **add** to the family's surface, so an operation of the live
// tier is held to everything an operation of the protocol tier is — a
// request that is an object, a result, declared errors, a name nobody
// else's operation takes and a name outside a layer's namespace — and to
// one rule of its own.
//
// That rule is the reason the tier exists: **an operation of the live tier
// carries a callable.** A method whose request, result and errors are all
// self-contained data is an ordinary RPC method and belongs in
// protocol.json, where a consumer can use it with no live runtime at all.
// Admitting it here would make live.json the place operations drift to,
// and the independence of the lower tiers would decay one misplacement at
// a time rather than being a property of the language. The same rule holds
// for a type: a declaration in live.json whose values carry no callable is
// declared a tier too high, where every family that names it acquires a
// dependency it does not need.
//
// The upward direction needs no rule here: a callable is declared in
// live.json, so it ranks with the live tier, so a lower tier that names one
// — or names anything that reaches one — is already refused by the
// direction rule every tier shares.
func Live(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	l := f.Live
	if l == nil {
		return nil
	}
	context := model.Rank(model.LiveFile)
	where := site{context: context, inline: true}

	// Every operation the protocol tier already declares, so that a live
	// operation cannot quietly take a name that flows the same way.
	wire := map[string]diag.Location{}
	if f.Protocol != nil {
		for _, side := range []struct {
			side            *model.Side
			calls, notifies string
		}{
			{&f.Protocol.Server, "client to server", "server to client"},
			{&f.Protocol.Client, "server to client", "client to server"},
		} {
			for i := range side.side.Methods {
				wire[side.calls+":"+side.side.Methods[i].Name] = side.side.Methods[i].At
			}
			for i := range side.side.Events {
				wire[side.notifies+":"+side.side.Events[i].Name] = side.side.Events[i].At
			}
		}
	}
	operation := func(direction, name string, at diag.Location) {
		c.reservedOperation(name, at)
		key := direction + ":" + name
		if previous, ok := wire[key]; ok {
			c.Addf(at, "operation_collision", "Operation %s collides with the one at %s: both flow %s under one name.", name, previous, direction)
		} else {
			wire[key] = at
		}
	}

	for _, side := range []struct {
		side            *model.Side
		calls, notifies string
		label           string
	}{
		{&l.Server, "client to server", "server to client", "server"},
		{&l.Client, "server to client", "client to server", "client"},
	} {
		if len(side.side.Extends) > 0 {
			c.Addf(side.side.At.Sub("extends"), "invalid_extends", "The live tier's %s side extends nothing: a side is extended once, in the protocol tier, and the live tier adds operations to the surface that produces.", side.label)
		}
		for i := range side.side.Methods {
			m := &side.side.Methods[i]
			operation(side.calls, m.Name, m.At)
			if m.Request != nil {
				before := len(c.Diagnostics)
				c.expression(m.Request, m.At.Sub("request"), where)
				if len(c.Diagnostics) == before && !f.IsObject(m.Request) {
					c.Add(m.At.Sub("request"), "invalid_request", "A method's request is a record, of this family or an imported one, written inline, or a type drawn from a parameter: params is an object on the wire.")
				}
			}
			c.expression(m.Result, m.At.Sub("result"), where)
			for j, code := range m.Errors {
				if f.Protocol == nil {
					continue
				}
				if _, declared := f.Protocol.Error(code); !declared {
					c.Addf(m.At.Sub("errors", j), "unknown_error", "Method %s may return %s, which the family does not declare among its errors.", m.Name, code)
				}
			}
			if !f.IsLive(m.Request) && !f.IsLive(m.Result) {
				c.Addf(m.At, "not_live", "Method %s carries no callable, so it needs no live runtime: declare it in %s, where it is usable without one.", m.Name, model.ProtocolFile)
			}
		}
		for i := range side.side.Events {
			e := &side.side.Events[i]
			operation(side.notifies, e.Name, e.At)
			c.expression(e.Type, e.At.Sub("type"), where)
			if !f.IsLive(e.Type) {
				c.Addf(e.At, "not_live", "Event %s carries no callable, so it needs no live runtime: declare it in %s, where it is usable without one.", e.Name, model.ProtocolFile)
			}
		}
		if len(side.side.CRUD) > 0 {
			c.Addf(side.side.At.Sub("crud"), "unsupported", "crud is not expanded yet; write the %s side's methods out.", side.label)
		}
	}

	for _, name := range f.TypeNames() {
		t := f.Types[name]
		if f.IsCarried(name) || t.At.File != model.LiveFile || t.IsCallable() {
			continue
		}
		if !f.IsLiveType(name) {
			c.Addf(t.At, "not_live", "Type %s carries no callable, so nothing about it needs the live tier: declare it in %s or %s, so that a family naming it does not acquire a live dependency it has no use for.", name, model.ModelFile, model.ProtocolFile)
		}
	}
	return c.Diagnostics
}

// reservedOperation refuses a consumer operation in a layer's namespace. A
// layer that speaks on the wire owns a prefix — `channel.` for the tunnel,
// `live.` for the live layer — declared by the built-in family that is that
// layer's vocabulary, so the reservation is read off the built-ins rather
// than written out here ([the wire vocabulary](../../docs/wire/vocabulary.md)).
func (c *checker) reservedOperation(name string, at diag.Location) {
	prefix, _, qualified := strings.Cut(name, ".")
	if !qualified {
		return
	}
	owner, reserved := c.f.Namespaces()[prefix]
	if !reserved || owner == c.f.Name {
		return
	}
	c.Addf(at, "reserved_name", "Operation %s is in the namespace of the built-in %s family, which is the wire vocabulary of a layer; a layer owns its namespace and a consumer declares no operation in one.", name, owner)
}
