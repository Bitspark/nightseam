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
import { memoryLog, Registry, type Attachment, type Change, type Governance, type Log, type Role } from './index.ts';

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

/** The members a change carries and no others: what it says, beside what it never does. */
const MEMBERS = ['at', 'session', 'kind', 'attachment', 'sequence', 'method', 'trace'];

/** One change as a line: its kind, whom it concerns, the method it names and the sequence it carries — what a consumer's own events are built from. */
function spell(change: Change): string {
  const parts: (string | undefined)[] = [change.kind, change.attachment?.origin, change.method];
  if (change.sequence !== undefined) parts.push('#' + change.sequence);
  return parts.filter((part): part is string => part !== undefined).join(' ');
}

/** What a change says, as text: every member of it, an attachment read as the two facts it is rather than as the channel it speaks on. */
function words(change: Change): string {
  return JSON.stringify({ ...change, attachment: change.attachment && { role: change.attachment.role, origin: change.attachment.origin } });
}

/** Every change a registry makes, in the order it made them: the lines, and the changes themselves for what a line does not say. */
function watching(registry: Registry) {
  const lines: string[] = [];
  const changes: Change[] = [];
  const stop = registry.onChange(change => { lines.push(spell(change)); changes.push(change); });
  return { lines, changes, stop, of: (kind: string) => changes.filter(change => change.kind === kind) };
}

/** The traces of the suite's two traced frames: a change that concerns one of them carries the trace that frame carried. */
const traceparent = '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01';
const asked = '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b8-01';

/** run registers the suite over the channels connect supplies. */
export function run(connect: Connect): void {
  /** A bound session: the wire its channels come from, the registry, the log, and the end the machine speaks on. */
  async function bound(maxFrameBytes = 1 << 20, before: (registry: Registry) => void = () => { /* A run that watches the session from before it is bound says so. */ }) {
    const wire = await connect();
    const registry = new Registry();
    before(registry);
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
    // What the peer sends is the peer's — a trace context among it — so the
    // event is held to what it is, not to the members it arrives with.
    const event = await heard.next();
    assert.equal(event.kind, 'event');
    assert.equal(event.event, 'changed');
    assert.deepEqual(event.data, { text: 'moved', count: 2 });
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
    // Control moves while the ask is open: it is asked of the new holder
    // under the id the machine gave it, and the one it was asked of no
    // longer answers it.
    registry.control('s', two.attachment);
    const again = await atTwo.next();
    assert.deepEqual(again, asked);
    say(one.near, { version: 1, kind: 'response', id: asked.id, result: payload('too late') });
    await tick();
    assert.deepEqual(registry.attention(), ['s']);
    say(two.near, { version: 1, kind: 'response', id: again.id, result: payload('reveiled') });
    assert.deepEqual(await answer, { text: 'reveiled', count: 1 });
    assert.deepEqual(registry.attention(), []);
    // An observer is never given control.
    const watching = await consumer(wire, registry, 'observer', 'watching');
    assert.throws(() => registry.control('s', watching.attachment), (error: unknown) => error instanceof DuplexError && error.code === 'not_controlling');
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

  test('every domain change of a session reaches onChange, in the order the registry made them', async () => {
    let seen!: ReturnType<typeof watching>;
    const { wire, registry, machine } = await bound(1 << 20, registry => { seen = watching(registry); });
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    registry.control('s', one.attachment);
    // The holder's deciding request, and the machine's answer to it.
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload('by the holder'), traceparent });
    const request = await atMachine.next();
    say(machine, { version: 1, kind: 'response', id: request.id, result: payload('answered') });
    await tick();
    // A request from a consumer that does not hold control, which the relay
    // answers in the machine's place: a refusal reaches neither the machine
    // nor the log, and is a change all the same.
    say(two.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload('by the other') });
    await tick();
    // What the machine asks: appended, raised, and routed to the holder.
    say(machine, { version: 1, kind: 'request', id: 's:1', method: 'reverse', params: payload('ask'), traceparent: asked });
    await tick();
    // Control moves while the ask is open, so it is routed afresh, and the
    // consumer it moved to is the one whose answer the machine reads.
    registry.control('s', two.attachment);
    await tick();
    say(two.near, { version: 1, kind: 'response', id: 's:1', result: payload('reveiled') });
    await tick();
    one.attachment.detach();
    await tick();
    machine.close(4002, 'the machine went away');
    await tick();
    assert.deepEqual(seen.lines, [
      'bound',
      'attached one #0',
      'attached two #0',
      'control_changed one',
      'frame_appended one echo #1',
      'frame_appended #2',
      'refused two echo',
      'frame_appended reverse #3',
      'ask_raised reverse',
      'ask_routed one reverse',
      'control_changed two',
      'ask_routed two reverse',
      'ask_answered two reverse',
      'frame_appended two #4',
      'detached one',
      'unbound',
    ]);
    // Every change is of the session it was made on, at the time it was made.
    for (const change of seen.changes) {
      assert.equal(change.session, 's');
      assert.equal(change.at instanceof Date, true);
    }
    // A change that concerns a frame carries that frame's trace, and one that
    // concerns none carries no trace at all.
    assert.deepEqual(seen.of('ask_raised')[0]!.trace, { traceparent: asked });
    assert.deepEqual(seen.of('ask_routed').map(change => change.trace?.traceparent), [asked, asked]);
    assert.deepEqual(seen.of('frame_appended')[0]!.trace, { traceparent });
    assert.equal(seen.of('frame_appended')[1]!.trace, undefined);
    for (const kind of ['bound', 'attached', 'control_changed', 'detached', 'unbound']) {
      for (const change of seen.of(kind)) assert.equal(change.trace, undefined);
    }
    wire.close();
  });

  test('a change says what a frame was and never what it carried', async () => {
    const sentinel = 'squeamish-ossifrage';
    let seen!: ReturnType<typeof watching>;
    const { wire, registry, machine } = await bound(1 << 20, registry => { seen = watching(registry); });
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'two');
    registry.control('s', one.attachment);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload(sentinel) });
    const request = await atMachine.next();
    say(machine, { version: 1, kind: 'response', id: request.id, result: payload(sentinel) });
    say(machine, { version: 1, kind: 'event', event: 'changed', data: payload(sentinel) });
    say(machine, { version: 1, kind: 'request', id: 's:1', method: 'reverse', params: payload(sentinel) });
    await tick();
    say(one.near, { version: 1, kind: 'response', id: 's:1', result: payload(sentinel) });
    say(two.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload(sentinel) });
    await tick();
    assert.equal(seen.changes.length > 0, true);
    for (const change of seen.changes) {
      assert.equal(words(change).includes(sentinel), false, `a change of kind ${change.kind} carried what a frame carried`);
      // What it says is the members it declares, so a message cannot arrive
      // under a name the sentinel was not looked for under.
      for (const member of Object.keys(change)) assert.equal(MEMBERS.includes(member), true, `a change carried ${member}`);
    }
    wire.close();
  });

  test('the function onChange returns stops that registration and no other', async () => {
    const heard: string[] = [];
    const hook = (change: Change) => { heard.push(spell(change)); };
    let stop!: () => void;
    const { wire, registry, machine } = await bound(1 << 20, registry => {
      // The same hook registered twice is two registrations, and stopping one
      // leaves the other standing; stopping one twice stops nothing else.
      stop = registry.onChange(hook);
      registry.onChange(hook);
      const stopped = registry.onChange(() => { heard.push('gone'); });
      stopped();
      stopped();
    });
    const one = await consumer(wire, registry, 'participant', 'one');
    stop();
    registry.control('s', one.attachment);
    await tick();
    assert.deepEqual(heard, ['bound', 'bound', 'attached one #0', 'attached one #0', 'control_changed one']);
    machine.close(4002, 'the machine went away');
    await tick();
    assert.equal(heard.at(-1), 'unbound');
    wire.close();
  });

  test('a request beyond what a session may have open is refused, and the refusal is a change like the other', async () => {
    const wire = await connect();
    const registry = new Registry({ maxInflight: 1 });
    const seen = watching(registry);
    const { near: machine, far: up } = await wire.open();
    registry.bind('s', up, governance, memoryLog(1 << 20));
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    registry.control('s', one.attachment);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload('first') });
    await atMachine.next();
    say(one.near, { version: 1, kind: 'request', id: 'c:2', method: 'echo', params: payload('second') });
    await tick();
    assert.deepEqual(seen.lines, ['bound', 'attached one #0', 'control_changed one', 'frame_appended one echo #1', 'refused one echo']);
    wire.close();
  });
}
