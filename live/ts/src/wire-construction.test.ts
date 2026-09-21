import assert from 'node:assert/strict';
import { createServer, createConnection, type Socket } from 'node:net';
import test, { type TestContext } from 'node:test';
import { at, mount, pipe, type Wire, type FrameConnection, type ConnectionHandlers } from '@nightseam/duplex';
import { callWire, handleWire, wirePair, DuplexError, DuplexPeer, UnpublishedError } from '@nightseam/runtime';
import { liveOver, type LiveOwner, type LiveScope, type Reference } from './index.ts';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}

// The attempted composition keeps import-time interpretation and the active
// owner explicit. This helper is evidence, not an additional shipped live API.
function checkedBindingWire(t: TestContext, owner: LiveOwner, ref: Reference, contract: string): Wire {
  const invoke = owner.import(ref, contract, '');
  const { binding } = ref.toJSON();
  const [access, endpoint] = wirePair();
  t.after(() => access.close());
  t.after(handleWire(endpoint, [binding], (params, context) => invoke(params, { signal: context.signal })));
  return at(access, [binding]);
}

interface Pair {
  a: LiveScope;
  b: LiveScope;
  close(): void;
}

// A text-frame adapter over an actual TCP socket. JSON text has no literal
// newline, so newline framing leaves the production profile bytes unchanged.
function textSocket(socket: Socket): FrameConnection {
  let state: 'open' | 'closed' = 'open';
  let pending = '';
  const listeners = new Set<ConnectionHandlers>();
  socket.setEncoding('utf8');
  socket.on('data', (chunk: string) => {
    pending += chunk;
    let end: number;
    while ((end = pending.indexOf('\n')) >= 0) {
      const data = pending.slice(0, end);
      pending = pending.slice(end + 1);
      for (const listener of listeners) listener.frame?.({ kind: 'text', data });
    }
  });
  socket.on('error', () => {
    for (const listener of listeners) listener.error?.();
  });
  socket.on('close', () => {
    state = 'closed';
    for (const listener of listeners) listener.close?.(1000, '');
  });
  return {
    get state() {
      return state;
    },
    get buffered() {
      return socket.writableLength;
    },
    send(frame) {
      if (state !== 'open' || frame.kind !== 'text') throw new Error('Text socket is not writable.');
      socket.write(frame.data + '\n');
    },
    close() {
      state = 'closed';
      socket.destroy();
    },
    listen(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

async function scopes(t: TestContext, socket = false): Promise<Pair> {
  let connections: [FrameConnection, FrameConnection];
  if (socket) {
    const accepted = deferred<Socket>();
    const server = createServer((connection) => accepted.resolve(connection));
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    assert.ok(address && typeof address !== 'string');
    const client = createConnection({ port: address.port, host: '127.0.0.1' });
    await new Promise<void>((resolve, reject) => {
      client.once('connect', resolve);
      client.once('error', reject);
    });
    const remote = await accepted.promise;
    connections = [textSocket(client), textSocket(remote)];
    t.after(() => {
      client.destroy();
      remote.destroy();
      server.close();
    });
  } else connections = pipe();
  const pa = new DuplexPeer({ role: 'client' });
  const pb = new DuplexPeer({ role: 'server' });
  const a = liveOver(pa);
  const b = liveOver(pb);
  await Promise.all([pa.attach(connections[0]), pb.attach(connections[1])]);
  const result = {
    a,
    b,
    close: () => {
      pa.close();
      pb.close();
    },
  };
  t.after(result.close);
  return result;
}

for (const carrier of ['local', 'socket']) {
  test(`live binding Wire construction retains guards and release barrier over ${carrier}`, async (t) => {
    const p = await scopes(t, carrier === 'socket');
    const holder = p.a.owner().child();
    const target = (carrier === 'local' ? p.a : p.b).owner().child();
    let guards = 0,
      effects = 0,
      permitted = false;
    const entered = deferred<void>();
    const finish = deferred<void>();
    let ref = target.export('test/Guarded', '', async (params) => {
      guards++;
      if (!permitted) throw new DuplexError('denied', 'guard denied');
      effects++;
      entered.resolve();
      await finish.promise;
      return params;
    });
    if (carrier === 'socket') ref = p.a.decode(ref.toJSON());
    const selected = checkedBindingWire(t, holder, ref, 'test/Guarded');
    const wire = at(mount(new Map([['outer', mount(new Map([['binding', selected]]))]])), ['outer', 'binding']);
    await assert.rejects(callWire(wire, [], 1), { code: 'denied' });
    assert.equal(effects, 0, 'wire bypassed the explicit guard');
    permitted = true;
    const pending = callWire(wire, [], 42);
    await entered.promise;
    assert.deepEqual(target.counts(), { exports: 1, imports: 0 });
    assert.deepEqual(holder.counts(), { exports: 0, imports: carrier === 'socket' ? 1 : 0 });
    (carrier === 'socket' ? holder : target).release();
    await assert.rejects(callWire(wire, [], 43), { code: 'reference_released' });
    finish.resolve();
    assert.equal(await pending, 42, 'release cancelled an admitted invocation');
    assert.equal(guards, 2);
    assert.equal(effects, 1);
    holder.release();
    target.release();
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
    assert.deepEqual(p.b.counts(), { exports: 0, imports: 0 });
  });
}

test('live binding Wire construction checks interpretation and nonce', async (t) => {
  const p = await scopes(t);
  let effects = 0;
  const ref = p.b.owner().export('test/Checked', '', async (value) => {
    effects++;
    return value;
  });
  const holder = p.a.owner().child();
  assert.throws(() => checkedBindingWire(t, holder, ref, 'test/Checked'), { code: 'reference_foreign' });
  const arrived = p.a.decode(ref.toJSON());
  assert.throws(() => checkedBindingWire(t, holder, arrived, 'test/Other'), { code: 'contract_mismatch' });
  assert.deepEqual(holder.counts(), { exports: 0, imports: 0 });
  assert.equal(effects, 0);
  const unknown = p.a.decode({ binding: '0000000000000000.1', contract: 'test/Checked' });
  const wire = checkedBindingWire(t, holder, unknown, 'test/Checked');
  await assert.rejects(callWire(wire, [], 1), { code: 'reference_unknown' });
  assert.equal(effects, 0);
  assert.deepEqual(holder.counts(), { exports: 0, imports: 1 });
  holder.release();
  p.b.owner().release();
  assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(p.b.counts(), { exports: 0, imports: 0 });
});

test('live binding Wire construction retains uncertain publication', async (t) => {
  const p = await scopes(t);
  const target = p.a.owner().child();
  const outgoing = p.a.owner().child();
  const retained = deferred<ReturnType<LiveOwner['import']>>();
  const ref = target.export('test/Retain', '', async (payload, options) => {
    retained.resolve(target.import(p.a.decode(payload), 'test/Callback', ''));
    await new Promise<void>((resolve) => {
      options!.signal!.addEventListener('abort', () => resolve(), { once: true });
      if (options!.signal!.aborted) resolve();
    });
    return null;
  });
  const wire = checkedBindingWire(t, p.a.owner(), ref, 'test/Retain');
  const controller = new AbortController();
  let callbacks = 0;
  const pending = outgoing.publishValue(
    (owner) =>
      owner.export('test/Callback', '', async (value) => {
        callbacks++;
        return value;
      }),
    (payload) => callWire(wire, [], payload, { signal: controller.signal }),
  );
  const callback = await retained.promise;
  controller.abort();
  await assert.rejects(pending, (error) => !(error instanceof UnpublishedError));
  assert.deepEqual(outgoing.counts(), { exports: 1, imports: 0 });
  assert.equal(await callback(51), 51);
  assert.equal(callbacks, 1);
  outgoing.release();
  target.release();
  assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(p.b.counts(), { exports: 0, imports: 0 });
});

test('exporting an imported binding Wire gives the destination its own lifetime', async (t) => {
  const origin = await scopes(t);
  const destination = await scopes(t);
  const sourceOwner = origin.a.owner().child();
  const sourceHolder = origin.b.owner().child();
  const forwardOwner = destination.a.owner().child();
  const forwardHolder = destination.b.owner().child();
  let calls = 0;
  const ref = sourceOwner.export('test/Forward', '', async (value) => {
    calls++;
    return value;
  });
  const imported = checkedBindingWire(t, sourceHolder, origin.b.decode(ref.toJSON()), 'test/Forward');
  // Opaque JSON only: forwarding does not discover or translate references
  // hidden inside a model payload. Those positions need generated converters.
  const forwarded = forwardOwner.export('test/Forward', '', (value, options) =>
    callWire(imported, [], value, { signal: options?.signal }),
  );
  const access = checkedBindingWire(t, forwardHolder, destination.b.decode(forwarded.toJSON()), 'test/Forward');
  assert.equal(await callWire(access, [], 73), 73);
  assert.equal(calls, 1);
  assert.deepEqual(origin.a.counts(), { exports: 1, imports: 0 });
  assert.deepEqual(origin.b.counts(), { exports: 0, imports: 1 });
  assert.deepEqual(destination.a.counts(), { exports: 1, imports: 0 });
  assert.deepEqual(destination.b.counts(), { exports: 0, imports: 1 });
  forwardHolder.release();
  forwardOwner.release();
  await assert.rejects(callWire(access, [], 74), { code: 'reference_released' });
  assert.equal(await callWire(imported, [], 75), 75);
  assert.equal(calls, 2);
  assert.deepEqual(origin.a.counts(), { exports: 1, imports: 0 });
  assert.deepEqual(origin.b.counts(), { exports: 0, imports: 1 });
  assert.deepEqual(destination.a.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(destination.b.counts(), { exports: 0, imports: 0 });
  sourceHolder.release();
  sourceOwner.release();
  assert.deepEqual(origin.a.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(origin.b.counts(), { exports: 0, imports: 0 });
});

test('closing a binding Wire exposure is not the live release barrier', async (t) => {
  const p = await scopes(t);
  const owner = p.a.owner().child();
  const entered = deferred<void>();
  const finish = deferred<void>();
  let calls = 0;
  const ref = owner.export('test/Close', '', async (value) => {
    if (++calls === 1) {
      entered.resolve();
      await finish.promise;
    }
    return value;
  });
  const wire = checkedBindingWire(t, owner, ref, 'test/Close');
  const pending = callWire(wire, [], 81);
  await entered.promise;
  wire.close(1000, 'end this exposure');
  await assert.rejects(pending, { code: 'disconnected' });
  finish.resolve();
  assert.deepEqual(owner.counts(), { exports: 1, imports: 0 });
  // Carrier close ended the outstanding reply, but the intact live scope
  // still owns the binding. It therefore cannot stand in for owner release.
  const invoke = owner.import(ref, 'test/Close', '');
  assert.equal(await invoke(82), 82);
  assert.equal(calls, 2);
  owner.release();
  assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(p.b.counts(), { exports: 0, imports: 0 });
});
