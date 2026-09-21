import assert from 'node:assert/strict';
import { pipe } from '@nightseam/duplex';
import { DuplexPeer, forwardWire, jsonAdapter, type ValueAdapter } from '@nightseam/runtime';
import { liveOver, valueEnvironment, type LiveOwner } from '@nightseam/live';
import * as cell from '@example/compose-cell-binding';
import * as functions from '@example/functions-client/types';
import * as numbers from '@example/numbers-client';
import * as texts from '@example/texts-client';
import * as generic from '@example/holder-client/types';
import * as numberSource from './bound/numbers/ts/holder-client/src/types.ts';
import * as textSource from './bound/texts/ts/holder-client/src/types.ts';

const signal = AbortSignal.timeout(30000);
const empty = { exports: 0, imports: 0 };
type Effects = { job: number; notify: number };
type Observation = { job: number | string; notify: number | string };
type Spec<T> = {
  value(seed: number, effects: Effects): T;
  observe(value: T, owner: LiveOwner): Promise<Observation>;
};

// This single state machine uses no provider or callable construction API.
function memory<T>() {
  let value: T;
  let revision = 0;
  let sessions = 0;
  const transitions: string[] = [];
  const model: cell.ServerModel<T> = () => {
    sessions++;
    return { methods: {
      put(input) { value = input.value; transitions.push('put'); return ++revision; },
      get() { transitions.push('get'); return value; },
    }, events: { noted() {} } };
  };
  return { model, transitions, sessions: () => sessions };
}

async function pair() {
  const client = new DuplexPeer({ role: 'client' });
  const server = new DuplexPeer({ role: 'server' });
  const near = liveOver(client), far = liveOver(server);
  const [a, b] = pipe();
  await Promise.all([client.attach(a), server.attach(b)]);
  return { client, server, near, far };
}

async function record<T>(sender: ValueAdapter<T>, receiver: ValueAdapter<T>, spec: Spec<T>) {
  const p = await pair(), state = memory<T>();
  const wire = cell.toWire(state.model, { valueEnvironment: valueEnvironment(p.far) }, receiver);
  const detach = forwardWire(p.server.wire(), wire);
  const owner = p.near.owner().child();
  const effects: Effects = { job: 0, notify: 0 };
  try {
    const model = await cell.prepareFromWire(p.client.wire(), { valueEnvironment: valueEnvironment(p.near) }, sender).complete({ signal });
    const client = model({ methods: { mirror: input => input.value }, events: { changed() {} } });
    const context = { signal, valueContext: owner };
    const revisions = [await client.methods.put({ value: spec.value(1, effects) }, context)];
    const saved = await client.methods.get({}, context);
    revisions.push(await client.methods.put({ value: spec.value(2, effects) }, context));
    const current = await client.methods.get({}, context);
    const observations = [await spec.observe(saved, owner), await spec.observe(current, owner), await spec.observe(saved, owner)];
    const counts = [p.near.counts(), p.far.counts()];
    assert(counts.every(count => count.exports > 0 && count.imports > 0));
    assert.deepEqual(revisions, [1, 2]);
    assert.deepEqual(state.transitions, ['put', 'get', 'put', 'get']);
    assert.equal(state.sessions(), 1);
    owner.release(); p.near.owner().release(); p.far.owner().release();
    const deadline = Date.now() + 5000;
    while ([p.near, p.far].some(scope => scope.counts().exports || scope.counts().imports)) {
      assert(Date.now() < deadline, 'independent route leaked before transport teardown');
      await new Promise(resolve => setTimeout(resolve, 1));
    }
    return { revisions, observations, effects, counts, released: [p.near.counts(), p.far.counts()] };
  } finally { detach(); wire.close(); p.client.close(); p.server.close(); }
}

const integer = jsonAdapter<number>({ type: 'integer', validate: functions.validateWire });
const functionGeneric = functions.adapterFunction(integer, integer);
const functionSource = functions.adapterIntegerFunction();
const functionSpec: Spec<functions.IntegerFunction> = {
  value: (seed, effects) => async n => { effects.job++; return n + seed; },
  observe: async (value, owner) => ({ job: await value(5, { signal, owner }), notify: 0 }),
};
assert.deepEqual(functions.contractIntegerFunction(), functions.contractFunction(integer, integer));
assert.equal(functions.contractIntegerFunction().path, 'functions/Function<integer,integer>');
const functionExpected = await record(functionGeneric, functionGeneric, functionSpec);
for (const [sender, receiver] of [[functionSource, functionSource], [functionGeneric, functionSource], [functionSource, functionGeneric]]) {
  assert.deepEqual(await record(sender!, receiver!, functionSpec), functionExpected);
}
assert.deepEqual(functionExpected.observations, [{ job: 6, notify: 0 }, { job: 7, notify: 0 }, { job: 6, notify: 0 }]);
assert.deepEqual(functionExpected.effects, { job: 3, notify: 0 });

type NumberBatch = generic.Batch<numbers.Family>;
type TextBatch = generic.Batch<texts.Family>;
const numberSpec: Spec<NumberBatch> = {
  value: (seed, effects) => [null, {
    empty: { kind: 'empty' }, value: { kind: 'value', value: {
      job: { run: async n => { effects.job++; return n + seed; } },
      progress: { label: 'fixed', notify: { run: async n => { effects.notify++; return n + seed * 10; } } },
    } },
  }],
  observe: async (value, owner) => {
    assert.equal(value[0], null); assert.equal(value.length, 2);
    assert.deepEqual(value[1]!.empty, { kind: 'empty' });
    const choice = value[1]!.value!; assert.equal(choice.kind, 'value');
    assert(choice.kind === 'value'); assert.equal(choice.value.progress.label, 'fixed');
    return { job: await choice.value.job.run(5, { signal, owner }), notify: await choice.value.progress.notify.run(5, { signal, owner }) };
  },
};
const textSpec: Spec<TextBatch> = {
  value: (seed, effects) => [null, {
    empty: { kind: 'empty' }, value: { kind: 'value', value: {
      job: { run: async n => { effects.job++; return n + ':' + seed; } },
      progress: { label: 'fixed', notify: { run: async n => { effects.notify++; return n + ':' + seed * 10; } } },
    } },
  }],
  observe: async (value, owner) => {
    assert.equal(value[0], null); assert.equal(value.length, 2);
    assert.deepEqual(value[1]!.empty, { kind: 'empty' });
    const choice = value[1]!.value!; assert(choice.kind === 'value');
    assert.equal(choice.value.progress.label, 'fixed');
    return { job: await choice.value.job.run('v', { signal, owner }), notify: await choice.value.progress.notify.run('v', { signal, owner }) };
  },
};
const genericNumbers = generic.adapterBatch<numbers.Family>(numbers.family);
const genericTexts = generic.adapterBatch<texts.Family>(texts.family);
const numberResult = await record(genericNumbers, genericNumbers, numberSpec);
const textResult = await record(genericTexts, genericTexts, textSpec);
assert.deepEqual(await record(numberSource.adapterBatch(), numberSource.adapterBatch(), numberSpec), numberResult);
assert.deepEqual(await record(textSource.adapterBatch(), textSource.adapterBatch(), textSpec), textResult);
assert.deepEqual(numberResult.observations, [{ job: 6, notify: 15 }, { job: 7, notify: 25 }, { job: 6, notify: 15 }]);
assert.deepEqual(textResult.observations, [{ job: 'v:1', notify: 'v:10' }, { job: 'v:2', notify: 'v:20' }, { job: 'v:1', notify: 'v:10' }]);
for (const result of [numberResult, textResult]) {
  assert.deepEqual(result.effects, { job: 3, notify: 3 });
  assert.deepEqual(result.released, [empty, empty]);
}

// Admission must refuse before any method effects or live value acquisitions.
// toWire constructs the local model once; interpreting it must add no effects.
async function mismatch<A, B>(serverAdapter: ValueAdapter<A>, clientAdapter: ValueAdapter<B>) {
  const p = await pair(), state = memory<A>();
  const wire = cell.toWire(state.model, { valueEnvironment: valueEnvironment(p.far) }, serverAdapter);
  const detach = forwardWire(p.server.wire(), wire);
  try {
    await assert.rejects(cell.prepareFromWire(p.client.wire(), { valueEnvironment: valueEnvironment(p.near) }, clientAdapter).complete({ signal }), { code: 'contract_mismatch' });
    assert.equal(state.sessions(), 1);
    assert.deepEqual(state.transitions, []);
    assert.deepEqual([p.near.counts(), p.far.counts()], [empty, empty]);
  } finally { detach(); wire.close(); p.client.close(); p.server.close(); }
}
await mismatch(functionGeneric, functions.adapterOtherFunction(integer, integer));
await mismatch(functions.adapterOtherFunction(integer, integer), functionSource);
await mismatch(genericNumbers, genericTexts);
await mismatch(genericTexts, genericNumbers);
console.log('GEN-COMPOSE-ROUTES TypeScript: callable 4 routes, Holder 2 providers × 2 routes, 4 pre-effect refusals');
