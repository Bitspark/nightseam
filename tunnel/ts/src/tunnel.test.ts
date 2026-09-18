import assert from 'node:assert/strict';
import test from 'node:test';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { pipe } from '@nightseam/duplex';
import type { Frame } from '@nightseam/duplex';
import { Tunnel, type Channel, type TunnelOptions } from './index.ts';

/** Two outer peers over an in-memory pipe, with a tunnel each. */
async function tunnels(options: TunnelOptions = {}) {
  const [left, right] = pipe();
  const client = new DuplexPeer({ role: 'client' });
  const server = new DuplexPeer({ role: 'server' });
  await Promise.all([client.attach(left), server.attach(right)]);
  return { client, server, ct: new Tunnel(client, options), st: new Tunnel(server, options) };
}

/** One channel opened by the client and accepted by the server. */
async function pair(ct: Tunnel, st: Tunnel, family = 'probe', after = 0): Promise<[Channel, Channel]> {
  const accepted = st.accept();
  const opened = await ct.open(family, after);
  return [opened, await accepted];
}

/** The frames a connection delivers, and a promise of the next one not yet taken. */
function collect(channel: Channel) {
  const frames: Frame[] = [];
  const waiters: ((frame: Frame) => void)[] = [];
  let taken = 0;
  let closed: { code: number; reason: string } | undefined;
  channel.listen({
    frame: frame => { frames.push(frame); waiters.shift()?.(frame); },
    close: (code, reason) => { closed = { code, reason }; },
  });
  return {
    frames,
    next: () => {
      if (taken < frames.length) return Promise.resolve(frames[taken++]!);
      taken++;
      return new Promise<Frame>(resolve => { waiters.push(resolve); });
    },
    get closed() { return closed; },
  };
}

const tick = () => new Promise(resolve => setTimeout(resolve, 5));

test('a channel opens, carries frames both ways in order, text and binary, and a handle resolves it', async () => {
  const { ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st, 'probe', 7);
  assert.equal(opened.id % 2, 1);
  assert.equal(accepted.id, opened.id);
  assert.equal(accepted.family, 'probe');
  assert.equal(accepted.after, 7);
  assert.equal(st.channel(opened.id), accepted);
  assert.equal(ct.channel(opened.id), opened);
  const atServer = collect(accepted);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: 'one' });
  opened.send({ kind: 'binary', data: new Uint8Array([1, 2, 3, 255]) });
  opened.send({ kind: 'text', data: 'three' });
  accepted.send({ kind: 'text', data: 'back' });
  await atServer.next(); await atServer.next(); await atServer.next();
  await atClient.next();
  assert.deepEqual(atServer.frames.map(frame => frame.kind), ['text', 'binary', 'text']);
  assert.equal(atServer.frames[0]!.data, 'one');
  assert.deepEqual([...(atServer.frames[1]!.data as Uint8Array)], [1, 2, 3, 255]);
  assert.equal(atServer.frames[2]!.data, 'three');
  assert.equal(atClient.frames[0]!.data, 'back');
});

test('either side opens, and ids never collide', async () => {
  const { ct, st } = await tunnels();
  const fromClient = await pair(ct, st);
  const fromServer = await pair(st, ct, 'codex');
  assert.equal(fromClient[0].id % 2, 1);
  assert.equal(fromServer[0].id % 2, 0);
  assert.equal(ct.channel(fromServer[0].id), fromServer[1]);
});

test('a close carries its code and reason across, after what was sent before it', async () => {
  const { ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  opened.send({ kind: 'text', data: 'last' });
  opened.close(1008, 'an observer sent a deciding frame');
  await atServer.next();
  await tick();
  assert.equal(atServer.frames[0]!.data, 'last');
  assert.deepEqual(atServer.closed, { code: 1008, reason: 'an observer sent a deciding frame' });
  assert.equal(accepted.state, 'closed');
  assert.equal(opened.state, 'closed');
  assert.throws(() => opened.send({ kind: 'text', data: 'late' }));
  assert.equal(st.channel(opened.id), undefined);
});

test('credit paces the sender: beyond the window a frame waits in the channel until the receiver takes one', async () => {
  const { ct, st } = await tunnels({ window: 2 });
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  opened.send({ kind: 'text', data: '1' });
  opened.send({ kind: 'text', data: '2' });
  opened.send({ kind: 'text', data: '3' });
  assert.equal(opened.buffered, 1);
  await atServer.next(); await atServer.next(); await atServer.next();
  assert.deepEqual(atServer.frames.map(frame => frame.data), ['1', '2', '3']);
  assert.equal(opened.buffered, 0);
});

test('a frame over the limit is refused, and the channel with it', async () => {
  const { ct, st } = await tunnels({ maxFrameBytes: 16 });
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: 'x'.repeat(17) });
  await tick(); await tick();
  assert.equal(atServer.frames.length, 0);
  assert.equal(atServer.closed?.code, 1009);
  assert.equal(atClient.closed?.code, 1009);
  assert.equal(accepted.state, 'closed');
});

test('an open beyond the accept capacity is refused, and the tunnel stands', async () => {
  const { ct } = await tunnels({ acceptCapacity: 1 });
  await ct.open('probe');
  await assert.rejects(ct.open('probe'), (error: unknown) => error instanceof DuplexError && error.code === 'channel_refused');
  await assert.rejects(ct.open(''), (error: unknown) => error instanceof DuplexError && error.code === 'channel_invalid');
});

test('a peer of the profile runs over a channel', async () => {
  const { ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st);
  const server = new DuplexPeer({ role: 'server' });
  server.handle('echo', params => params);
  const events: unknown[] = [];
  server.onEvent('hello', data => { events.push(data); });
  await server.attach(accepted);
  const client = new DuplexPeer({ role: 'client' });
  await client.attach(opened);
  assert.deepEqual(await client.call('echo', { through: 'the tunnel' }), { through: 'the tunnel' });
  await client.emit('hello', 'there');
  await tick();
  assert.deepEqual(events, ['there']);
  client.close();
  await tick();
  assert.equal(accepted.state, 'closed');
});

test('the connection carrying the channels closing ends every channel with going away', async () => {
  const { server, ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st);
  const atClient = collect(opened);
  const atServer = collect(accepted);
  const rejected = assert.rejects(st.accept(), (error: unknown) => error instanceof DuplexError && error.code === 'disconnected');
  server.close();
  await tick();
  assert.equal(atServer.closed?.code, 1001);
  assert.equal(atClient.closed?.code, 1001);
  await rejected;
});

test('frames that arrive before anyone listens are held, and delivered in order when someone does, with a close behind them', async () => {
  const { ct, st } = await tunnels({ window: 4 });
  const [opened, accepted] = await pair(ct, st);
  opened.send({ kind: 'text', data: 'early one' });
  opened.send({ kind: 'text', data: 'early two' });
  await tick();
  assert.equal(accepted.state, 'open');
  const late = collect(accepted);
  assert.deepEqual(late.frames.map(frame => frame.data), ['early one', 'early two']);
  opened.send({ kind: 'text', data: 'three' });
  await late.next(); await late.next(); await late.next();
  assert.equal(late.frames[2]!.data, 'three');
  const [again, acceptedAgain] = await pair(ct, st);
  again.send({ kind: 'text', data: 'last' });
  again.close(1000, 'done');
  await tick();
  assert.equal(acceptedAgain.state, 'closed');
  const afterwards = collect(acceptedAgain);
  assert.deepEqual(afterwards.frames.map(frame => frame.data), ['last']);
  assert.deepEqual(afterwards.closed, { code: 1000, reason: 'done' });
});
