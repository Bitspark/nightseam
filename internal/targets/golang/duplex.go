package golang

import (
	"fmt"

	"github.com/Bitspark/nightseam/internal/render"
)

// emitBinding renders the server side: the Handler interface a server
// implements, the Remote it calls back through, NewHandler for a WebSocket
// endpoint and Serve for any connection of the seam — a tunnel channel, a
// pipe. Those of a generic family are generic in the parameters its types
// use, and a server instantiates them with the families that fill the
// slots; the entry points hold every type drawn from one parameter to one
// family's Tag, so that they cannot be bound to different families.
func emitBinding(f *file) {
	p, fam := f.plan, f.family
	decl, args, open := declare(fam.Uses), apply(fam.Uses), f.entry(fam.Uses)
	runtime, ctx := f.runtime(), f.std("context")
	f.linef("// %s provides typed calls back to the connected client.", identRemote)
	f.linef("type %s%s struct{ %s *%s.Peer }", identRemote, decl, identPeer, runtime)
	f.w.Block(fmt.Sprintf("type %s%s interface {", identHandler, decl), "}", func() {
		for _, m := range fam.Server.Methods {
			if m.Description != "" {
				f.linef("// %s: %s", p.operations[m.Name], m.Description)
			}
			f.linef("%s(ctx %s.Context, remote *%s%s%s) (%s, error)", p.operations[m.Name], ctx, identRemote, args, f.request(m), f.spell(m.Result))
		}
	})
	for _, m := range fam.Client.Methods {
		f.caller(m, identRemote+args)
	}
	// install registers the family's methods on the options a peer is made
	// with, beside whatever the caller registered, and labels every name of
	// the family with it; NewHandler and Serve share it.
	f.linef("// %s registers the family's methods on the options a peer is made with and labels its names with the family.", identInstall)
	f.w.Block(fmt.Sprintf("func %s%s(handler %s%s, options *%s.Options) error {", identInstall, decl, identHandler, args, runtime), "}", func() {
		f.linef("if handler == nil { return %s.Errorf(\"handler is required\") }", f.std("fmt"))
		f.linef("handlers := map[string]%s.Handler{}", runtime)
		f.line("for name, existing := range options.Handlers { handlers[name] = existing }")
		for _, m := range fam.Server.Methods {
			f.registration(m, "handler", "&"+identRemote+args+"{"+identPeer+": peer}")
		}
		f.line("options.Handlers = handlers")
		f.labels()
		f.liveScope(nil)
		f.line("return nil")
	})
	f.linef("// %s serves the family at a WebSocket endpoint; it requires explicit authentication and origin policy through options.", identNewHandler)
	f.linef("func %s%s(handler %s%s, options %s.ServerOptions) (%s.Handler, error) { if err := %s%s(handler, &options.Options); err != nil { return nil, err }; return %s.NewHandler(options) }", identNewHandler, open, identHandler, args, runtime, f.std("http"), identInstall, args, runtime)
	f.linef("// %s serves the family over a connection of the seam — a tunnel channel, a pipe, an accepted socket — as the server side of it; the peer is the caller's to close.", identServe)
	f.linef("func %s%s(ctx %s.Context, conn %s.Conn, options %s.Options, handler %s%s) (*%s.Peer, error) { if err := %s%s(handler, &options); err != nil { return nil, err }; return %s.NewPeer(ctx, conn, %s.ServerRole, options) }", identServe, open, ctx, f.seam(), runtime, identHandler, args, runtime, identInstall, args, runtime, runtime)
	f.events(identRemote+args, fam.Client.Events, fam.Server.Events)
}

// emitClient renders the caller side: Dial over a WebSocket, Attach over
// any connection of the seam, Open over the channel a handle names on a
// tunnel, the typed calls, and the Handler interface for calls the server
// makes back. Those of a generic family are generic as the binding's are.
func emitClient(f *file) {
	p, fam := f.plan, f.family
	decl, args, open := declare(fam.Uses), apply(fam.Uses), f.entry(fam.Uses)
	runtime, ctx := f.runtime(), f.std("context")
	clientValue := "&" + identClient + args + "{" + identPeer + ": peer}"
	f.linef("type %s%s struct{ %s *%s.Peer }", identClient, decl, identPeer, runtime)
	f.linef("// %s installs typed event handlers before the client reads its first frame; nil fields leave events unhandled.", identEvents)
	f.w.Block(fmt.Sprintf("type %s%s struct {", identEvents, decl), "}", func() {
		for _, e := range fam.Server.Events {
			f.linef("%s func(%s.Context, %s)", p.operations[e.Name], ctx, f.spell(e.Type))
		}
	})
	f.w.Block(fmt.Sprintf("type %s%s interface {", identHandler, decl), "}", func() {
		for _, m := range fam.Client.Methods {
			if m.Description != "" {
				f.linef("// %s: %s", p.operations[m.Name], m.Description)
			}
			f.linef("%s(ctx %s.Context, client *%s%s%s) (%s, error)", p.operations[m.Name], ctx, identClient, args, f.request(m), f.spell(m.Result))
		}
	})
	// The protocol's caller side as an interface; a consumer may stand
	// another implementation in the client's place.
	f.linef("// %s is the protocol's caller side: every operation a client sends. %s implements it.", identCaller, identClient)
	f.w.Block(fmt.Sprintf("type %s%s interface {", identCaller, decl), "}", func() {
		for _, m := range fam.Server.Methods {
			f.linef("%s(ctx %s.Context%s) (%s, error)", p.operations[m.Name], ctx, f.request(m), f.spell(m.Result))
		}
	})
	if fam.Generic {
		f.linef("func _%s() { var _ %s%s = (*%s%s)(nil) }", decl, identCaller, args, identClient, args)
	} else {
		f.linef("var _ %s = (*%s)(nil)", identCaller, identClient)
	}
	// install registers the reverse-call handlers on the options a peer is
	// made with and labels every name of the family with it; Dial and Attach
	// share it.
	f.linef("// %s registers the reverse-call handlers on the options a peer is made with and labels its names with the family.", identInstall)
	f.w.Block(fmt.Sprintf("func %s%s(handler %s%s, events %s%s, options *%s.Options) error {", identInstall, decl, identHandler, args, identEvents, args, runtime), "}", func() {
		if len(fam.Client.Methods) > 0 {
			f.linef("if handler == nil { return %s.Errorf(\"reverse-call handler is required\") }", f.std("fmt"))
		}
		f.linef("handlers := map[string]%s.Handler{}", runtime)
		f.line("for name, existing := range options.Handlers { handlers[name] = existing }")
		for _, m := range fam.Client.Methods {
			f.registration(m, "handler", clientValue)
		}
		f.line("options.Handlers = handlers")
		f.labels()
		f.line("prepare := options.Prepare")
		f.w.Block(fmt.Sprintf("options.Prepare = func(peer *%s.Peer) error {", runtime), "}", func() {
			f.liveOver()
			if len(fam.Server.Events) > 0 {
				f.linef("client := &%s%s{%s: peer}", identClient, args, identPeer)
			}
			for _, e := range fam.Server.Events {
				name := p.operations[e.Name]
				f.linef("if events.%s != nil { if err := client.%s%s(events.%s); err != nil { return err } }", name, identOn, name, name)
			}
			f.line("if prepare != nil { return prepare(peer) }")
			f.line("return nil")
		})
		f.line("return nil")
	})
	f.linef("// %s connects to a WebSocket endpoint after installing reverse-call handlers. No request is retried.", identDial)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, url string, options %s.DialOptions, handler %s%s, events %s%s) (*%s%s, error) {", identDial, open, ctx, runtime, identHandler, args, identEvents, args, identClient, args), "}", func() {
		f.linef("if err := %s%s(handler, events, &options.Options); err != nil { return nil, err }", identInstall, args)
		f.linef("peer, response, err := %s.Dial(ctx, url, options)", runtime)
		f.line("if err != nil { if response != nil && response.Body != nil { _ = response.Body.Close() }; return nil, err }")
		f.linef("return %s, nil", clientValue)
	})
	f.linef("// %s speaks the family over a connection of the seam — a tunnel channel, a pipe, a dialled socket — as the client side of it, after installing reverse-call handlers.", identAttach)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, conn %s.Conn, options %s.Options, handler %s%s, events %s%s) (*%s%s, error) {", identAttach, open, ctx, f.seam(), runtime, identHandler, args, identEvents, args, identClient, args), "}", func() {
		f.linef("if err := %s%s(handler, events, &options); err != nil { return nil, err }", identInstall, args)
		f.linef("peer, err := %s.NewPeer(ctx, conn, %s.ClientRole, options)", runtime, runtime)
		f.line("if err != nil { return nil, err }")
		f.linef("return %s, nil", clientValue)
	})
	f.linef("// %s resolves a handle to the channel it names on a tunnel and speaks the family over it.", identOpen)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, t *%s.Tunnel, handle %sHandle, options %s.Options, handler %s%s, events %s%s) (*%s%s, error) {", identOpen, open, ctx, f.tunnel(), f.proto(), runtime, identHandler, args, identEvents, args, identClient, args), "}", func() {
		f.line("channel, ok := t.Channel(handle.Channel)")
		f.linef("if !ok { return nil, %s.Errorf(\"no channel %%d on the connection\", handle.Channel) }", f.std("fmt"))
		f.linef("return %s%s(ctx, channel, options, handler, events)", identAttach, args)
	})
	f.linef("func (c *%s%s) %s() error { return c.%s.Close() }", identClient, args, identClose, identPeer)
	for _, m := range fam.Server.Methods {
		f.caller(m, identClient+args)
	}
	f.events(identClient+args, fam.Server.Events, fam.Client.Events)
}

// registration renders the runtime handler for one method a peer receives:
// validate and decode the params, dispatch, validate the result.
func (f *file) registration(m render.Method, handler, remote string) {
	p, runtime, json := f.plan, f.runtime(), f.std("json")
	f.linef("if _, exists := handlers[%q]; exists { return %s.Errorf(\"duplicate handler %%s\", %q) }", m.Name, f.std("fmt"), m.Name)
	f.w.Block(fmt.Sprintf("handlers[%q] = func(ctx %s.Context, peer *%s.Peer, raw %s.RawMessage) (any, error) {", m.Name, f.std("context"), runtime, json), "}", func() {
		params := ""
		if m.Request != nil && f.family.IsLive(m.Request) {
			// A live request is imported rather than unmarshalled: the
			// handler is given native functions, and never a reference.
			f.linef("scope, ok := %s.ScopeOf(peer)", f.live())
			f.linef("if !ok { return nil, &%s.PublicError{Code: %s.ErrorScopeClosed, Message: \"the connection carries no live scope\"} }", runtime, f.live())
			f.linef("params, err := %s(scope, raw)", f.liveCall(m.Request, false))
			f.linef("if err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", runtime)
			params = ", params"
		} else if m.Request != nil {
			f.linef("var params %s", f.spell(m.Request))
			f.linef("if err := %s.%s(%s%s(%s), raw); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", f.boundSchema(f.uses), identValidateExpressionRaw, f.proto(), identMustTypeExpression, expression(m.Request), runtime)
			f.linef("if err := %s.Unmarshal(raw, &params); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", json, runtime)
			params = ", params"
		} else {
			f.linef("if err := %s.%s(map[string]any{\"empty\": true}, raw); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", f.boundSchema(f.uses), identValidateExpressionRaw, runtime)
		}
		// := stays even when err is already declared: result is new, which
		// is what a short declaration needs.
		f.linef("result, err := %s.%s(ctx, %s%s)", handler, p.operations[m.Name], remote, params)
		f.line("if err != nil { return nil, err }")
		if f.family.IsLive(m.Result) {
			if params == "" || !f.family.IsLive(m.Request) {
				f.linef("scope, ok := %s.ScopeOf(peer)", f.live())
				f.linef("if !ok { return nil, &%s.PublicError{Code: %s.ErrorScopeClosed, Message: \"the connection carries no live scope\"} }", runtime, f.live())
			}
			f.linef("return %s(scope, result)", f.liveCall(m.Result, true))
			return
		}
		f.linef("if err = %s.%s(%s%s(%s), result); err != nil { return nil, err }", f.boundSchema(f.uses), identValidateValue, f.proto(), identMustTypeExpression, expression(m.Result))
		f.line("return result, nil")
	})
}

// labels renders, into install, the family label of every method and event
// of the family — both sides, whichever side this peer is — merged onto the
// options beside whatever the caller labelled, as the handlers above it are
// merged. An observer then says which family a name belongs to without
// parsing it.
func (f *file) labels() {
	f.linef("%s := map[string]string{}", identFamilies)
	f.linef("for name, existing := range options.Families { %s[name] = existing }", identFamilies)
	for _, name := range operations(f.family) {
		f.linef("%s[%q] = %q", identFamilies, name, f.family.Name)
	}
	f.linef("options.Families = %s", identFamilies)
}

// operations is every method and event name a family declares, each side's
// methods before each side's events, in the order the sides hold them.
func operations(fam *render.Family) []string {
	names := make([]string, 0, len(fam.Server.Methods)+len(fam.Client.Methods)+len(fam.Server.Events)+len(fam.Client.Events))
	for _, m := range fam.Server.Methods {
		names = append(names, m.Name)
	}
	for _, m := range fam.Client.Methods {
		names = append(names, m.Name)
	}
	for _, e := range fam.Server.Events {
		names = append(names, e.Name)
	}
	for _, e := range fam.Client.Events {
		names = append(names, e.Name)
	}
	return names
}

// caller renders the typed call of one method a peer sends: validate the
// params, call, validate and decode the result.
func (f *file) caller(m render.Method, receiver string) {
	p, json := f.plan, f.std("json")
	result := f.spell(m.Result)
	if m.Description != "" {
		f.linef("// %s: %s", p.operations[m.Name], m.Description)
	}
	f.w.Block(fmt.Sprintf("func (c *%s) %s(ctx %s.Context%s) (%s, error) {", receiver, p.operations[m.Name], f.std("context"), f.request(m), result), "}", func() {
		f.linef("var result %s", result)
		live := (m.Request != nil && f.family.IsLive(m.Request)) || f.family.IsLive(m.Result)
		argument := argument(m)
		if live {
			f.linef("scope, ok := %s.ScopeOf(c.%s)", f.live(), identPeer)
			f.linef("if !ok { return result, &%s.PublicError{Code: %s.ErrorScopeClosed, Message: \"the connection carries no live scope\"} }", f.runtime(), f.live())
		}
		switch {
		case m.Request != nil && f.family.IsLive(m.Request):
			f.linef("sent, err := %s(scope, params)", f.liveCall(m.Request, true))
			f.line("if err != nil { return result, err }")
			argument = "sent"
		case m.Request != nil:
			f.linef("if err := %s.%s(%s%s(%s), params); err != nil { return result, err }", f.boundSchema(f.uses), identValidateValue, f.proto(), identMustTypeExpression, expression(m.Request))
		}
		f.linef("var raw %s.RawMessage", json)
		f.linef("if err := c.%s.Call(ctx, %q, %s, &raw); err != nil { return result, err }", identPeer, m.Name, argument)
		if f.family.IsLive(m.Result) {
			f.linef("return %s(scope, raw)", f.liveCall(m.Result, false))
			return
		}
		f.linef("if err := %s.%s(%s%s(%s), raw); err != nil { return result, err }", f.boundSchema(f.uses), identValidateExpressionRaw, f.proto(), identMustTypeExpression, expression(m.Result))
		f.linef("if err := %s.Unmarshal(raw, &result); err != nil { return result, err }", json)
		f.line("return result, nil")
	})
}

// events renders, on a receiver, the emitters of the events it sends and
// the handlers of the events it receives.
func (f *file) events(receiver string, received, sent []render.Event) {
	p := f.plan
	for _, e := range sent {
		ctx := f.std("context")
		if f.family.IsLive(e.Type) {
			f.w.Block(fmt.Sprintf("func (c *%s) %s%s(ctx %s.Context, data %s) error {", receiver, identEmit, p.operations[e.Name], ctx, f.spell(e.Type)), "}", func() {
				f.linef("scope, ok := %s.ScopeOf(c.%s)", f.live(), identPeer)
				f.linef("if !ok { return &%s.PublicError{Code: %s.ErrorScopeClosed, Message: \"the connection carries no live scope\"} }", f.runtime(), f.live())
				f.linef("sent, err := %s(scope, data)", f.liveCall(e.Type, true))
				f.line("if err != nil { return err }")
				f.linef("return c.%s.Emit(ctx, %q, sent)", identPeer, e.Name)
			})
			continue
		}
		f.linef("func (c *%s) %s%s(ctx %s.Context, data %s) error { if err := %s.%s(%s%s(%s), data); err != nil { return err }; return c.%s.Emit(ctx, %q, data) }", receiver, identEmit, p.operations[e.Name], ctx, f.spell(e.Type), f.boundSchema(f.uses), identValidateValue, f.proto(), identMustTypeExpression, expression(e.Type), identPeer, e.Name)
	}
	for _, e := range received {
		ctx := f.std("context")
		data := f.spell(e.Type)
		f.w.Block(fmt.Sprintf("func (c *%s) %s%s(handler func(%s.Context, %s)) error {", receiver, identOn, p.operations[e.Name], ctx, data), "}", func() {
			f.eventHandler(e, func() {
				f.line("handler(ctx, data)")
			})
		})
	}
}

// eventHandler installs the typed validation path shared by internal and
// user callbacks, retaining the payload's declaring family.
func (f *file) eventHandler(e render.Event, callback func()) {
	f.w.Block(fmt.Sprintf("return c.%s.HandleEvent(%q, func(ctx %s.Context, peer *%s.Peer, raw %s.RawMessage) {", identPeer, e.Name, f.std("context"), f.runtime(), f.std("json")), "})", func() {
		if f.family.IsLive(e.Type) {
			f.linef("scope, ok := %s.ScopeOf(peer)", f.live())
			f.line("if !ok { _ = peer.Close(); return }")
			f.linef("data, err := %s(scope, raw)", f.liveCall(e.Type, false))
			f.line("if err != nil { _ = peer.Close(); return }")
			callback()
			return
		}
		f.linef("if err := %s.%s(%s%s(%s), raw); err != nil { _ = peer.Close(); return }", f.boundSchema(f.uses), identValidateExpressionRaw, f.proto(), identMustTypeExpression, expression(e.Type))
		f.linef("var data %s", f.spell(e.Type))
		f.linef("if err := %s.Unmarshal(raw, &data); err != nil { _ = peer.Close(); return }", f.std("json"))
		callback()
	})
}

// liveScope installs the live layer on the options a peer is made with, in
// Prepare, exactly as a tunnel is made: a peer already reading would answer
// the other side's first live.invoke method_not_found before the handler is
// there. A family with no live tier installs nothing, which is what keeps an
// ordinary data or RPC checkout free of the live package.
func (f *file) liveScope(inner func()) {
	if !f.family.Live {
		if inner != nil {
			inner()
		}
		return
	}
	f.line("prepare := options.Prepare")
	f.w.Block(fmt.Sprintf("options.Prepare = func(peer *%s.Peer) error {", f.runtime()), "}", func() {
		f.liveOver()
		if inner != nil {
			inner()
		}
		f.line("if prepare != nil { return prepare(peer) }")
		f.line("return nil")
	})
}

// liveOver is the one statement that makes the scope, written inside a
// Prepare the caller already had.
func (f *file) liveOver() {
	if !f.family.Live {
		return
	}
	f.linef("if _, err := %s.Over(peer, %s.Options{}); err != nil { return err }", f.live(), f.live())
}
