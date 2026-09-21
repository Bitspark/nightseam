package main

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

// The generated server owns validation and typed dispatch over a connection
// accepted by its host, including a reverse call made inside a served method.
func TestGeneratedTypeScriptBinding(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"service": {
			"model.json": `{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`,
			"protocol.json": modeltest.Protocol(`
			 "server":{"methods":{"run":{"request":"Input","result":"integer"},"broken":{"result":"integer"}},"events":{"changed":{"type":"integer"}}},
			 "client":{"methods":{"reverse":{"request":"Input","result":"integer"}},"events":{"noticed":{"type":"integer"}}}`),
		},
	}))
	typescriptLanguageFixture(t, world, tsBindingFixture, `import './consumer.ts';`)
}

const tsBindingFixture = `import {pipe, encodePath, type FrameConnection} from '@nightseam/duplex';
import {DuplexPeer, DuplexError, callWire, forwardWire} from '@nightseam/runtime';
import {toWire, fromWire, type Client, type ServerModel} from '@example/service-binding';

function check(value: unknown, message: string): asserts value { if (!value) throw new Error(message); }
async function refuses(call: Promise<unknown>, code?: string) {
 try { await call; } catch (error) {
  if (code) check(error instanceof DuplexError && error.code === code, 'wrong refusal: ' + String(error));
  return;
 }
 throw new Error('invalid value was accepted');
}
async function delivered(test:()=>boolean) {const deadline=Date.now()+5000;while(!test()){check(Date.now()<deadline,'event not delivered');await new Promise(resolve=>setTimeout(resolve,1));}}
const options = {signal: AbortSignal.timeout(5000)};
let changed = 0;
let noticed = 0;
let reverse!: Client;
const model: ServerModel = remote => {
 reverse=remote;
 return {methods:{
  async run(value, context) {
   check(context?.signal instanceof AbortSignal, 'handler lost its call context');
   await remote.events.changed(value.value);
   return remote.methods.reverse(value, options);
  },
  broken() { return NaN; },
 },events:{noticed:value=>{noticed=value;}}};
};
const [near, far] = pipe();
const peer = new DuplexPeer({role:'server', dispatch: async method => {
 check(method === 'host.extra', 'unknown fallback method');
 return 19;
}});
const wire=toWire(model,{});
forwardWire(peer.wire(),wire);
const consumer=new DuplexPeer({role:'client'});
const bind=await fromWire(consumer.wire(),{});
const client=bind({methods:{reverse:value=>value.value+1},events:{changed:value=>{changed=value;}}});
await Promise.all([peer.attach(far),consumer.attach(near)]);
try {
 check(await client.methods.run({value:4}, options) === 5 && changed === 4, 'typed served and reverse call failed');
 await client.events.noticed(7);
 await delivered(()=>noticed===7);
 check(await consumer.call('host.extra', {}, options) === 19 && noticed === 7, 'host dispatch or incoming event was lost');
 await refuses(callWire(consumer.wire(), ['run'], 'invalid', options), 'invalid_params');
 await refuses(Promise.resolve(client.methods.broken({},options)));
 await refuses(Promise.resolve(reverse.methods.reverse({value:NaN}, options)));
} finally { consumer.close(); peer.close(); wire.close(); }

// The host binds generated incoming events before attach reads its first frame.
let first = 0;
const immediate: FrameConnection = {state:'open', buffered:0, send() {}, close() {}, listen(listener) {
 listener.frame?.({kind:'text',data:JSON.stringify({version:1,kind:'event',event:encodePath(['noticed']),data:11})});
 return () => {};
}};
const ready = new DuplexPeer({role:'server'});
const early=toWire(()=>({methods:{run:value=>value.value,broken:()=>0},events:{noticed:value=>{first=value;}}}),{});
forwardWire(ready.wire(),early);
await ready.attach(immediate);
await delivered(()=>first===11);
check(first === 11, 'first incoming event was lost');
ready.close();early.close();

let touched = false;
const unopened: FrameConnection = {state:'open', buffered:0, send() {}, close() {}, listen() { touched = true; return () => {}; }};
await refuses((async()=>{const invalid=toWire((()=>({methods:{},events:{}})) as unknown as ServerModel,{});const host=new DuplexPeer();forwardWire(host.wire(),invalid);await host.attach(unopened);})());
check(!touched, 'an incomplete model consumed the accepted connection');
`
