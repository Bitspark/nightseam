import assert from 'node:assert/strict';
import test from 'node:test';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import type { ObserverEvent } from '@nightseam/runtime';
import { pipe } from '@nightseam/duplex';
import type { Frame } from '@nightseam/duplex';
import { Tunnel, type Connection, type TunnelOptions } from './index.ts';

/** The five a tunnel adds to the runtime's ten, which is how they are told apart here. */
const TUNNEL_EVENTS = new Set(['channel.opened', 'channel.accepted', 'channel.closed', 'credit.stall', 'open.refused']);

test('a send on a closed channel is a coded disconnection', async (t) => {
  const { client, server, ct, st } = await tunnels();
  t.after(() => {
    client.close();
    server.close();
  });
  const [channel] = await pair(ct, st);
  channel.close();
  assert.throws(() => channel.send({ kind: 'text', data: 'late' }), {
    name: 'DuplexError',
    code: 'disconnected',
    message: 'Connection is not open.',
  });
});

/** Everything one peer's observer was told, in order; a tunnel observes through no other. */
function watching() {
  const events: ObserverEvent[] = [];
  return {
    events,
    observe(event: ObserverEvent): void {
      events.push(event);
    },
    /** The tunnel's events alone, in the order they were told. */
    tunnel(): ObserverEvent[] {
      return events.filter((event) => TUNNEL_EVENTS.has(event.type));
    },
    of<K extends ObserverEvent['type']>(type: K): Extract<ObserverEvent, { type: K }>[] {
      return events.filter((event) => event.type === type) as Extract<ObserverEvent, { type: K }>[];
    },
  };
}

/** Two outer peers over an in-memory pipe, with a tunnel each and an observer each. */
async function tunnels(options: TunnelOptions = {}) {
  const [left, right] = pipe();
  const seenByClient = watching();
  const seenByServer = watching();
  const client = new DuplexPeer({ role: 'client', observer: seenByClient });
  const server = new DuplexPeer({ role: 'server', observer: seenByServer });
  await Promise.all([client.attach(left), server.attach(right)]);
  return {
    client,
    server,
    ct: new Tunnel(client, options),
    st: new Tunnel(server, options),
    seenByClient,
    seenByServer,
  };
}

/** One channel opened by the client and accepted by the server. */
async function pair(ct: Tunnel, st: Tunnel, family = 'probe'): Promise<[Connection, Connection]> {
  const accepted = st.acceptConnection();
  const opened = await ct.openConnection(family);
  return [opened, await accepted];
}

/** The frames a connection delivers, and a promise of the next one not yet taken. */
function collect(channel: Connection) {
  const frames: Frame[] = [];
  const waiters: ((frame: Frame) => void)[] = [];
  let taken = 0;
  let closed: { code: number; reason: string } | undefined;
  channel.listen({
    frame: (frame) => {
      frames.push(frame);
      waiters.shift()?.(frame);
    },
    close: (code, reason) => {
      closed = { code, reason };
    },
  });
  return {
    frames,
    next: () => {
      if (taken < frames.length) return Promise.resolve(frames[taken++]!);
      taken++;
      return new Promise<Frame>((resolve) => {
        waiters.push(resolve);
      });
    },
    get closed() {
      return closed;
    },
  };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 5));

test('a channel opens, carries frames both ways in order, text and binary, and a handle resolves it', async () => {
  const { ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st, 'probe');
  assert.equal(opened.id % 2, 1);
  assert.equal(accepted.id, opened.id);
  assert.equal(accepted.family, 'probe');
  assert.equal(st.connection(opened.id), accepted);
  assert.equal(ct.connection(opened.id), opened);
  const atServer = collect(accepted);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: 'one' });
  opened.send({ kind: 'binary', data: new Uint8Array([1, 2, 3, 255]) });
  opened.send({ kind: 'text', data: 'three' });
  accepted.send({ kind: 'text', data: 'back' });
  await atServer.next();
  await atServer.next();
  await atServer.next();
  await atClient.next();
  assert.deepEqual(
    atServer.frames.map((frame) => frame.kind),
    ['text', 'binary', 'text'],
  );
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
  assert.equal(ct.connection(fromServer[0].id), fromServer[1]);
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
  assert.equal(st.connection(opened.id), undefined);
});

test('credit paces the sender: beyond the window a frame waits in the channel until the receiver takes one', async () => {
  const { ct, st } = await tunnels({ window: 2 });
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  opened.send({ kind: 'text', data: '1' });
  opened.send({ kind: 'text', data: '2' });
  opened.send({ kind: 'text', data: '3' });
  assert.equal(opened.buffered, 1);
  await atServer.next();
  await atServer.next();
  await atServer.next();
  assert.deepEqual(
    atServer.frames.map((frame) => frame.data),
    ['1', '2', '3'],
  );
  assert.equal(opened.buffered, 0);
});

test('a frame over the limit is refused, and the channel with it', async () => {
  const { ct, st } = await tunnels({ maxFrameBytes: 16 });
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: 'x'.repeat(17) });
  await tick();
  await tick();
  assert.equal(atServer.frames.length, 0);
  assert.equal(atServer.closed?.code, 1009);
  assert.equal(atClient.closed?.code, 1009);
  assert.equal(accepted.state, 'closed');
});

test('an open beyond the accept capacity is refused, and the tunnel stands', async () => {
  const { ct } = await tunnels({ acceptCapacity: 1 });
  await ct.openConnection('probe');
  await assert.rejects(
    ct.openConnection('probe'),
    (error: unknown) => error instanceof DuplexError && error.code === 'channel_refused',
  );
  await assert.rejects(
    ct.openConnection(''),
    (error: unknown) => error instanceof DuplexError && error.code === 'channel_invalid',
  );
});

test('a peer of the profile runs over a channel', async () => {
  const { ct, st } = await tunnels();
  const [opened, accepted] = await pair(ct, st);
  const server = new DuplexPeer({ role: 'server' });
  server.handle('echo', (params) => params);
  const events: unknown[] = [];
  server.onEvent('hello', (data) => {
    events.push(data);
  });
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
  const rejected = assert.rejects(
    st.acceptConnection(),
    (error: unknown) => error instanceof DuplexError && error.code === 'disconnected',
  );
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
  assert.deepEqual(
    late.frames.map((frame) => frame.data),
    ['early one', 'early two'],
  );
  opened.send({ kind: 'text', data: 'three' });
  await late.next();
  await late.next();
  await late.next();
  assert.equal(late.frames[2]!.data, 'three');
  const [again, acceptedAgain] = await pair(ct, st);
  again.send({ kind: 'text', data: 'last' });
  again.close(1000, 'done');
  await tick();
  assert.equal(acceptedAgain.state, 'closed');
  const afterwards = collect(acceptedAgain);
  assert.deepEqual(
    afterwards.frames.map((frame) => frame.data),
    ['last'],
  );
  assert.deepEqual(afterwards.closed, { code: 1000, reason: 'done' });
});

test('a channel opened, accepted, carried and closed is what both observers saw, in order and with its stated fields', async () => {
  const { ct, st, seenByClient, seenByServer } = await tunnels();
  const [opened, accepted] = await pair(ct, st, 'probe');
  const atServer = collect(accepted);
  opened.send({ kind: 'text', data: 'one' });
  await atServer.next();
  opened.close(1008, 'the family said so');
  await tick();
  // The opener opened it and closed it; the other side saw it opened, took it,
  // and saw it close — neither tunnel was given an observer of its own.
  assert.deepEqual(
    seenByClient.tunnel().map((event) => event.type),
    ['channel.opened', 'channel.closed'],
  );
  assert.deepEqual(
    seenByServer.tunnel().map((event) => event.type),
    ['channel.opened', 'channel.accepted', 'channel.closed'],
  );
  const openedHere = seenByClient.of('channel.opened')[0]!;
  assert.equal(openedHere.family, 'probe');
  assert.equal(openedHere.id, opened.id);
  assert.equal(openedHere.opener, true);
  assert.ok(openedHere.at instanceof Date);
  const openedThere = seenByServer.of('channel.opened')[0]!;
  assert.equal(openedThere.id, opened.id);
  assert.equal(openedThere.opener, false);
  assert.deepEqual((({ family, id }) => ({ family, id }))(seenByServer.of('channel.accepted')[0]!), {
    family: 'probe',
    id: opened.id,
  });
  for (const seen of [seenByClient, seenByServer]) {
    const closed = seen.of('channel.closed')[0]!;
    assert.deepEqual((({ family, id, code, reason }) => ({ family, id, code, reason }))(closed), {
      family: 'probe',
      id: opened.id,
      code: 1008,
      reason: 'the family said so',
    });
  }
});

test('an observer of a tunnel is told nothing of what a channel carried', async () => {
  const sentinel = 'grant-8f31-that-no-observer-may-see';
  const { ct, st, seenByClient, seenByServer } = await tunnels();
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: sentinel });
  opened.send({ kind: 'binary', data: new TextEncoder().encode(sentinel) });
  accepted.send({ kind: 'text', data: sentinel });
  await atServer.next();
  await atServer.next();
  await atClient.next();
  opened.close();
  await tick();
  const everything = JSON.stringify([...seenByClient.events, ...seenByServer.events]);
  assert.ok(!everything.includes(sentinel), "a frame's payload reached an observer");
  assert.ok(!everything.includes(btoa(sentinel)), "a binary frame's payload reached an observer");
  assert.ok(seenByClient.tunnel().length > 0);
});

test('a send beyond the window stalls, and the stall says which channel and how much waits', async () => {
  const { ct, st, seenByClient } = await tunnels({ window: 2 });
  const [opened, accepted] = await pair(ct, st);
  const atServer = collect(accepted);
  opened.send({ kind: 'text', data: '1' });
  opened.send({ kind: 'text', data: '2' });
  opened.send({ kind: 'text', data: '3' });
  opened.send({ kind: 'text', data: '4' });
  const stalls = seenByClient.of('credit.stall');
  assert.deepEqual(
    stalls.map((event) => event.waiting),
    [1, 2],
  );
  assert.deepEqual(
    stalls.map((event) => event.family),
    ['probe', 'probe'],
  );
  assert.deepEqual(
    stalls.map((event) => event.id),
    [opened.id, opened.id],
  );
  await atServer.next();
  await atServer.next();
  await atServer.next();
  await atServer.next();
  assert.deepEqual(
    atServer.frames.map((frame) => frame.data),
    ['1', '2', '3', '4'],
  );
});

test('an open that becomes no channel is refused where it was refused and where it was asked', async () => {
  const { ct, seenByClient, seenByServer } = await tunnels({ acceptCapacity: 1 });
  await ct.openConnection('probe');
  await assert.rejects(
    ct.openConnection('codex'),
    (error: unknown) => error instanceof DuplexError && error.code === 'channel_refused',
  );
  await assert.rejects(
    ct.openConnection(''),
    (error: unknown) => error instanceof DuplexError && error.code === 'channel_invalid',
  );
  // The side that refused it says why in its own words; the side that asked
  // says what it was told, and an open refused before it left says so too.
  assert.deepEqual(
    seenByServer.of('open.refused').map((event) => [event.family, event.reason]),
    [['codex', 'no room for a channel nobody has accepted']],
  );
  const asked = seenByClient.of('open.refused');
  assert.deepEqual(
    asked.map((event) => event.family),
    ['codex', ''],
  );
  assert.match(asked[0]!.reason, /No room for a channel nobody has accepted/);
  assert.equal(asked[1]!.reason, 'a channel is opened for a family');
  assert.deepEqual(
    seenByClient.of('channel.opened').map((event) => event.family),
    ['probe'],
  );
});

test('a frame beyond the window ends the channel, and what is held is one window deep', async () => {
  // The receiver holds what nobody has listened for, one window deep, as the
  // Go channel's inbox does: a sender that ignores the credit it was granted
  // is refused rather than held without bound.
  const { client, ct, st } = await tunnels({ window: 2 });
  const [opened, accepted] = await pair(ct, st);
  const atClient = collect(opened);
  opened.send({ kind: 'text', data: '1' });
  opened.send({ kind: 'text', data: '2' });
  await tick();
  assert.equal(accepted.state, 'open', "a window's worth is held, not refused");
  // One more, past the credit the receiver granted. Only the outer peer can
  // send it: the channel's own send would wait for credit that never comes.
  await client.emit('channel.frame', { channel: opened.id, text: 'beyond' });
  await tick();
  assert.equal(accepted.state, 'closed');
  assert.equal(atClient.closed?.code, 1002);
  assert.equal(atClient.closed?.reason, 'a frame beyond the window of 2');
});
