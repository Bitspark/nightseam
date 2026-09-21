import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { pipe, type Frame, type FrameConnection } from '@nightseam/duplex';
import { DuplexPeer, DuplexError, UnpublishedError } from './peer.ts';
import type { ObserverEvent } from './observer.ts';
import { callWire, forwardWire, handleWire } from './wire.ts';
import { wirePair } from './wire-pair.ts';

function heldConnection() {
  const [a, b] = pipe();
  let buffered = 1;
  const sent: Frame[] = [];
  const connection: FrameConnection = {
    get state() {
      return a.state;
    },
    get buffered() {
      return buffered;
    },
    send(frame) {
      sent.push(frame);
      a.send(frame);
    },
    close: (code, reason) => a.close(code, reason),
    listen: (receiver) => a.listen(receiver),
  };
  return {
    connection,
    sent,
    receive: (frame: Frame) => b.send(frame),
    drain: () => {
      buffered = 0;
    },
    close: () => {
      a.close();
      b.close();
    },
  };
}

test('public emit waits for room while a shorter caller wrapper times out before the write deadline', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const held = heldConnection();
  const observed: ObserverEvent[] = [];
  const peer = new DuplexPeer({
    queueCapacity: 1,
    writeTimeoutMs: 1000,
    observer: { observe: (event) => observed.push(event) },
  });
  t.after(() => {
    peer.close();
    held.close();
  });
  await peer.attach(held.connection);
  await peer.emit('accepted');
  const pending = peer.emit('paced').then(
    () => 'accepted',
    (error: unknown) => error,
  );
  const outcome = Promise.race([
    pending,
    new Promise<string>((resolve) => setTimeout(() => resolve('wrapper_timeout'), 300)),
  ]);
  await Promise.resolve();
  t.mock.timers.tick(300);
  assert.equal(await outcome, 'wrapper_timeout');
  assert.equal(peer.status, 'connected');
  assert.deepEqual(
    observed.filter((event) => event.type === 'backpressure').map((event) => event.stalled),
    [false],
  );
  t.mock.timers.tick(700);
  assert.ok((await pending) instanceof UnpublishedError);
  assert.equal(peer.status, 'disconnected');
  assert.equal(held.sent.length, 0);
  assert.equal(observed.filter((event) => event.type === 'backpressure' && event.stalled).length, 1);
});

test('a paced public emit enters in order when the transport drains within its deadline', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const held = heldConnection();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 1000 });
  t.after(() => {
    peer.close();
    held.close();
  });
  await peer.attach(held.connection);
  await peer.emit('first');
  const second = peer.emit('second');
  held.drain();
  t.mock.timers.tick(5);
  await second;
  assert.equal(peer.status, 'connected');
  assert.deepEqual(
    held.sent.map((frame) => (frame.kind === 'text' ? JSON.parse(frame.data).event : null)),
    ['first', 'second'],
  );
});

test('a paced raw call retains its own timeout and cancellation without publishing afterward', async (t) => {
  for (const stop of ['timeout', 'abort'] as const) {
    await t.test(stop, async (t) => {
      t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
      const held = heldConnection();
      const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 1000 });
      t.after(() => {
        peer.close();
        held.close();
      });
      await peer.attach(held.connection);
      await peer.emit('accepted');
      const controller = new AbortController();
      const pending = peer
        .call('unadmitted', {}, { signal: controller.signal, timeoutMs: 300 })
        .catch((error: unknown) => error);
      if (stop === 'abort') controller.abort();
      else t.mock.timers.tick(300);
      const error = await pending;
      assert.equal((error as DuplexError).code, stop === 'abort' ? 'cancelled' : 'request_timeout');
      assert.ok(error instanceof UnpublishedError);
      assert.equal(peer.status, 'connected');
      held.drain();
      t.mock.timers.tick(5);
      await Promise.resolve();
      assert.deepEqual(
        held.sent.map((frame) => (frame.kind === 'text' ? JSON.parse(frame.data).kind : null)),
        ['event'],
      );
    });
  }
});

test('a public raw response waits for queue room and then follows the accepted event', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const held = heldConnection();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 1000 });
  t.after(() => {
    peer.close();
    held.close();
  });
  peer.handle('echo', (value) => value);
  await peer.attach(held.connection);
  await peer.emit('accepted');
  held.receive({
    kind: 'text',
    data: JSON.stringify({ version: 1, kind: 'request', id: 's:1', method: 'echo', params: 7 }),
  });
  await nextTurn();
  assert.equal(peer.status, 'connected');
  held.drain();
  t.mock.timers.tick(5);
  await nextTurn();
  assert.deepEqual(
    held.sent.map((frame) => (frame.kind === 'text' ? JSON.parse(frame.data).kind : null)),
    ['event', 'response'],
  );
  assert.equal(JSON.parse((held.sent[1] as { data: string }).data).result, 7);
});

test('a composed wire handoff still refuses a full physical queue immediately and ends only its destination', async (t) => {
  const held = heldConnection();
  const destination = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 5000 });
  const [caller, forwarding] = wirePair();
  const detach = forwardWire(forwarding, destination.wire());
  t.after(() => {
    detach();
    caller.close();
    forwarding.close();
    destination.close();
    held.close();
  });
  await destination.attach(held.connection);
  await destination.emit('accepted');
  const refused = await Promise.race([
    callWire(caller, ['refused']).catch((error: unknown) => error),
    nextTurn().then(() => 'waited'),
  ]);
  assert.equal((refused as DuplexError).code, 'busy');
  assert.equal(destination.status, 'disconnected');
  detach();
  handleWire(forwarding, ['healthy'], () => 'still open');
  assert.equal(await callWire(caller, ['healthy']), 'still open');
  assert.equal(held.sent.length, 0);
});
