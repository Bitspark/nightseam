package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGeneratedTypeScriptIdentityPreparation(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/same/model.json", []byte(`{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`))
	writeFixture(t, directory, "api/contracts/same/protocol.json", []byte(`{"profile":"nightseam.duplex/1","server":{"methods":{"run":{"request":"Input","result":"integer"}},"events":{"changed":{"type":"Input"}}},"client":{"methods":{"reverse":{"request":"Input","result":"integer"}},"events":{"noticed":{"type":"Input"}}}}`))
	writeFixture(t, directory, "api/contracts/generic/model.json", []byte(`{"nightseam":2}`))
	writeFixture(t, directory, "api/contracts/generic/protocol.json", []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{"methods":{"read":{"result":"T"}}}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": fixtureTypeScriptPaths(t, directory)},
		"include":         []string{"api/ts/**/*.ts", "identity.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "identity.ts", []byte(tsIdentityPreparationProgram))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "identity.ts")
}

const tsIdentityPreparationProgram = `import type { Wire } from '@nightseam/duplex';
import { DuplexError, IDENTITY_METHOD, callWire, emitWire, identityHandler, registerWire, wirePair } from '@nightseam/runtime';
import * as binding from '@example/same-binding';
import * as client from '@example/same-client';
import * as generic from '@example/generic-binding';
import type { Client } from '@example/same-client/types';

function check(value: unknown, message: string): asserts value { if (!value) throw new Error(message); }
async function rejected(run: () => unknown, code: string) {
  try { await run(); } catch (error) { check(error instanceof DuplexError && error.code === code, 'wrong refusal: '+String(error)); return; }
  throw new Error('accepted '+code);
}
async function delivered(test: () => boolean) {
  const deadline = Date.now()+1000;
  while (!test()) { check(Date.now()<deadline, 'event delivery timed out'); await new Promise(resolve => setTimeout(resolve, 1)); }
}
const context = { options: { requestTimeoutMs: 1000 } };
const expected = { path: 'same', digest: client.wireDigest };

// Receivers must exist before reading starts; their implementation arrives
// only after complete checks the peer and the returned factory is bound.
{
  const [access, provider] = wirePair(context.options);
  let callbacks = 0, events = 0;
  registerWire(provider, [IDENTITY_METHOD], { request: identityHandler(expected) });
  registerWire(provider, ['run'], { request: () => 42 });
  registerWire(access, ['unrelated'], { request: () => 7 });
  const preparation = binding.prepareFromWire(access, context);
  const early = callWire<number>(provider, ['reverse'], {value:41}, {timeoutMs:1000});
  emitWire(provider, ['changed'], {value:41});
  const factory = await preparation.complete({timeoutMs:1000});
  check(Number(callbacks)===0 && Number(events)===0, 'incoming operation reached an unbound model');
  const implementation: Client = {
    methods: { reverse({value}) { callbacks++; return value+1; } },
    events: { changed() { events++; } },
  };
  const model = factory(implementation);
  check(await early===42, 'preregistered reverse request lost its bound implementation');
  await delivered(() => events===1);
  check(callbacks===1 && events===1, 'deferred operation was duplicated');
  await rejected(() => preparation.complete(), 'already_bound');
  await rejected(() => factory(implementation), 'already_bound');
  check(await model.methods.run({value:41})===42, 'duplicate completion closed the valid interpretation');
  preparation.close(); preparation.close();
  check(await callWire(provider, ['unrelated'], {})===7, 'cleanup detached an unrelated receiver or closed the carrier');
  const detachIdentity = registerWire(access, [IDENTITY_METHOD], {request: identityHandler(expected)});
  const detachReverse = registerWire(access, ['reverse'], {request: () => 9});
  check(await callWire(provider, ['reverse'], {})===9, 'cleanup retained an owned receiver');
  detachReverse(); detachIdentity(); access.close();
}

// Both generated roles reject a same-name revision before exposing a factory.
for (const prepare of [binding.prepareFromWire, client.prepareFromWire]) {
  const [access, provider] = wirePair(context.options);
  registerWire(provider, [IDENTITY_METHOD], {request: identityHandler({...expected, digest:'0'.repeat(64)})});
  registerWire(access, ['unrelated'], {request: () => 7});
  const preparation = prepare(access, context);
  let exposed = false;
  await rejected(async () => { await preparation.complete({timeoutMs:1000}); exposed = true; }, 'contract_mismatch');
  check(!exposed, 'mismatch exposed a model factory');
  check(await callWire(provider, ['unrelated'], {})===7, 'refusal closed the carrier');
  const detach = registerWire(access, [IDENTITY_METHOD], {request: identityHandler(expected)});
  detach(); preparation.close(); access.close();
}

// A peer with no identity handler remains an unspecified declaration.
{
  const [access, provider] = wirePair(context.options);
  registerWire(provider, ['run'], {request: () => 42});
  const factory = await binding.fromWire(access, context);
  const model = factory({methods:{reverse: () => 0},events:{changed() {}}});
  check(await model.methods.run({value:41})===42, 'unspecified peer was refused');
  access.close();
}

// Local model adaptation also advertises identity before it returns a wire.
{
  let constructions = 0;
  const wire = binding.toWire(() => { constructions++; return {methods:{run: ({value}) => value+1},events:{noticed() {}}}; }, context);
  const factory = await binding.fromWire(wire, context);
  const model = factory({methods:{reverse: () => 0},events:{changed() {}}});
  check(constructions===1 && await model.methods.run({value:41})===42, 'typed local roundtrip changed model construction');
  wire.close();
}

// Missing type bindings fail synchronously before any registration or model.
{
  let operations = 0, constructions = 0;
  const wire: Wire = {send() {operations++;},receive() {operations++;return () => {};},close() {operations++;}};
  let refused = 0;
  try { generic.prepareFromWire(wire, context, undefined as never); } catch { refused++; }
  try { generic.toWire(() => { constructions++; return {methods:{read: () => 0},events:{}}; }, context, undefined as never); } catch { refused++; }
  check(refused===2 && operations===0 && constructions===0, 'missing binding reached wire setup or model code');
}
`
