package golang

import (
	"fmt"
	"strings"

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
	// with, beside whatever the caller registered; NewHandler and Serve
	// share it.
	f.linef("// %s registers the family's methods on the options a peer is made with.", identInstall)
	f.w.Block(fmt.Sprintf("func %s%s(handler %s%s, options *%s.Options) error {", identInstall, decl, identHandler, args, runtime), "}", func() {
		f.linef("if handler == nil { return %s.Errorf(\"handler is required\") }", f.std("fmt"))
		f.linef("handlers := map[string]%s.Handler{}", runtime)
		f.line("for name, existing := range options.Handlers { handlers[name] = existing }")
		for _, m := range fam.Server.Methods {
			f.registration(m, "handler", "&"+identRemote+args+"{"+identPeer+": peer}")
		}
		f.line("options.Handlers = handlers")
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
	f.linef("type %s%s struct{ %s *%s.Peer }", identClient, decl, identPeer, runtime)
	f.w.Block(fmt.Sprintf("type %s%s interface {", identHandler, decl), "}", func() {
		for _, m := range fam.Client.Methods {
			if m.Description != "" {
				f.linef("// %s: %s", p.operations[m.Name], m.Description)
			}
			f.linef("%s(ctx %s.Context, client *%s%s%s) (%s, error)", p.operations[m.Name], ctx, identClient, args, f.request(m), f.spell(m.Result))
		}
	})
	// The protocol's caller side as an interface, which the session type
	// implements; a consumer may stand another implementation in its place.
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
	if s := fam.Session; s != nil {
		// The session tier's governance, as code both halves read.
		f.linef("// %s reports whether a method needs control to send: an observer's is refused.", identDecides)
		f.linef("func %s(method string) bool { %s }", identDecides, membership(s.Decides))
		f.linef("// %s reports whether a method the server sends raises a request the holder of control must answer.", identAsks)
		f.linef("func %s(method string) bool { %s }", identAsks, membership(s.Asks))
		if s.Conversation != nil {
			f.linef("// %s is where the agent's own conversation id arrives: the event, and the path to the id in its data.", identConversation)
			f.linef("var %s = struct{ Event, Path string }{%q, %q}", identConversation, s.Conversation.Event, s.Conversation.Path)
		}
	}
	// install registers the reverse-call handlers on the options a peer is
	// made with; Dial and Attach share it.
	f.linef("// %s registers the reverse-call handlers on the options a peer is made with.", identInstall)
	f.w.Block(fmt.Sprintf("func %s%s(handler %s%s, options *%s.Options) error {", identInstall, decl, identHandler, args, runtime), "}", func() {
		if len(fam.Client.Methods) > 0 {
			f.linef("if handler == nil { return %s.Errorf(\"reverse-call handler is required\") }", f.std("fmt"))
		}
		f.linef("handlers := map[string]%s.Handler{}", runtime)
		f.line("for name, existing := range options.Handlers { handlers[name] = existing }")
		for _, m := range fam.Client.Methods {
			f.registration(m, "handler", "&"+identClient+args+"{"+identPeer+": peer}")
		}
		f.line("options.Handlers = handlers")
		f.line("return nil")
	})
	f.linef("// %s connects to a WebSocket endpoint after installing reverse-call handlers. No request is retried.", identDial)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, url string, options %s.DialOptions, handler %s%s) (*%s%s, error) {", identDial, open, ctx, runtime, identHandler, args, identClient, args), "}", func() {
		f.linef("if err := %s%s(handler, &options.Options); err != nil { return nil, err }", identInstall, args)
		f.linef("peer, response, err := %s.Dial(ctx, url, options)", runtime)
		f.line("if err != nil { if response != nil && response.Body != nil { _ = response.Body.Close() }; return nil, err }")
		f.linef("return &%s%s{%s: peer}, nil", identClient, args, identPeer)
	})
	f.linef("// %s speaks the family over a connection of the seam — a tunnel channel, a pipe, a dialled socket — as the client side of it, after installing reverse-call handlers.", identAttach)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, conn %s.Conn, options %s.Options, handler %s%s) (*%s%s, error) {", identAttach, open, ctx, f.seam(), runtime, identHandler, args, identClient, args), "}", func() {
		f.linef("if err := %s%s(handler, &options); err != nil { return nil, err }", identInstall, args)
		f.linef("peer, err := %s.NewPeer(ctx, conn, %s.ClientRole, options)", runtime, runtime)
		f.line("if err != nil { return nil, err }")
		f.linef("return &%s%s{%s: peer}, nil", identClient, args, identPeer)
	})
	f.linef("// %s resolves a handle to the channel it names on a tunnel and speaks the family over it.", identOpen)
	f.w.Block(fmt.Sprintf("func %s%s(ctx %s.Context, t *%s.Tunnel, handle %sHandle, options %s.Options, handler %s%s) (*%s%s, error) {", identOpen, open, ctx, f.tunnel(), f.proto(), runtime, identHandler, args, identClient, args), "}", func() {
		f.line("channel, ok := t.Channel(handle.Channel)")
		f.linef("if !ok { return nil, %s.Errorf(\"no channel %%d on the connection\", handle.Channel) }", f.std("fmt"))
		f.linef("return %s%s(ctx, channel, options, handler)", identAttach, args)
	})
	f.linef("func (c *%s%s) %s() error { return c.%s.Close() }", identClient, args, identClose, identPeer)
	for _, m := range fam.Server.Methods {
		f.caller(m, identClient+args)
	}
	f.events(identClient+args, fam.Server.Events, fam.Client.Events)
}

// membership renders the body of a function reporting whether its string
// argument is one of the names.
func membership(names []string) string {
	if len(names) == 0 {
		return "return false"
	}
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quote(name)
	}
	return "switch method { case " + strings.Join(quoted, ", ") + ": return true }; return false"
}

// registration renders the runtime handler for one method a peer receives:
// validate and decode the params, dispatch, validate the result.
func (f *file) registration(m render.Method, handler, remote string) {
	p, runtime, json := f.plan, f.runtime(), f.std("json")
	f.linef("if _, exists := handlers[%q]; exists { return %s.Errorf(\"duplicate handler %%s\", %q) }", m.Name, f.std("fmt"), m.Name)
	f.w.Block(fmt.Sprintf("handlers[%q] = func(ctx %s.Context, peer *%s.Peer, raw %s.RawMessage) (any, error) {", m.Name, f.std("context"), runtime, json), "}", func() {
		params := ""
		if m.Request != nil {
			f.linef("var params %s", f.spell(m.Request))
			f.linef("if err := %s%s(%s%s(%s), raw); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", f.proto(), identValidateExpressionRaw, f.proto(), identTypeExpression, expression(m.Request), runtime)
			f.linef("if err := %s.Unmarshal(raw, &params); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", json, runtime)
			params = ", params"
		} else {
			f.linef("if err := %s%s(map[string]any{\"empty\": true}, raw); err != nil { return nil, &%s.PublicError{Code: \"invalid_params\", Message: err.Error()} }", f.proto(), identValidateExpressionRaw, runtime)
		}
		f.linef("result, err := %s.%s(ctx, %s%s)", handler, p.operations[m.Name], remote, params)
		f.line("if err != nil { return nil, err }")
		f.linef("if err = %s%s(%s%s(%s), result); err != nil { return nil, err }", f.proto(), identValidateValue, f.proto(), identTypeExpression, expression(m.Result))
		f.line("return result, nil")
	})
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
		if m.Request != nil {
			f.linef("if err := %s%s(%s%s(%s), params); err != nil { return result, err }", f.proto(), identValidateValue, f.proto(), identTypeExpression, expression(m.Request))
		}
		f.linef("var raw %s.RawMessage", json)
		f.linef("if err := c.%s.Call(ctx, %q, %s, &raw); err != nil { return result, err }", identPeer, m.Name, argument(m))
		f.linef("if err := %s%s(%s%s(%s), raw); err != nil { return result, err }", f.proto(), identValidateExpressionRaw, f.proto(), identTypeExpression, expression(m.Result))
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
		f.linef("func (c *%s) %s%s(ctx %s.Context, data %s) error { if err := %s%s(%s%s(%s), data); err != nil { return err }; return c.%s.Emit(ctx, %q, data) }", receiver, identEmit, p.operations[e.Name], ctx, f.spell(e.Type), f.proto(), identValidateValue, f.proto(), identTypeExpression, expression(e.Type), identPeer, e.Name)
	}
	for _, e := range received {
		runtime, json, ctx := f.runtime(), f.std("json"), f.std("context")
		data := f.spell(e.Type)
		f.w.Block(fmt.Sprintf("func (c *%s) %s%s(handler func(%s.Context, %s)) error {", receiver, identOn, p.operations[e.Name], ctx, data), "}", func() {
			f.w.Block(fmt.Sprintf("return c.%s.HandleEvent(%q, func(ctx %s.Context, peer *%s.Peer, raw %s.RawMessage) {", identPeer, e.Name, ctx, runtime, json), "})", func() {
				f.linef("if err := %s%s(%s%s(%s), raw); err != nil { _ = peer.Close(); return }", f.proto(), identValidateExpressionRaw, f.proto(), identTypeExpression, expression(e.Type))
				f.linef("var data %s", data)
				f.linef("if err := %s.Unmarshal(raw, &data); err != nil { _ = peer.Close(); return }", json)
				f.line("handler(ctx, data)")
			})
		})
	}
}
