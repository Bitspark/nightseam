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

const tsBindingFixture = `import {pipe, type FrameConnection} from '@nightseam/duplex';
import {Client, DuplexError} from '@example/service-client';
import {serve, Remote, type Handler} from '@example/service-binding';

function check(value: unknown, message: string): asserts value { if (!value) throw new Error(message); }
async function refuses(call: Promise<unknown>, code?: string) {
 try { await call; } catch (error) {
  if (code) check(error instanceof DuplexError && error.code === code, 'wrong refusal: ' + String(error));
  return;
 }
 throw new Error('invalid value was accepted');
}
const options = {signal: AbortSignal.timeout(5000)};
let changed = 0;
let noticed = 0;
const handler: Handler = {
 async run(value, remote, context) {
  check(context.peer === remote.peer, 'handler context and remote disagree');
  await remote.emitChanged(value.value);
  return remote.reverse(value, options);
 },
 broken() { return NaN; },
};
const [near, far] = pipe();
const [peer, client] = await Promise.all([
 serve(far, {role:'client', dispatch: async method => {
  check(method === 'host.extra', 'unknown fallback method');
  return 19;
 }}, handler, {noticed: value => { noticed = value; }}),
 Client.attach(near, {}, {reverse: value => value.value + 1}, {changed: value => { changed = value; }}),
]);
try {
 check(await client.run({value:4}, options) === 5 && changed === 4, 'typed served and reverse call failed');
 await client.emitNoticed(7);
 check(await client.peer.call('host.extra', {}, options) === 19 && noticed === 7, 'host dispatch or incoming event was lost');
 await refuses(client.peer.call('run', 'invalid', options), 'invalid_params');
 await refuses(client.broken(options));
 await refuses(new Remote(peer).reverse({value:NaN}, options));
} finally { client.close(); peer.close(); }

// Events supplied to serve must exist before attach reads its first frame.
let first = 0;
const immediate: FrameConnection = {state:'open', buffered:0, send() {}, close() {}, listen(listener) {
 listener.frame?.({kind:'text',data:JSON.stringify({version:1,kind:'event',event:'noticed',data:11})});
 return () => {};
}};
const ready = await serve(immediate, {}, handler, {noticed: value => { first = value; }});
check(first === 11, 'first incoming event was lost');
ready.close();

let touched = false;
const unopened: FrameConnection = {state:'open', buffered:0, send() {}, close() {}, listen() { touched = true; return () => {}; }};
await refuses(serve(unopened, {}, {} as Handler));
check(!touched, 'an incomplete handler consumed the accepted connection');
`
