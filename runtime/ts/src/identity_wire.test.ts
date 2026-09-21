import assert from 'node:assert/strict';
import test from 'node:test';
import type { Wire } from '@nightseam/duplex';
import { IDENTITY_METHOD, identityHandler } from './identity.ts';
import { prepareIdentity } from './identity_wire.ts';
import { callWire, handleWire, emitWire, onWireEvent } from './wire.ts';
import { wirePair } from './wire-pair.ts';

const expected = { path: 'service', digest: 'a'.repeat(64) };
function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
function observing(wire: Wire, observe: (kind: string) => void): Wire {
  return {
    ...wire,
    receive(path, receiver) {
      return wire.receive(path, {
        ...receiver,
        message(path, message) {
          observe(message.frame.kind);
          return receiver.message?.(path, message);
        },
      });
    },
  };
}

for (const absent of [false, true])
  test(`identity preparation retains the earliest event through check and binding (${absent ? 'absent' : 'matching'})`, async (t) => {
    const [near, far] = wirePair();
    t.after(() => near.close());
    if (!absent) handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
    const arrived = deferred(),
      delivered = deferred<number>();
    const preparation = prepareIdentity(
      observing(near, () => arrived.resolve()),
      expected,
    );
    t.after(() => preparation.close());
    let effects = 0;
    onWireEvent(preparation.wire, ['event'], (value) => {
      effects++;
      delivered.resolve(value as number);
    });
    emitWire(far, ['event'], 7);
    await arrived.promise;
    await preparation.check();
    assert.equal(effects, 0, 'checked but not yet bound');
    preparation.ready();
    assert.equal(await delivered.promise, 7);
    assert.equal(effects, 1);
  });

test('identity mismatch discards a held event and preserves other users of the carrier', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler({ ...expected, digest: 'b'.repeat(64) }));
  const arrived = deferred();
  const preparation = prepareIdentity(
    observing(near, () => arrived.resolve()),
    expected,
  );
  t.after(() => preparation.close());
  let effects = 0;
  onWireEvent(preparation.wire, ['event'], () => {
    effects++;
  });
  handleWire(near, ['unrelated'], () => 19);
  emitWire(far, ['event'], 7);
  await arrived.promise;
  await assert.rejects(preparation.check(), { code: 'contract_mismatch' });
  assert.throws(() => preparation.ready(), { code: 'contract_mismatch' });
  assert.equal(await callWire(far, ['unrelated']), 19);
  assert.equal(effects, 0);
  assert.doesNotThrow(() => onWireEvent(near, ['event'], () => {}));
});

test('deferred request does not block the root and cancellation never dispatches later', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
  const arrived = deferred(),
    cancelled = deferred();
  const preparation = prepareIdentity(
    observing(near, (kind) => {
      if (kind === 'request') arrived.resolve();
      if (kind === 'cancel') cancelled.resolve();
    }),
    expected,
  );
  t.after(() => preparation.close());
  let effects = 0;
  handleWire(preparation.wire, ['model'], () => {
    effects++;
    return 11;
  });
  handleWire(near, ['unrelated'], () => 7);
  const controller = new AbortController();
  const first = callWire(far, ['model'], null, { signal: controller.signal }).catch((error: unknown) => error);
  await arrived.promise;
  assert.equal(await callWire(far, ['unrelated']), 7);
  controller.abort();
  assert.equal(((await first) as { code: string }).code, 'cancelled');
  await cancelled.promise;
  await preparation.check();
  preparation.ready();
  assert.equal(await callWire(far, ['model']), 11);
  assert.equal(effects, 1);
});

for (const checked of [false, true])
  test(`preparation bounds a factory ${checked ? 'never bound' : 'never started'}`, async (t) => {
    const [near, far] = wirePair();
    t.after(() => near.close());
    handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
    const preparation = prepareIdentity(near, expected, { requestTimeoutMs: 250 });
    t.after(() => preparation.close());
    handleWire(preparation.wire, ['model'], () => {
      throw new Error('expired model dispatched');
    });
    if (checked) await preparation.check();
    await assert.rejects(callWire(far, ['model']), { code: 'cancelled' });
    assert.throws(() => preparation.ready(), { code: 'cancelled' });
    assert.doesNotThrow(() => handleWire(near, ['model'], () => 1));
    const replacement = prepareIdentity(near, expected);
    replacement.close();
  });

test('cancellation and explicit close abandon only their interpretation', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  const preparation = prepareIdentity(near, expected);
  t.after(() => preparation.close());
  handleWire(preparation.wire, ['model'], () => {
    throw new Error('cancelled model dispatched');
  });
  handleWire(near, ['unrelated'], () => 7);
  await assert.rejects(preparation.check({ signal: AbortSignal.abort() }), { code: 'cancelled' });
  assert.equal(await callWire(far, ['unrelated']), 7);
  const second = prepareIdentity(near, expected);
  handleWire(second.wire, ['model'], () => 1);
  second.close();
  assert.throws(() => second.ready(), { code: 'disconnected' });
  assert.equal(await callWire(far, ['unrelated']), 7);
});

test('carrier closure releases held events and model requests', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  const arrived = deferred(),
    closed = deferred();
  const preparation = prepareIdentity(
    observing(near, () => arrived.resolve()),
    expected,
  );

  preparation.wire.receive(['event'], {
    message: () => {
      throw new Error('closed model dispatched');
    },
    closed: () => closed.resolve(),
  });
  emitWire(far, ['event']);
  await arrived.promise;
  far.close();
  await closed.promise;
  assert.throws(() => preparation.ready(), { code: 'disconnected' });
});

test('deferred request budget is bounded separately from the carrier budget', async (t) => {
  const [near, far] = wirePair({ maxConcurrentHandlers: 8 });
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
  const arrived = deferred();
  const preparation = prepareIdentity(
    observing(near, () => arrived.resolve()),
    expected,
    { maxConcurrentHandlers: 1 },
  );
  t.after(() => preparation.close());
  handleWire(preparation.wire, ['model'], () => 11);
  const first = callWire(far, ['model']);
  await arrived.promise;
  await assert.rejects(callWire(far, ['model']), { code: 'busy' });
  await preparation.check();
  preparation.ready();
  assert.equal(await first, 11);
});

test('readiness cannot precede the check, and check/ready are once only', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
  const preparation = prepareIdentity(near, expected);
  t.after(() => preparation.close());
  assert.throws(() => preparation.ready(), /not been checked/);
  await assert.rejects(callWire(preparation.wire, ['model']), { code: 'busy' });
  await preparation.check();
  await assert.rejects(preparation.check(), /already started/);
  preparation.ready();
  assert.throws(() => preparation.ready(), /already ready/);
});

test('identity mismatch refuses an already deferred model request', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler({ ...expected, digest: 'b'.repeat(64) }));
  const arrived = deferred();
  const preparation = prepareIdentity(
    observing(near, () => arrived.resolve()),
    expected,
  );
  t.after(() => preparation.close());
  handleWire(preparation.wire, ['model'], () => {
    throw new Error('mismatched request dispatched');
  });
  const pending = callWire(far, ['model']).catch((error: unknown) => error);
  await arrived.promise;
  await assert.rejects(preparation.check(), { code: 'contract_mismatch' });
  assert.equal(((await pending) as { code: string }).code, 'contract_mismatch');
});

test('closing immediately after ready answers each deferred request only once', async (t) => {
  const [near, far] = wirePair();
  t.after(() => near.close());
  handleWire(far, [IDENTITY_METHOD], identityHandler(expected));
  const arrived = deferred();
  let responses = 0;
  const source: Wire = {
    ...near,
    receive(path, receiver) {
      return near.receive(path, {
        ...receiver,
        message(path, message) {
          if (path[0] === 'model' && message.frame.kind === 'request' && message.return) {
            const original = message.return;
            arrived.resolve();
            return receiver.message?.(path, {
              ...message,
              return: {
                wire: {
                  send(path, response) {
                    responses++;
                    original.wire.send(path, response);
                  },
                  receive: (path, receiver) => original.wire.receive(path, receiver),
                  close: (code, reason) => original.wire.close(code, reason),
                },
              },
            });
          }
          return receiver.message?.(path, message);
        },
      });
    },
  };
  const preparation = prepareIdentity(source, expected);
  handleWire(preparation.wire, ['model'], () => {
    throw new Error('closed model dispatched');
  });
  const pending = callWire(far, ['model']).catch((error: unknown) => error);
  await arrived.promise;
  await preparation.check();
  preparation.ready();
  preparation.close();
  assert.equal(((await pending) as { code: string }).code, 'disconnected');
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(responses, 1);
});
