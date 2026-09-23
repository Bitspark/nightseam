import assert from 'node:assert/strict';
import test from 'node:test';
import { at, Declared, pipe, refusingOrigin } from '@nightseam/duplex';
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

// A caller cancels through the same captured access and path. Plain composition
// delegates it unchanged; the destination profile owns the admitted invocation.
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
    const tree = (operation: string) => {
      const branch = Declared.compose(refusingOrigin, [['run', at(caller, ['svc', operation])]]).bind();
      // This consumer guard owns admission checks and passes controls through.
      const guard: Wire = {
        send(path, message) {
          if (message.frame.kind === 'request' || message.frame.kind === 'event') checks++;
          branch.send(path, message);
        },
      };
      return Declared.compose(refusingOrigin, [['svc', guard]]);
    };
    let root = tree('held');
    const { origin, children } = root.decompose();
    const rebuilt = Declared.compose(origin, children);
    for (const [access, wire, path] of [
      ['bound', root.bind(), ['svc', 'run']],
      ['selected', at(root.bind(), ['svc']), ['run']],
      ['reconstructed', rebuilt.bind(), ['svc', 'run']],
    ] as [string, Wire, string[]][]) {
      arrived = deferred();
      ended = deferred();
      const controller = new AbortController();
      const pending = callWire(wire, path, null, { signal: controller.signal }).catch((error: unknown) => error);
      await arrived.promise;
      const before = checks;
      // Rebuild from the parts and rebind the route to another handler.
      const { origin, children } = root.decompose();
      Declared.compose(origin, children);
      root = tree('replacement');
      controller.abort();
      assert.equal(((await pending) as DuplexError).code, 'cancelled', access);
      await ended.promise;
      assert.deepEqual(cancelled, started, access);
      assert.equal(checks, before, `${access}: the cancel entered admission again`);
      root = tree('held');
    }
    assert.deepEqual(started, ['held', 'held', 'held']);
  });
}
