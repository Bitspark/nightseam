import assert from 'node:assert/strict';
import test, { type TestContext } from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { at, encodePath, mount, pipe, type Wire } from '@nightseam/duplex';
import { Tunnel, type Connection } from '@nightseam/tunnel';
import { DuplexPeer } from './peer.ts';
import { createDispatcher } from './dispatcher.ts';
import { callWire, emitWire, handleWire } from './wire.ts';
import type { Observer, ObserverEvent } from './observer.ts';

class ChannelLog implements Observer {
  readonly events: ObserverEvent[] = [];
  private readonly changed = new Set<() => void>();

  observe(event: ObserverEvent): void {
    this.events.push(event);
    for (const change of [...this.changed]) change();
  }

  count(matches: (event: ObserverEvent) => boolean): number {
    return this.events.filter(matches).length;
  }

  wait(matches: (event: ObserverEvent) => boolean, count = 1): Promise<void> {
    if (this.count(matches) >= count) return Promise.resolve();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.changed.delete(check);
        reject(new Error('Observer did not reach the channel barrier.'));
      }, 1_000);
      const check = () => {
        if (this.count(matches) < count) return;
        clearTimeout(timer);
        this.changed.delete(check);
        resolve();
      };
      this.changed.add(check);
    });
  }
}

const closed = (event: ObserverEvent) => event.type === 'connection.closed';
const pressure = (event: ObserverEvent) => event.type === 'backpressure' && event.stalled;

async function channelHarness(t: TestContext) {
  const clientLog = new ChannelLog(),
    serverLog = new ChannelLog();
  const outerClient = new DuplexPeer({ observer: clientLog }),
    outerServer = new DuplexPeer({ role: 'server', observer: serverLog });
  t.after(() => {
    outerClient.close();
    outerServer.close();
  });
  const [left, right] = pipe();
  await Promise.all([outerClient.attach(left), outerServer.attach(right)]);
  const client = new Tunnel(outerClient, { window: 1 }),
    server = new Tunnel(outerServer, { window: 1 });
  const open = async () => {
    const a = await client.openConnection('wire-channel-test', '');
    const b = await server.acceptConnection();
    assert.equal(a.id, b.id);
    return { a, b };
  };
  const sibling = await open();
  const siblingClient = new DuplexPeer(),
    siblingServer = new DuplexPeer({ role: 'server' });
  t.after(() => {
    siblingClient.close();
    siblingServer.close();
  });
  await Promise.all([siblingClient.attach(sibling.a), siblingServer.attach(sibling.b)]);
  let outerCalls = 0,
    siblingCalls = 0;
  outerServer.handle('outer.echo', (value) => {
    outerCalls++;
    return value;
  });
  const siblingDispatcher = createDispatcher(siblingServer.wire());
  t.after(() => siblingDispatcher.close());
  handleWire(siblingDispatcher, ['echo'], (value) => {
    siblingCalls++;
    return value;
  });
  const healthy = async () => {
    const beforeOuter = outerCalls,
      beforeSibling = siblingCalls;
    assert.equal(await outerClient.call('outer.echo', 'outer alive'), 'outer alive');
    assert.equal(await callWire(siblingClient.wire(), ['echo'], 'sibling alive'), 'sibling alive');
    assert.equal(outerCalls, beforeOuter + 1);
    assert.equal(siblingCalls, beforeSibling + 1);
    for (const peer of [outerClient, outerServer, siblingClient, siblingServer]) assert.equal(peer.status, 'connected');
  };
  await healthy();
  return { clientLog, serverLog, open, healthy };
}

async function destination(t: TestContext, channel: Connection) {
  const log = new ChannelLog();
  const peer = new DuplexPeer({ queueCapacity: 2, writeTimeoutMs: 5_000, observer: log });
  t.after(() => peer.close());
  await peer.attach(channel);
  const mounted = mount(new Map([['destination', peer.wire()]]));
  t.after(() => mounted.close());
  const wire = at(mounted, ['destination', 'events']);
  return { log, peer, wire };
}

// The remote listener remains absent: one event spends the channel window,
// the next waits for credit, and the third fills the bounded peer output.
async function fill(channel: Connection, wire: Wire, log: ChannelLog, outerLog: ChannelLog) {
  emitWire(wire, ['item'], 0);
  await log.wait((event) => event.type === 'frame.sent');
  emitWire(wire, ['item'], 1);
  await outerLog.wait((event) => event.type === 'credit.stall' && event.id === channel.id);
  emitWire(wire, ['item'], 2);
  await log.wait((event) => event.type === 'event.emitted', 3);
}

test('a never-reading wire channel ends only its own carrier at overflow', async (t) => {
  const h = await channelHarness(t);
  const { a } = await h.open();
  const { peer, wire, log } = await destination(t, a);
  await fill(a, wire, log, h.clientLog);
  const ended = log.wait(closed);
  emitWire(wire, ['item'], 3);
  assert.equal(
    await Promise.race([ended.then(() => 'closed'), nextTurn().then(() => 'waited')]),
    'closed',
    'overflow waited for the channel consumer or transport deadline',
  );
  assert.equal(peer.status, 'disconnected');
  assert.equal(log.count(pressure), 1);
  assert.equal(log.count(closed), 1);
  await h.serverLog.wait((event) => event.type === 'channel.closed' && event.id === a.id);
  await h.healthy();
});

test('a slow wire channel reader keeps accepted order while its sibling progresses', async (t) => {
  const h = await channelHarness(t);
  const { a, b } = await h.open();
  const { peer, wire, log } = await destination(t, a);
  await fill(a, wire, log, h.clientLog);
  await h.healthy();
  const frames: unknown[] = [];
  let drained!: () => void;
  const received = new Promise<void>((resolve) => {
    drained = resolve;
  });
  const detach = b.listen({
    frame: (frame) => {
      frames.push(frame.kind === 'text' ? JSON.parse(frame.data) : { kind: frame.kind });
      if (frames.length === 3) drained();
    },
  });
  t.after(detach);
  await received;
  assert.deepEqual(
    frames.map((frame) => {
      const value = frame as { kind: unknown; event: unknown; data: unknown };
      return { kind: value.kind, event: value.event, data: value.data };
    }),
    [0, 1, 2].map((data) => ({ kind: 'event', event: encodePath(['events', 'item']), data })),
  );
  assert.equal(peer.status, 'connected');
  assert.equal(log.count(pressure), 0);
  assert.equal(log.count(closed), 0);
  await h.healthy();
});

test('a failed wire channel ends its blocked writer and preserves siblings', async (t) => {
  const h = await channelHarness(t);
  const { a, b } = await h.open();
  const { peer, wire, log } = await destination(t, a);
  await fill(a, wire, log, h.clientLog);
  b.close(1011, 'destination failed');
  await log.wait(closed);
  assert.equal(peer.status, 'disconnected');
  const ending = log.events.filter((event) => event.type === 'connection.closed');
  assert.equal(ending.length, 1);
  assert.equal(ending[0]!.code, 1011);
  assert.equal(ending[0]!.reason, 'destination failed');
  assert.equal(ending[0]!.local, false);
  assert.equal(log.count(pressure), 0);
  assert.throws(() => emitWire(wire, ['item'], 99));
  await h.healthy();
});
