package typescript

import (
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/examples"
)

func emitTransparency(f *file, side string) {
	f.scope = familyScope(f.family)
	f.operationAdapters = true
	f.adapterReceiver = "bindings."
	decl, args := f.declare(f.family.Uses), apply(f.family.Uses)
	opposite := "Client"
	if side == "Client" {
		opposite = "Server"
	}
	protocol := quote(f.config.pkg(f.family.Name) + "/types")
	if side == "Client" {
		protocol = "'./types.ts'"
	}
	f.linef("import { DuplexPeer, DuplexError, wirePair, forwardWire, createDispatcher, type AdapterContext, type WireModelContext, type ValueAdapter, type ValueContext } from %s;", quote(f.config.Runtime))
	f.line("import { mount, pipe as framePipe } from '@nightseam/duplex';
import type { Endpoint } from '@bitspark/bitwire';")
	f.line("import { toWire, prepareFromWire } from './index.ts';")
	f.linef("import type * as Protocol from %s;", protocol)
	f.linef("import type { AnyFamily, FamilyBinding } from %s;", protocol)
	f.imports(false)
	f.line(tsTransparencySupport)
	var bind, pass, fields []string
	for _, name := range parameters(f.family.Uses) {
		// Keep valid parameter names such as Inputs and Close separate from
		// helper locals, while retaining the adapter receiver's property names.
		field := bindingName(name)
		argument := "binding_" + field
		bind = append(bind, argument+": "+f.bindingType(name))
		pass = append(pass, argument)
		fields = append(fields, field+": "+argument)
	}
	binding, passing := "", ""
	if len(bind) > 0 {
		binding = ", " + strings.Join(bind, ", ")
		passing = ", " + strings.Join(pass, ", ")
	}
	f.w.Block(fmt.Sprintf("export async function pair%s(model: Protocol.%sModel%s, options: Options%s): Promise<{ model: Protocol.%sModel%s; close(): void }> {", decl, side, args, binding, side, args), "}", func() {
		f.linef("const wire = toWire%s(model, options.context ?? {}%s);", args, passing)
		f.line("let close = once(() => wire.close(1000, ''));")
		f.line("try {")
		f.line("const view = await (options.presentation ?? pipe)(wire);")
		f.line("const rootClose = close; close = once(() => { try { view.close(); } finally { rootClose(); } });")
		f.linef("const prepared = prepareFromWire%s(view.wire, options.remoteContext ?? {}%s);", args, passing)
		f.line("const viewClose = close; close = once(() => { try { prepared.close(); } finally { viewClose(); } });")
		f.line("return { model: await prepared.complete(options.callContext), close };")
		f.line("} catch (error) { close(); throw error; }")
	})
	emitExamples(f)
	f.line("/** Exercise every method on two fresh equivalent models; missing evidence is an error. */")
	f.w.Block(fmt.Sprintf("export async function smoke%s(model: Protocol.%sModel%s, opposite: Protocol.%s%s, options: Options%s): Promise<void> {", decl, side, args, opposite, args, binding), "}", func() {
		f.linef("const bindings = {%s};", strings.Join(fields, ", "))
		f.line("const inputs = new Map<string,unknown>(); const seen = new Map<string,number>(); let inputError: unknown;")
		f.linef("const observed: Protocol.%sModel%s = remote => { const value=model(remote);", side, args)
		methods, _ := f.wireSide(side)
		for _, m := range methods {
			f.linef("if(!hasMethod(value?.methods,%s))throw new Error(%s);", quote(f.plan.operations[m.Name]), quote("model method "+m.Name+" is required"))
		}
		f.line("return { ...value, methods: {")
		for _, m := range methods {
			name := f.plan.operations[m.Name]
			f.linef("async %s(input,context) { const key=%s; seen.set(key,(seen.get(key)??0)+1); const tracked=inputs.has(key),expected=inputs.get(key);inputs.delete(key); if(tracked)try { await compare(%s,{ok:true,value:expected},{ok:true,value:input},options.equal); } catch(error){inputError=error;throw error;} return value.methods.%s(input,context); },", name, quote(m.Name), quote(m.Name+".request"), name)
		}
		f.line("} }; };")
		f.linef("const prepared = await pair%s(observed, options%s);", args, passing)
		f.line("try {")
		f.line("const remote = prepared.model(opposite); const direct = model(opposite);")
		for _, m := range methods {
			raw, info := examples.New(f.family).Request(m)
			reason := ""
			if info.ExampleUnavailable != nil {
				reason = info.ExampleUnavailable.Reason
			}
			f.line("{")
			f.line("let input: unknown;")
			f.linef("if (options.inputs && Object.prototype.hasOwnProperty.call(options.inputs,%s)) { input=options.inputs[%s]; } else {", quote(m.Name), quote(m.Name))
			if m.Request != nil {
				f.linef("if (%s) throw new Error(%s);", f.boundaryLive(m.Request), quote("input "+m.Name+" needs a caller-supplied native value"))
			}
			if reason != "" {
				f.linef("throw new Error(%s);", quote("input "+m.Name+" unavailable: "+reason))
			} else {
				f.linef("input = JSON.parse(%s);", quote(string(raw)))
			}
			f.line("}")
			name := f.plan.operations[m.Name]
			var live []string
			for _, boundary := range []string{f.boundaryLive(m.Request), f.boundaryLive(m.Result)} {
				if boundary == "true" {
					live = []string{"true"}
					break
				}
				if boundary != "false" && (len(live) == 0 || live[0] != boundary) {
					live = append(live, boundary)
				}
			}
			if len(live) > 0 {
				condition := "!options.equal"
				if live[0] != "true" {
					condition += " && (" + strings.Join(live, " || ") + ")"
				}
				f.linef("if (%s) throw new Error(%s);", condition, quote(m.Name+": requires an equal observer for live values"))
			}
			// Reuse the model signature rather than the shadowable Parameters utility.
			callArgs := fmt.Sprintf("input as %s, options.callContext as %s", f.request(m), f.lifetimeType("WireModelContext", "ValueContext", m.Request, m.Result))
			f.linef("inputs.set(%s,input);", quote(m.Name))
			f.linef("const before=seen.get(%s)??0;", quote(m.Name))
			f.linef("const actual = await outcome(() => remote.methods.%s(%s));", name, callArgs)
			f.line("if(inputError!==undefined)throw inputError;")
			f.linef("if((seen.get(%s)??0)<=before)throw new Error(%s);", quote(m.Name), quote(m.Name+": model was not reached"))
			f.linef("const expected = await outcome(() => direct.methods.%s(%s));", name, callArgs)
			f.linef("await compare(%s, expected, actual, options.equal);", quote(m.Name))
			f.line("}")
		}
		f.line("} finally { prepared.close(); }")
	})
}

func emitExampleTest(f *file) {
	f.linef("import type {ValueAdapter} from %s;", quote(f.config.Runtime))
	emitExamples(f)
}

func emitExamples(f *file) {
	builder := examples.New(f.family)
	f.line("const examples: Readonly<Record<string, { raw?: string; reason?: string }>> = {")
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
		f.linef("%s: { raw: %s, reason: %s },", quote(t.Name), quote(string(raw)), quote(reason))
	}
	f.line("};")
	f.line("/** A fresh documented data witness, validated by the caller's exact value adapter. */")
	f.line("export function example<T>(name: string, adapter: ValueAdapter<T>): T { const value = Object.prototype.hasOwnProperty.call(examples,name) ? examples[name] : undefined; if (!value) throw new Error('unknown example '+name); if (value.reason || adapter.needsContext) throw new Error('example '+name+' unavailable: '+(value.reason || 'an acquiring adapter needs a native witness')); return adapter.import(undefined, JSON.parse(value.raw!)); }")
}

const tsTransparencySupport = `
export type Presentation = (wire: Endpoint) => { wire: Endpoint; close(): void } | Promise<{ wire: Endpoint; close(): void }>;
export interface Options {
 context?: AdapterContext;
 remoteContext?: AdapterContext;
 presentation?: Presentation;
 inputs?: Readonly<Record<string, unknown>>;
 equal?: (method: string, direct: unknown, roundTrip: unknown) => void | Promise<void>;
 callContext?: WireModelContext & { valueContext?: unknown };
}
function once(action: () => void): () => void { let closed=false; return () => { if (!closed) { closed=true; action(); } }; }
function hasMethod(facet:unknown,name:string):boolean{for(let current=facet;current!=null&&current!==Object.prototype;current=Object.getPrototypeOf(current)){if(Object.prototype.hasOwnProperty.call(current,name))return typeof (facet as Record<string,unknown>)[name]==='function';}return false;}
export const local: Presentation = wire => ({wire,close(){}});
export const mounted: Presentation = wire => {
 const root=mount(new Map([['family',wire]]));
 try { const dispatcher=createDispatcher(root); return {wire:dispatcher.select(['family']),close:once(()=>{try{dispatcher.close(1000,'');}finally{root.close(1000,'');}})}; }
 catch(error){root.close(1000,'');throw error;}
};
export const forwarded: Presentation = wire => {
 const [left,right]=wirePair();
 try { const detach=forwardWire(right,wire); return {wire:left,close:once(()=>{detach();left.close(1000,'');})}; }
 catch(error){left.close(1000,'');throw error;}
};
export const pipe: Presentation = async wire => {
 const [left,right]=framePipe(); let detach=()=>{};
 const server=new DuplexPeer({role:'server',prepare(peer){detach=forwardWire(peer.wire(),wire);}});
 const client=new DuplexPeer({role:'client'});
 const close=once(()=>{detach();client.close();server.close();left.close(1000,'');right.close(1000,'');});
 try {await Promise.all([server.attach(right),client.attach(left)]);return {wire:client.wire(),close};}
 catch(error){close();throw error;}
};
type Outcome = {ok:true;value:unknown}|{ok:false;error:unknown};
async function outcome(call:()=>unknown):Promise<Outcome>{try{return {ok:true,value:await call()};}catch(error){return {ok:false,error};}}
function canonical(value:unknown):string {
 if(value===undefined)return 'undefined';
 if(typeof value==='function'||typeof value==='symbol'||typeof value==='bigint')throw new Error('result needs an equal observer');
 if(value===null||typeof value!=='object')return JSON.stringify(value);
 if(Array.isArray(value))return '['+value.map(canonical).join(',')+']';
 return '{'+Object.keys(value).sort().map(key=>JSON.stringify(key)+':'+canonical((value as Record<string,unknown>)[key])).join(',')+'}';
}
async function compare(method:string,expected:Outcome,actual:Outcome,equal:Options['equal']):Promise<void>{
 if(!expected.ok || !actual.ok){
  const code=(error:unknown)=>error instanceof DuplexError?error.code:'internal';
  if(!expected.ok&&!actual.ok&&code(expected.error)===code(actual.error)){
   if(expected.error instanceof DuplexError && actual.error instanceof DuplexError && (expected.error.message!==actual.error.message || canonical(expected.error.data)!==canonical(actual.error.data)))throw new Error(method+': public error changed');
   return;
  }
  throw new Error(method+': direct and round-trip outcomes differ');
 }
 if(equal){await equal(method,expected.value,actual.value);return;}
 if(canonical(expected.value)!==canonical(actual.value))throw new Error(method+': direct and round-trip results differ');
}
`
