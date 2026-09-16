import assert from 'node:assert/strict';
import test from 'node:test';
import { DuplexPeer, DuplexError } from './peer.ts';
import type { PeerOptions, WebSocketLike } from './peer.ts';

class Socket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  sent: Record<string, unknown>[] = [];
  partner?: Socket;
  closeCount = 0;
  send(text: string): void {
    if (this.readyState !== 1) throw new Error('Closed');
    this.sent.push(JSON.parse(text));
    const partner = this.partner;
    if (partner) queueMicrotask(() => { if (partner.readyState === 1) partner.receive(text); });
  }
  receive(frame: unknown): void {
    this.dispatchEvent(new MessageEvent('message', { data: typeof frame === 'string' ? frame : JSON.stringify(frame) }));
  }
  close(): void {
    this.closeCount++;
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
    this.partner?.close();
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

function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>(accept => { resolve = accept; });
  return { promise, resolve };
}

test('duplex routing permits reverse calls during an outstanding request', async t => {
  const { client, server, left, right } = await paired();
  t.after(() => client.close());
  client.handle('multiply', params => (params as { value: number }).value * 3);
  server.handle('roundtrip', async (_params, context) => ({ value: await context.peer.call('multiply', { value: 7 }) }));
  assert.deepEqual(await client.call('roundtrip'), { value: 21 });
  assert.equal(left.sent[0].id, 'c:1');
  assert.equal(right.sent[0].id, 's:1');
  assert.equal(left.sent[1].kind, 'response');
});

test('responses correlate out of order while events run independently', async t => {
  const { client, server } = await paired();
  t.after(() => client.close());
  const first = deferred<number>();
  const blockEvent = deferred();
  const eventStarted = deferred();
  server.handle('first', () => first.promise);
  server.handle('second', () => 2);
  client.onEvent('notice', async () => { eventStarted.resolve(); await blockEvent.promise; });
  const one = client.call('first');
  await server.emit('notice', { value: 1 });
  await eventStarted.promise;
  assert.equal(await client.call('second'), 2);
  first.resolve(1);
  assert.equal(await one, 1);
  blockEvent.resolve();
});

test('public handler errors survive and unexpected errors remain private', async t => {
  const { client, server } = await paired();
  t.after(() => client.close());
  server.handle('public', () => { throw new DuplexError('denied', 'Access denied', { field: 'project' }); });
  server.handle('private', () => { throw new Error('Database password: secret'); });
  await assert.rejects(client.call('public'), { code: 'denied', message: 'Access denied', data: { field: 'project' } });
  await assert.rejects(client.call('private'), { code: 'internal', message: 'Request handler failed.' });
  await assert.rejects(client.call('missing'), { code: 'method_not_found' });
});

test('AbortSignal sends cancellation and aborts the remote handler', async t => {
  const { client, server, left } = await paired();
  t.after(() => client.close());
  const started = deferred();
  const aborted = deferred();
  server.handle('wait', (_params, context) => new Promise(resolve => {
    context.signal.addEventListener('abort', () => { aborted.resolve(); resolve('ignored late result'); }, { once: true });
    started.resolve();
  }));
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

test('pre-aborted calls and outstanding capacity do not send extra requests', async t => {
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

test('local request deadline cancels remotely without retrying', async t => {
  const { client, server, left } = await paired();
  t.after(() => client.close());
  server.handle('wait', (_params, context) => new Promise(resolve => {
    context.signal.addEventListener('abort', () => resolve(null), { once: true });
  }));
  await assert.rejects(client.call('wait', {}, { timeoutMs: 10 }), { code: 'request_timeout' });
  assert.deepEqual(left.sent.map(frame => frame.kind), ['request', 'cancel']);
});

test('incoming deadlines abort handlers and retain occupied slots until completion', async t => {
  const { client, server } = await paired({}, { maxIncomingRequests: 1, requestTimeoutMs: 10 });
  t.after(() => client.close());
  const blocked = deferred();
  let signal: AbortSignal | undefined;
  server.handle('wait', (_params, context) => { signal = context.signal; return blocked.promise; });
  await assert.rejects(client.call('wait'), { code: 'request_timeout' });
  assert.equal(signal?.aborted, true);
  await assert.rejects(client.call('wait'), { code: 'busy' });
  blocked.resolve();
});

test('disconnect cancels handlers and rejects pending calls without reconnecting', async () => {
  const { client, server, left } = await paired();
  const started = deferred();
  const aborted = deferred();
  server.handle('wait', (_params, context) => new Promise(resolve => {
    context.signal.addEventListener('abort', () => { aborted.resolve(); resolve(1); }, { once: true });
    started.resolve();
  }));
  const waiting = client.call('wait');
  const failed = assert.rejects(waiting, { code: 'disconnected' });
  await started.promise;
  client.close();
  await Promise.all([failed, aborted.promise]);
  assert.equal(client.status, 'disconnected');
  assert.equal(server.status, 'disconnected');
  assert.equal(left.sent.filter(frame => frame.kind === 'request').length, 1);
  await assert.rejects(client.call('another'), { code: 'not_connected' });
});

test('incoming saturation responds busy without blocking responses', async t => {
  const { client, server } = await paired({}, { maxIncomingRequests: 1 });
  t.after(() => client.close());
  const occupied = deferred();
  const started = deferred();
  server.handle('wait', () => { started.resolve(); return occupied.promise; });
  client.handle('ping', () => 'pong');
  const first = client.call('wait');
  await started.promise;
  await assert.rejects(client.call('wait'), { code: 'busy' });
  assert.equal(await server.call('ping'), 'pong');
  occupied.resolve();
  assert.equal(await first, null);
});

test('output queues are bounded and a stalled socket is disconnected', async () => {
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const peer = new DuplexPeer({ maxQueuedMessages: 1 });
  await peer.attach(socket);
  const first = assert.rejects(peer.emit('first'), { code: 'busy' });
  await assert.rejects(peer.emit('second'), { code: 'busy' });
  await first;
  assert.equal(peer.status, 'disconnected');
});

test('socket output has a write deadline', async () => {
  const socket = new Socket();
  socket.bufferedAmount = 1;
  const peer = new DuplexPeer({ writeTimeoutMs: 10 });
  await peer.attach(socket);
  await assert.rejects(peer.emit('blocked'), { code: 'write_timeout' });
  assert.equal(peer.status, 'disconnected');
});

test('event queues are bounded and responses are still routed during a slow listener', async () => {
  const socket = new Socket();
  const peer = new DuplexPeer({ maxQueuedMessages: 1 });
  await peer.attach(socket);
  const blocked = deferred();
  peer.onEvent(() => blocked.promise);
  socket.receive({ version: 1, kind: 'event', event: 'one', data: {} });
  socket.receive({ version: 1, kind: 'event', event: 'two', data: {} });
  assert.equal(peer.status, 'disconnected');
  blocked.resolve();
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

test('large replay bursts do not queue already completed synchronous listeners', async t => {
  const socket = new Socket();
  const peer = new DuplexPeer();
  await peer.attach(socket);
  t.after(() => peer.close());
  const received: number[] = [];
  peer.onEvent('replay', value => { received.push(value as number); });
  for (let sequence = 1; sequence <= 300; sequence++) {
    socket.receive({ version: 1, kind: 'event', event: 'replay', data: sequence });
  }
  assert.equal(peer.status, 'connected');
  assert.deepEqual(received, Array.from({ length: 300 }, (_, index) => index + 1));
});

test('event subscriptions preserve order, isolate failures, and unsubscribe', async t => {
  const errors: string[] = [];
  const { client, server } = await paired({ onError: error => errors.push(error.code) });
  t.after(() => client.close());
  const observed: unknown[] = [];
  const done = deferred();
  client.onEvent('notice', () => { throw new Error('listener failure'); });
  const off = client.onEvent('notice', value => { observed.push(value); if (observed.length === 2) done.resolve(); });
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
    '{}', '{bad', { version: 2, kind: 'event', event: 'notice', data: null },
    { version: 1, kind: 'request', id: 'c:1', method: 'x', params: {} },
    { version: 1, kind: 'response', id: 'c:1', result: 1, error: { code: 'bad', message: 'bad' } },
    { version: 1, kind: 'event', event: 'x', data: 1, extra: true },
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

test('outgoing oversize or unserializable values reject without sending', async t => {
  const socket = new Socket();
  const peer = new DuplexPeer({ maxFrameBytes: 100 });
  await peer.attach(socket);
  t.after(() => peer.close());
  await assert.rejects(peer.emit('large', 'é'.repeat(100)), { code: 'frame_too_large' });
  await assert.rejects(peer.call('bigint', 1n), { code: 'invalid_message' });
  await assert.rejects(peer.call('function', () => 1), { code: 'invalid_message' });
  await assert.rejects(peer.emit('nan', Number.NaN), { code: 'invalid_message' });
  assert.equal(socket.sent.length, 0);
  assert.equal(peer.status, 'connected');
});

test('connection requires an explicit ws/wss endpoint and never sets browser headers', async t => {
  const socket = new Socket();
  socket.readyState = 0;
  let observedURL = '';
  const peer = new DuplexPeer({ webSocketFactory: url => { observedURL = url; return socket; } });
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

test('connection timeout closes the socket; manual reconnection remains explicit', async t => {
  const socket = new Socket();
  socket.readyState = 0;
  let created = 0;
  const peer = new DuplexPeer({ connectTimeoutMs: 10, webSocketFactory: () => { created++; return socket; } });
  await assert.rejects(peer.connect('ws://localhost/api'), { code: 'connect_timeout' });
  assert.equal(socket.readyState, 3);
  assert.equal(created, 1);
  await peer.attach(new Socket());
  t.after(() => peer.close());
  assert.equal(peer.status, 'connected');
});

test('late work from an old connection cannot answer a new connection', async t => {
  const peer = new DuplexPeer();
  const old = new Socket();
  const gate = deferred<string>();
  const started = deferred();
  peer.handle('wait', () => { started.resolve(); return gate.promise; });
  await peer.attach(old);
  old.receive({ version: 1, kind: 'request', id: 's:1', method: 'wait', params: {} });
  await started.promise;
  peer.close();
  const current = new Socket();
  await peer.attach(current);
  t.after(() => peer.close());
  gate.resolve('old');
  await gate.promise;
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(current.sent, []);
});

test('handler registration is explicit, removable, and rejects duplicates', async t => {
  const { client, server } = await paired();
  t.after(() => client.close());
  const remove = server.handle('x', () => 1);
  assert.throws(() => server.handle('x', () => 2), { code: 'duplicate_handler' });
  assert.equal(await client.call('x'), 1);
  remove();
  await assert.rejects(client.call('x'), { code: 'method_not_found' });
});
