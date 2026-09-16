package generate

import (
	"fmt"
	"strings"
)

func generateBinding(api API, options Options) string {
	var b strings.Builder
	b.WriteString("// Remote provides typed calls back to the connected client.\ntype Remote struct{Peer *wsruntime.Peer}\n")
	b.WriteString("type Handler interface {\n")
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			fmt.Fprintf(&b, "%s(ctx context.Context,remote *Remote%s)(%s,error)\n", m.GoName, paramSignature(m), goType(m.Result, "protocol."))
		}
	}
	b.WriteString("}\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			writeCaller(&b, m, "Remote")
		}
	}
	b.WriteString("// NewHandler requires explicit authentication and origin policy through options.\nfunc NewHandler(handler Handler,options wsruntime.ServerOptions)(http.Handler,error){if handler==nil{return nil,fmt.Errorf(\"handler is required\")};handlers:=map[string]wsruntime.Handler{};for name,existing:=range options.Options.Handlers{handlers[name]=existing};\n")
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			writeRegistration(&b, m, "handler", "&Remote{Peer:peer}")
		}
	}
	b.WriteString("options.Options.Handlers=handlers;return wsruntime.NewHandler(options)}\n")
	writeEvents(&b, api, "Remote", "server_to_client")
	return goFile(api, "binding", b.String(), options)
}

func generateClient(api API, options Options) string {
	var b strings.Builder
	b.WriteString("type Client struct{Peer *wsruntime.Peer}\n")
	b.WriteString("type Handler interface{\n")
	reverse := false
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			reverse = true
			fmt.Fprintf(&b, "%s(ctx context.Context,client *Client%s)(%s,error)\n", m.GoName, paramSignature(m), goType(m.Result, "protocol."))
		}
	}
	b.WriteString("}\n")
	b.WriteString("// Dial connects after installing reverse-call handlers. No request is retried.\nfunc Dial(ctx context.Context,url string,options wsruntime.DialOptions,handler Handler)(*Client,error){\n")
	if reverse {
		b.WriteString("if handler==nil{return nil,fmt.Errorf(\"reverse-call handler is required\")};\n")
	}
	b.WriteString("handlers:=map[string]wsruntime.Handler{};for name,existing:=range options.Options.Handlers{handlers[name]=existing};\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			writeRegistration(&b, m, "handler", "&Client{Peer:peer}")
		}
	}
	b.WriteString("options.Options.Handlers=handlers;peer,response,err:=wsruntime.Dial(ctx,url,options);if err!=nil{if response!=nil&&response.Body!=nil{_ = response.Body.Close()};return nil,err};return &Client{Peer:peer},nil}\n")
	b.WriteString("func(c *Client)Close()error{return c.Peer.Close()}\n")
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			writeCaller(&b, m, "Client")
		}
	}
	writeEvents(&b, api, "Client", "client_to_server")
	return goFile(api, "client", b.String(), options)
}

func writeRegistration(b *strings.Builder, m Method, handler, remote string) {
	fmt.Fprintf(b, "if _,exists:=handlers[%q];exists{return nil,fmt.Errorf(\"duplicate handler %%s\",%q)}\n", m.Name, m.Name)
	fmt.Fprintf(b, "handlers[%q]=func(ctx context.Context,peer *wsruntime.Peer,raw json.RawMessage)(any,error){\n", m.Name)
	params := ""
	if m.Request != "" {
		fmt.Fprintf(b, "var params protocol.%s;if err:=protocol.ValidateRaw(%q,raw);err!=nil{return nil,&wsruntime.PublicError{Code:\"invalid_params\",Message:err.Error()}};if err:=json.Unmarshal(raw,&params);err!=nil{return nil,&wsruntime.PublicError{Code:\"invalid_params\",Message:err.Error()}};\n", m.Request, m.Request)
		params = ",params"
	} else {
		b.WriteString("if err:=protocol.ValidateExpressionRaw(map[string]any{\"empty\":true},raw);err!=nil{return nil,&wsruntime.PublicError{Code:\"invalid_params\",Message:err.Error()}};")
	}
	fmt.Fprintf(b, "result,err:=%s.%s(ctx,%s%s);if err!=nil{return nil,err};if err=protocol.ValidateValue(protocol.TypeExpression(%q),result);err!=nil{return nil,err};return result,nil}\n", handler, m.GoName, remote, params, expression(m.Result))
}
func writeCaller(b *strings.Builder, m Method, receiver string) {
	result := goType(m.Result, "protocol.")
	fmt.Fprintf(b, "func(c *%s)%s(ctx context.Context%s)(%s,error){var result %s;", receiver, m.GoName, paramSignature(m), result, result)
	if m.Request != "" {
		fmt.Fprintf(b, "if err:=protocol.ValidateValue(%q,params);err!=nil{return result,err};", m.Request)
	}
	fmt.Fprintf(b, "var raw json.RawMessage;if err:=c.Peer.Call(ctx,%q,%s,&raw);err!=nil{return result,err};if err:=protocol.ValidateExpressionRaw(protocol.TypeExpression(%q),raw);err!=nil{return result,err};if err:=json.Unmarshal(raw,&result);err!=nil{return result,err};return result,nil}\n", m.Name, paramValue(m), expression(m.Result))
}
func writeEvents(b *strings.Builder, api API, receiver, outbound string) {
	for _, event := range api.Events {
		if event.Direction == outbound {
			fmt.Fprintf(b, "func(c *%s)Emit%s(ctx context.Context,data %s)error{if err:=protocol.ValidateValue(protocol.TypeExpression(%q),data);err!=nil{return err};return c.Peer.Emit(ctx,%q,data)}\n", receiver, event.GoName, goType(event.Type, "protocol."), expression(event.Type), event.Name)
		} else {
			fmt.Fprintf(b, "func(c *%s)On%s(handler func(context.Context,%s))error{return c.Peer.HandleEvent(%q,func(ctx context.Context,peer *wsruntime.Peer,raw json.RawMessage){if err:=protocol.ValidateExpressionRaw(protocol.TypeExpression(%q),raw);err!=nil{_ = peer.Close();return};var data %s;if err:=json.Unmarshal(raw,&data);err!=nil{_ = peer.Close();return};handler(ctx,data)})}\n", receiver, event.GoName, goType(event.Type, "protocol."), event.Name, expression(event.Type), goType(event.Type, "protocol."))
		}
	}
}
