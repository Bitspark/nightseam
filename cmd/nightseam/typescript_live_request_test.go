package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Empty requests must keep their ordinary validation when only the result
// carries live values, on both the client and the server's reverse caller.
func TestGeneratedTypeScriptEmptyLiveRequests(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/service/model.json", []byte(`{"nightseam":2,"types":{}}`))
	writeFixture(t, directory, "api/contracts/service/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/service/live.json", []byte(`{
	 "types":{"Callback":{"kind":"callable","request":"integer","result":"integer"}},
	 "server":{"methods":{"create":{"result":"Callback"}}},
	 "client":{"methods":{"reverse":{"result":"Callback"}}}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)},
		"include":         []string{"api/ts/**/*.ts", "consumer.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "consumer.ts", []byte(tsEmptyLiveRequestsFixture))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "consumer.ts")
}

const tsEmptyLiveRequestsFixture = `import {pipe} from '@nightseam/duplex';
import {Client, DuplexError} from '@example/service-client';
import {serve, Remote} from '@example/service-binding';
import {scopeOf} from '@nightseam/live';

function check(value: unknown, message: string): asserts value { if (!value) throw new Error(message); }
async function refuses(call: Promise<unknown>, code: string) {
 try { await call; } catch (error) {
  check(error instanceof DuplexError && error.code === code, 'wrong refusal: ' + String(error));
  return;
 }
 throw new Error('expected ' + code);
}
const options = {signal: AbortSignal.timeout(5000)};
const [near, far] = pipe();
const [peer, client] = await Promise.all([
 serve(far, {}, {create(params) {
  check(Object.keys(params).length === 0, 'served request was not empty');
  return async value => value + 1;
 }}),
 Client.attach(near, {}, {reverse(params) {
  check(Object.keys(params).length === 0, 'reverse request was not empty');
  return async value => value + 2;
 }}, {}),
]);
try {
 const scopes = [scopeOf(client.peer), scopeOf(peer)];
 check(scopes[0] && scopes[1], 'generated peers installed no live scope');
 const remote = new Remote(peer);
 for (const side of [0, 1] as const) {
  const scope = scopes[side]!;
  const owner = scope.owner().child();
  const callback = side === 0 ? await client.create({...options, owner}) : await remote.reverse({...options, owner});
  check(owner.counts().imports === 1 && owner.counts().exports === 0, 'result did not belong to the selected owner');
  check(await callback(40, options) === 41 + side, 'returned callback was not callable');
  owner.release();
  await refuses(callback(40, options), 'reference_released');
  const deadline = Date.now() + 5000;
  while (scopes.some(value => value!.counts().exports || value!.counts().imports)) {
   check(Date.now() < deadline, 'released callback left a binding behind');
   await new Promise(resolve => setTimeout(resolve, 1));
  }
 }
 await refuses(client.peer.call('create', {extra: 1}, options), 'invalid_params');
 await refuses(peer.call('reverse', {extra: 1}, options), 'invalid_params');
} finally { client.close(); peer.close(); }
`
