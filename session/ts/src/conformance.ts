/**
 * The suite a session component is held to: the routing table of the
 * component — a response to the one channel that asked, an event to every
 * attached one, an ask to the holder of control, a decision refused where
 * control is not held — the ids it mints, the log it keeps and the closes it
 * carries. It runs over whatever channels a Connect supplies: in-memory
 * pipes here, a real connection where a cross-language gate runs it, and the
 * same suite in either language.
 *
 * The family is probe, as the generator renders it from the corpus — its
 * session tier decides `echo` and asks `reverse` — and the consumers of the
 * suite are its generated client where a peer of the family is what a rule
 * is about, and raw channels where a frame's own members are.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer } from '@nightseam/runtime';
import { Tunnel, type Channel } from '@nightseam/tunnel';
import { asks, Client, decides, type Handler, type Payload } from '../../../cmd/nightseam/testdata/golden/api/ts/probe-client/src/index.ts';
import { memoryLog, Registry, type Attachment, type Governance, type Log, type Role } from './index.ts';

/** The channels a run of the suite is given: each open is one channel, the end a peer speaks on and the end the registry is given. */
export interface Wire {
  open(after?: number): Promise<{ near: Channel; far: Channel }>;
  close(): void;
}
export type Connect = () => Promise<Wire>;

/** The probe family's session tier, as its generated client states it. */
export const governance: Governance = { decides: method => decides.has(method), asks: method => asks.has(method) };

/** Two peers over an in-memory pipe, with a tunnel each: what the suite runs on where there is no connection to hand. */
export const pipes: Connect = async () => {
  const [left, right] = pipe();
  const near = new DuplexPeer({ role: 'client' });
  const far = new DuplexPeer({ role: 'server' });
  await Promise.all([near.attach(left), far.attach(right)]);
  const nt = new Tunnel(near);
  const ft = new Tunnel(far);
  return {
    async open(after = 0) {
      const accepted = ft.accept();
      const opened = await nt.open('probe', after);
      return { near: opened, far: await accepted };
    },
    close() { near.close(); far.close(); },
  };
};

/** One message of the profile, as a raw end of the suite reads and writes it. */
type Envelope = Record<string, unknown>;

/** The envelopes a channel receives, its close, and a promise of the next one not yet taken. */
function listen(channel: Channel) {
  const envelopes: Envelope[] = [];
  const waiters: ((envelope: Envelope) => void)[] = [];
  let taken = 0;
  let closed: { code: number; reason: string } | undefined;
  channel.listen({
    frame: frame => {
      if (frame.kind !== 'text') return;
      const envelope = JSON.parse(frame.data) as Envelope;
      envelopes.push(envelope);
      waiters.shift()?.(envelope);
    },
    close: (code, reason) => { closed = { code, reason }; },
  });
  return {
    envelopes,
    next(): Promise<Envelope> {
      if (taken < envelopes.length) return Promise.resolve(envelopes[taken++]!);
      taken++;
      return new Promise<Envelope>(resolve => { waiters.push(resolve); });
    },
    get closed() { return closed; },
  };
}

/** say writes one envelope to a channel, as a peer of the family would. */
function say(channel: Channel, envelope: Envelope): void {
  channel.send({ kind: 'text', data: JSON.stringify(envelope) });
}

const tick = () => new Promise(resolve => setTimeout(resolve, 5));
const payload = (text: string, count = 1): Payload => ({ text, count });
/** A consumer of the family that answers what the machine asks. */
const answering: Handler = { reverse: params => ({ ...params, text: [...params.text].reverse().join('') }) };

/** run registers the suite over the channels connect supplies. */
export function run(connect: Connect): void {
  /** A bound session: the wire its channels come from, the registry, the log, and the end the machine speaks on. */
  async function bound(maxFrameBytes = 1 << 20) {
    const wire = await connect();
    const registry = new Registry();
    const log = memoryLog(maxFrameBytes);
    const { near: machine, far: up } = await wire.open();
    registry.bind('s', up, governance, log);
    return { wire, registry, log, machine };
  }

  /** A consumer attached to that session: the end it speaks on, and what the registry knows it by. */
  async function consumer(wire: Wire, registry: Registry, role: Role, origin: string, after = 0): Promise<{ near: Channel; attachment: Attachment }> {
    const { near, far } = await wire.open(after);
    return { near, attachment: registry.attach('s', far, role, origin, after) };
  }

  /** The machine of the suite: a peer of the profile serving the family's server side over the up channel. */
  async function serving(machine: Channel): Promise<DuplexPeer> {
    const peer = new DuplexPeer({ role: 'server' });
    peer.handle('echo', params => ({ ...(params as Payload), text: 'machine:' + (params as Payload).text }));
    peer.handle('no_args', () => 'ok');
    await peer.attach(machine);
    return peer;
  }

  test('a response reaches the one channel that asked, and an event reaches every attached channel', async () => {
    const { wire, registry, machine } = await bound();
    const peer = await serving(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'two');
    registry.control('s', one.attachment);
    const heard = listen(two.near);
    const client = await Client.attach(one.near, {}, answering);
    assert.deepEqual(await client.echo(payload('value')), { text: 'machine:value', count: 1 });
    await peer.emit('changed', payload('moved', 2));
    assert.deepEqual(await heard.next(), { version: 1, kind: 'event', event: 'changed', data: { text: 'moved', count: 2 } });
    await tick();
    // The observer saw the event and nothing of the exchange it was not part of.
    assert.deepEqual(heard.envelopes.map(envelope => envelope.kind), ['event']);
    wire.close();
  });

  test('an observer decides nothing, a participant that does not hold control decides nothing, and what decides nothing either may send', async () => {
    const { wire, registry, machine } = await bound();
    await serving(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    const three = await consumer(wire, registry, 'observer', 'three');
    registry.control('s', one.attachment);
    const holding = await Client.attach(one.near, {}, answering);
    const idle = await Client.attach(two.near, {}, answering);
    const observing = await Client.attach(three.near, {}, answering);
    const refused = (error: unknown) => error instanceof DuplexError && error.code === 'not_controlling';
    await assert.rejects(observing.echo(payload('by an observer')), refused);
    await assert.rejects(idle.echo(payload('by a participant')), refused);
    assert.equal((await holding.echo(payload('by the holder'))).text, 'machine:by the holder');
    // no_args decides nothing, so the role it is sent in does not matter.
    assert.equal(await observing.noArgs(), 'ok');
    wire.close();
  });

  test('a cancel decides: the holder\'s reaches the machine and another consumer\'s does not', async () => {
    const { wire, registry, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    registry.control('s', one.attachment);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'seen', params: {} });
    const request = await atMachine.next();
    say(two.near, { version: 1, kind: 'cancel', id: 'c:1' });
    await tick();
    say(one.near, { version: 1, kind: 'cancel', id: 'c:1' });
    assert.deepEqual(await atMachine.next(), { version: 1, kind: 'cancel', id: request.id });
    assert.deepEqual(atMachine.envelopes.map(envelope => envelope.kind), ['request', 'cancel']);
    wire.close();
  });

  test('an asking request reaches the holder, follows a transfer while it is open, and is what attention names', async () => {
    const { wire, registry, machine } = await bound();
    const peer = await serving(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    const atOne = listen(one.near);
    const atTwo = listen(two.near);
    assert.deepEqual(registry.attention(), []);
    registry.control('s', one.attachment);
    const answer = peer.call('reverse', payload('deliver'));
    const asked = await atOne.next();
    assert.equal(asked.method, 'reverse');
    assert.deepEqual(registry.attention(), ['s']);
    // Control moves while the ask is open: it is asked of the new holder, and
    // the one it was asked of no longer answers it.
    registry.control('s', two.attachment);
    const again = await atTwo.next();
    assert.deepEqual({ ...again, id: '' }, { ...asked, id: '' });
    assert.notEqual(again.id, asked.id);
    say(one.near, { version: 1, kind: 'response', id: asked.id, result: payload('too late') });
    await tick();
    assert.deepEqual(registry.attention(), ['s']);
    say(two.near, { version: 1, kind: 'response', id: again.id, result: payload('reveiled') });
    assert.deepEqual(await answer, { text: 'reveiled', count: 1 });
    assert.deepEqual(registry.attention(), []);
    wire.close();
  });

  test('two consumers that each mint c:1 are distinct on the session, and each response reaches the one that asked', async () => {
    const { wire, registry, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    const atOne = listen(one.near);
    const atTwo = listen(two.near);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'seen', params: { of: 'one' } });
    const first = await atMachine.next();
    say(two.near, { version: 1, kind: 'request', id: 'c:1', method: 'seen', params: { of: 'two' } });
    const second = await atMachine.next();
    assert.notEqual(first.id, second.id);
    assert.deepEqual([first.params, second.params], [{ of: 'one' }, { of: 'two' }]);
    // Answered in the other order, each answer still reaches the one that asked.
    say(machine, { version: 1, kind: 'response', id: second.id, result: 'for two' });
    say(machine, { version: 1, kind: 'response', id: first.id, result: 'for one' });
    assert.deepEqual(await atOne.next(), { version: 1, kind: 'response', id: 'c:1', result: 'for one' });
    assert.deepEqual(await atTwo.next(), { version: 1, kind: 'response', id: 'c:1', result: 'for two' });
    wire.close();
  });

  test('a frame carries the members the relay knows nothing of to the other side intact, in either direction', async () => {
    const { wire, registry, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const atOne = listen(one.near);
    registry.control('s', one.attachment);
    const sent = {
      version: 1,
      kind: 'request',
      id: 'c:9',
      method: 'echo',
      params: { text: 'traced', count: 1 },
      traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01',
      tracestate: 'nightseam=1',
      whatever: { deep: ['later', 1, null] },
    };
    say(one.near, sent);
    const arrived = await atMachine.next();
    assert.notEqual(arrived.id, sent.id);
    assert.deepEqual({ ...arrived, id: sent.id }, sent);
    const emitted = { version: 1, kind: 'event', event: 'changed', data: payload('back'), traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000001-01' };
    say(machine, emitted);
    assert.deepEqual(await atOne.next(), emitted);
    wire.close();
  });

  test('a consumer attached after a sequence is given the log from it, before anything live, in one order', async () => {
    const { wire, registry, log, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    registry.control('s', one.attachment);
    for (const text of ['first', 'second', 'third']) {
      say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload(text) });
      const request = await atMachine.next();
      say(machine, { version: 1, kind: 'response', id: request.id, result: payload(text) });
      await tick();
    }
    const recorded: unknown[] = [];
    await log.replay(2, frame => { recorded.push(frame.message); return Promise.resolve(); });
    assert.equal(recorded.length, 4);
    // The consumer attaches holding the first two frames, and the machine
    // speaks before the replay could have finished.
    const two = await consumer(wire, registry, 'observer', 'two', 2);
    const atTwo = listen(two.near);
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('live') };
    say(machine, live);
    for (let i = 0; i < recorded.length + 1; i++) await atTwo.next();
    assert.deepEqual(atTwo.envelopes, [...recorded, live]);
    wire.close();
  });

  test('a message over the log\'s bound is kept cut and replayed truncated', async () => {
    const { wire, registry, log, machine } = await bound(128);
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    registry.control('s', one.attachment);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload('x'.repeat(400)) });
    await atMachine.next();
    const frames: { truncated: boolean; message: unknown }[] = [];
    await log.replay(0, frame => { frames.push(frame); return Promise.resolve(); });
    assert.equal(frames.length, 1);
    assert.equal(frames[0]!.truncated, true);
    const cut = frames[0]!.message as string;
    assert.equal(typeof cut, 'string');
    assert.equal(cut.length, 128);
    assert.equal(cut.startsWith('{"version":1,"kind":"request","id":"c:1"'), true);
    wire.close();
  });

  test('the machine\'s channel closing ends every attached channel with the same close, and a consumer\'s closing detaches it and nothing else', async () => {
    const { wire, registry, machine } = await bound();
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    const atTwo = listen(two.near);
    registry.control('s', one.attachment);
    one.near.close(1000, 'done with it');
    await tick();
    // The session stands: what the one that left held is released, and the
    // other consumer is where it was.
    assert.throws(() => registry.control('s', one.attachment), (error: unknown) => error instanceof DuplexError && error.code === 'not_attached');
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('still here') };
    say(machine, live);
    assert.deepEqual(await atTwo.next(), live);
    machine.close(4001, 'the machine went away');
    await tick();
    assert.deepEqual(atTwo.closed, { code: 4001, reason: 'the machine went away' });
    assert.equal(two.near.state, 'closed');
    assert.throws(() => registry.attach('s', two.attachment.channel, 'participant', 'again', 0), (error: unknown) => error instanceof DuplexError && error.code === 'no_session');
    assert.deepEqual(registry.attention(), []);
    wire.close();
  });
}
