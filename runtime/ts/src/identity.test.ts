import assert from 'node:assert/strict';
import { test, type TestContext } from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexPeer, DuplexError, type RequestHandler, type CallOptions } from './peer.ts';
import { checkIdentity, identityHandler, IDENTITY_METHOD, type DeclarationIdentity } from './identity.ts';

const ONE = '4433469c3fb5e66b667a7b4463cb878ab59d214bb1f9163e92bc2005b9987cc3';
const TWO = 'c4dbce5dafcb23c6863de70876927aee504ca5323397908f8abacebc089322b1';

async function pair(t: TestContext, handlers: Record<string, RequestHandler> = {}): Promise<DuplexPeer> {
  const [a, b] = pipe();
  const client = new DuplexPeer({ role: 'client' }),
    server = new DuplexPeer({ role: 'server' });
  t.after(() => {
    client.close();
    server.close();
  });
  for (const [name, handler] of Object.entries(handlers)) server.handle(name, handler);
  await Promise.all([client.attach(a), server.attach(b)]);
  return client;
}

test('identity exchange precedes model effects and leaves the carrier open', async (t) => {
  for (const remote of [
    { path: '世界/service', digest: ONE },
    { path: '世界/service', digest: TWO },
    { path: 'other', digest: ONE },
    { path: '世界/service' },
  ]) {
    let effects = 0;
    const peer = await pair(t, {
      [IDENTITY_METHOD]: identityHandler(remote),
      model: () => {
        effects++;
        return null;
      },
    });
    const check = checkIdentity(peer.call.bind(peer), { path: '世界/service', digest: ONE });
    if (remote.path !== '世界/service' || remote.digest === TWO) {
      await assert.rejects(
        check,
        (error: unknown) =>
          error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes(remote.path),
      );
    } else await check;
    assert.equal(effects, 0);
    await peer.call('model', null);
    assert.equal(effects, 1);
  }
  const peer = await pair(t, { [IDENTITY_METHOD]: identityHandler({ path: 'same', digest: ONE }) });
  await checkIdentity(peer.call.bind(peer), { path: 'same' });
});

test('identity exchange accepts only method_not_found as absent identity', async (t) => {
  const empty = await pair(t);
  await checkIdentity(empty.call.bind(empty), { path: 'same', digest: ONE });
  for (const code of ['contract_invalid', 'busy', 'internal', 'contract_mismatch']) {
    const peer = await pair(t, {
      [IDENTITY_METHOD]: () => {
        throw new DuplexError(code, 'refused');
      },
    });
    await assert.rejects(checkIdentity(peer.call.bind(peer), { path: 'same' }), { code });
  }
  const closed = await pair(t);
  closed.close();
  await assert.rejects(checkIdentity(closed.call.bind(closed), { path: 'same' }), { code: 'not_connected' });
});

test('identity exchange refuses malformed request and response identity', async (t) => {
  const malformed: unknown[] = [
    null,
    [],
    {},
    { path: '' },
    { path: 1 },
    { path: 'same', extra: true },
    ...['', null, 1, 'short', 'A'.repeat(64), ONE + '\n'].map((digest) => ({ path: 'same', digest })),
  ];
  const peer = await pair(t, { [IDENTITY_METHOD]: identityHandler({ path: 'same', digest: ONE }) });
  for (const raw of malformed) {
    await assert.rejects(peer.call(IDENTITY_METHOD, raw), { code: 'contract_invalid' });
    const remote = await pair(t, { [IDENTITY_METHOD]: () => raw });
    await assert.rejects(checkIdentity(remote.call.bind(remote), { path: 'same' }), { code: 'contract_invalid' });
  }
  for (const remote of [
    { path: 'same', digest: TWO },
    { path: 'other', digest: ONE },
  ]) {
    const peer = await pair(t, { [IDENTITY_METHOD]: () => remote });
    await assert.rejects(
      checkIdentity(peer.call.bind(peer), { path: 'same', digest: ONE }),
      (error: unknown) =>
        error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes('same'),
    );
  }
  for (const expected of [{}, { path: '\uD800' }, { path: 'same', digest: '' }, { path: 'same', digest: null }]) {
    assert.throws(() => identityHandler(expected as DeclarationIdentity), { code: 'contract_invalid' });
    let called = false;
    await assert.rejects(
      checkIdentity(async () => {
        called = true;
        return null;
      }, expected as DeclarationIdentity),
      { code: 'contract_invalid' },
    );
    assert.equal(called, false);
  }
  const handler = identityHandler({ path: 'same' });
  for (const raw of [
    Object.create({ path: 'same' }) as unknown,
    { path: 'same', digest: undefined },
    { path: '\uD800' },
  ]) {
    assert.throws(() => handler(raw), { code: 'contract_invalid' });
  }
});

test('identity checks preserve bounded caller options and cancel on timeout', async (t) => {
  let cancelled!: () => void;
  const didCancel = new Promise<void>((resolve) => {
    cancelled = resolve;
  });
  const peer = await pair(t, {
    [IDENTITY_METHOD]: async (_raw, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener(
          'abort',
          () => {
            cancelled();
            resolve(null);
          },
          { once: true },
        );
      }),
  });
  await assert.rejects(checkIdentity(peer.call.bind(peer), { path: 'same' }, { timeoutMs: 30 }), {
    code: 'request_timeout',
  });
  await didCancel;
  const controller = new AbortController();
  for (const options of [undefined, { timeoutMs: 123, signal: controller.signal, meta: { tenant: 'a' } }]) {
    let calls = 0;
    await checkIdentity(
      async (method, params, actual?: CallOptions) => {
        calls++;
        assert.equal(method, IDENTITY_METHOD);
        assert.deepEqual(params, { path: 'same' });
        assert.equal(actual?.timeoutMs, options?.timeoutMs ?? 30_000);
        assert.equal(actual?.signal, options?.signal);
        assert.equal(actual?.meta, options?.meta);
        throw new DuplexError('method_not_found', 'missing');
      },
      { path: 'same', digest: undefined },
      options,
    );
    assert.equal(calls, 1);
  }
});

test('identity handler snapshots its own declaration', async (t) => {
  const expected = { path: 'same', digest: ONE };
  const handler = identityHandler(expected);
  expected.digest = TWO;
  const peer = await pair(t, { [IDENTITY_METHOD]: handler });
  await checkIdentity(peer.call.bind(peer), { path: 'same', digest: ONE });
});

test('identity exchange preserves invalid timeout refusals', async (t) => {
  let called = 0;
  const peer = await pair(t, {
    [IDENTITY_METHOD]: () => {
      called++;
      return { path: 'same' };
    },
  });
  for (const timeoutMs of [null, 0, -1, Infinity, NaN]) {
    await assert.rejects(checkIdentity(peer.call.bind(peer), { path: 'same' }, { timeoutMs } as CallOptions), {
      code: 'invalid_options',
    });
  }
  assert.equal(called, 0);
});
