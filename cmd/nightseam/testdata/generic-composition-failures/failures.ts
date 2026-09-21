import assert from 'node:assert/strict';
import * as functions from '@example/functions-client/types';
import * as holder from '@example/holder-client/types';
import * as numbers from '@example/numbers-client/types';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, jsonAdapter, type ValueAdapter } from '@nightseam/runtime';
import { LiveOwner, liveOver, valueEnvironment, type LiveScope } from '@nightseam/live';

type Unary = functions.Function<number, number>;
type Factory = functions.Function<Unary, Unary>;
type Held = holder.Value<numbers.Family>;
type Batch = holder.Batch<numbers.Family>;
const empty = { exports: 0, imports: 0 };
const number = jsonAdapter<number>({ type: 'integer', validate: functions.validateWire });
const unary = functions.adapterFunction(number, number);
const invokeOptions = (owner: LiveOwner) => ({ owner, signal: AbortSignal.timeout(8000) });

async function pair(maxImports = 128) {
  const [a, b] = pipe();
  const pa = new DuplexPeer(), pb = new DuplexPeer({ role: 'server' });
  const sa = liveOver(pa, { maxImports }), sb = liveOver(pb, { maxImports });
  await Promise.all([pa.attach(a), pb.attach(b)]);
  return { sa, sb, close() { pa.close(); pb.close(); } };
}

function exportValue<T>(owner: LiveOwner, recipe: ValueAdapter<T>, value: T): unknown {
  return valueEnvironment(owner.scope).export(owner, active => recipe.export(active, value));
}

function importValue<T>(owner: LiveOwner, recipe: ValueAdapter<T>, raw: unknown): T {
  return valueEnvironment(owner.scope).import(owner, active => recipe.import(active, raw));
}

function transfer<T>(source: LiveOwner, destination: LiveOwner, recipe: ValueAdapter<T>, value: T): T {
  return importValue(destination, recipe, exportValue(source, recipe, value));
}

async function zero(scopes: LiveScope[]): Promise<void> {
  const deadline = Date.now() + 5000;
  for (const scope of scopes) {
    while (scope.counts().exports || scope.counts().imports) {
      assert(Date.now() < deadline, `bindings survived explicit release: ${JSON.stringify(scope.counts())}`);
      await new Promise(resolve => setTimeout(resolve, 1));
    }
  }
}

const plain = (offset: number): Unary => async value => value + offset;
const specimen = (offset: number): Held => ({ job: { run: plain(offset) }, progress: { label: 'fixed', notify: { run: plain(offset + 10) } } });
const packed = (value: Held): Batch => [null, { entry: { kind: 'value', value }, empty: { kind: 'empty' } }];
function unpack(value: Batch): Held {
  const entry = value[1]!.entry;
  assert(entry.kind === 'value');
  return entry.value;
}

async function rollback(): Promise<void> {
  const pairOfScopes = await pair(4), { sa, sb } = pairOfScopes;
  const sender = sa.owner().child(), receiver = sb.owner().child();
  const unrelatedA = sa.owner().child(), unrelatedB = sb.owner().child();
  const failed = sb.owner().child(), freshOwner = sa.owner().child();
  try {
    // Diagnostic wrappers observe the actual active transaction owner. They
    // do not select a lifetime and contain no captured connection or principal.
    const progress = numbers.family.types.Progress;
    let peakImports = 0, peakExports = 0;
    const observed: typeof progress = { ...progress,
      export(context, value) {
        assert(context instanceof LiveOwner);
        peakExports = Math.max(peakExports, context.scope.counts().exports);
        return progress.export(context, value);
      },
      import(context, raw) {
        assert(context instanceof LiveOwner);
        peakImports = Math.max(peakImports, context.scope.counts().imports);
        return progress.import(context, raw);
      },
    };
    const family = { ...numbers.family, types: { ...numbers.family.types, Progress: observed } };
    const recipe = holder.adapterBatch<numbers.Family>(family);
    const original = exportValue(sender, recipe, packed(specimen(10))) as unknown[];
    const borrowed = importValue(receiver, recipe, original);
    const otherB = transfer(unrelatedA, unrelatedB, unary, plain(100));
    const otherA = transfer(unrelatedB, unrelatedA, unary, plain(200));
    assert.deepEqual(sa.counts(), { exports: 3, imports: 1 });
    assert.deepEqual(sb.counts(), { exports: 1, imports: 3 });
    const fresh = exportValue(freshOwner, recipe, packed(specimen(30))) as unknown[];
    peakImports = 0;
    assert.throws(() => importValue(failed, recipe, [original[0], original[1], fresh[1]]), { code: 'too_many_imports' });
    assert.equal(peakImports, 4, 'no fresh nested attachment preceded failure');
    assert.deepEqual(failed.counts(), empty);
    assert.deepEqual(receiver.counts(), { exports: 0, imports: 2 });
    assert.deepEqual(unrelatedB.counts(), { exports: 1, imports: 1 });
    assert.deepEqual(sb.counts(), { exports: 1, imports: 3 });
    const bad = specimen(50);
    bad.progress.notify.run = undefined as unknown as Unary;
    peakExports = 0;
    assert.throws(() => exportValue(failed, recipe, [...packed(unpack(borrowed)), packed(bad)[1]]));
    assert(peakExports > 1, 'export failure happened before a fresh export');
    assert.deepEqual(failed.counts(), empty);
    assert.deepEqual(sb.counts(), { exports: 1, imports: 3 });
    assert.equal(await unpack(borrowed).job.run(1, invokeOptions(receiver)), 11);
    assert.equal(await unpack(borrowed).progress.notify.run(1, invokeOptions(receiver)), 21);
    assert.equal(await otherB(1, invokeOptions(unrelatedB)), 101);
    assert.equal(await otherA(1, invokeOptions(unrelatedA)), 201);
    failed.release();
    receiver.release();
    assert.equal(await otherB(2, invokeOptions(unrelatedB)), 102, 'releasing one child invalidated its sibling');
    assert.equal(await otherA(2, invokeOptions(unrelatedA)), 202, 'releasing one child invalidated an unrelated export');
    for (const owner of [sender, unrelatedA, unrelatedB, freshOwner]) owner.release();
    await zero([sa, sb]);
  } finally { pairOfScopes.close(); }
}

// Each synthetic consumer exposure owns its policy. Every protected invocation
// rereads it, including returned functions and a self-reference. This does not
// implement #356's exhaustive binding or authenticated principal selection.
class Policy {
  allowed = true;
  factories = 0;
  calls = 0;
  supplied = 0;
  readonly offset: number;
  constructor(offset: number) { this.offset = offset; }
  check() { if (!this.allowed) throw new DuplexError('denied', 'consumer exposure revoked'); }
  snapshot() { return [this.factories, this.calls, this.supplied]; }
  callback(): Unary {
    return async value => { this.check(); this.supplied++; return value + 3; };
  }
  function(): Factory {
    return async callback => {
      this.check(); this.factories++;
      return async (value, options) => {
        this.check(); this.calls++;
        return this.offset + await callback(value, options);
      };
    };
  }
  draw(): Held {
    const call: Unary = async value => { this.check(); this.calls++; return value + this.offset; };
    return { job: { run: call }, progress: { label: 'fixed', notify: { run: call } } };
  }
}

async function expose<T>(recipe: ValueAdapter<T>, makeValue: (policy: Policy) => T, offset: number) {
  const ab = await pair(), bc = await pair();
  const scopes = [ab.sa, ab.sb, bc.sa, bc.sb];
  const owners = scopes.map(scope => scope.owner().child());
  const policy = new Policy(offset);
  const raw = exportValue(owners[0], recipe, makeValue(policy));
  const self = importValue(owners[0], recipe, raw);
  const remote = importValue(owners[1], recipe, raw);
  const returned = transfer(owners[1], owners[0], recipe, remote);
  // The intermediate import belongs to A-B; its exported wrapper belongs to
  // B-C. The same recipe object crosses both independently created scopes.
  const forwarded = transfer(owners[2], owners[3], recipe, remote);
  return { scopes, owners, policy, values: [self, remote, returned, forwarded],
    async release() {
      for (const owner of owners) owner.release();
      // Origin conversion of a forwarded higher-order call uses its own
      // connection's documented root fallback for a foreign supplied owner.
      for (const scope of scopes) scope.owner().release();
      await zero(scopes);
    },
    close() { ab.close(); bc.close(); },
  };
}

async function exerciseExposures<T>(recipe: ValueAdapter<T>, makeValue: (p: Policy) => T,
  capture: (value: T, owner: LiveOwner, policy: Policy) => Promise<() => Promise<void>>): Promise<void> {
  // One immutable generated interpretation exists before either exposure.
  const exposures = await Promise.all([10, 20].map(offset => expose(recipe, makeValue, offset)));
  try {
    for (const exposure of exposures) for (const scope of exposure.scopes) assert.deepEqual(scope.owner().counts(), empty, 'explicit child conversion allocated in the root owner');
    const retained = await Promise.all(exposures.map(e => Promise.all(e.values.map((value, index) =>
      capture(value, e.owners[index === 1 ? 1 : index === 3 ? 3 : 0], e.policy)))));
    await Promise.all(retained.flat().map(invoke => invoke()));
    exposures[0].policy.allowed = false;
    let before = exposures[0].policy.snapshot();
    for (const invoke of retained[0]) await assert.rejects(invoke, { code: 'denied' });
    assert.deepEqual(exposures[0].policy.snapshot(), before, 'revocation allowed a protected effect');
    const secondCounts = exposures[1].scopes.map(scope => scope.counts());
    await exposures[0].release();
    assert.deepEqual(exposures[1].scopes.map(scope => scope.counts()), secondCounts, 'release crossed independent scopes');
    for (const invoke of retained[1]) await invoke();
    exposures[1].policy.allowed = false;
    before = exposures[1].policy.snapshot();
    for (const invoke of retained[1]) await assert.rejects(invoke, { code: 'denied' });
    assert.deepEqual(exposures[1].policy.snapshot(), before, 'second policy bypassed revocation');
    await exposures[1].release();
  } finally { for (const exposure of exposures) exposure.close(); }
}

await rollback();
for (const recipe of [functions.adapterFunction(unary, unary), functions.adapterFactory()]) {
  await exerciseExposures(recipe, policy => policy.function(), async (value, owner, policy) => {
    const returned = await value(policy.callback(), invokeOptions(owner));
    // The supplied-call result has settled before any retained invocation.
    return async () => { assert.equal(await returned(5, invokeOptions(owner)), policy.offset + 8, 'wrong exposure selected'); };
  });
}
await exerciseExposures(holder.adapterValue<numbers.Family>(numbers.family), policy => policy.draw(), async (value, owner, policy) => {
  return async () => {
    assert.equal(value.progress.label, 'fixed');
    assert.equal(await value.job.run(5, invokeOptions(owner)), policy.offset + 5, 'wrong Job exposure selected');
    assert.equal(await value.progress.notify.run(5, invokeOptions(owner)), policy.offset + 5, 'wrong Progress exposure selected');
  };
});
