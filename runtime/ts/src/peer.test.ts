import { decodeEnvelope } from './envelope.ts';
import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { DuplexPeer, DuplexError } from './peer.ts';
import type { PeerOptions, WebSocketLike } from './peer.ts';
import type { Observer, ObserverEvent } from './observer.ts';
import { webSocketConnection } from '@nightseam/duplex';
import type { ConnectionHandlers, ConnectionState, Frame, FrameConnection } from '@nightseam/duplex';
import { positiveInteger } from './index.ts';
import { emitWire } from './wire.ts';

test('component limits share validation while the peer keeps its safe-integer bound', () => {
  for (const safe of [false, true]) {
    assert.equal(positiveInteger(32, 'window', safe), 32);
    for (const value of [undefined, null, '32', 0, -1, 1.5, NaN, Infinity]) {
      assert.throws(() => positiveInteger(value, 'window', safe), {
        name: 'DuplexError',
        code: 'invalid_options',
        message: `window must be a positive ${safe ? 'safe ' : ''}integer.`,
      });
    }
  }
  assert.equal(positiveInteger(Number.MAX_SAFE_INTEGER + 1, 'window'), Number.MAX_SAFE_INTEGER + 1);
  assert.throws(() => positiveInteger(Number.MAX_SAFE_INTEGER + 1, 'timeoutMs', true), {
    code: 'invalid_options',
    message: 'timeoutMs must be a positive safe integer.',
  });
});

class Socket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  /** What the handshake selected, as a real WebSocket spells it. */
  protocol = '';
  sent: Record<string, unknown>[] = [];
  partner?: Socket;
  closeCount = 0;
  send(text: string): void {
    if (this.readyState !== 1) throw new Error('Closed');
    this.sent.push(JSON.parse(text));
    const partner = this.partner;
    if (partner)
      queueMicrotask(() => {
        if (partner.readyState === 1) partner.receive(text);
      });
  }
  receive(frame: unknown): void {
    this.dispatchEvent(
      new MessageEvent('message', { data: typeof frame === 'string' ? frame : JSON.stringify(frame) }),
    );
  }
  close(): void {
    this.closeCount++;
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
    this.partner?.close();
  }
}

/** An in-memory frames duplex connection; no WebSocket is involved anywhere. */
class Pipe implements FrameConnection {
  state: ConnectionState = 'open';
  readonly buffered = 0;
  partner!: Pipe;
  readonly frames: Frame[] = [];
  private readonly listeners = new Set<ConnectionHandlers>();
  static pair(): [Pipe, Pipe] {
    const left = new Pipe();
    const right = new Pipe();
    left.partner = right;
    right.partner = left;
    return [left, right];
  }
  send(frame: Frame): void {
    if (this.state !== 'open') throw new Error('Not open');
    this.frames.push(frame);
    const partner = this.partner;
    queueMicrotask(() => {
      if (partner.state === 'open') for (const handlers of [...partner.listeners]) handlers.frame?.(frame);
    });
  }
  close(code = 1000, reason = ''): void {
    if (this.state === 'closed') return;
    this.state = 'closed';
    for (const handlers of [...this.listeners]) handlers.close?.(code, reason);
    this.partner.close(code, reason);
  }
  listen(handlers: ConnectionHandlers): () => void {
    this.listeners.add(handlers);
    return () => {
      this.listeners.delete(handlers);
    };
  }
}

async function paired(clientOptions: PeerOptions = {}, serverOptions: PeerOptions = {}) {
  const left = new Socket();
  const right = new Socket();
  left.partner = right;
  right.partner = left;
  const client = new DuplexPeer(clientOptions);
  const server = new DuplexPeer({ ...serverOptions, role: 'server' });
  await Promise.all([client.attach(left), server.attach(right)]);
  return { client, server, left, right };
}

test('peer refuses malformed outgoing Unicode without losing valid strings', async (t) => {
  const { client, server, left } = await paired();
  t.after(() => client.close());
  server.handle('echo', (value) => value);
  server.handle('bad', () => '\uD800');
  for (const value of ['\uD800', { x: ['\uDC00'] }, { ['\uD800']: 1 }, { toJSON: () => '\uD800' }]) {
    await assert.rejects(client.emit('probe', value), { code: 'invalid_message' });
    await assert.rejects(client.call('echo', value), { code: 'invalid_message' });
  }
  await assert.rejects(client.emit('\uD800', null), { code: 'invalid_message' });
  await assert.rejects(client.emit('probe', null, { meta: { x: '\uD800' } }), { code: 'invalid_message' });
  assert.equal(left.sent.length, 0);
  await assert.rejects(client.call('bad'), { code: 'internal' });
  assert.equal(await client.call('echo', '😀�'), '😀�');
});

function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((accept) => {
    resolve = accept;
  });
  return { promise, resolve };
}

/** Lets whatever a frame set going run: the handlers, the queues, the writer. */
function settled(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 10));
}

test('duplex routing permits reverse calls during an outstanding request', async (t) => {
  const { client, server, left, right } = await paired();
  t.after(() => client.close());
  client.handle('multiply', (params) => (params as { value: number }).value * 3);
  server.handle('roundtrip', async (_params, context) => ({
    value: await context.peer.call('multiply', { value: 7 }),
  }));
  assert.deepEqual(await client.call('roundtrip'), { value: 21 });
  assert.equal(left.sent[0].id, 'c:1');
  assert.equal(right.sent[0].id, 's:1');
  assert.equal(left.sent[1].kind, 'response');
});

test('responses correlate out of order while events run independently', async (t) => {
  const { client, server } = await paired();
  t.after(() => client.close());
  const first = deferred<number>();
  const blockEvent = deferred();
  const eventStarted = deferred();
  server.handle('first', () => first.promise);
  server.handle('second', () => 2);
  client.onEvent('notice', async () => {
    eventStarted.resolve();
    await blockEvent.promise;
  });
  const one = client.call('first');
  await server.emit('notice', { value: 1 });
  await eventStarted.promise;
  assert.equal(await client.call('second'), 2);
  first.resolve(1);
  assert.equal(await one, 1);
  blockEvent.resolve();
});

test('public handler errors survive and unexpected errors remain private', async (t) => {
  const { client, server } = await paired();
  t.after(() => client.close());
  server.handle('public', () => {
    throw new DuplexError('denied', 'Access denied', { field: 'project' });
  });
  server.handle('private', () => {
    throw new Error('Database password: secret');
  });
  await assert.rejects(client.call('public'), { code: 'denied', message: 'Access denied', data: { field: 'project' } });
  await assert.rejects(client.call('private'), { code: 'internal', message: 'Request handler failed.' });
  await assert.rejects(client.call('missing'), { code: 'method_not_found' });
});

test('AbortSignal sends cancellation and aborts the remote handler', async (t) => {
  const { client, server, left } = await paired();
  t.after(() => client.close());
  const started = deferred();
  const aborted = deferred();
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener(
          'abort',
          () => {
            aborted.resolve();
            resolve('ignored late result');
          },
          { once: true },
        );
        started.resolve();
      }),
  );
  const controller = new AbortController();
  const call = client.call('wait', {}, { signal: controller.signal });
  const failure = assert.rejects(call, { code: 'cancelled' });
  await started.promise;
  controller.abort();
  await failure;
  await aborted.promise;
  assert.equal(left.sent.at(-1)?.kind, 'cancel');
  assert.equal(client.status, 'connected');
});

test('a cancel answers nothing itself: the response is the handler returning, and it is cancelled', async (t) => {
  const watching = recorder();
  const { client, server, right } = await paired({}, { observer: watching });
  t.after(() => {
    client.close();
    server.close();
  });
  const started = deferred();
  const release = deferred<string>();
  // A handler that ignores its signal. The cancel withdraws the request; what
  // answers it is this returning, whenever it does, as the profile says and
  // the Go peer does.
  server.handle('deaf', () => {
    started.resolve();
    return release.promise;
  });
  const controller = new AbortController();
  const call = assert.rejects(client.call('deaf', {}, { signal: controller.signal }), { code: 'cancelled' });
  await started.promise;
  controller.abort();
  await call;
  await settled();
  // The caller has given up and nothing has answered the request, because
  // nothing has finished it: where the cancel answered at once, a response
  // stood here and the request had ended.
  assert.deepEqual(
    right.sent.filter((frame) => frame.kind === 'response'),
    [],
  );
  assert.equal(
    watching.events.some((event) => event.type === 'request.ended'),
    false,
  );
  release.resolve('a result nobody is waiting for');
  await settled();
  const responses = right.sent.filter((frame) => frame.kind === 'response');
  assert.equal(responses.length, 1);
  assert.equal(responses[0].id, 'c:1');
  assert.deepEqual(responses[0].error, { code: 'cancelled', message: 'Request was cancelled.' });
  assert.deepEqual(
    watching.events.filter((event) => event.type === 'request.ended').map((event) => event.outcome),
    ['cancelled'],
  );
});

test('pre-aborted calls and outstanding capacity do not send extra requests', async (t) => {
  const { client, server, left } = await paired({ maxPendingRequests: 1 });
  t.after(() => client.close());
  const blocked = deferred();
  server.handle('wait', () => blocked.promise);
  const waiting = client.call('wait');
  const failed = assert.rejects(waiting, { code: 'disconnected' });
  await assert.rejects(client.call('overflow'), { code: 'busy' });
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(client.call('cancelled', {}, { signal: controller.signal }), { code: 'cancelled' });
  assert.equal(left.sent.length, 1);
  client.close();
  blocked.resolve();
  await failed;
});

test('local request deadline cancels remotely without retrying', async (t) => {
  const { client, server, left } = await paired();
  t.after(() => client.close());
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener('abort', () => resolve(null), { once: true });
      }),
  );
  await assert.rejects(client.call('wait', {}, { timeoutMs: 10 }), { code: 'request_timeout' });
  assert.deepEqual(
    left.sent.map((frame) => frame.kind),
    ['request', 'cancel'],
  );
});

test('incoming deadlines abort handlers and retain occupied slots until completion', async (t) => {
  const { client, server } = await paired({}, { maxConcurrentHandlers: 1, requestTimeoutMs: 10 });
  t.after(() => client.close());
  const blocked = deferred();
  let signal: AbortSignal | undefined;
  server.handle('wait', (_params, context) => {
    signal = context.signal;
    return blocked.promise;
  });
  // The receiver's own deadline abandons the request, and `cancelled` is what
  // it answers: `request_timeout` is a caller's own error and never a frame.
  await assert.rejects(client.call('wait'), { code: 'cancelled' });
  assert.equal(signal?.aborted, true);
  await assert.rejects(client.call('wait'), { code: 'busy' });
  blocked.resolve();
});

test('disconnect cancels handlers and rejects pending calls without reconnecting', async () => {
  const { client, server, left } = await paired();
  const started = deferred();
  const aborted = deferred();
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener(
          'abort',
          () => {
            aborted.resolve();
            resolve(1);
          },
          { once: true },
        );
        started.resolve();
      }),
  );
  const waiting = client.call('wait');
  const failed = assert.rejects(waiting, { code: 'disconnected' });
  await started.promise;
  client.close();
  await Promise.all([failed, aborted.promise]);
  assert.equal(client.status, 'disconnected');
  assert.equal(server.status, 'disconnected');
  assert.equal(left.sent.filter((frame) => frame.kind === 'request').length, 1);
  await assert.rejects(client.call('another'), { code: 'not_connected' });
});

test('incoming saturation responds busy without blocking responses', async (t) => {
  const { client, server } = await paired({}, { maxConcurrentHandlers: 1 });
  t.after(() => client.close());
  const occupied = deferred();
  const started = deferred();
  server.handle('wait', () => {
    started.resolve();
    return occupied.promise;
  });
  client.handle('ping', () => 'pong');
  const first = client.call('wait');
  await started.promise;
  await assert.rejects(client.call('wait'), { code: 'busy' });
  assert.equal(await server.call('ping'), 'pong');
  occupied.resolve();
  assert.equal(await first, null);
});

test('wire output overflow ends the carrier without waiting for the socket to drain', async (t) => {
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 5_000 });
  t.after(() => peer.close());
  await peer.attach(socket);
  // The first is accepted for sending, which is queued and no more.
  emitWire(peer.wire(), ['first']);
  await Promise.resolve();
  // The consumer remains blocked. Admission must settle before another turn,
  // independently of the much longer transport write deadline.
  const ended = deferred<DuplexError>();
  peer.onClose(ended.resolve);
  emitWire(peer.wire(), ['second']);
  const outcome = await Promise.race([
    ended.promise.then((error) => {
      assert.equal(error.code, 'busy');
      return 'refused';
    }),
    nextTurn().then(() => 'waited'),
  ]);
  assert.equal(outcome, 'refused');
  assert.equal(peer.status, 'disconnected');
  assert.equal(socket.closeCount, 1);
  assert.equal(socket.sent.length, 0);
});

test('the accepted output prefix drains in order within its bound', async (t) => {
  for (const capacity of [2, 8]) {
    await t.test(`capacity ${capacity}`, async (t) => {
      const socket = new Socket();
      socket.bufferedAmount = 1;
      const drained = deferred();
      const send = socket.send.bind(socket);
      socket.send = (text) => {
        send(text);
        if (socket.sent.length === capacity) drained.resolve();
      };
      const peer = new DuplexPeer({ queueCapacity: capacity, writeTimeoutMs: 5_000 });
      t.after(() => peer.close());
      await peer.attach(socket);
      for (let sequence = 0; sequence < capacity; sequence++) await peer.emit('item', sequence);
      assert.equal(socket.sent.length, 0);
      // Every emit already completed while the destination remained held.
      socket.bufferedAmount = 0;
      await drained.promise;
      assert.equal(peer.status, 'connected');
      assert.deepEqual(
        socket.sent.map((frame) => frame.data),
        Array.from({ length: capacity }, (_, i) => i),
      );
      await peer.emit('marker', capacity);
      assert.equal(socket.sent.at(-1)?.data, capacity);
    });
  }
});

test('a pre-aborted call does not attempt admission to a full output queue', async (t) => {
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const drained = deferred();
  const send = socket.send.bind(socket);
  socket.send = (text) => {
    send(text);
    drained.resolve();
  };
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 5_000 });
  t.after(() => peer.close());
  await peer.attach(socket);
  await peer.emit('accepted');
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(peer.call('unadmitted', {}, { signal: controller.signal }), { code: 'cancelled' });
  assert.equal(peer.status, 'connected');
  assert.equal(socket.closeCount, 0);
  assert.equal(socket.sent.length, 0);
  socket.bufferedAmount = 0;
  await drained.promise;
  assert.deepEqual(
    socket.sent.map((frame) => frame.event),
    ['accepted'],
  );
});

test('a sent call cancels promptly when its best-effort cancellation cannot be queued', async (t) => {
  const { client, server, left } = await paired({ queueCapacity: 1, writeTimeoutMs: 5_000 });
  t.after(() => client.close());
  const started = deferred();
  server.handle('wait', (_params, context) => {
    started.resolve();
    return new Promise((resolve) => context.signal.addEventListener('abort', () => resolve(null), { once: true }));
  });
  const controller = new AbortController();
  const cancelled = assert.rejects(client.call('wait', {}, { signal: controller.signal }), { code: 'cancelled' });
  await started.promise;
  left.bufferedAmount = 1;
  await client.emit('accepted');
  controller.abort();
  const outcome = await Promise.race([cancelled.then(() => 'cancelled'), nextTurn().then(() => 'waited')]);
  assert.equal(outcome, 'cancelled');
  assert.equal(client.status, 'connected');
  assert.equal(server.status, 'connected');
  assert.deepEqual(
    left.sent.map((frame) => frame.kind),
    ['request'],
  );
});

test('an emit over a transport that never drains resolves anyway, and the write deadline still ends the connection', async () => {
  // What an emit promises is that the frame was accepted for sending, as the
  // profile says and the Go peer returns: queued for this connection, with
  // the drain and its deadline continuing behind the caller.
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const peer = new DuplexPeer({ writeTimeoutMs: 10 });
  const closed = deferred<DuplexError>();
  peer.onClose(closed.resolve);
  await peer.attach(socket);
  await peer.emit('blocked');
  assert.equal(peer.status, 'connected');
  assert.equal(socket.sent.length, 0);
  assert.equal((await closed.promise).code, 'write_timeout');
  assert.equal(peer.status, 'disconnected');
});

test('event queues are bounded and a slow listener is paced, then disconnected', async () => {
  const socket = new Socket();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 40 });
  const closed = deferred<DuplexError>();
  peer.onClose(closed.resolve);
  await peer.attach(socket);
  const blocked = deferred();
  peer.onEvent(() => blocked.promise);
  socket.receive({ version: 1, kind: 'event', event: 'one', data: {} });
  socket.receive({ version: 1, kind: 'event', event: 'two', data: {} });
  // A full queue is a burst until its deadline passes. This listener never
  // returns, so its own deadline is the first to pass and names what stalled;
  // the queue's deadline behind it is the backstop for a consumer that does
  // return, only never fast enough.
  assert.equal(peer.status, 'connected');
  assert.equal((await closed.promise).code, 'stalled_consumer');
  assert.equal(peer.status, 'disconnected');
  blocked.resolve();
});

test('a producer that outruns its consumer for a whole deadline is a stalled consumer', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const socket = new Socket();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 40 });
  const closed = deferred<DuplexError>();
  peer.onClose(closed.resolve);
  await peer.attach(socket);
  // The first listener finishes before its deadline and the second is still
  // within its deadline when the backlog gives out. Advance a controlled
  // clock so that scheduler load cannot make the listener lose that race.
  const first = deferred();
  const second = deferred();
  let calls = 0;
  peer.onEvent(() => (++calls === 1 ? first.promise : second.promise));
  t.after(() => {
    first.resolve();
    second.resolve();
    peer.close();
  });
  for (let i = 0; i < 20; i++) socket.receive({ version: 1, kind: 'event', event: `burst-${i}`, data: {} });
  t.mock.timers.tick(20);
  first.resolve();
  await nextTurn();
  assert.equal(calls, 2);
  // The backlog has now lasted 40 ms; neither listener has reached its own deadline.
  t.mock.timers.tick(20);
  assert.equal((await closed.promise).code, 'busy');
});

test('an event burst that drains within the deadline is paced, not disconnected', async (t) => {
  const socket = new Socket();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 1_000 });
  await peer.attach(socket);
  const held = deferred();
  const drained = deferred();
  t.after(() => {
    held.resolve();
    peer.close();
  });
  const delivered: string[] = [];
  peer.onEvent(async (name) => {
    await held.promise;
    delivered.push(name);
    if (delivered.length === 2) drained.resolve();
  });
  socket.receive({ version: 1, kind: 'event', event: 'one', data: {} });
  socket.receive({ version: 1, kind: 'event', event: 'two', data: {} });
  held.resolve();
  // The backlog clears inside the deadline, so the burst was a burst: both
  // events arrive in order and the connection is whole.
  await drained.promise;
  assert.deepEqual(delivered, ['one', 'two']);
  assert.equal(peer.status, 'connected');
});

test('stalled asynchronous event listener closes the peer', async () => {
  const socket = new Socket();
  const peer = new DuplexPeer({ writeTimeoutMs: 10 });
  const closed = deferred<DuplexError>();
  const blocked = deferred();
  peer.onClose(closed.resolve);
  peer.onEvent(() => blocked.promise);
  await peer.attach(socket);
  socket.receive({ version: 1, kind: 'event', event: 'one', data: {} });
  assert.equal((await closed.promise).code, 'stalled_consumer');
  blocked.resolve();
});

test('large replay bursts do not queue already completed synchronous listeners', async (t) => {
  const socket = new Socket();
  const peer = new DuplexPeer();
  await peer.attach(socket);
  t.after(() => peer.close());
  const received: number[] = [];
  peer.onEvent('replay', (value) => {
    received.push(value as number);
  });
  for (let sequence = 1; sequence <= 300; sequence++) {
    socket.receive({ version: 1, kind: 'event', event: 'replay', data: sequence });
  }
  assert.equal(peer.status, 'connected');
  assert.deepEqual(
    received,
    Array.from({ length: 300 }, (_, index) => index + 1),
  );
});

test('event subscriptions preserve order, isolate failures, and unsubscribe', async (t) => {
  const errors: string[] = [];
  const { client, server } = await paired({ onError: (error) => errors.push(error.code) });
  t.after(() => client.close());
  const observed: unknown[] = [];
  const done = deferred();
  client.onEvent('notice', () => {
    throw new Error('listener failure');
  });
  const off = client.onEvent('notice', (value) => {
    observed.push(value);
    if (observed.length === 2) done.resolve();
  });
  await server.emit('notice', 1);
  await server.emit('notice', 2);
  await done.promise;
  off();
  await server.emit('notice', 3);
  assert.deepEqual(observed, [1, 2]);
  assert.equal(errors[0], 'event_handler_failed');
});

test('malformed envelopes, binary messages, opposite IDs, and oversize frames close the peer', async () => {
  const invalid: unknown[] = [
    '{}',
    '{bad',
    '{"version":1,"version":1,"kind":"cancel","id":"c:1"}',
    { version: 2, kind: 'event', event: 'notice', data: null },
    { version: 1, kind: 'request', id: 'c:1', method: 'x', params: {} },
    { version: 1, kind: 'response', id: 'c:1', result: 1, error: { code: 'bad', message: 'bad' } },
    { version: 1, kind: 'event', event: 'x', data: 1, extra: true },
    // An error is a code and a message and both are non-empty, as the Go peer
    // refuses them: a response nobody can read is no answer to a call.
    { version: 1, kind: 'response', id: 'c:1', error: { code: 'denied', message: '' } },
    { version: 1, kind: 'response', id: 'c:1', error: { code: '', message: 'Denied' } },
  ];
  for (const frame of invalid) {
    const socket = new Socket();
    const peer = new DuplexPeer();
    await peer.attach(socket);
    socket.receive(frame);
    assert.equal(peer.status, 'disconnected', JSON.stringify(frame));
  }
  const socket = new Socket();
  const peer = new DuplexPeer({ maxFrameBytes: 70 });
  await peer.attach(socket);
  socket.receive({ version: 1, kind: 'event', event: 'x', data: 'é'.repeat(30) });
  assert.equal(peer.status, 'disconnected');
  const binary = new Socket();
  const binaryPeer = new DuplexPeer();
  await binaryPeer.attach(binary);
  binary.dispatchEvent(new MessageEvent('message', { data: new Uint8Array([1]) }));
  assert.equal(binaryPeer.status, 'disconnected');
});

test('outgoing oversize or unserializable values reject without sending', async (t) => {
  const socket = new Socket();
  const peer = new DuplexPeer({ maxFrameBytes: 100 });
  await peer.attach(socket);
  t.after(() => peer.close());
  await assert.rejects(peer.emit('large', 'é'.repeat(100)), { code: 'frame_too_large' });
  await assert.rejects(peer.call('bigint', 1n), { code: 'invalid_message' });
  await assert.rejects(
    peer.call('function', () => 1),
    { code: 'invalid_message' },
  );
  await assert.rejects(peer.emit('nan', Number.NaN), { code: 'invalid_message' });
  assert.equal(socket.sent.length, 0);
  assert.equal(peer.status, 'connected');
});

test('connection requires an explicit ws/wss endpoint and never sets browser headers', async (t) => {
  const socket = new Socket();
  socket.readyState = 0;
  let observedURL = '';
  const peer = new DuplexPeer({
    webSocketFactory: (url) => {
      observedURL = url;
      return socket;
    },
  });
  t.after(() => peer.close());
  await assert.rejects(peer.connect('/api'), { code: 'invalid_url' });
  await assert.rejects(peer.connect('https://localhost/api'), { code: 'invalid_url' });
  await assert.rejects(peer.connect('ws://user:password@localhost/api'), { code: 'invalid_url' });
  const connecting = peer.connect('ws://localhost/api');
  assert.equal(peer.status, 'connecting');
  socket.readyState = 1;
  socket.dispatchEvent(new Event('open'));
  await connecting;
  assert.equal(observedURL, 'ws://localhost/api');
  await assert.rejects(peer.connect('ws://localhost/api'), { code: 'already_connected' });
});

test('connection timeout closes the socket; manual reconnection remains explicit', async (t) => {
  const socket = new Socket();
  socket.readyState = 0;
  let created = 0;
  const peer = new DuplexPeer({
    connectTimeoutMs: 10,
    webSocketFactory: () => {
      created++;
      return socket;
    },
  });
  await assert.rejects(peer.connect('ws://localhost/api'), { code: 'connect_timeout' });
  assert.equal(socket.readyState, 3);
  assert.equal(created, 1);
  await peer.attach(new Socket());
  t.after(() => peer.close());
  assert.equal(peer.status, 'connected');
});

test('late work from an old connection cannot answer a new connection', async (t) => {
  const peer = new DuplexPeer();
  const old = new Socket();
  const gate = deferred<string>();
  const started = deferred();
  peer.handle('wait', () => {
    started.resolve();
    return gate.promise;
  });
  await peer.attach(old);
  old.receive({ version: 1, kind: 'request', id: 's:1', method: 'wait', params: {} });
  await started.promise;
  peer.close();
  const current = new Socket();
  await peer.attach(current);
  t.after(() => peer.close());
  gate.resolve('old');
  await gate.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(current.sent, []);
});

test('handler registration is explicit, removable, and rejects duplicates', async (t) => {
  const { client, server } = await paired();
  t.after(() => client.close());
  const remove = server.handle('x', () => 1);
  assert.throws(() => server.handle('x', () => 2), { code: 'duplicate_handler' });
  assert.equal(await client.call('x'), 1);
  remove();
  await assert.rejects(client.call('x'), { code: 'method_not_found' });
});

test('two peers complete a call, an event and a cancel over an in-memory frame pipe', async (t) => {
  const [left, right] = Pipe.pair();
  const client = new DuplexPeer();
  const server = new DuplexPeer({ role: 'server' });
  await Promise.all([client.attach(left), server.attach(right)]);
  t.after(() => client.close());
  const notice = deferred<unknown>();
  const started = deferred();
  const aborted = deferred();
  server.handle('add', (params) => {
    const { a, b } = params as { a: number; b: number };
    return a + b;
  });
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener(
          'abort',
          () => {
            aborted.resolve();
            resolve(null);
          },
          { once: true },
        );
        started.resolve();
      }),
  );
  client.onEvent('notice', (data) => {
    notice.resolve(data);
  });
  assert.equal(await client.call('add', { a: 2, b: 3 }), 5);
  await server.emit('notice', { value: 1 });
  assert.deepEqual(await notice.promise, { value: 1 });
  const controller = new AbortController();
  const cancelled = assert.rejects(client.call('wait', {}, { signal: controller.signal }), { code: 'cancelled' });
  await started.promise;
  controller.abort();
  await Promise.all([cancelled, aborted.promise]);
  assert.equal(client.status, 'connected');
  assert.equal(server.status, 'connected');
  assert.deepEqual(
    left.frames.map((frame) => frame.kind),
    ['text', 'text', 'text'],
  );
  assert.deepEqual(
    left.frames.map((frame) => JSON.parse(frame.data as string).kind),
    ['request', 'request', 'cancel'],
  );
  assert.deepEqual(
    right.frames.map((frame) => JSON.parse(frame.data as string).kind),
    ['response', 'event', 'response'],
  );
});

/** One W3C traceparent, the example of the specification, and a vendor's state beside it. */
const TRACEPARENT = '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01';
const TRACESTATE = 'vendor=t61rcWkgMzE';
/** Every kind as a server peer receives it: a request and a cancel from the client, a response to its own. */
const everyKind: Record<string, unknown>[] = [
  { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: {} },
  { version: 1, kind: 'response', id: 's:1', result: 1 },
  { version: 1, kind: 'cancel', id: 'c:1' },
  { version: 1, kind: 'event', event: 'notice', data: null },
];

test('trace context is kept on the decoded envelope of every kind, and tracestate stands alone', () => {
  for (const kind of everyKind) {
    const traced = { ...kind, traceparent: TRACEPARENT, tracestate: TRACESTATE };
    assert.deepEqual(decodeEnvelope(JSON.stringify(traced), 's:', 'c:'), traced, kind.kind as string);
    // An intermediary may strip one member and not the other.
    const alone = { ...kind, tracestate: TRACESTATE };
    const decoded = decodeEnvelope(JSON.stringify(alone), 's:', 'c:');
    assert.deepEqual(decoded, alone);
    assert.equal(Object.hasOwn(decoded, 'traceparent'), false);
    // A frame without either decodes as before.
    assert.deepEqual(decodeEnvelope(JSON.stringify(kind), 's:', 'c:'), kind);
  }
});

test("a traced frame of every kind routes as before, and a response carries its request's trace", async (t) => {
  const socket = new Socket();
  const peer = new DuplexPeer();
  await peer.attach(socket);
  t.after(() => peer.close());
  const trace = { traceparent: TRACEPARENT, tracestate: TRACESTATE };
  const notice = deferred<unknown>();
  const started = deferred();
  const aborted = deferred();
  peer.onEvent('notice', (data) => {
    notice.resolve(data);
  });
  peer.handle('echo', (params) => params);
  peer.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener(
          'abort',
          () => {
            aborted.resolve();
            resolve(null);
          },
          { once: true },
        );
        started.resolve();
      }),
  );
  const pending = peer.call('ping');
  socket.receive({ version: 1, kind: 'response', id: 'c:1', result: 'pong', ...trace });
  assert.equal(await pending, 'pong');
  socket.receive({ version: 1, kind: 'event', event: 'notice', data: { value: 1 }, ...trace });
  assert.deepEqual(await notice.promise, { value: 1 });
  socket.receive({ version: 1, kind: 'request', id: 's:1', method: 'echo', params: { value: 2 }, ...trace });
  socket.receive({ version: 1, kind: 'request', id: 's:2', method: 'wait', params: {}, ...trace });
  await started.promise;
  socket.receive({ version: 1, kind: 'cancel', id: 's:2', ...trace });
  await aborted.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(peer.status, 'connected');
  assert.deepEqual(
    socket.sent.map((frame) => frame.id),
    ['c:1', 's:1', 's:2'],
  );
  assert.deepEqual(socket.sent.find((frame) => frame.id === 's:1')?.result, { value: 2 });
  // Each response repeats the members of the request it answers; trace.test.ts holds the rest.
  for (const id of ['s:1', 's:2']) {
    const response = socket.sent.find((frame) => frame.id === id);
    assert.equal(response?.traceparent, TRACEPARENT, id);
    assert.equal(response?.tracestate, TRACESTATE, id);
  }
});

test('a malformed traceparent is refused as any invalid frame is', async () => {
  const malformed = [
    '',
    '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7',
    '00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01',
    '00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01',
    ' 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01',
  ];
  for (const kind of everyKind) {
    for (const traceparent of malformed) {
      const socket = new Socket();
      const peer = new DuplexPeer({ role: 'server' });
      await peer.attach(socket);
      socket.receive({ ...kind, traceparent });
      assert.equal(peer.status, 'disconnected', `${kind.kind as string} ${traceparent}`);
    }
    // A member that is present but not a string is refused the same way.
    const socket = new Socket();
    const peer = new DuplexPeer({ role: 'server' });
    await peer.attach(socket);
    socket.receive({ ...kind, traceparent: TRACEPARENT, tracestate: 7 });
    assert.equal(peer.status, 'disconnected', kind.kind as string);
  }
});

test('the WebSocket adapter maps state, buffered bytes, frames, and the close code and reason', async () => {
  const socket = new Socket();
  socket.readyState = 0;
  const connection = webSocketConnection(socket);
  assert.equal(connection.state, 'connecting');
  assert.throws(() => connection.send({ kind: 'text', data: 'early' }));
  socket.readyState = 1;
  assert.equal(connection.state, 'open');
  socket.bufferedAmount = 7;
  assert.equal(connection.buffered, 7);
  connection.send({ kind: 'text', data: '{"a":1}' });
  assert.deepEqual(socket.sent, [{ a: 1 }]);
  const frames: Frame[] = [];
  let closed: [number, string] | undefined;
  const off = connection.listen({
    frame: (frame) => {
      frames.push(frame);
    },
    close: (code, reason) => {
      closed = [code, reason];
    },
  });
  socket.receive('"text"');
  socket.dispatchEvent(new MessageEvent('message', { data: new Uint8Array([1, 2]) }));
  socket.dispatchEvent(new MessageEvent('message', { data: new ArrayBuffer(3) }));
  assert.deepEqual(frames, [
    { kind: 'text', data: '"text"' },
    { kind: 'binary', data: new Uint8Array([1, 2]) },
    { kind: 'binary', data: new ArrayBuffer(3) },
  ]);
  // A Blob is read asynchronously; a text frame behind it keeps its place.
  socket.dispatchEvent(new MessageEvent('message', { data: new Blob([new Uint8Array([9])]) }));
  socket.receive('"after"');
  assert.equal(frames.length, 3);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(frames.slice(3), [
    { kind: 'binary', data: new Uint8Array([9]).buffer },
    { kind: 'text', data: '"after"' },
  ]);
  socket.readyState = 2;
  assert.equal(connection.state, 'closing');
  assert.throws(() => connection.send({ kind: 'text', data: 'late' }));
  socket.readyState = 3;
  socket.dispatchEvent(new CloseEvent('close', { code: 4001, reason: 'gone' }));
  assert.equal(connection.state, 'closed');
  assert.deepEqual(closed, [4001, 'gone']);
  off();
  // The peer refuses a binary frame delivered through the adapter.
  const binary = new Socket();
  const peer = new DuplexPeer();
  const failure = deferred<DuplexError>();
  peer.onClose(failure.resolve);
  await peer.attach(binary);
  binary.dispatchEvent(new MessageEvent('message', { data: new ArrayBuffer(1) }));
  const error = await failure.promise;
  assert.equal(error.code, 'invalid_message');
  assert.equal(error.message, 'Only JSON text frames are supported.');
  assert.equal(peer.status, 'disconnected');
  assert.equal(binary.readyState, 3);
});

test('a close from the far side surfaces its code and reason to the close handler', async () => {
  const [left, right] = Pipe.pair();
  const client = new DuplexPeer();
  const server = new DuplexPeer({ role: 'server' });
  await Promise.all([client.attach(left), server.attach(right)]);
  let observed: [number, string] | undefined;
  left.listen({
    close: (code, reason) => {
      observed = [code, reason];
    },
  });
  const closed = deferred<DuplexError>();
  client.onClose(closed.resolve);
  server.close();
  assert.deepEqual(observed, [1000, 'Duplex connection closed']);
  assert.equal((await closed.promise).code, 'disconnected');
  assert.equal(client.status, 'disconnected');
  // The same through the adapter: the socket's close event carries the far side's code.
  const socket = new Socket();
  const connection = webSocketConnection(socket);
  const peer = new DuplexPeer();
  await peer.attach(connection);
  let seen: [number, string] | undefined;
  connection.listen({
    close: (code, reason) => {
      seen = [code, reason];
    },
  });
  const failure = deferred<DuplexError>();
  peer.onClose(failure.resolve);
  socket.readyState = 3;
  socket.dispatchEvent(new CloseEvent('close', { code: 1008, reason: 'policy violation' }));
  assert.deepEqual(seen, [1008, 'policy violation']);
  assert.equal((await failure.promise).code, 'disconnected');
  assert.equal(socket.closeCount, 0);
});

/** A string that stands for a payload: it is in every params, result, error and event below. */
const SENTINEL = 'sentinel-6d9f2c-payload';

/** An observer that keeps what it saw, which is all an observer is asked to do. */
function recorder(): Observer & { events: ObserverEvent[] } {
  const events: ObserverEvent[] = [];
  return {
    events,
    observe(event) {
      events.push(event);
    },
  };
}

/**
 * One event as a test reads it: the wall clock and the trace are asserted where
 * they are the point, a size and a duration as facts that are reported at all.
 */
function shape(event: ObserverEvent): Record<string, unknown> {
  const { at, trace, bytes, durationMs, ...rest } = event as unknown as Record<string, unknown>;
  void at;
  void trace;
  if (typeof bytes === 'number') rest.bytes = bytes > 0;
  if (typeof durationMs === 'number') rest.durationMs = durationMs >= 0;
  for (const [key, value] of Object.entries(rest)) if (value === undefined) delete rest[key];
  return rest;
}

function traceparent(event: ObserverEvent): unknown {
  return (event as unknown as { trace?: { traceparent?: unknown } }).trace?.traceparent;
}

function backpressure(events: ObserverEvent[]): Record<string, unknown>[] {
  return events.filter((event) => event.type === 'backpressure').map(shape);
}

test('no payload reaches an observer: not params, not a result, not an error, not an event', async () => {
  const consumer = recorder();
  const machine = recorder();
  const { client, server } = await paired({ observer: consumer }, { observer: machine });
  const delivered = deferred();
  client.onEvent('notice', () => {
    delivered.resolve();
  });
  server.handle('read', (params) => ({ echoed: params, secret: SENTINEL }));
  server.handle('deny', () => {
    throw new DuplexError('denied', 'Access denied', { secret: SENTINEL });
  });
  server.handle('boom', () => {
    throw new Error('handler failed');
  });
  assert.deepEqual(await client.call('read', { secret: SENTINEL }), { echoed: { secret: SENTINEL }, secret: SENTINEL });
  await assert.rejects(client.call('deny', { secret: SENTINEL }), { code: 'denied', data: { secret: SENTINEL } });
  await assert.rejects(client.call('boom', { secret: SENTINEL }), { code: 'internal' });
  await server.emit('notice', { secret: SENTINEL });
  await delivered.promise;
  client.close();
  for (const observed of [consumer.events, machine.events]) {
    assert.ok(observed.length > 0);
    for (const event of observed) assert.equal(JSON.stringify(event).includes(SENTINEL), false, event.type);
    assert.equal(JSON.stringify(observed).includes(SENTINEL), false);
  }
  // The paths that carried it were the observed ones, not some other traffic.
  assert.deepEqual(machine.events.filter((event) => event.type === 'handler.panic').map(shape), [
    { type: 'handler.panic', method: 'boom', value: 'Error: handler failed', family: '' },
  ]);
  assert.ok(consumer.events.some((event) => event.type === 'event.delivered'));
  assert.ok(consumer.events.some((event) => event.type === 'request.ended' && event.outcome === 'error'));
});

test('for one call, one event and one close an observer sees the events in order, in both directions', async () => {
  const [left, right] = Pipe.pair();
  const consumer = recorder();
  const machine = recorder();
  const families = { 'work.read': 'work' };
  const client = new DuplexPeer({ observer: consumer, families });
  const server = new DuplexPeer({ role: 'server', observer: machine, families });
  await Promise.all([client.attach(left), server.attach(right)]);
  const delivered = deferred();
  client.onEvent('notice', () => {
    delivered.resolve();
  });
  server.handle('work.read', () => ({ ok: true }));
  assert.deepEqual(await client.call('work.read', { id: 'w1' }), { ok: true });
  await server.emit('notice', { value: 1 });
  await delivered.promise;
  client.close();
  assert.deepEqual(consumer.events.map(shape), [
    { type: 'connection.opened', role: 'client' },
    { type: 'request.started', id: 'c:1', method: 'work.read', incoming: false, family: 'work' },
    { type: 'frame.sent', kind: 'request', name: 'work.read', bytes: true, id: 'c:1', family: 'work' },
    { type: 'frame.received', kind: 'response', name: '', bytes: true, id: 'c:1', family: '' },
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'work.read',
      incoming: false,
      durationMs: true,
      outcome: 'ok',
      family: 'work',
    },
    { type: 'frame.received', kind: 'event', name: 'notice', bytes: true, family: '' },
    { type: 'event.delivered', name: 'notice', bytes: true, family: '' },
    { type: 'connection.closed', code: 1000, reason: 'Duplex connection closed', local: true },
  ]);
  assert.deepEqual(machine.events.map(shape), [
    { type: 'connection.opened', role: 'server' },
    { type: 'frame.received', kind: 'request', name: 'work.read', bytes: true, id: 'c:1', family: 'work' },
    { type: 'request.started', id: 'c:1', method: 'work.read', incoming: true, family: 'work' },
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'work.read',
      incoming: true,
      durationMs: true,
      outcome: 'ok',
      family: 'work',
    },
    { type: 'frame.sent', kind: 'response', name: '', bytes: true, id: 'c:1', family: '' },
    { type: 'event.emitted', name: 'notice', bytes: true, family: '' },
    { type: 'frame.sent', kind: 'event', name: 'notice', bytes: true, family: '' },
    { type: 'connection.closed', code: 1000, reason: 'Duplex connection closed', local: false },
  ]);
  // What concerns a frame carries that frame's trace; the call's four events carry one.
  const call = consumer.events.slice(1, 5).map(traceparent);
  assert.equal(typeof call[0], 'string');
  assert.deepEqual(call, [call[0], call[0], call[0], call[0]]);
  assert.deepEqual(machine.events.slice(1, 5).map(traceparent), [call[0], call[0], call[0], call[0]]);
  // A connection or backpressure event concerns none, stated rather than nullable.
  assert.equal(traceparent(consumer.events[0]), undefined);
  assert.equal(traceparent(consumer.events[7]), undefined);
});

test('a handler that throws yields handler.panic with the value, never its params', async (t) => {
  const machine = recorder();
  const { client, server } = await paired({}, { observer: machine, families: { boom: 'work' } });
  t.after(() => client.close());
  server.handle('boom', () => {
    throw new Error('handler exploded');
  });
  server.handle('deny', () => {
    throw new DuplexError('denied', 'Access denied');
  });
  await assert.rejects(client.call('boom', { secret: SENTINEL }), { code: 'internal' });
  await assert.rejects(client.call('deny', { secret: SENTINEL }), { code: 'denied' });
  // A public error is the handler answering, not the runtime's panic.
  assert.deepEqual(machine.events.filter((event) => event.type === 'handler.panic').map(shape), [
    { type: 'handler.panic', method: 'boom', value: 'Error: handler exploded', family: 'work' },
  ]);
  assert.deepEqual(machine.events.filter((event) => event.type === 'request.ended').map(shape), [
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'boom',
      incoming: true,
      durationMs: true,
      outcome: 'error',
      errorCode: 'internal',
      family: 'work',
    },
    {
      type: 'request.ended',
      id: 'c:2',
      method: 'deny',
      incoming: true,
      durationMs: true,
      outcome: 'error',
      errorCode: 'denied',
      family: '',
    },
  ]);
  assert.equal(JSON.stringify(machine.events).includes(SENTINEL), false);
});

test('an outcome is what ended the call: an error code, a cancellation, a deadline, a disconnect', async () => {
  const consumer = recorder();
  const { client, server } = await paired({ observer: consumer });
  server.handle('deny', () => {
    throw new DuplexError('denied', 'Access denied');
  });
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener('abort', () => resolve(null), { once: true });
      }),
  );
  await assert.rejects(client.call('deny'), { code: 'denied' });
  const controller = new AbortController();
  const cancelled = assert.rejects(client.call('wait', {}, { signal: controller.signal }), { code: 'cancelled' });
  controller.abort();
  await cancelled;
  await assert.rejects(client.call('wait', {}, { timeoutMs: 10 }), { code: 'request_timeout' });
  const outstanding = assert.rejects(client.call('wait'), { code: 'disconnected' });
  client.close();
  await outstanding;
  assert.deepEqual(consumer.events.filter((event) => event.type === 'request.ended').map(shape), [
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'deny',
      incoming: false,
      durationMs: true,
      outcome: 'error',
      errorCode: 'denied',
      family: '',
    },
    {
      type: 'request.ended',
      id: 'c:2',
      method: 'wait',
      incoming: false,
      durationMs: true,
      outcome: 'cancelled',
      errorCode: 'cancelled',
      family: '',
    },
    {
      type: 'request.ended',
      id: 'c:3',
      method: 'wait',
      incoming: false,
      durationMs: true,
      outcome: 'timeout',
      errorCode: 'request_timeout',
      family: '',
    },
    {
      type: 'request.ended',
      id: 'c:4',
      method: 'wait',
      incoming: false,
      durationMs: true,
      outcome: 'error',
      errorCode: 'disconnected',
      family: '',
    },
  ]);
});

for (const code of ['cancelled', 'request_timeout']) {
  test(`a public ${code} refusal is observed as an error in both directions`, async (t) => {
    const consumer = recorder();
    const machine = recorder();
    const { client, server } = await paired({ observer: consumer }, { observer: machine });
    t.after(() => client.close());
    server.handle('deny', () => {
      throw new DuplexError(code, 'Refused.');
    });
    await assert.rejects(client.call('deny'), { code });
    for (const [observer, incoming] of [
      [consumer, false],
      [machine, true],
    ] as const) {
      assert.deepEqual(observer.events.filter((event) => event.type === 'request.ended').map(shape), [
        {
          type: 'request.ended',
          id: 'c:1',
          method: 'deny',
          incoming,
          durationMs: true,
          outcome: 'error',
          errorCode: code,
          family: '',
        },
      ]);
    }
  });
}

test('a handler public refusal stays an error after the caller withdraws', async (t) => {
  const started = deferred();
  const ended = deferred<Extract<ObserverEvent, { type: 'request.ended' }>>();
  const { client, server } = await paired(
    {},
    {
      observer: {
        observe(event) {
          if (event.type === 'request.ended') ended.resolve(event);
        },
      },
    },
  );
  t.after(() => client.close());
  server.handle(
    'deny',
    (_params, context) =>
      new Promise((_resolve, reject) => {
        started.resolve();
        context.signal.addEventListener('abort', () => reject(new DuplexError('cancelled', 'Refused.')), {
          once: true,
        });
      }),
  );
  const controller = new AbortController();
  const call = assert.rejects(client.call('deny', {}, { signal: controller.signal }), { code: 'cancelled' });
  await started.promise;
  controller.abort();
  await call;
  assert.deepEqual(shape(await ended.promise), {
    type: 'request.ended',
    id: 'c:1',
    method: 'deny',
    incoming: true,
    durationMs: true,
    outcome: 'error',
    errorCode: 'cancelled',
    family: '',
  });
});

test('a cancellation before handler dispatch is observed once as a local cancellation', async (t) => {
  const socket = new Socket();
  const watching = recorder();
  const responded = deferred();
  const peer = new DuplexPeer({
    observer: {
      observe(event) {
        watching.observe(event);
        if (event.type === 'frame.sent' && event.kind === 'response') responded.resolve();
      },
    },
  });
  t.after(() => peer.close());
  await peer.attach(socket);
  let dispatched = false;
  peer.handle('wait', () => {
    dispatched = true;
    return null;
  });
  socket.receive({ version: 1, kind: 'request', id: 's:1', method: 'wait', params: {} });
  socket.receive({ version: 1, kind: 'cancel', id: 's:1' });
  await responded.promise;
  assert.equal(dispatched, false);
  assert.deepEqual(watching.events.filter((event) => event.type === 'request.ended').map(shape), [
    {
      type: 'request.ended',
      id: 's:1',
      method: 'wait',
      incoming: true,
      durationMs: true,
      outcome: 'cancelled',
      errorCode: 'cancelled',
      family: '',
    },
  ]);
  assert.equal(
    watching.events.some((event) => event.type === 'handler.panic'),
    false,
  );
  assert.deepEqual(socket.sent.find((frame) => frame.kind === 'response')?.error, {
    code: 'cancelled',
    message: 'Request was cancelled.',
  });
});

for (const outcome of ['cancelled', 'timeout'] as const) {
  test(`a local ${outcome} is observed before its cancel, and the receiver observes a withdrawal`, async (t) => {
    const consumer = recorder();
    const cancelSent = deferred();
    const incomingEnded = deferred<Extract<ObserverEvent, { type: 'request.ended' }>>();
    const { client, server } = await paired(
      {
        observer: {
          observe(event) {
            consumer.observe(event);
            if (event.type === 'frame.sent' && event.kind === 'cancel') cancelSent.resolve();
          },
        },
      },
      {
        observer: {
          observe(event) {
            if (event.type === 'request.ended') incomingEnded.resolve(event);
          },
        },
      },
    );
    t.after(() => client.close());
    const started = deferred();
    server.handle('wait', (_params, context) => {
      started.resolve();
      return new Promise((resolve) => {
        context.signal.addEventListener('abort', () => resolve(null), { once: true });
      });
    });
    const controller = new AbortController();
    const code = outcome === 'timeout' ? 'request_timeout' : 'cancelled';
    const call = assert.rejects(
      client.call('wait', {}, { signal: controller.signal, timeoutMs: outcome === 'timeout' ? 20 : 5000 }),
      { code },
    );
    await started.promise;
    if (outcome === 'cancelled') controller.abort();
    await call;
    await cancelSent.promise;
    const endedIndex = consumer.events.findIndex((event) => event.type === 'request.ended');
    const cancelIndex = consumer.events.findIndex((event) => event.type === 'frame.sent' && event.kind === 'cancel');
    assert.ok(endedIndex >= 0 && endedIndex < cancelIndex);
    assert.deepEqual(shape(consumer.events[endedIndex]), {
      type: 'request.ended',
      id: 'c:1',
      method: 'wait',
      incoming: false,
      durationMs: true,
      outcome,
      errorCode: code,
      family: '',
    });
    assert.deepEqual(shape(await incomingEnded.promise), {
      type: 'request.ended',
      id: 'c:1',
      method: 'wait',
      incoming: true,
      durationMs: true,
      outcome: 'cancelled',
      errorCode: 'cancelled',
      family: '',
    });
  });
}

test('a handler deadline is observed locally as a timeout and remotely as a refusal', async (t) => {
  const consumer = recorder();
  const machine = recorder();
  const { client, server } = await paired({ observer: consumer }, { observer: machine, requestTimeoutMs: 20 });
  t.after(() => client.close());
  server.handle(
    'wait',
    (_params, context) =>
      new Promise((resolve) => {
        context.signal.addEventListener('abort', () => resolve(null), { once: true });
      }),
  );
  await assert.rejects(client.call('wait'), { code: 'cancelled' });
  assert.deepEqual(machine.events.filter((event) => event.type === 'request.ended').map(shape), [
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'wait',
      incoming: true,
      durationMs: true,
      outcome: 'timeout',
      errorCode: 'request_timeout',
      family: '',
    },
  ]);
  assert.deepEqual(consumer.events.filter((event) => event.type === 'request.ended').map(shape), [
    {
      type: 'request.ended',
      id: 'c:1',
      method: 'wait',
      incoming: false,
      durationMs: true,
      outcome: 'error',
      errorCode: 'cancelled',
      family: '',
    },
  ]);
});

test('wire output overflow is observed once and accepted writes retain their transport deadline', async () => {
  // A socket whose buffer never drains: the first frame waits, the second meets a full queue.
  // A frame that merely waits on the socket is not backpressure; a queue full or a deadline passed is, as the Go peer tells it.
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const full = recorder();
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 40, observer: full });
  await peer.attach(socket);
  emitWire(peer.wire(), ['first']);
  await Promise.resolve();
  // The queue limit ends admission immediately and reports one terminal
  // pressure event; a transport timeout is a separate case below.
  const ended = deferred<DuplexError>();
  peer.onClose(ended.resolve);
  emitWire(peer.wire(), ['second']);
  assert.equal((await ended.promise).code, 'busy');
  assert.equal(peer.status, 'disconnected');
  assert.deepEqual(backpressure(full.events), [{ type: 'backpressure', queued: 1, stalled: true, deadlineMs: 40 }]);

  const slow = new Socket();
  slow.bufferedAmount = 1;
  const missed = recorder();
  const blocked = new DuplexPeer({ writeTimeoutMs: 10, observer: missed });
  const gaveOut = deferred<DuplexError>();
  blocked.onClose(gaveOut.resolve);
  await blocked.attach(slow);
  // The emit is accepted for sending and the caller is told so; what the
  // deadline holds is the queue it went into, and a frame that never drains
  // is what passes it.
  await blocked.emit('blocked');
  assert.equal((await gaveOut.promise).code, 'write_timeout');
  assert.deepEqual(backpressure(missed.events), [{ type: 'backpressure', queued: 1, stalled: true, deadlineMs: 10 }]);

  // The event queue is the other side of the same limit.
  const listening = new Socket();
  const queued = recorder();
  const receiver = new DuplexPeer({ queueCapacity: 1, observer: queued });
  await receiver.attach(listening);
  const held = deferred();
  receiver.onEvent(() => held.promise);
  listening.receive({ version: 1, kind: 'event', event: 'one', data: {} });
  listening.receive({ version: 1, kind: 'event', event: 'two', data: {} });
  // Paced, not disconnected: the consumer has a deadline to drain in and has
  // not passed it. This peer cannot pause what a socket hands it, so the
  // event is held where the Go peer stops reading; the deadline is the same.
  assert.equal(receiver.status, 'connected');
  held.resolve();
  await Promise.resolve();
  assert.deepEqual(backpressure(queued.events), [
    { type: 'backpressure', queued: 1, stalled: false, deadlineMs: 10_000 },
  ]);
  // Both are delivered, in order: the second was held while the first was in
  // hand and went the moment it was free, which is what pacing is for.
  assert.deepEqual(queued.events.filter((event) => event.type === 'event.delivered').map(shape), [
    { type: 'event.delivered', name: 'one', bytes: true, family: '' },
    { type: 'event.delivered', name: 'two', bytes: true, family: '' },
  ]);
});

test('an observer that throws interrupts no routing', async (t) => {
  const { client, server } = await paired({
    observer: {
      observe() {
        throw new Error('observer failed');
      },
    },
  });
  t.after(() => client.close());
  server.handle('ping', () => 'pong');
  assert.equal(await client.call('ping'), 'pong');
  assert.equal(client.status, 'connected');
});

test('a subprotocol is offered at the handshake and the selection is what the peer reports', async (t) => {
  const socket = new Socket();
  socket.readyState = 0;
  let offered: string[] | undefined;
  const peer = new DuplexPeer({
    subprotocols: ['a', 'b'],
    webSocketFactory: (_url, protocols) => {
      offered = protocols;
      return socket;
    },
  });
  t.after(() => peer.close());
  const connecting = peer.connect('ws://localhost/api');
  assert.deepEqual(offered, ['a', 'b'], 'the factory is handed what to offer, so a custom one honours it');
  assert.equal(peer.subprotocol, '', 'nothing is selected until the handshake is done');
  socket.protocol = 'b';
  socket.readyState = 1;
  socket.dispatchEvent(new Event('open'));
  await connecting;
  assert.equal(peer.subprotocol, 'b');
  peer.close();
  assert.equal(peer.subprotocol, '', 'a peer with no connection negotiated nothing');
});

test('an offer the server selected none of leaves the peer with none, and the profile is spoken anyway', async (t) => {
  const socket = new Socket();
  const peer = new DuplexPeer({ subprotocols: ['c'], webSocketFactory: () => socket });
  t.after(() => peer.close());
  await peer.connect('ws://localhost/api');
  assert.equal(peer.subprotocol, '');
  await peer.emit('progress', 1);
  assert.equal(socket.sent.length, 1);
  const { version, kind, event, data } = socket.sent[0] as Record<string, unknown>;
  assert.deepEqual({ version, kind, event, data }, { version: 1, kind: 'event', event: 'progress', data: 1 });
});

test('a peer that offers no subprotocol offers nothing at all', async (t) => {
  const socket = new Socket();
  let offered: string[] | undefined = ['unasked'];
  const peer = new DuplexPeer({
    webSocketFactory: (_url, protocols) => {
      offered = protocols;
      return socket;
    },
  });
  t.after(() => peer.close());
  await peer.connect('ws://localhost/api');
  assert.equal(offered, undefined);
  assert.equal(peer.subprotocol, '');
});

test('a peer over a connection that is no WebSocket negotiated nothing', async (t) => {
  const [near, far] = Pipe.pair();
  const peer = new DuplexPeer();
  const other = new DuplexPeer({ role: 'server' });
  t.after(() => {
    peer.close();
    other.close();
  });
  await peer.attach(near);
  await other.attach(far);
  assert.equal(peer.subprotocol, '');
  assert.equal(other.subprotocol, '');
});

test('a frame is observed sent immediately before its bytes reach the transport', async () => {
  // The ordering promise of docs/runtime/observer.md, held where it is narrowest:
  // one place observes every send, and it is the writer. Where the send was
  // observed by whoever queued the frame, a queue holding two frames observed
  // both before either reached the transport, and anything the first drew
  // could be observed received before the second was observed sent.
  const order: string[] = [];
  const socket = new Socket();
  const write = socket.send.bind(socket);
  socket.send = (text: string) => {
    order.push(`wire ${(JSON.parse(text) as { event: string }).event}`);
    write(text);
  };
  const peer = new DuplexPeer({
    observer: {
      observe(event) {
        if (event.type === 'frame.sent') order.push(`sent ${event.name}`);
      },
    },
  });
  await peer.attach(socket);

  // Nothing drains while the socket is behind, so both frames are queued before
  // either is written; the queue then empties in order.
  socket.bufferedAmount = 1;
  await peer.emit('one');
  await peer.emit('two');
  socket.bufferedAmount = 0;
  for (let waited = 0; waited < 50 && socket.sent.length < 2; waited++)
    await new Promise((resolve) => setTimeout(resolve, 5));

  assert.deepEqual(order, ['sent one', 'wire one', 'sent two', 'wire two']);
});
