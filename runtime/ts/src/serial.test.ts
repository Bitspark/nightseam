import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { DuplexPeer } from './peer.ts';
import type { PeerOptions, WebSocketLike } from './peer.ts';
import { callWire } from './wire.ts';

/** A raw socket, so a test spells the frames it sends byte for byte. */
class RawSocket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  protocol = '';
  sent: Record<string, unknown>[] = [];
  send(text: string): void {
    if (this.readyState !== 1) throw new Error('Closed');
    this.sent.push(JSON.parse(text) as Record<string, unknown>);
  }
  receive(frame: string): void {
    this.dispatchEvent(new MessageEvent('message', { data: frame }));
  }
  close(): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
  }
}

async function raw(options: PeerOptions = {}) {
  const socket = new RawSocket();
  const peer = new DuplexPeer({ ...options, role: 'server' });
  await peer.attach(socket);
  return { socket, peer };
}

const request = (id: string, method = 'echo'): string =>
  JSON.stringify({ version: 1, kind: 'request', id, method, params: 1 });

// A serial that does not increase is a protocol violation: the peer could not
// otherwise say which invocation a later control names.
for (const second of ['c:3', 'c:5', 'c:1']) {
  test(`a request serial of ${second} after c:5 ends the connection`, async (t) => {
    const { socket, peer } = await raw();
    t.after(() => peer.close());
    peer.handle('echo', (data) => data);
    socket.receive(request('c:5'));
    socket.receive(request(second));
    await nextTurn();
    assert.equal(socket.readyState, 3, 'a serial that did not increase was admitted');
  });
}

test('request serials may leave gaps', async (t) => {
  const { socket, peer } = await raw();
  t.after(() => peer.close());
  peer.handle('echo', (data) => data);
  for (const id of ['c:2', 'c:9', 'c:10000']) socket.receive(request(id));
  await nextTurn();
  await nextTurn();
  assert.equal(socket.readyState, 1, 'gaps ended the connection');
  assert.deepEqual(
    socket.sent.filter((frame) => frame.kind === 'response').map((frame) => frame.id),
    ['c:2', 'c:9', 'c:10000'],
  );
});

test('only request admission advances the mark', async (t) => {
  const { socket, peer } = await raw();
  t.after(() => peer.close());
  peer.handle(
    'wait',
    (_data, context) =>
      new Promise((_resolve, reject) => {
        context.signal.addEventListener('abort', () => reject(new Error('cancelled')), { once: true });
      }),
  );
  socket.receive(JSON.stringify({ version: 1, kind: 'request', id: 'c:4', method: 'wait', params: null }));
  socket.receive(JSON.stringify({ version: 1, kind: 'cancel', id: 'c:4' }));
  socket.receive(JSON.stringify({ version: 1, kind: 'response', id: 's:1', result: null }));
  socket.receive(JSON.stringify({ version: 1, kind: 'request', id: 'c:5', method: 'wait', params: null }));
  await nextTurn();
  await nextTurn();
  assert.equal(socket.readyState, 1, 'a control or a response advanced the mark');
});

test('serials are published in the order they were reserved', async (t) => {
  const { socket, peer } = await raw({ queueCapacity: 4 });
  t.after(() => peer.close());
  const calls: Promise<unknown>[] = [];
  for (let index = 0; index < 24; index++) calls.push(peer.call('probe').catch(() => undefined));
  await nextTurn();
  await nextTurn();
  let previous = 0;
  for (const frame of socket.sent) {
    if (frame.kind !== 'request') continue;
    const serial = Number((frame.id as string).slice('s:'.length));
    assert.ok(serial > previous, `published ${serial} after ${previous}`);
    previous = serial;
  }
  assert.ok(previous > 0, 'nothing was published');
  peer.close();
  await Promise.all(calls);
});

test('a carrier bridge mints its own serials', async (t) => {
  const { socket, peer } = await raw();
  t.after(() => peer.close());
  void callWire(peer.wire(), ['probe']).catch(() => undefined);
  await nextTurn();
  const published = socket.sent.find((frame) => frame.kind === 'request');
  assert.ok(published, 'the bridge published nothing');
  const id = published.id as string;
  assert.ok(id.startsWith('s:'), `the bridge published ${id} rather than a serial of its own`);
  assert.ok(Number(id.slice(2)) > 0, `the bridge published ${id}`);
});
