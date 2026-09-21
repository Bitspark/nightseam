package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGeneratedTypeScriptValueAdapters(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/cell/model.json", []byte(`{"nightseam":2,"types":{}}`))
	writeFixture(t, directory, "api/contracts/cell/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{"methods":{"exchange":{"request":{"kind":"record","fields":[{"name":"value","type":"T"}]},"result":"T"}}}}`))
	writeFixture(t, directory, "api/contracts/values/model.json", []byte(`{"nightseam":2,"types":{"Count":{"kind":"alias","type":"integer"},"Page":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"array":"T"}}]}}}`))
	writeFixture(t, directory, "api/contracts/values/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/values/live.json", []byte(`{"types":{"Unary":{"kind":"callable","request":"Count","result":"Count"},"Factory":{"kind":"callable","request":"Unary","result":"Unary"},"Bundle":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"},{"name":"run","type":"Unary"}]}}}`))
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
	writeFixture(t, directory, "consumer.ts", []byte(tsValueAdaptersFixture))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "consumer.ts")
}

const tsValueAdaptersFixture = `import {pipe} from '@nightseam/duplex';
import {Client} from '@example/cell-client';
import {serve, type Handler} from '@example/cell-binding';
import {adapterCount, adapterUnary, adapterFactory, adapterPage, adapterBundle} from '@example/values-client';
import {scopeOf, liveOver, type ValueAdapter, type LiveOwner} from '@nightseam/live';

function check(value: unknown, message: string): asserts value { if (!value) throw new Error(message); }
const options = {signal: AbortSignal.timeout(5000)};
const observations: string[] = [];
let callbacks = 0;
function cell<T>(): Handler<T> {
 return {exchange(request) { observations.push('exchange'); return request.value; }};
}
async function exercise<T>(adapter: ValueAdapter<T>, input: T, observe: (value: T, owner: LiveOwner | undefined) => Promise<number>) {
 const [a,b] = pipe();
 const [peer, client] = await Promise.all([serve(b, adapter, {}, cell<T>()), Client.attach(a, adapter, {}, undefined, {})]);
 try {
  const scopes = [scopeOf(client.peer), scopeOf(peer)].filter(s => s !== undefined);
  check(scopes.length === (adapter.live ? 2 : 0), 'generic boundary acquired the wrong scopes');
  const owner = scopes[0]?.owner().child();
  const result = await client.exchange({value: input}, {...options, owner} as Parameters<typeof client.exchange>[1]);
  check(await observe(result, owner) === 42, 'generic value changed behavior');
  owner?.release();
  const until = Date.now() + 5000;
  while (scopes.some(s => s.counts().exports || s.counts().imports)) {
   check(Date.now() < until, 'generic conversion leaked retained bindings');
   await new Promise(resolve => setTimeout(resolve, 1));
  }
  if (!adapter.live) {
   const scope = liveOver(client.peer, {});
   const released = scope.owner().child(); released.release();
   check(adapter.export(released, input) === input, 'scalar export changed after owner release');
   check(adapter.import(released, input) === input, 'scalar import changed after owner release');
   check(await client.exchange({value: input}, {...options, owner: released} as Parameters<typeof client.exchange>[1]) === input, 'scalar operation acquired a released owner');
   scope.close();
  }
 } finally { client.close(); peer.close(); }
}
await exercise(adapterCount(), 42, async value => value);
await exercise(adapterUnary(), async value => { callbacks++; return value + 1; }, value => value(41, options));
await exercise(adapterFactory(), async callback => async value => { callbacks++; return callback(value, options); }, async (factory, owner) => (await factory(async value => value + 1, {...options, owner}))(41, options));
await exercise(adapterPage(adapterBundle(adapterUnary())), {items:[{value:async value => {callbacks++; return value+1;},run:async value => value+1}]}, async page => { check(await page.items[0]!.run(41,options)===42,'nested sibling callback'); return page.items[0]!.value(41,options); });
check(observations.length === 5 && callbacks === 3, 'state or callback multiplicity changed');
`
