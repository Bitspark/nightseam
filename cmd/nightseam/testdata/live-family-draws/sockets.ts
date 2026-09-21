import assert from 'node:assert/strict';
import * as first from '@example/first-client/types';
import * as second from '@example/second-client/types';
import * as holder from '@example/holder-binding';
import { DuplexError, DuplexPeer, forwardWire, type FamilyBinding, type ValueAdapter } from '@nightseam/runtime';
import { liveOver, valueEnvironment, type LiveOwner, type LiveScope } from '@nightseam/live';

type Family = first.Family | second.Family;
type Held = holder.Held<Family>;
type Counts = { exports: number; imports: number };
const url = process.argv[2]!;
const selected: FamilyBinding<Family, 'Job' | 'Progress'> = process.argv[3] === 'first' ? first.family : second.family;
const role = process.argv[4]!;

// These are the very same objects in both active physical scopes. No factory
// runs inside connection assembly, and the generic Holder imports no provider.
const adapter: ValueAdapter<Held> = holder.adapterHeld<Family>(selected);
const options = () => ({ signal: AbortSignal.timeout(5000) });
const empty: Counts = { exports: 0, imports: 0 };

function specimen(seed: number): Held {
  const original = async (n: number) => seed + n;
  const guarded = async (n: number) => {
    if (n < 0) throw new DuplexError('denied', 'guarded job refuses negative input');
    return original(n);
  };
  return {
    job: { run: guarded }, progress: { read: async n => seed + n + 1 },
    nested: {
      jobs: [{ run: async n => seed + n + 2 }],
      progress: { value: { read: async n => seed + n + 3 }, none: null },
      choice: { kind: 'job', value: { run: async n => seed + n + 4 } },
    },
  };
}

async function observe(value: Held): Promise<number[]> {
  assert.equal(value.nested.jobs.length, 1);
  assert.equal(value.nested.progress.none, null);
  assert.equal(Object.hasOwn(value.nested, 'absent'), false);
  assert.equal(value.nested.choice.kind, 'job');
  if (value.nested.choice.kind !== 'job') throw new Error('wrong union variant');
  const values = [
    await value.job.run(1, options()),
    await value.progress.read(1, options()),
    await value.nested.jobs[0]!.run(1, options()),
    await value.nested.progress.value!.read(1, options()),
    await value.nested.choice.value.run(1, options()),
  ];
  // A callable returned by exchange still traverses the supplied guard after
  // the exchange RPC has returned; equal declared identities cannot unwrap it.
  await assert.rejects(() => value.job.run(-1, options()), { code: 'denied' });
  return values;
}

async function zero(scope: LiveScope): Promise<void> {
  const deadline = Date.now() + 5000;
  while (scope.counts().exports || scope.counts().imports) {
    assert(Date.now() < deadline, 'bindings remained before teardown: ' + JSON.stringify(scope.counts()));
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}

function connection() {
  const peer = new DuplexPeer();
  const scope = liveOver(peer, { maxImports: 7, maxExports: 64 });
  const environment = valueEnvironment(scope);
  const modelOwners: LiveOwner[] = [];
  let modelCalls = 0;
  peer.handle('test.drop', () => { for (const owner of modelOwners.splice(0)) owner.release(); return null; });
  const prepared = role === 'go-server'
    ? holder.prepareFromWire<Family>(peer.wire(), { valueEnvironment: environment }, selected)
    : undefined;
  const wire = role === 'typescript-server' ? holder.toWire<Family>(() => ({
    methods: { exchange(value, context) {
      assert(context?.valueContext, 'generated TypeScript handler received no active owner');
      modelOwners.push(context.valueContext as LiveOwner);
      modelCalls++;
      return value;
    } }, events: {},
  }), { valueEnvironment: environment }, selected) : undefined;
  const detach = wire && forwardWire(peer.wire(), wire);
  return { peer, scope, environment, prepared, modelOwners, calls: () => modelCalls,
    close() { prepared?.close(); detach?.(); wire?.close(); peer.close(); } };
}

type Connection = ReturnType<typeof connection>;

async function generatedCalls(c: Connection, index: number): Promise<void> {
  if (role === 'go-server') {
    const model = await c.prepared!.complete(options());
    const access = model({ methods: {}, events: {} });
    const owner = c.scope.owner().child();
    const seed = 100 + index * 10;
    const returned = await access.methods.exchange(specimen(seed), { ...options(), valueContext: owner });
    assert.deepEqual(await observe(returned), [seed + 1, seed + 2, seed + 3, seed + 4, seed + 5]);
    assert.deepEqual(owner.counts(), { exports: 5, imports: 5 });
    assert.deepEqual(c.scope.owner().counts(), empty, 'generated call captured the root owner');
    assert.deepEqual(await c.peer.call('test.counts', {}, options()), { exports: 5, imports: 5 });
    owner.release();
    await c.peer.call('test.drop', {}, options());
  } else {
    const result = await c.peer.call('test.exercise', {}, options());
    assert.deepEqual(result, { observed: [101, 102, 103, 104, 105], released: empty });
    assert.equal(c.calls(), 1, 'generated TypeScript server was not exercised exactly once');
  }
  await zero(c.scope);
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), empty);
}

function exportValue(c: Connection, owner: LiveOwner, value: Held): unknown {
  return c.environment.export(owner, active => adapter.export(active, value));
}

function importValue(c: Connection, owner: LiveOwner, value: unknown): Held {
  return c.environment.import(owner, active => adapter.import(active, value));
}

// Reuse two references already attached under a different owner, then reach
// three fresh nested references with only two free import positions.
function borrowedPrefix(original: unknown, fresh: unknown): unknown {
  const held = original as { job: unknown; progress: unknown };
  return { ...(fresh as object), job: held.job, progress: held.progress };
}

async function rollback(c: Connection): Promise<void> {
  const sender = c.scope.owner().child(), receiver = c.scope.owner().child();
  const failedImport = c.scope.owner().child(), failedExport = c.scope.owner().child();
  const original = exportValue(c, sender, specimen(200));
  const firstImport = await c.peer.call('test.import', { value: original }, options());
  assert.deepEqual(firstImport, {
    accepted: true, observed: [201, 202, 203, 204, 205], counts: { exports: 0, imports: 5 }, owner: { exports: 0, imports: 5 },
  });
  const next = exportValue(c, sender, specimen(300));
  const refused = await c.peer.call('test.import', { value: borrowedPrefix(original, next), failure: true }, options());
  assert.deepEqual(refused, {
    accepted: false, observed: [201, 202, 203, 204, 205], counts: { exports: 0, imports: 5 }, owner: empty,
  });

  const remoteOriginal = await c.peer.call('test.export', { seed: 400 }, options());
  const retained = importValue(c, receiver, remoteOriginal);
  assert.deepEqual(await observe(retained), [401, 402, 403, 404, 405]);
  const remoteFresh = await c.peer.call('test.export', { seed: 500 }, options());
  assert.throws(() => importValue(c, failedImport, borrowedPrefix(remoteOriginal, remoteFresh)), { code: 'too_many_imports' });
  assert.deepEqual(failedImport.counts(), empty, 'partial import retained fresh acquisitions');
  assert.deepEqual(receiver.counts(), { exports: 0, imports: 5 }, 'rollback released a borrowed attachment');
  assert.deepEqual(await observe(retained), [401, 402, 403, 404, 405]);

  const before = c.scope.counts();
  const invalid: Held = { ...retained, progress: { read: undefined as unknown as first.Run } };
  assert.throws(() => exportValue(c, failedExport, invalid));
  assert.deepEqual(failedExport.counts(), empty, 'partial export retained fresh acquisitions');
  assert.deepEqual(c.scope.counts(), before);
  assert.deepEqual(await observe(retained), [401, 402, 403, 404, 405]);
  // Each failed import reached and released two fresh references. Its sender
  // keeps the five original exports and the three fresh references not reached.
  assert.deepEqual(await c.peer.call('test.bad_export', {}, options()), {
    observed: [201, 202, 203, 204, 205], counts: { exports: 8, imports: 5 },
  });
  assert.deepEqual(sender.counts(), { exports: 8, imports: 0 });
  assert.deepEqual(c.scope.counts(), { exports: 8, imports: 5 });
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), { exports: 8, imports: 5 });

  for (const owner of [failedImport, failedExport, sender, receiver]) owner.release();
  await c.peer.call('test.drop', {}, options());
  await zero(c.scope);
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), empty);
  await assert.rejects(() => retained.job.run(1, options()), { code: 'reference_released' });
}

const connections = [connection(), connection()];
try {
  // Both scopes are active before either starts; the shared adapter is reused
  // concurrently during model calls and during acquired value conversions.
  await Promise.all(connections.map(c => c.peer.connect(url)));
  await Promise.all(connections.map(generatedCalls));
  await Promise.all(connections.map(rollback));
} finally {
  for (const c of connections) c.close();
}
