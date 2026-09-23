import assert from 'node:assert/strict';
import test from 'node:test';
import { at, Declared, permitAdmission, pipe, refusingOrigin, type AdmissionPolicy } from '@nightseam/duplex';
import type { Endpoint, Wire } from '@bitspark/bitwire';
import { DuplexPeer, type DuplexError } from './peer.ts';
import { callWire, handleWire } from './wire.ts';
import { createDispatcher } from './dispatcher.ts';
import { wirePair } from './wire-pair.ts';

function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}

const carriers: Record<string, () => Promise<{ caller: Endpoint; callee: Endpoint; close(): void }>> = {
  'a local pair': async () => {
    const [caller, callee] = wirePair();
    return { caller, callee, close: () => (caller.close(), callee.close()) };
  },
  peers: async () => {
    const [a, b] = pipe();
    const left = new DuplexPeer(),
      right = new DuplexPeer({ role: 'server' });
    await Promise.all([left.attach(a), right.attach(b)]);
    return { caller: left.wire(), callee: right.wire(), close: () => (left.close(), right.close()) };
  },
};

// A caller cancels through the access it called through. Declared access routes
// that cancel to the destination which admitted the call, even after the tree is
// rebuilt and rebound, without another admission check.
for (const [name, carrier] of Object.entries(carriers)) {
  test(`declared access carries its caller's cancellation over ${name}`, async (t) => {
    const { caller, callee, close } = await carrier();
    t.after(close);
    const dispatcher = createDispatcher(callee);
    t.after(() => dispatcher.close());
    const started: string[] = [],
      cancelled: string[] = [];
    let arrived = deferred(),
      ended = deferred();
    for (const operation of ['held', 'replacement']) {
      handleWire(dispatcher, ['svc', operation], async (_params, context) => {
        started.push(operation);
        arrived.resolve();
        await new Promise<void>((done) => context.signal.addEventListener('abort', () => done(), { once: true }));
        cancelled.push(operation);
        ended.resolve();
        return null;
      });
    }
    let checks = 0;
    const quota: AdmissionPolicy = { admit: () => void checks++ };
    const tree = (operation: string) =>
      Declared.compose({ own: refusingOrigin, policy: quota }, [
        [
          'svc',
          Declared.compose({ own: refusingOrigin, policy: permitAdmission }, [
            ['run', Declared.compose({ own: at(caller, ['svc', operation]), policy: permitAdmission }, [])],
          ]),
        ],
      ]);
    let root = tree('held');
    for (const [access, wire, path] of [
      ['bound', root.bind(), ['svc', 'run']],
      ['selected', at(root.bind(), ['svc']), ['run']],
    ] as [string, Wire, string[]][]) {
      arrived = deferred();
      ended = deferred();
      const controller = new AbortController();
      const pending = callWire(wire, path, null, { signal: controller.signal }).catch((error: unknown) => error);
      await arrived.promise;
      const before = checks;
      // Rebuild from the parts and rebind the route to another handler.
      const { value, children } = root.decompose();
      Declared.compose(value, children);
      root = tree('replacement');
      controller.abort();
      assert.equal(((await pending) as DuplexError).code, 'cancelled', access);
      await ended.promise;
      assert.deepEqual(cancelled, started, access);
      assert.equal(checks, before, `${access}: the cancel entered admission again`);
      root = tree('held');
    }
    assert.deepEqual(started, ['held', 'held']);
  });
}
