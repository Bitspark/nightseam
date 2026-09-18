package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/contract"
)

// generateBinding renders the server side: the Handler interface a server
// implements, the Remote it calls back through, and NewHandler. Those of a
// generic family are generic in the parameters its types use, E and H, and
// a server instantiates them with the family that fills the slots.
func generateBinding(api contract.API, g contract.Generics, p paths) string {
	decl, args := declare(g.Family), apply(g.Family)
	var b strings.Builder
	fmt.Fprintf(&b, "// Remote provides typed calls back to the connected client.\ntype Remote%s struct{Peer *runtime.Peer}\n", decl)
	fmt.Fprintf(&b, "type Handler%s interface {\n", decl)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			fmt.Fprintf(&b, "%s(ctx context.Context,remote *Remote%s%s)(%s,error)\n", m.GoName, args, paramSignature(g, m), goType(g, m.Result, "protocol."))
		}
	}
	b.WriteString("}\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			writeCaller(&b, g, m, "Remote"+args)
		}
	}
	fmt.Fprintf(&b, "// NewHandler requires explicit authentication and origin policy through options.\nfunc NewHandler%s(handler Handler%s,options runtime.ServerOptions)(http.Handler,error){if handler==nil{return nil,fmt.Errorf(\"handler is required\")};handlers:=map[string]runtime.Handler{};for name,existing:=range options.Options.Handlers{handlers[name]=existing};\n", decl, args)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			writeRegistration(&b, g, m, "handler", "&Remote"+args+"{Peer:peer}")
		}
	}
	b.WriteString("options.Options.Handlers=handlers;return runtime.NewHandler(options)}\n")
	writeEvents(&b, g, api, "Remote"+args, "server_to_client")
	return goFile(api, "binding", b.String(), p)
}

// generateClient renders the caller side: Dial, the typed calls, and the
// Handler interface for calls the server makes back. Those of a generic
// family are generic as the binding's are.
func generateClient(api contract.API, g contract.Generics, p paths) string {
	decl, args := declare(g.Family), apply(g.Family)
	var b strings.Builder
	fmt.Fprintf(&b, "type Client%s struct{Peer *runtime.Peer}\n", decl)
	fmt.Fprintf(&b, "type Handler%s interface{\n", decl)
	reverse := false
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			reverse = true
			fmt.Fprintf(&b, "%s(ctx context.Context,client *Client%s%s)(%s,error)\n", m.GoName, args, paramSignature(g, m), goType(g, m.Result, "protocol."))
		}
	}
	b.WriteString("}\n")
	// The RPC layer's caller side as an interface, which the session type
	// implements; a consumer may stand another implementation in its place.
	fmt.Fprintf(&b, "// Caller is the RPC layer's caller side: every operation a client sends. Client implements it.\ntype Caller%s interface{\n", decl)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			fmt.Fprintf(&b, "%s(ctx context.Context%s)(%s,error)\n", m.GoName, paramSignature(g, m), goType(g, m.Result, "protocol."))
		}
	}
	if g.Generic() {
		fmt.Fprintf(&b, "}\nfunc _%s(){var _ Caller%s=(*Client%s)(nil)}\n", decl, args, args)
	} else {
		b.WriteString("}\nvar _ Caller=(*Client)(nil)\n")
	}
	if s := api.Session; s != nil {
		// The sess layer's governance, as code both halves read.
		b.WriteString("// Decides reports whether a method needs control to send: an observer's is refused.\nfunc Decides(method string)bool{" + membership(s.Decides) + "}\n")
		b.WriteString("// Asks reports whether a server-to-client method raises a request the holder of control must answer.\nfunc Asks(method string)bool{" + membership(s.Asks) + "}\n")
		if s.Conversation != nil {
			fmt.Fprintf(&b, "// Conversation is where the agent's own conversation id arrives: the event, and the path to the id in its data.\nvar Conversation=struct{Event,Path string}{%q,%q}\n", s.Conversation.Event, s.Conversation.Path)
		}
	}
	fmt.Fprintf(&b, "// Dial connects after installing reverse-call handlers. No request is retried.\nfunc Dial%s(ctx context.Context,url string,options runtime.DialOptions,handler Handler%s)(*Client%s,error){\n", decl, args, args)
	if reverse {
		b.WriteString("if handler==nil{return nil,fmt.Errorf(\"reverse-call handler is required\")};\n")
	}
	b.WriteString("handlers:=map[string]runtime.Handler{};for name,existing:=range options.Options.Handlers{handlers[name]=existing};\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			writeRegistration(&b, g, m, "handler", "&Client"+args+"{Peer:peer}")
		}
	}
	fmt.Fprintf(&b, "options.Options.Handlers=handlers;peer,response,err:=runtime.Dial(ctx,url,options);if err!=nil{if response!=nil&&response.Body!=nil{_ = response.Body.Close()};return nil,err};return &Client%s{Peer:peer},nil}\n", args)
	fmt.Fprintf(&b, "func(c *Client%s)Close()error{return c.Peer.Close()}\n", args)
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			writeCaller(&b, g, m, "Client"+args)
		}
	}
	writeEvents(&b, g, api, "Client"+args, "client_to_server")
	return goFile(api, "client", b.String(), p)
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
	return "switch method{case " + strings.Join(quoted, ",") + ":return true};return false"
}

func writeRegistration(b *strings.Builder, g contract.Generics, m contract.Method, handler, remote string) {
	fmt.Fprintf(b, "if _,exists:=handlers[%q];exists{return nil,fmt.Errorf(\"duplicate handler %%s\",%q)}\n", m.Name, m.Name)
	fmt.Fprintf(b, "handlers[%q]=func(ctx context.Context,peer *runtime.Peer,raw json.RawMessage)(any,error){\n", m.Name)
	params := ""
	if m.Request != "" {
		fmt.Fprintf(b, "var params %s;if err:=protocol.ValidateRaw(%q,raw);err!=nil{return nil,&runtime.PublicError{Code:\"invalid_params\",Message:err.Error()}};if err:=json.Unmarshal(raw,&params);err!=nil{return nil,&runtime.PublicError{Code:\"invalid_params\",Message:err.Error()}};\n", goType(g, m.Request, "protocol."), m.Request)
		params = ",params"
	} else {
		b.WriteString("if err:=protocol.ValidateExpressionRaw(map[string]any{\"empty\":true},raw);err!=nil{return nil,&runtime.PublicError{Code:\"invalid_params\",Message:err.Error()}};")
	}
	fmt.Fprintf(b, "result,err:=%s.%s(ctx,%s%s);if err!=nil{return nil,err};if err=protocol.ValidateValue(protocol.TypeExpression(%q),result);err!=nil{return nil,err};return result,nil}\n", handler, m.GoName, remote, params, expression(m.Result))
}
func writeCaller(b *strings.Builder, g contract.Generics, m contract.Method, receiver string) {
	result := goType(g, m.Result, "protocol.")
	fmt.Fprintf(b, "func(c *%s)%s(ctx context.Context%s)(%s,error){var result %s;", receiver, m.GoName, paramSignature(g, m), result, result)
	if m.Request != "" {
		fmt.Fprintf(b, "if err:=protocol.ValidateValue(%q,params);err!=nil{return result,err};", m.Request)
	}
	fmt.Fprintf(b, "var raw json.RawMessage;if err:=c.Peer.Call(ctx,%q,%s,&raw);err!=nil{return result,err};if err:=protocol.ValidateExpressionRaw(protocol.TypeExpression(%q),raw);err!=nil{return result,err};if err:=json.Unmarshal(raw,&result);err!=nil{return result,err};return result,nil}\n", m.Name, paramValue(m), expression(m.Result))
}
func writeEvents(b *strings.Builder, g contract.Generics, api contract.API, receiver, outbound string) {
	for _, event := range api.Events {
		data := goType(g, event.Type, "protocol.")
		if event.Direction == outbound {
			fmt.Fprintf(b, "func(c *%s)Emit%s(ctx context.Context,data %s)error{if err:=protocol.ValidateValue(protocol.TypeExpression(%q),data);err!=nil{return err};return c.Peer.Emit(ctx,%q,data)}\n", receiver, event.GoName, data, expression(event.Type), event.Name)
		} else {
			fmt.Fprintf(b, "func(c *%s)On%s(handler func(context.Context,%s))error{return c.Peer.HandleEvent(%q,func(ctx context.Context,peer *runtime.Peer,raw json.RawMessage){if err:=protocol.ValidateExpressionRaw(protocol.TypeExpression(%q),raw);err!=nil{_ = peer.Close();return};var data %s;if err:=json.Unmarshal(raw,&data);err!=nil{_ = peer.Close();return};handler(ctx,data)})}\n", receiver, event.GoName, data, event.Name, expression(event.Type), data)
		}
	}
}
