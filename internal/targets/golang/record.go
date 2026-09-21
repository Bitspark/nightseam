package golang

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/render"
)

// Recording is a composition of the existing outgoing event adapter. Native
// values are converted once before entering the opaque runtime history.
func (f *file) emitRecordedEvents(side, opposite string) {
	decl, args, open := declare(f.family.Uses), apply(f.family.Uses), f.entry(f.family.Uses)
	seam, rt, proto, ctx := f.seam(), f.runtime(), f.proto(), f.std("context")
	_, events := f.sideOperations(opposite)
	marker := strings.TrimSuffix(strings.TrimPrefix(args, "["), "]")
	f.line("// RecordedEvent is the closed union of this side's outgoing event payloads.")
	f.linef("type RecordedEvent%s interface { recordedEvent(%s) }", decl, marker)
	for _, e := range events {
		name := "Recorded" + f.plan.operations[e.Name]
		f.linef("type %s%s struct { Data %s }", name, decl, f.spell(e.Type))
		f.linef("func (%s%s) recordedEvent(%s) {}", name, args, marker)
	}
	f.line("// Recorder records converted messages without retaining or rebinding their live values.")
	f.linef("type Recorder%s struct { *%s.RecordedWire; events %s%sEvents%s; identity %s.DeclarationIdentity; options %s.Options }", decl, seam, proto, opposite, args, rt, rt)
	f.w.Block(fmt.Sprintf("func(r *Recorder%s) Append(ctx %s.Context,event RecordedEvent%s) error {", args, ctx, args), "}", func() {
		if len(events) == 0 {
			f.linef("return %s.Errorf(\"this side declares no outgoing events\")", f.std("fmt"))
			return
		}
		f.w.Block("switch value:=event.(type) {", "}", func() {
			for _, e := range events {
				name := "Recorded" + f.plan.operations[e.Name]
				f.linef("case %s%s: return r.events.%s(ctx,value.Data)", name, args, f.plan.operations[e.Name])
				f.linef("case *%s%s: if value==nil { return %s.Errorf(\"nil recorded event\") }; return r.events.%s(ctx,value.Data)", name, args, f.std("fmt"), f.plan.operations[e.Name])
			}
			f.linef("default: return %s.Errorf(\"unknown or nil recorded event\")", f.std("fmt"))
		})
	})
	f.line("// Follow checks the subscriber's declaration before registering any replay.")
	f.w.Block(fmt.Sprintf("func(r *Recorder%s) Follow(ctx %s.Context,after uint64,target %s.Wire)(*%s.Follower,error){", args, ctx, seam, seam), "}", func() {
		f.linef("if err := %s.CheckIdentity(ctx,func(ctx %s.Context,method string,params,result any)error{return %s.CallWire(ctx,target,[]string{method},params,result,%s.WireCallOptions{RequestTimeout:r.options.RequestTimeout,Observer:r.options.Observer,Propagator:r.options.Propagator})},r.identity);err!=nil{return nil,err}", rt, ctx, rt, rt)
		f.line("return r.RecordedWire.Follow(ctx,after,target)")
	})
	f.line("// Record checks a prepared origin before exposing typed event append. Setup")
	f.line("// failure detaches this interpretation and leaves the borrowed target usable.")
	f.w.Block(fmt.Sprintf("func Record%s(ctx %s.Context,target %s.Wire,log %s.WireLog,options %s.RecordOptions,environment %s%s)(*Recorder%s,error){", open, ctx, seam, seam, seam, f.adapterContext(), f.slotParameters(), args), "}", func() {
		f.linef("environment,err:=normalizeContext%s(environment%s);if err!=nil{return nil,err}", args, f.slotArguments())
		f.linef("identity,err:=declarationIdentity%s(%s);if err!=nil{return nil,err}", args, identityArguments(f))
		f.linef("preparation,err:=%s.PrepareIdentity(target,identity,environment.Options);if err!=nil{return nil,err}", rt)
		f.line("if err:=preparation.Check(ctx);err!=nil{preparation.Close();return nil,err}")
		f.line("if err:=preparation.Ready();err!=nil{preparation.Close();return nil,err}")
		f.line("onClose:=options.OnClose;options.OnClose=func(err error){preparation.Close();if onClose!=nil{onClose(err)}}")
		f.linef("wire,err:=%s.Record(ctx,preparation.Wire(),log,options);if err!=nil{preparation.Close();return nil,err}", seam)
		f.linef("return &Recorder%s{RecordedWire:wire,events:access%s%s(wire,environment%s).Events,identity:identity,options:environment.Options},nil", args, opposite, args, f.slotArguments())
	})
}

func (p *plan) planRecordedEvents() {
	if !p.family.HasModel() {
		return
	}
	for _, side := range []render.Side{p.family.Server, p.family.Client} {
		ns := emit.NewNamespace("recorded event entry package")
		ns.Fix("generated recording declaration", "Record", "Recorder", "RecordedEvent")
		for _, e := range side.Events {
			p.declare(ns, "Recorded"+p.operations[e.Name], e.At, "recorded event variant")
		}
		for _, use := range p.family.Uses {
			at := diag.Location{}
			for _, parameter := range p.family.Parameters {
				if parameter.Name == use.Parameter {
					at = parameter.At.Sub("name")
				}
			}
			names := []string{parameterName(use)}
			if use.Type != "" {
				names = append(names, tagName(use.Parameter))
			}
			for _, name := range names {
				if what, taken := ns.Reserved(name); taken {
					p.Addf(at, "generated_name_collision", "Generated Go type parameter %s collides with the %s in an entry-point package.", name, what)
				}
			}
		}
	}
}
