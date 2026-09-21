package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/examples"
)

// Test helpers live in their own package: consumer declarations cannot collide
// with their names, and importing a generated model does not import its tests.
func emitServerTest(f *file) { f.emitTransparency("Server", "Client", "Binding") }
func emitClientTest(f *file) { f.emitTransparency("Client", "Server", "Client") }

func (f *file) emitTransparency(side, opposite, role string) {
	f.operationAdapters()
	decl, args := f.entry(f.family.Uses), apply(f.family.Uses)
	proto, rt, seam := f.proto(), f.runtime(), f.seam()
	for _, name := range []string{"context", "fmt", "json", "reflect", "sync", "errors", "bytes"} {
		f.std(name)
	}
	layout := f.config.layout(f.family.Name)
	dir := layout.Binding
	if role == "Client" {
		dir = layout.Client
	}
	f.use("adapter", f.config.Module+"/"+expand(dir, f.family.Name))
	f.linef("type Presentation func(context.Context, %s.Wire) (%s.Wire, func(), error)", seam, seam)
	f.linef("type Options struct { Context %s.AdapterContext; RemoteContext %s.AdapterContext; Presentation Presentation; Inputs map[string]any; Equal func(string, any, any) error }", rt, rt)
	f.line(goTransparencySupport)
	f.linef("// Pair presents one fresh model over a frame pipe by default. The returned factory is bound once.")
	f.w.Block(fmt.Sprintf("func Pair%s(ctx context.Context, model %s%sModel%s, options Options%s) (%s%sModel%s, func(), error) {", decl, proto, side, args, f.slotParameters(), proto, side, args), "}", func() {
		f.linef("wire, err := adapter.ToWire%s(model, options.Context%s)", args, f.slotArguments())
		f.line("if err != nil { return nil, nil, err }")
		f.line("present := options.Presentation; if present == nil { present = Pipe }")
		f.line("view, detach, err := present(ctx, wire)")
		f.line("if err != nil { if detach != nil { detach() }; _ = wire.Close(1000, \"\"); return nil, nil, err }")
		f.line("close := once(func(){ if detach != nil { detach() }; _ = wire.Close(1000, \"\") })")
		f.linef("complete, unbind, err := adapter.PrepareFromWire%s(view, options.RemoteContext%s)", args, f.slotArguments())
		f.line("if err != nil { close(); return nil,nil,err }")
		f.line("stop := once(func(){ unbind(); close() })")
		f.line("paired,err := complete(ctx); if err != nil { stop(); return nil,nil,err }; return paired,stop,nil")
	})
	emitExampleTest(f)
	f.line("// Smoke calls every method and compares two freshly constructed equivalent models.")
	f.line("// Inputs supplies native witnesses by wire method name; Equal supplies live observational equivalence.")
	f.linef("type observingMethods%s struct { inner %s%sMethods%s; check func(string,any) error }", declare(f.family.Uses), proto, side, args)
	methods, _ := f.sideOperations(side)
	for _, m := range methods {
		name := f.plan.operations[m.Name]
		argument := ""
		input := "nil"
		if m.Request != nil {
			argument = ", params"
			input = "params"
		}
		f.linef("func (o observingMethods%s) %s(ctx context.Context%s) (%s,error) { if err:=o.check(%q,%s);err!=nil{var zero %s;return zero,err};return o.inner.%s(ctx%s) }", args, name, f.request(m), f.spell(m.Result), m.Name, input, f.spell(m.Result), name, argument)
	}
	f.w.Block(fmt.Sprintf("func Smoke%s(ctx context.Context, model %s%sModel%s, opposite %s%s%s, options Options%s) error {", decl, proto, side, args, proto, opposite, args, f.slotParameters()), "}", func() {
		f.line("if model==nil{return fmt.Errorf(\"model factory is required\")}")
		f.line("var mutex sync.Mutex; inputs:=map[string]any{}; seen:=map[string]uint64{}; var inputError error; _ = inputError")
		f.line("check:=func(method string,actual any)error{mutex.Lock();seen[method]++;expected,tracked:=inputs[method];delete(inputs,method);mutex.Unlock();if !tracked{return nil};err:=compare(method+\".request\",expected,actual,nil,nil,options.Equal);if err!=nil{mutex.Lock();inputError=err;mutex.Unlock()};return err}")
		validate := ""
		if len(methods) > 0 {
			validate = "if value.Methods==nil{return value,fmt.Errorf(\"model methods are required\")};"
		}
		f.linef("observed:=func(remote %s%s%s)(%s%s%s,error){value,err:=model(remote);if err==nil{%svalue.Methods=observingMethods%s{inner:value.Methods,check:check}};return value,err}", proto, opposite, args, proto, side, args, validate, args)
		f.linef("paired,stop,err := Pair%s(ctx,observed,options%s); if err != nil { return err }; defer stop()", args, f.slotArguments())
		f.line("remote,err := paired(opposite); if err != nil { return err }")
		f.line("direct,err := model(opposite); if err != nil { return err }; _ = remote; _ = direct")
		for _, m := range methods {
			f.line("{")
			name := f.plan.operations[m.Name]
			argument := ""
			if m.Request != nil {
				reason := ""
				raw, info := examples.New(f.family).Request(m)
				if info.ExampleUnavailable != nil {
					reason = info.ExampleUnavailable.Reason
				}
				f.linef("var input %s", f.spell(m.Request))
				f.linef("if supplied,ok := options.Inputs[%q]; ok { var valid bool; input,valid=supplied.(%s); if !valid { return fmt.Errorf(%q) } } else {", m.Name, f.spell(m.Request), "input "+m.Name+" has the wrong native type")
				f.linef("if %s { return fmt.Errorf(%q) }", f.expressionLive(m.Request), "input "+m.Name+" needs a caller-supplied native value")
				if reason != "" {
					f.linef("return fmt.Errorf(%q)", "input "+m.Name+" unavailable: "+reason)
				} else {
					f.linef("if err := json.Unmarshal([]byte(%q), &input); err != nil { return fmt.Errorf(%q,err) }", string(raw), "input "+m.Name+" unavailable for this instantiation: %w")
				}
				f.line("}")
				argument = ", input"
			}
			input := "nil"
			if m.Request != nil {
				input = "input"
			}
			live := []string{f.expressionLive(m.Result)}
			if m.Request != nil {
				live = append(live, f.expressionLive(m.Request))
			}
			seenLive := map[string]bool{}
			for _, boundary := range live {
				if boundary == "false" || seenLive[boundary] {
					continue
				}
				seenLive[boundary] = true
				condition := "options.Equal==nil"
				if boundary != "true" {
					condition += " && (" + boundary + ")"
				}
				f.linef("if %s { return fmt.Errorf(%q) }", condition, m.Name+": requires an Equal observer for live values")
			}
			f.linef("mutex.Lock();inputs[%q]=%s;before:=seen[%q];mutex.Unlock()", m.Name, input, m.Name)
			f.linef("actual,actualErr := remote.Methods.%s(ctx%s)", name, argument)
			f.linef("mutex.Lock();observedError:=inputError;called:=seen[%q]>before;mutex.Unlock();if observedError!=nil{return observedError};if !called{return fmt.Errorf(%q,actualErr)}", m.Name, m.Name+": model was not reached: %v")
			f.linef("expected,expectedErr := direct.Methods.%s(ctx%s)", name, argument)
			f.linef("if err := compare(%q, expected,actual,expectedErr,actualErr,options.Equal); err != nil { return err }", m.Name)
			f.line("}")
		}
		f.line("return nil")
	})
}

func emitExampleTest(f *file) {
	f.runtime()
	f.std("context")
	f.std("json")
	f.std("fmt")
	builder := examples.New(f.family)
	f.line("type candidate struct { raw string; reason string }")
	f.line("var examples = map[string]candidate{")
	for _, t := range f.family.Types {
		if t.Carried {
			continue
		}
		raw, info := builder.Type(t)
		reason := ""
		if info.ExampleUnavailable != nil {
			reason = info.ExampleUnavailable.Reason
		}
		if f.family.Type(t.Name).IsLive {
			reason = "a live reference illustration needs a caller-supplied native value"
		}
		f.linef("%q: {raw:%q, reason:%q},", t.Name, string(raw), reason)
	}
	f.line("}")
	f.line("// Example validates the document's candidate against T; incompatible generic witnesses fail explicitly.")
	f.line("func Example[T any](name string) (T,error) { var zero T; c,ok:=examples[name]; if !ok { return zero,fmt.Errorf(\"unknown example %s\",name) }; if c.reason!=\"\" { return zero,fmt.Errorf(\"example %s unavailable: %s\",name,c.reason) }; return runtime.JSONAdapter[T]().Import(context.Background(),json.RawMessage(c.raw)) }")
}

var goTransparencySupport = strings.TrimSpace(`
func once(f func()) func() { var once sync.Once; return func(){ once.Do(f) } }

// Local keeps the bounded asynchronous wire created by ToWire.
func Local(_ context.Context, wire duplex.Wire) (duplex.Wire,func(),error) { return wire,func(){},nil }

// Mounted selects a nonempty origin from a mount without allocating a carrier.
func Mounted(_ context.Context, wire duplex.Wire) (duplex.Wire,func(),error) {
 root:=duplex.Mount(map[string]duplex.Wire{"family":wire})
 return duplex.At(root,[]string{"family"}),once(func(){_ = root.Close(1000,"")}),nil
}

// Forwarded introduces one local forwarding hop.
func Forwarded(_ context.Context, wire duplex.Wire) (duplex.Wire,func(),error) {
 left,right,err:=runtime.NewWirePair(runtime.Options{});if err!=nil{return nil,nil,err}
 detach,err:=runtime.ForwardWire(right,wire);if err!=nil{_ = left.Close(1000,"");return nil,nil,err}
 return left,once(func(){detach();_ = left.Close(1000,"")}),nil
}

// Pipe carries real serialized frames between two prepared peers.
func Pipe(ctx context.Context, wire duplex.Wire) (duplex.Wire,func(),error) {
 left,right:=duplex.Pipe(1<<20)
 var detach func()
 server,err:=runtime.NewPeer(ctx,right,runtime.ServerRole,runtime.Options{Prepare:func(peer *runtime.Peer)error{var err error;detach,err=runtime.ForwardWire(peer.Wire(),wire);return err}})
 if err!=nil{_ = left.Close(context.Background(),1000,"");_ = right.Close(context.Background(),1000,"");return nil,nil,err}
 client,err:=runtime.NewPeer(ctx,left,runtime.ClientRole,runtime.Options{})
 if err!=nil{if detach!=nil{detach()};_ = server.Close();_ = left.Close(context.Background(),1000,"");return nil,nil,err}
 return client.Wire(),once(func(){detach();_ = client.Close();_ = server.Close()}),nil
}

func compare(method string, expected,actual any, expectedErr,actualErr error, equal func(string,any,any)error) error {
 if expectedErr!=nil || actualErr!=nil {
  if expectedErr==nil || actualErr==nil { return fmt.Errorf("%s: direct error %v, round-trip error %v",method,expectedErr,actualErr) }
  code:=func(err error)string{var public *runtime.PublicError;if errors.As(err,&public){return public.Code};return "internal"}
  if code(expectedErr)!=code(actualErr){return fmt.Errorf("%s: direct error %v, round-trip error %v",method,expectedErr,actualErr)}
  var expectedPublic,actualPublic *runtime.PublicError
  if errors.As(expectedErr,&expectedPublic)&&errors.As(actualErr,&actualPublic){return compare(method+".error",expectedPublic,actualPublic,nil,nil,nil)}
  return nil
 }
 if equal!=nil{return equal(method,expected,actual)}
 a,err:=runtime.MarshalJSON(expected);if err!=nil{return fmt.Errorf("%s: result needs an Equal observer: %w",method,err)}
 b,err:=runtime.MarshalJSON(actual);if err!=nil{return fmt.Errorf("%s: round-trip result needs an Equal observer: %w",method,err)}
 var x,y any
 da,db:=json.NewDecoder(bytes.NewReader(a)),json.NewDecoder(bytes.NewReader(b));da.UseNumber();db.UseNumber()
 if err=da.Decode(&x);err!=nil{return err};if err=db.Decode(&y);err!=nil{return err}
 if !reflect.DeepEqual(x,y){return fmt.Errorf("%s: direct and round-trip results differ: %s / %s",method,a,b)}
 return nil
}
`)
