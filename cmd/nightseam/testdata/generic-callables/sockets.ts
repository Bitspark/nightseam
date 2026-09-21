import assert from 'node:assert/strict';
import * as functions from '@example/functions-client/types';
import * as cell from '@example/cell-binding';
import { DuplexError, DuplexPeer, forwardWire, jsonAdapter, type ValueAdapter } from '@nightseam/runtime';
import { liveOver, valueEnvironment, type LiveOwner, type LiveScope } from '@nightseam/live';

type Unary = functions.Function<number, number>;
type Factory = functions.Function<Unary, Unary>;
type Bundle = functions.Bundle<Unary>;
const url = process.argv[2]!;
const role = process.argv[3]!;
const number = jsonAdapter<number>({ type: 'integer', validate: functions.validateWire });
// All conversion objects are constructed before either physical scope exists.
const unary = functions.adapterFunction(number, number);
const closedAlias = functions.adapterIntFunction();
const factory = functions.adapterFunction(unary, unary);
const bundle = functions.adapterBundle(unary);
const empty = { exports: 0, imports: 0 };
const options = () => ({ signal: AbortSignal.timeout(8000) });

function guarded(minimum: number): Unary {
  return async n => {
    if (n < minimum) throw new DuplexError('denied', `minimum ${minimum}`);
    return n + 3;
  };
}

function makeFactory(seed: number): Factory {
  return async callback => async (n, options) => seed + await callback(n, options);
}

// Neither the generic state nor either method selects a concrete converter.
function memoryCell<T>() {
  let value: T;
  let revision = 0;
  const owners: LiveOwner[] = [];
  const retain = (context: unknown) => {
    const owner = (context as { valueContext?: LiveOwner })?.valueContext;
    assert(owner, 'generated handler has no active value lifetime');
    owners.push(owner);
  };
  const model: cell.ServerModel<T> = () => ({ methods: {
    replace(input, context) { retain(context); value = input.value; return ++revision; },
    get(_params, context) { retain(context); return value; },
  }, events: {} });
  return { model, drop() { for (const owner of owners.splice(0)) owner.release(); } };
}

async function zero(scope: LiveScope): Promise<void> {
  const deadline = Date.now() + 8000;
  while (scope.counts().exports || scope.counts().imports) {
    assert(Date.now() < deadline, 'bindings remained before teardown: ' + JSON.stringify(scope.counts()));
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}

function connection() {
  const peer = new DuplexPeer();
  const scope = liveOver(peer, { maxImports: 16, maxExports: 128 });
  const environment = valueEnvironment(scope);
  const state = memoryCell<Factory>();
  peer.handle('test.drop', () => { state.drop(); return null; });
  const prepared = role === 'go-server' ? cell.prepareFromWire(peer.wire(), { valueEnvironment: environment }, factory) : undefined;
  const wire = role === 'typescript-server' ? cell.toWire(state.model, { valueEnvironment: environment }, factory) : undefined;
  const detach = wire && forwardWire(peer.wire(), wire);
  return { peer, scope, environment, prepared,
    close() { state.drop(); prepared?.close(); detach?.(); wire?.close(); peer.close(); } };
}

type Connection = ReturnType<typeof connection>;

function exportValue<T>(c: Connection, owner: LiveOwner, adapter: ValueAdapter<T>, value: T): unknown {
  return c.environment.export(owner, active => adapter.export(active, value));
}

function importValue<T>(c: Connection, owner: LiveOwner, adapter: ValueAdapter<T>, raw: unknown): T {
  return c.environment.import(owner, active => adapter.import(active, raw));
}

async function generatedCalls(c: Connection): Promise<void> {
  if (role === 'go-server') {
    const model = await c.prepared!.complete(options());
    const access = model({ methods: {}, events: {} });
    const owner = c.scope.owner().child();
    const call = () => ({ ...options(), valueContext: owner });
    const invoke = () => ({ ...options(), owner });
    assert.equal(await access.methods.replace({ value: makeFactory(100) }, call()), 1);
    const first = await access.methods.get({}, call());
    assert.equal(await access.methods.replace({ value: makeFactory(200) }, call()), 2);
    const second = await access.methods.get({}, call());
    const one = await first(guarded(0), invoke());
    const two = await second(guarded(10), invoke());
    // Both factory calls have returned. The returned functions still import
    // and invoke their distinct supplied callback instances on the live scope.
    assert.equal(await one(5, invoke()), 108);
    assert.equal(await two(15, invoke()), 218);
    await assert.rejects(() => one(-1, invoke()), { code: 'denied' });
    await assert.rejects(() => two(5, invoke()), { code: 'denied' });
    assert.equal(await one(5, invoke()), 108);
    assert.equal(await access.methods.replace({ value: first }, call()), 3);
    const roundtrip = await access.methods.get({}, call());
    const three = await roundtrip(guarded(20), invoke());
    assert.equal(await three(21, invoke()), 124);
    await assert.rejects(() => three(19, invoke()), { code: 'denied' });
    assert.equal(await one(5, invoke()), 108, 'cell replacement invalidated an earlier returned function');
    assert(owner.counts().exports > 0 && owner.counts().imports > 0);
    assert.deepEqual(c.scope.owner().counts(), empty, 'generic adapter captured the root lifetime');
    owner.release();
    await c.peer.call('test.drop', {}, options());
  } else {
    assert.deepEqual(await c.peer.call('test.exercise', {}, options()), { values: [108, 218, 108], released: empty });
  }
  await zero(c.scope);
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), empty);
}

function malformedIdentities(raw: unknown): unknown[] {
  const value = raw as { binding: string; contract: string; digest?: string };
  assert.match(value.contract, /^functions\/Function</);
  const { digest: _digest, ...withoutDigest } = value;
  return [
    { ...value, contract: value.contract.replace('integer', 'string') },
    { ...withoutDigest, contract: value.contract.replace('integer', 'string') },
    { ...value, contract: value.contract.replace('/Function<', '/Other<') },
    { ...value, digest: '0'.repeat(64) },
  ];
}

async function identityBoundaries(c: Connection): Promise<void> {
  const owner = c.scope.owner().child();
  const remote = await c.peer.call('test.identity_export', {}, options());
  for (const bad of malformedIdentities(remote)) {
    assert.throws(() => importValue(c, owner, unary, bad), 'bad applied contract reached invocation');
    assert.deepEqual(owner.counts(), empty);
  }
  assert.equal(await c.peer.call('test.effects', {}, options()), 0, 'mismatch invoked the protected Go handler');
  let effects = 0;
  const raw = exportValue(c, owner, unary, async n => { effects++; return n + 1; });
  for (const bad of malformedIdentities(raw)) {
    const result = await c.peer.call('test.identity_import', bad, options());
    assert.deepEqual(result, { refused: true, counts: { exports: 1, imports: 0 } });
  }
  assert.equal(effects, 0, 'mismatch invoked the protected TypeScript handler');
  // The source alias and the dynamically applied constructor interoperate.
  const accepted = importValue(c, owner, closedAlias, remote);
  assert.equal(await accepted(1, { ...options(), owner }), 2);
  owner.release();
  await c.peer.call('test.drop', {}, options());
  await zero(c.scope);
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), empty);
}

function specimen(seed: number, length: number): Bundle {
  const fn = (offset: number): Unary => async n => {
    if (n < 0) throw new DuplexError('denied', 'negative');
    return seed + offset + n;
  };
  return { borrowed: fn(0), fresh: Array.from({ length }, (_, i) => fn(i + 1)) };
}

function borrow(original: unknown, fresh: unknown): unknown {
  return { ...(fresh as object), borrowed: (original as { borrowed: unknown }).borrowed };
}

async function rollback(c: Connection): Promise<void> {
  const sender = c.scope.owner().child(), receiver = c.scope.owner().child();
  const failed = c.scope.owner().child();
  const original = exportValue(c, sender, bundle, specimen(200, 2));
  assert.deepEqual(await c.peer.call('test.import', { value: original }, options()), { refused: false, seen: 201, counts: { exports: 0, imports: 3 } });
  const fresh = exportValue(c, sender, bundle, specimen(300, 14));
  assert.deepEqual(await c.peer.call('test.import', { value: borrow(original, fresh), failure: true }, options()), { refused: true, seen: 201, counts: { exports: 0, imports: 3 } });
  const remoteOriginal = await c.peer.call('test.export', { seed: 400, length: 2 }, options());
  const retained = importValue(c, receiver, bundle, remoteOriginal);
  const remoteFresh = await c.peer.call('test.export', { seed: 500, length: 14 }, options());
  assert.throws(() => importValue(c, failed, bundle, borrow(remoteOriginal, remoteFresh)), { code: 'too_many_imports' });
  assert.deepEqual(failed.counts(), empty, 'partial import kept fresh attachments');
  assert.deepEqual(receiver.counts(), { exports: 0, imports: 3 }, 'partial import released borrowed attachments');
  assert.equal(await retained.borrowed(1, options()), 401);
  await assert.rejects(() => retained.borrowed(-1, options()), { code: 'denied' });
  const before = c.scope.counts();
  assert.throws(() => exportValue(c, failed, bundle, { borrowed: retained.borrowed, fresh: [guarded(0), undefined as unknown as Unary] }));
  assert.deepEqual(failed.counts(), empty, 'partial export kept fresh bindings');
  assert.deepEqual(c.scope.counts(), before);
  assert.equal(await retained.borrowed(1, options()), 401);
  await assert.rejects(() => retained.borrowed(-1, options()), { code: 'denied' });
  assert.deepEqual(await c.peer.call('test.bad_export', {}, options()), { seen: 201, counts: { exports: 5, imports: 3 } });
  assert.deepEqual(c.scope.counts(), { exports: 5, imports: 3 });
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), { exports: 5, imports: 3 });
  for (const owner of [failed, receiver, sender]) owner.release();
  await c.peer.call('test.drop', {}, options());
  await zero(c.scope);
  assert.deepEqual(await c.peer.call('test.counts', {}, options()), empty);
  await assert.rejects(() => retained.borrowed(1, options()), { code: 'reference_released' });
}

const connections = [connection(), connection()];
try {
  await Promise.all(connections.map(c => c.peer.connect(url)));
  await Promise.all(connections.map(generatedCalls));
  await Promise.all(connections.map(identityBoundaries));
  await Promise.all(connections.map(rollback));
} finally {
  for (const c of connections) c.close();
}
