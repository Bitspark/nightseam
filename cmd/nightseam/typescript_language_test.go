package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// Each fixture compiles the actual generated packages against the runtime,
// then executes values through those same packages, including the built-in
// dependencies that the kernel emits beside a session family.
func typescriptLanguageFixture(t *testing.T, world analysis.World, source, script string) {
	t.Helper()
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	paths := map[string][]string{}
	modules := map[string]string{}
	for _, component := range []string{"runtime", "duplex", "tunnel"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
		entry := "./" + component + "/ts/src/index.ts"
		paths["@nightseam/"+component] = []string{entry}
		modules["@nightseam/"+component] = entry
	}
	k := kernel.New(typescript.New(typescript.Config{Scope: "@example"}))
	for name := range world {
		result, err := k.Render(&kernel.World{Families: world}, name)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range result.Files {
			writeFixture(t, directory, path, data)
			if rest, ok := strings.CutPrefix(path, "api/ts/"); ok && strings.HasSuffix(rest, "/src/index.ts") {
				name := "@example/" + strings.TrimSuffix(rest, "/src/index.ts")
				paths[name] = []string{"./" + path}
				modules[name] = "./" + path
			}
		}
		stubs, err := k.Scaffold(&kernel.World{Families: world}, name, "handlers/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range stubs {
			writeFixture(t, directory, file.Path, file.Data)
		}
	}
	config, _ := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths},
		"include":         []string{"api/ts/**/*.ts", "handlers/**/*.ts", "consumer.ts"},
	})
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "consumer.ts", []byte(source))
	writeFixture(t, directory, "consumer.mjs", []byte(script))
	encoded, _ := json.Marshal(modules)
	writeFixture(t, directory, "loader.mjs", []byte("const modules = "+string(encoded)+"; export async function resolve(name, context, next) { if (modules[name]) return { url: new URL(modules[name], import.meta.url).href, shortCircuit: true }; return next(name, context); }"))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./loader.mjs", "consumer.mjs")
}

func TestTypeScriptSettledLanguageProof(t *testing.T) {
	typescriptLanguageFixture(t, proofWorld(t), tsLanguageProofTypes, tsLanguageProofValues)
}

func TestTypeScriptProofGolden(t *testing.T) {
	k := kernel.New(typescript.New(typescript.Config{Scope: "@example"}))
	world := k.Load(os.DirFS(familiesRoot), "api/contracts")
	files := map[string][]byte{}
	for _, name := range []string{"probe", "proof"} {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range result.Files {
			files[path] = data
		}
	}
	holdGolden(t, "testdata/golden-proof-typescript", files)
}

func TestTypeScriptBuiltinFamilies(t *testing.T) {
	typescriptLanguageFixture(t, analysis.World(builtin.Families()), `
import type {Envelope} from '@example/duplex-client';
import type {Control, Cursor} from '@example/session-client';
const envelope: Envelope = {version:1, kind:'event', event:'changed', data:{}};
`, `
import assert from 'node:assert/strict';
import * as duplex from '@example/duplex-client';
assert.equal('Client' in duplex, false);
duplex.validateWire('Envelope', {version:1, kind:'event', event:'changed', data:{}});
assert.throws(() => duplex.validateWire('Envelope', {version:1}));
`)
}

// Forwarding the bindings through an inherited side must have the same
// consumer types and validation as fixing those bindings in the declaration.
// The inherited names come from the source family's overrides on both paths.
func TestTypeScriptAppliedInheritance(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"typed": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"T"}],"types":{"Input":{"kind":"record","fields":[{"name":"item","type":"T"}]}},"client":{"methods":{"echo":{"request":"Input","result":"T"}}}`)},
		"probe": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol("")},
		"catalog": {"model.json": `{"nightseam":2,"types":{
		 "Entry":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]},
		 "Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]},
		 "Choice":{"kind":"union","parameters":[{"name":"U"}],"tag":"tag","value":"body","variants":{
		  "some":{"kind":"record","fields":[{"name":"item","type":"U"},{"name":"owner","type":{"ref":"Entry"}}]},"none":{"empty":true}
		 }}
		}}`, "typescript.json": `{"names":{"Box":"Crate","Choice":"Outcome"}}`},
		"base": {"model.json": `{"nightseam":2,"imports":["catalog"]}`, "protocol.json": modeltest.Protocol(`
		 "parameters":[{"name":"S","of":"protocol"},{"name":"Item"}],
		 "types":{
		  "Packet":{"kind":"record","fields":[{"name":"message","type":"S.Envelope"},{"name":"handle","type":{"nullable":"S.Handle"}},{"name":"values","type":{"map":{"array":{"nullable":"Item"}}}}]},
		  "Nested":{"kind":"alias","parameters":[{"name":"T"}],"type":{"apply":"catalog.Box","with":{"T":{"apply":"catalog.Box","with":{"T":{"array":{"nullable":"T"}}}}}}}
		 },
		 "server":{"methods":{"read":{"request":"Packet","result":{"apply":"catalog.Choice","with":{"U":"Item"}}}},"events":{"changed":{"type":"Packet"}}},
		 "client":{"methods":{"reverse":{"request":"Packet","result":"Packet"}},"events":{"report":{"type":"Packet"}}},
		 "errors":{"not_found":"Gone"}
		`), "typescript.json": `{"names":{"read":"fetch","changed":"updated","errors.not_found":"missing"}}`},
		"child": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`
		 "parameters":[{"name":"F","of":"protocol"},{"name":"Value"}],
		 "types":{
		  "Forward":{"kind":"alias","type":{"apply":"base.Packet","with":{"S":"F","Item":"Value"}}},
		  "Inherited":{"kind":"record","extends":[{"apply":"base.Packet","with":{"S":"F","Item":"Value"}}],"fields":[{"name":"note","type":{"literal":"extended"}}]},
		  "FixedLiteral":{"kind":"alias","type":{"apply":"base.Nested","with":{"T":{"literal":"ok"}}}}},
		 "server":{"extends":[{"apply":"base","with":{"S":"F","Item":"Value"}}]},
		 "client":{"extends":[{"apply":"base","with":{"S":"F","Item":"Value"}}]}
		`)},
		"fixed": {"model.json": `{"nightseam":2,"imports":["base","probe"]}`, "protocol.json": modeltest.Protocol(`
		 "types":{"Packet":{"kind":"alias","type":{"apply":"base.Packet","with":{"S":"probe","Item":"string"}}}},
		 "server":{"extends":[{"apply":"base","with":{"S":"probe","Item":"string"}}]},
		 "client":{"extends":[{"apply":"base","with":{"S":"probe","Item":"string"}}]}
		`)},
	}))
	typescriptLanguageFixture(t, world, tsInheritanceTypes, tsInheritanceValues)
}

const tsInheritanceTypes = `
import * as base from '@example/base-client';
import * as child from '@example/child-client';
import * as fixed from '@example/fixed-client';
import * as probe from '@example/probe-client';
import * as catalog from '@example/catalog-client';
type Equals<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
const packet: Equals<base.Packet<probe.Family,string>,fixed.Packet> = true;
const forwarded: Equals<child.Forward<probe.Family,string>,fixed.Packet> = true;
const inherited: Equals<Omit<child.Inherited<probe.Family,string>,'note'>,fixed.Packet> = true;
const caller: Equals<child.Caller<probe.Family,string>,fixed.Caller> = true;
const handler: Equals<child.Handler<probe.Family,string>,fixed.Handler> = true;
const events: Equals<child.Events<probe.Family,string>,fixed.Events> = true;
const nested: child.FixedLiteral = {value:{value:['ok',null]}};
const result: catalog.Outcome<string> = {tag:'some',body:{item:'text',owner:'entry'}};
const empty: catalog.Outcome<string> = {tag:'none'};
const code: 'not_found' = child.errors.missing;
// @ts-expect-error The imported literal type argument survives nested applications.
const bad: child.FixedLiteral = {value:{value:['wrong']}};
// @ts-expect-error An imported entity reference preserves the key's type.
const badKey: catalog.Outcome<string> = {tag:'some',body:{item:'text',owner:3}};
// @ts-expect-error A tag-only variant admits no payload.
const badEmpty: catalog.Outcome<string> = {tag:'none',body:{}};
`

const tsInheritanceValues = `
import assert from 'node:assert/strict';
import {handler as scaffolded} from './handlers/child/handler.ts';
import * as child from '@example/child-client';
import * as fixed from '@example/fixed-client';
import * as probe from '@example/probe-client';
import {DuplexPeer} from '@nightseam/runtime';
import {pipe} from '@nightseam/duplex';
const binding = {type:'string', validate:probe.validateWire};
const slots = {F:probe.family,Value:binding};
const value = {message:{version:1,kind:'event',event:'changed',data:{}},handle:null,values:{first:['hello',null]}};
await assert.rejects(scaffolded.reverse(value,{}), /reverse is not implemented/);
child.validateWire('Inherited',{...value,note:'extended'},'$',slots);
assert.throws(() => child.validateWire('Inherited',{...value,note:'wrong'},'$',slots));
assert.throws(() => child.validateWire('Inherited',{...value,note:'extended',values:{first:[1]}},'$',slots));
for (const validate of [v => child.validateWire('Forward',v,'$',slots),v => fixed.validateWire('Packet',v)]) {
 validate(value);
 assert.throws(() => validate({...value,values:{first:[1]}}));
 assert.throws(() => validate({...value,message:{version:1}}));
}
child.validateWire('FixedLiteral',{value:{value:['ok',null]}});
assert.throws(() => child.validateWire('FixedLiteral',{value:{value:['wrong']}}));
for (const kind of ['forwarded','fixed']) {
 const server = new DuplexPeer({role:'server'});
 let requests = 0;
 server.handle('read', data => {requests++;return {tag:'some',body:{item:data.values.first[0],owner:'entry'}};});
 const [near,far] = pipe();
 await server.attach(far);
 let event, report;
 const updated = new Promise(resolve => {event=resolve;});
 const reported = new Promise(resolve => {report=resolve;});
 server.onEvent('report', report);
 const reverse = {reverse: data => data};
 const client = kind === 'forwarded'
  ? await child.Client.attach(near,probe.family,binding,{},reverse,{updated:event})
  : await fixed.Client.attach(near,{},reverse,{updated:event});
 try {
  assert.deepEqual(await client.fetch(value),{tag:'some',body:{item:'hello',owner:'entry'}});
  await assert.rejects(client.fetch({...value,values:{first:[1]}}));
  assert.equal(requests,1,'invalid values must be rejected before sending');
  await server.emit('changed',value);
  assert.deepEqual(await updated,value);
  await client.emitReport(value);
  assert.deepEqual(await reported,value);
  assert.deepEqual(await server.call('reverse',value),value);
  await assert.rejects(server.call('reverse',{...value,values:{first:[1]}}),{code:'invalid_params'});
 } finally {client.close();server.close();}
}
`

func TestTypeScriptAdjacentUnionPayloads(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"values": {"model.json": `{"nightseam":2,"types":{
		 "Record":{"kind":"record","fields":[{"name":"tag","type":{"nullable":"string"},"required":false}]},
		 "Choice":{"kind":"union","parameters":[{"name":"T"}],"tag":"tag","value":"body","variants":{
		  "item":"T","json":"json","map":{"map":"json"},"record":{"nullable":"Record"},"empty":{"kind":"record","fields":[]},"none":{"empty":true}
		 }},
		 "Extended":{"kind":"union","parameters":[{"name":"X"}],"tag":"tag","value":"body","extends":[{"apply":"Choice","with":{"T":{"array":{"nullable":"X"}}}}],"variants":{"extra":{"literal":"ok"}}},
		 "Fixed":{"kind":"alias","type":{"apply":"Extended","with":{"X":"string"}}}
		}}`, "typescript.json": `{"names":{"Record":"Payload"}}`},
	}))
	typescriptLanguageFixture(t, world, `
import type {Fixed, Extended} from '@example/values-client';
const value: Fixed = {tag:'item',body:['hello',null]};
const extra: Extended<string> = {tag:'extra',body:'ok'};
// @ts-expect-error The inherited parameter is bound to an array of nullable strings.
const bad: Fixed = {tag:'item',body:[3]};
`, `
import assert from 'node:assert/strict';
import {validateWire} from '@example/values-client';
const examples = [
 {tag:'json',body:3},{tag:'json',body:{body:3}},
 {tag:'json',body:null},{tag:'json',body:{tag:'other',body:null}},
 {tag:'record',body:null},{tag:'record',body:{}},{tag:'record',body:{tag:null}},{tag:'record',body:{tag:'different'}},
 {tag:'map',body:{tag:'different',body:3}},{tag:'empty',body:{}},{tag:'none'},
 {tag:'item',body:['hello',null]},{tag:'extra',body:'ok'},
];
for (const value of examples) {
 validateWire('Fixed',value);
 const decoded = JSON.parse(JSON.stringify(value));
 validateWire('Fixed',decoded);
 assert.deepEqual(decoded,value);
}

assert.equal(new Set(examples.map(value => JSON.stringify(value))).size, examples.length);
for (const value of [{tag:'none',body:{}},{tag:'empty'},{tag:'record'},{tag:'item',body:[3]},{tag:'extra',body:'wrong'}]) {
 assert.throws(() => validateWire('Fixed',value));
}
assert.throws(() => validateWire({apply:'Choice',with:{T:'json'}},{tag:'extra',body:'ok'}));
`)
}

func TestTypeScriptLocalFamilyParametersAndDrawnNames(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"probe": {"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`, "protocol.json": modeltest.Protocol(""), "session.json": `{}`, "typescript.json": `{"names":{"Payload":"Data"}}`},
		"carrier": {"model.json": `{"nightseam":2,"imports":["probe"]}`, "protocol.json": modeltest.Protocol(`
		 "parameters":[{"name":"S","of":"session"},{"name":"Item"}],
		 "types":{
		  "Drawn":{"kind":"record","fields":[{"name":"payload","type":"S.Payload"}]},
		  "Local":{"kind":"record","parameters":[{"name":"F","of":"protocol"},{"name":"T"}],"fields":[{"name":"message","type":"F.Envelope"},{"name":"handle","type":"F.Handle"},{"name":"value","type":"T"}]},
		  "Filled":{"kind":"alias","type":{"apply":"Local","with":{"F":"S","T":{"array":{"nullable":"Item"}}}}},
		  "Inline":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"choices","type":{"array":{"kind":"union","tag":"kind","variants":{"some":"T","none":{"empty":true}}}}}]}
		 },
		 "server":{"methods":{"read":{"result":"Drawn"}}}
		`)},
	}))
	typescriptLanguageFixture(t, world, `
import {Client, type Drawn, type Filled, type Local, type Inline} from '@example/carrier-client';
import * as probe from '@example/probe-client';
import {DuplexPeer} from '@nightseam/runtime';
const drawn: Drawn<probe.Family> = {payload:{text:'hello'}};
const defaultDrawn: Drawn = drawn;
const value: Filled<probe.Family,string> = {message:{version:1,kind:'event',event:'changed',data:{}},handle:{channel:1},value:['hello',null]};
const local: Local<probe.Family,string> = {...value,value:'hello'};
const inline: Inline<string> = {choices:[{kind:'some',value:'hello'},{kind:'none'}]};
const client = new Client<probe.Family,string>(new DuplexPeer(),probe.family,{type:'string',validate:probe.validateWire},undefined,{});
// @ts-expect-error The type-local family slot cannot bind a string.
type BadFamily = Local<string,string>;
// @ts-expect-error The inline union captures its enclosing type parameter.
const badInline: Inline<string> = {choices:[{kind:'some',value:3}]};
`, `
import assert from 'node:assert/strict';
import {validateWire} from '@example/carrier-client';
import * as probe from '@example/probe-client';
const slots = {S:probe.family,Item:{type:'string',validate:probe.validateWire}};
validateWire('Drawn',{payload:{text:'hello'}},'$',slots);
assert.throws(() => validateWire('Drawn',{payload:{text:3}},'$',slots));
const value = {message:{version:1,kind:'event',event:'changed',data:{}},handle:{channel:1},value:['hello',null]};
validateWire('Filled',value,'$',slots);
assert.throws(() => validateWire('Filled',{...value,value:[3]},'$',slots));
validateWire({apply:'Inline',with:{T:'string'}},{choices:[{kind:'some',value:'hello'},{kind:'none'}]});
assert.throws(() => validateWire({apply:'Inline',with:{T:'string'}},{choices:[{kind:'some',value:3}]}));
`)
}

const tsLanguageProofTypes = `
import {Client, type Carried, type Option, type Page, type Part, type RichPart} from '@example/proof-client';
import * as probe from '@example/probe-client';
import {DuplexPeer, type TypeBinding} from '@nightseam/runtime';
const text: Part = {type:'text', value:{type:'text', body:'hello'}};
const rich: RichPart = text;
const some: Option<Page<string>> = {kind:'some', value:{items:['hello']}};
const none: Option<string> = {kind:'none', value:{}};
const table: RichPart = {type:'table', value:{rows:['hello', null]}};
const carried: Carried<probe.Family, string> = {message:{version:1,kind:'event',event:'changed',data:{}},back:null,page:{items:['hello']}};
const textBinding: TypeBinding = {type:'string', validate:probe.validateWire};
const client = new Client<probe.Family, string>(new DuplexPeer(), probe.family, textBinding, undefined, {});
// @ts-expect-error An empty record payload remains present.
const missing: Option<string> = {kind:'none'};
// @ts-expect-error An empty record payload is an object, never a scalar.
const scalarEmpty: Option<string> = {kind:'none',value:3};
// @ts-expect-error A payload is whole under value, including records.
const flat: Part = {type:'text', body:'hello'};
// @ts-expect-error The literal inside a payload is still enforced.
const literal: Part = {type:'text', value:{type:'other', body:'hello'}};
// @ts-expect-error Nullable string does not admit an integer.
const badTable: RichPart = {type:'table', value:{rows:[42]}};
// @ts-expect-error A family parameter requires its associated types.
type NotAFamily = Carried<string, string>;
`

const tsLanguageProofValues = `
import assert from 'node:assert/strict';
import {Client, validateWire} from '@example/proof-client';
import * as probe from '@example/probe-client';
import {DuplexPeer} from '@nightseam/runtime';
import {pipe} from '@nightseam/duplex';
const slots = {S:probe.family, Item:{type:'string',validate:probe.validateWire}};
const part = {type:'text', value:{type:'text',body:'hello'}};
for (const [type, value] of [
 ['Part',part], ['RichPart',part],
 ['RichPart',{type:'table',value:{rows:['hello',null]}}],
 ['Part',{type:'image',value:{url:'https://example.org/image',alt:null}}],
 ['Parts',{items:[part]}],
 [{apply:'Option',with:{T:'string'}},{kind:'none',value:{}}],
 [{apply:'Result',with:{T:{array:{nullable:'string'}},E:'string'}},{kind:'ok',value:['hello',null]}],
]) {
 validateWire(type, value, '$', slots);
 const decoded = JSON.parse(JSON.stringify(value));
 validateWire(type, decoded, '$', slots);
 assert.deepEqual(decoded, value);
}
for (const value of [{type:'text',body:'hello'},{type:'text',value:{type:'other',body:'hello'}},{type:'image',value:{url:'http://wrong'}}]) {
 assert.throws(() => validateWire('Part', value));
}
assert.throws(() => validateWire('Part', {type:'table',value:{rows:[]}}));
assert.throws(() => validateWire({apply:'Option',with:{T:'string'}},{kind:'none'}));
const server = new DuplexPeer({role:'server'});
server.handle('relay', data => ({kind:'some',value:data.message}));
const [near,far] = pipe();
await server.attach(far);
const client = await Client.attach(near, probe.family, slots.Item, {}, undefined, {});
try {
 const data = {message:{version:1,kind:'event',event:'changed',data:{}},back:null,page:{items:['hello']}};
 assert.deepEqual(await client.relay(data), {kind:'some',value:data.message});
 await assert.rejects(client.relay({...data,page:{items:[1]}}));
 await assert.rejects(client.relay({...data,message:{version:1}}));
 let receive;
 const received = new Promise(resolve => {receive=resolve;});
 client.onPartAdded(receive);
 await server.emit('part.added',part);
 assert.deepEqual(await received,part);
} finally {client.close();server.close();}
`
