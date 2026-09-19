import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { DuplexPeer, decodeEnvelope } from './peer.ts';
import type { EventContext, Meta, RequestContext, WebSocketLike } from './peer.ts';
import type { ObserverEvent } from './observer.ts';

/**
 * The carriage a request and an event may take: what is about the call rather
 * than the call. The peer accepts it and keeps it on the decoded envelope, and
 * sends what a call's or an event's options gave it — never one of its own.
 */

/** The socket of peer.test.ts, with only what a carriage assertion needs of it. */
class Socket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  sent: Record<string, unknown>[] = [];
  partner?: Socket;
  closed?: { code: number; reason: string };
  send(text: string): void {
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
  close(code?: number, reason?: string): void {
    this.closed ??= { code: code ?? 1005, reason: reason ?? '' };
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
  }
}

/** decodeEnvelope as a peer reads an incoming frame: the remote is the client. */
function decode(frame: unknown): Record<string, unknown> {
  return decodeEnvelope(JSON.stringify(frame), 's:', 'c:');
}

const REQUEST = { version: 1, kind: 'request', id: 'c:1', method: 'read', params: {} };
const EVENT = { version: 1, kind: 'event', event: 'updated', data: 1 };
const RESPONSE = { version: 1, kind: 'response', id: 's:1', result: 1 };
const CANCEL = { version: 1, kind: 'cancel', id: 'c:1' };

test('a request and an event carry meta, and it is kept on the decoded envelope', () => {
  const carried = { tenant: 'acme', idempotency: 'k-1' };
  assert.deepEqual(decode({ ...REQUEST, meta: carried }).meta, carried);
  assert.deepEqual(decode({ ...EVENT, meta: { cause: 'nightly' } }).meta, { cause: 'nightly' });
  // A carriage with nothing in it is a carriage, and is kept as one.
  assert.deepEqual(decode({ ...REQUEST, meta: {} }).meta, {});
  // The members are independent: a frame may carry both, and each arrives whole.
  const traceparent = '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01';
  const both = decode({ ...EVENT, meta: { tenant: 'acme' }, traceparent });
  assert.deepEqual(both.meta, { tenant: 'acme' });
  assert.equal(both.traceparent, traceparent);
  // A frame carrying none leaves the member absent rather than empty, so the
  // emitting half can tell a carriage with nothing in it from no carriage.
  assert.equal(Object.hasOwn(decode(REQUEST), 'meta'), false);
});

test('meta is refused on a response and a cancel, in any other form, and under the reserved prefix', () => {
  const refused: unknown[] = [
    // A response says what it says in its result; a cancel withdraws a call
    // rather than making one.
    { ...RESPONSE, meta: { tenant: 'acme' } },
    { ...CANCEL, meta: { tenant: 'acme' } },
    // An object of strings, and nothing else.
    { ...REQUEST, meta: 'acme' },
    { ...REQUEST, meta: ['acme'] },
    { ...REQUEST, meta: 7 },
    { ...REQUEST, meta: null },
    { ...REQUEST, meta: { attempt: 2 } },
    { ...REQUEST, meta: { tenant: null } },
    { ...EVENT, meta: { live: true } },
    { ...EVENT, meta: { who: { id: 'u1' } } },
    // The namespace the profile keeps for itself, which it fills with nothing
    // in this version.
    { ...REQUEST, meta: { 'nightseam.deadline': '2026-01-01T00:00:00Z' } },
    { ...EVENT, meta: { 'nightseam.cause': 'nightly' } },
  ];
  for (const frame of refused) {
    assert.throws(() => decode(frame), JSON.stringify(frame));
  }
});

test('every row of the conformance table is judged as the table judges it', () => {
  const table = JSON.parse(
    readFileSync(new URL('../../../conformance/tables/frames.json', import.meta.url), 'utf8'),
  ) as {
    rows: { name: string; to: string; frame: string; valid: boolean }[];
  };
  let carriages = 0;
  for (const row of table.rows) {
    // A row addressed to the server carries the client's ids and answers the
    // server's; one addressed to either is read as a server's.
    const [local, remote] = row.to === 'client' ? ['c:', 's:'] : ['s:', 'c:'];
    let accepted = true;
    try {
      decodeEnvelope(row.frame, local, remote);
    } catch {
      accepted = false;
    }
    assert.equal(accepted, row.valid, row.name);
    let members: unknown;
    try {
      members = JSON.parse(row.frame);
    } catch {
      continue;
    }
    if (typeof members === 'object' && members !== null && Object.hasOwn(members, 'meta')) carriages++;
  }
  assert.ok(
    table.rows.length >= 70 && carriages >= 12,
    `the table holds ${table.rows.length} rows and names meta in ${carriages}; the wire is held by more than that`,
  );
});

test('a frame with a refused meta closes the connection with 4011', async () => {
  const socket = new Socket();
  const peer = new DuplexPeer({ role: 'server' });
  await peer.attach(socket);
  socket.receive({ ...REQUEST, meta: { 'nightseam.cause': 'nightly' } });
  assert.equal(peer.status, 'disconnected');
  assert.equal(socket.closed?.code, 4011);
});

/** Two peers over one pair of sockets, as peer.test.ts pairs them. */
async function pair() {
  const clientSocket = new Socket();
  const serverSocket = new Socket();
  clientSocket.partner = serverSocket;
  serverSocket.partner = clientSocket;
  const client = new DuplexPeer({ role: 'client' });
  const server = new DuplexPeer({ role: 'server' });
  await client.attach(clientSocket);
  await server.attach(serverSocket);
  return { client, server, clientSocket };
}

test('a call and an event carry the meta their options gave, and a bare one carries none', async (t) => {
  const { client, server, clientSocket } = await pair();
  t.after(() => {
    client.close();
    server.close();
  });
  server.handle('read', () => 1);
  const carried = { tenant: 'acme', idempotency: 'k-1' };
  await client.call('read', {}, { meta: carried });
  await client.emit('updated', 1, { meta: { cause: 'nightly' } });
  await client.emit('updated', 1);
  // A key of the reserved prefix is the profile's; the option drops it rather
  // than sending a frame the far peer would refuse.
  await client.emit('updated', 1, { meta: { 'nightseam.cause': 'nightly', tenant: 'acme' } });
  const sent = clientSocket.sent;
  assert.deepEqual(sent.find((frame) => frame.kind === 'request')?.meta, carried);
  const events = sent.filter((frame) => frame.kind === 'event');
  assert.deepEqual(events[0]?.meta, { cause: 'nightly' });
  assert.equal(Object.hasOwn(events[1] ?? {}, 'meta'), false, 'a bare event carries the member nowhere');
  assert.deepEqual(events[2]?.meta, { tenant: 'acme' });
  // A carriage left with nothing in it is not sent at all.
  await client.emit('updated', 1, { meta: { 'nightseam.cause': 'nightly' } });
  const last = sent.filter((frame) => frame.kind === 'event').at(-1);
  assert.equal(Object.hasOwn(last ?? {}, 'meta'), false);
});

test('a handler reads the meta of its frame and forwards nothing of itself', async (t) => {
  const { client, server } = await pair();
  t.after(() => {
    client.close();
    server.close();
  });
  const nested: (Meta | undefined)[] = [];
  const delivered: (Meta | undefined)[] = [];
  client.handle('reverse', (_params, context: RequestContext) => {
    nested.push(context.meta);
    return 'back';
  });
  // Reads its own meta, then calls back without saying to forward it.
  server.handle('read', async (_params, context: RequestContext) => {
    await context.peer.call('reverse');
    return context.meta ?? null;
  });
  // Reads its own meta, then forwards it as a handler must say to.
  server.handle('relay', async (_params, context: RequestContext) => {
    await context.peer.call('reverse', {}, { meta: context.meta });
    return null;
  });
  server.onEvent('updated', (_data, context: EventContext) => {
    delivered.push(context.meta);
  });

  const carried = { tenant: 'acme' };
  assert.deepEqual(await client.call('read', {}, { meta: carried }), carried);
  assert.equal(nested.at(-1), undefined, "a call from the handler carried the caller's meta of its own accord");
  await client.call('relay', {}, { meta: carried });
  assert.deepEqual(nested.at(-1), carried);

  await client.emit('updated', 1, { meta: { cause: 'nightly' } });
  await tick();
  assert.deepEqual(delivered.at(-1), { cause: 'nightly' });
  await client.emit('updated', 1);
  await tick();
  assert.equal(delivered.at(-1), undefined, 'a listener of a bare event reads none');
});

function tick(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

test('no meta value reaches an observer, by any path', async (t) => {
  // The carriage may hold a credential, so it is held to the rule the payloads
  // are: an observer is told names, ids, sizes and outcomes, and nothing of
  // what a frame carried.
  const sentinel = 'payload-sentinel-4bf92f35';
  const seen: ObserverEvent[] = [];
  const clientSocket = new Socket();
  const serverSocket = new Socket();
  clientSocket.partner = serverSocket;
  serverSocket.partner = clientSocket;
  const client = new DuplexPeer({
    role: 'client',
    observer: {
      observe: (event) => {
        seen.push(event);
      },
    },
  });
  const server = new DuplexPeer({
    role: 'server',
    observer: {
      observe: (event) => {
        seen.push(event);
      },
    },
  });
  await client.attach(clientSocket);
  await server.attach(serverSocket);
  t.after(() => {
    client.close();
    server.close();
  });
  server.handle('echo', (params: unknown) => params);
  server.onEvent('told', () => {});
  const carried = { secret: sentinel };
  await client.call('echo', { text: sentinel }, { meta: carried });
  await client.emit('told', { text: sentinel }, { meta: carried });
  await tick();
  // The carriage did travel: the frames carried it, so this is about what the
  // observers were spared and not about an idle connection.
  assert.ok(clientSocket.sent.some((frame) => JSON.stringify(frame.meta) === JSON.stringify(carried)));
  assert.ok(seen.length > 0);
  for (const event of seen) {
    assert.equal(JSON.stringify(event).includes(sentinel), false, event.type);
  }
});
