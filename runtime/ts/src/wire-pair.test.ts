import assert from 'node:assert/strict';
import test from 'node:test';
import { wirePair } from './wire-pair.ts';
import { callWire, handleWire, emitWire, onWireEvent, setWireContext } from './wire.ts';
import type { Message, ReturnAddress, Wire } from '@bitspark/bitwire';
import { createDispatcher } from './dispatcher.ts';

function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}

test('local pair supports reverse calls and independent construction', async (t) => {
  const [a, b] = wirePair();
  const [x, y] = wirePair();
  const leftDispatcher = createDispatcher(a);
  const rightDispatcher = createDispatcher(b);
  const independentDispatcher = createDispatcher(y);
  t.after(() => {
    leftDispatcher.close();
    rightDispatcher.close();
    independentDispatcher.close();
    a.close();
    x.close();
  });
  handleWire(leftDispatcher, ['reverse'], (value) => value);
  handleWire(rightDispatcher, ['call'], (value) => callWire(b, ['reverse'], value));
  handleWire(independentDispatcher, ['call'], () => 'independent');
  assert.equal(await callWire(a, ['call'], 7), 7);
  a.close();
  assert.equal(await callWire(x, ['call']), 'independent');
});

test('local pending budget lives until the response and can then be reused', async (t) => {
  const [a, b] = wirePair({ maxPendingRequests: 1 });
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const started = deferred(),
    release = deferred();
  handleWire(dispatcher, ['hold'], async () => {
    started.resolve();
    await release.promise;
    return 1;
  });
  const first = callWire(a, ['hold']);
  await started.promise;
  await assert.rejects(callWire(a, ['hold']), { code: 'busy' });
  release.resolve();
  assert.equal(await first, 1);
  assert.equal(await callWire(a, ['hold']), 1);
});

test('local events await serially and cancellation owns capacity outside the full data queue', async (t) => {
  const [a, b] = wirePair({ queueCapacity: 1, maxPendingRequests: 1 });
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const started = deferred(),
    cancelled = deferred(),
    entered = deferred(),
    release = deferred(),
    drained = deferred();
  const seen: number[] = [];
  handleWire(dispatcher, ['hold'], async (_params, context) => {
    started.resolve();
    await new Promise<void>((resolve) =>
      context.signal.addEventListener(
        'abort',
        () => {
          cancelled.resolve();
          resolve();
        },
        { once: true },
      ),
    );
  });
  onWireEvent(dispatcher, ['event'], async (value) => {
    seen.push(value as number);
    if (value === 1) {
      entered.resolve();
      await release.promise;
    } else drained.resolve();
  });
  const controller = new AbortController();
  const first = callWire(a, ['hold'], null, { signal: controller.signal }).catch((error: unknown) => error);
  await started.promise;
  emitWire(a, ['event'], 1);
  await entered.promise;
  emitWire(a, ['event'], 2);
  controller.abort();
  assert.equal(((await first) as { code: string }).code, 'cancelled');
  release.resolve();
  await Promise.all([cancelled.promise, drained.promise]);
  assert.deepEqual(seen, [1, 2]);
});

test('a full local carrier closes even while its event consumer is held', async (t) => {
  const [a, b] = wirePair({ queueCapacity: 1 });
  t.after(() => a.close());
  const entered = deferred(),
    release = deferred(),
    ended = deferred();
  b.receive({
    message: async () => {
      entered.resolve();
      await release.promise;
    },
    closed: () => ended.resolve(),
  });
  emitWire(a, ['event']);
  await entered.promise;
  emitWire(a, ['event']);
  assert.throws(() => emitWire(a, ['event']), { code: 'busy' });
  await ended.promise;
  release.resolve();
});

test('request and cancellation use one mapped return capability and responses retire on throwing returns', async (t) => {
  const [a, b] = wirePair({ maxPendingRequests: 1 });
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const received = deferred<Message>(),
    cancelled = deferred<Message>();
  dispatcher.register(['raw'], {
    message: (_path, message) => {
      (message.frame.kind === 'request' ? received : cancelled).resolve(message);
    },
  });
  const returning: ReturnAddress = {
    wire: {
      send: () => {
        throw new Error('return failed');
      },
    },
  };
  a.send(['raw'], { frame: { version: 1, kind: 'request', id: 'c:1', params: null }, return: returning });
  const request = await received.promise;
  assert.notEqual(request.return, returning);
  a.send(['raw'], { frame: { version: 1, kind: 'cancel', id: 'c:1' }, return: returning });
  assert.equal((await cancelled.promise).return, request.return);
  assert.throws(
    () => request.return!.wire.send([], { frame: { version: 1, kind: 'response', id: 'c:1', result: null } }),
    /return failed/,
  );
  handleWire(dispatcher, ['next'], () => 'reused');
  assert.equal(await callWire(a, ['next']), 'reused');
});

test('local roots preserve private dispatch context and keep metadata explicit', async (t) => {
  const [a, b] = wirePair();
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const verified = Object.freeze({ identity: 'verified locally' });
  const source = { wire: a, signal: new AbortController().signal, requestId: 'physical:17' };
  Object.defineProperty(source, 'verified', { value: verified });
  const traced: Wire = {
    ...a,
    send: (path, message) => {
      if (message.frame.kind === 'request')
        setWireContext(message.return!, { context: source, panic: () => {}, maxFrameBytes: 1024 });
      a.send(path, message);
    },
  };
  handleWire(dispatcher, ['inspect'], (_value, context) => {
    assert.equal((context as typeof context & { verified: unknown }).verified, verified);
    assert.equal(context.requestId, 'physical:17');
    assert.deepEqual(context.meta, { explicit: 'yes' });
    return 'observed';
  });
  assert.equal(await callWire(traced, ['inspect'], null, { meta: { explicit: 'yes' } }), 'observed');
});

test('local exact routes win over the longest namespace and detach reveals fallback', async (t) => {
  const [a, b] = wirePair();
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const seen: string[] = [];
  const receiver = (label: string) => ({
    message: (path: readonly string[], message: Message) => {
      seen.push(`${label}:${path.join('/')}`);
      message.return!.wire.send([], {
        frame: { version: 1, kind: 'response', id: (message.frame as { id: string }).id, result: label },
      });
    },
  });
  dispatcher.registerPrefix([], receiver('root'));
  dispatcher.registerPrefix(['a'], receiver('a'));
  const detach = handleWire(dispatcher, ['a', 'b'], () => 'exact');
  assert.equal(await callWire(a, ['a', 'b']), 'exact');
  detach();
  assert.equal(await callWire(a, ['a', 'b']), 'a');
  assert.equal(await callWire(a, ['other']), 'root');
  assert.deepEqual(seen, ['a:a/b', 'root:other']);
});

test('local deadline cancels handlers but retains noncooperative handler capacity', async (t) => {
  const [a, b] = wirePair({ requestTimeoutMs: 15, maxConcurrentHandlers: 1 });
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const release = deferred(),
    started = deferred(),
    cancelled = deferred();
  let invocations = 0;
  handleWire(dispatcher, ['hold'], async (_value, context) => {
    invocations++;
    started.resolve();
    context.signal.addEventListener('abort', () => cancelled.resolve(), { once: true });
    await release.promise;
  });
  const first = callWire(a, ['hold']).catch((error: unknown) => error);
  await started.promise;
  assert.equal(((await first) as { code: string }).code, 'cancelled');
  await cancelled.promise;
  await assert.rejects(callWire(a, ['hold']), { code: 'busy' });
  assert.equal(invocations, 1);
  release.resolve();
});

test('local queued payload snapshots cannot be changed by the sender', async (t) => {
  const [a, b] = wirePair();
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  const seen = deferred<unknown>();
  onWireEvent(dispatcher, ['event'], (value) => seen.resolve(value));
  const data = { nested: ['original'] };
  a.send(['event'], { frame: { version: 1, kind: 'event', data } });
  data.nested[0] = 'mutated';
  assert.deepEqual(await seen.promise, { nested: ['original'] });
});

test('a stalled local event reaches the configured deadline and observer', async (t) => {
  const pressure = deferred<number>();
  const [a, b] = wirePair({
    writeTimeoutMs: 15,
    observer: {
      observe: (event) => {
        if (event.type === 'backpressure' && event.stalled) pressure.resolve(event.deadlineMs);
      },
    },
  });
  t.after(() => a.close());
  const release = deferred(),
    closed = deferred();
  b.receive({ message: () => release.promise, closed: () => closed.resolve() });
  emitWire(a, ['event']);
  assert.equal(await pressure.promise, 15);
  await closed.promise;
  assert.throws(() => emitWire(a, ['event']), { code: 'disconnected' });
  release.resolve();
});

test('an oversized local response settles through the bounded internal fallback', async (t) => {
  const [a, b] = wirePair({ maxFrameBytes: 512 });
  const dispatcher = createDispatcher(b);
  t.after(() => {
    dispatcher.close();
    a.close();
  });
  handleWire(dispatcher, ['large'], () => 'x'.repeat(2048));
  await assert.rejects(callWire(a, ['large'], null, { timeoutMs: 1000 }), { code: 'internal' });
});
