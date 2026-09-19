/**
 * The suite a session component is held to: the routing table of the
 * component — a response to the one consumer that asked, an event to every
 * attached one, an ask to the holder of control, a decision refused where
 * control is not held — the ids it mints, the log it keeps and the closes it
 * carries. It runs over whatever connections of the seam a Connect supplies:
 * channels of a tunnel over in-memory pipes, the pipes themselves with no
 * tunnel at all, a real connection where a cross-language gate runs it, and
 * the same suite in either language.
 *
 * The family is probe, as the generator renders it from the corpus — its
 * session tier decides `echo` and asks `reverse` — and the consumers of the
 * suite are its generated client where a peer of the family is what a rule
 * is about, and raw channels where a frame's own members are.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe, type FrameConnection } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, type Observer, type ObserverEvent } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';
import { asks, Client, decides, type Handler, type Payload } from '../../../cmd/nightseam/testdata/golden/api/ts/probe-client/src/index.ts';
import { CONTROL_EVENT, CURSOR_EVENT, memoryLog, PREFIX, Registry, type Attachment, type Change, type Governance, type Log, type Role } from './index.ts';

/** The connections a run of the suite is given: each open is one connected pair, the end a peer speaks on and the end the registry is given. */
export interface Wire {
  open(after?: number): Promise<{ near: FrameConnection; far: FrameConnection }>;
  close(): void;
}
/**
 * An observer a run is given is what a session bound over one of these
 * connections tells. A transport whose connections observe through something
 * of their own — a tunnel's channels — seats it on the peer they run over;
 * one whose connections do not ignores it, and the suite gives the registry
 * the same observer, which is the order a relay reads the two in.
 */
export type Connect = (observer?: Observer) => Promise<Wire>;

/** The probe family's session tier, as its generated client states it. */
export const governance: Governance = { decides: method => decides.has(method), asks: method => asks.has(method) };

/** Two peers over an in-memory pipe, with a tunnel each: channels of a tunnel, which is what a session ran over when it could run over nothing else. */
export const pipes: Connect = async observer => {
  const [left, right] = pipe();
  const near = new DuplexPeer({ role: 'client' });
  // The registry is given the far ends, so the far peer is the one a session
  // of them emits through: an observer of this run belongs to it.
  const far = new DuplexPeer({ role: 'server', observer });
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

/**
 * The seam's pipe and nothing else: connections that observe through nothing
 * of their own, which is what an in-process machine binds over and what a
 * consumer over a bare socket attaches over. Each open is one pair, and the
 * observer a run was given reaches the session through the registry instead.
 */
export const connections: Connect = async () => {
  const opened: FrameConnection[] = [];
  return {
    open() {
      const [near, far] = pipe();
      opened.push(near, far);
      return Promise.resolve({ near, far });
    },
    close() { for (const end of opened) if (end.state === 'open') end.close(1000, 'the run ended'); },
  };
};

/** One message of the profile, as a raw end of the suite reads and writes it. */
type Envelope = Record<string, unknown>;

/**
 * The envelopes a channel receives, its close, and a promise of the next one
 * not yet taken. A consumer's end also reads the session's own vocabulary:
 * `family` takes one frame of the conversation and the cursor the relay
 * sends straight after it, and `control` takes who holds control.
 */
function listen(channel: FrameConnection) {
  const envelopes: Envelope[] = [];
  const waiters: ((envelope: Envelope) => void)[] = [];
  let taken = 0;
  let cursor = 0;
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
  function next(): Promise<Envelope> {
    if (taken < envelopes.length) return Promise.resolve(envelopes[taken++]!);
    taken++;
    return new Promise<Envelope>(resolve => { waiters.push(resolve); });
  }
  return {
    envelopes,
    next,
    /** One frame of the family and the cursor that names its place, which is where this consumer now stands. */
    async family(): Promise<Envelope> {
      const frame = await next();
      const stamped = await next();
      assert.equal(stamped.event, CURSOR_EVENT, `${JSON.stringify(frame)} was followed by ${JSON.stringify(stamped)}, not by a cursor`);
      cursor = (stamped.data as { sequence: number }).sequence;
      return frame;
    },
    /** Who the next frame, held to being the session's control event, says holds control. */
    async control(): Promise<string | null> {
      const frame = await next();
      assert.equal(frame.event, CONTROL_EVENT, `${JSON.stringify(frame)} was sent where who holds control was due`);
      return (frame.data as { holder: string | null }).holder;
    },
    get cursor() { return cursor; },
    get closed() { return closed; },
  };
}

/** What a consumer was given of the family's conversation, the session's own vocabulary passed over. */
function conversation(envelopes: Envelope[]): Envelope[] {
  return envelopes.filter(envelope => typeof envelope.event !== 'string' || !envelope.event.startsWith(PREFIX));
}

/** say writes one envelope to a channel, as a peer of the family would. */
function say(channel: FrameConnection, envelope: Envelope): void {
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

/** The events a session declares, which is what a run of the suite reads off the observer it gave the session; the runtime's own and the tunnel's are a peer's traffic, not the session's. */
const SESSION_EVENTS = new Set(['session.bound', 'session.unbound', 'session.attached', 'session.detached', 'ask.raised', 'ask.routed', 'ask.answered', 'control.changed', 'frame.appended', 'session.refused']);

/** One session event as a line, the way a change is one: its type and the fields that say which session frame or consumer it is about. */
function tell(event: ObserverEvent): string {
  const parts: (string | undefined)[] = [event.type];
  switch (event.type) {
    case 'session.unbound': parts.push(String(event.code), event.reason); break;
    case 'session.attached': parts.push(event.origin, event.role, '#' + event.after); break;
    case 'session.detached': parts.push(event.origin, event.role); break;
    case 'ask.raised': parts.push(event.id, event.method, event.asking ? 'asking' : undefined); break;
    case 'ask.routed': case 'ask.answered': parts.push(event.id, event.method, event.origin); break;
    case 'control.changed': parts.push(event.origin); break;
    case 'frame.appended': parts.push(event.direction, event.origin || undefined, event.method, '#' + event.sequence); break;
    case 'session.refused': parts.push(event.code, event.method, event.origin, event.role); break;
    default: break;
  }
  return parts.filter((part): part is string => part !== undefined).join(' ');
}

/** Every session event a run is told, in the order the session emitted them. */
function observing() {
  const lines: string[] = [];
  const events: ObserverEvent[] = [];
  const observer: Observer = {
    observe(event) {
      if (!SESSION_EVENTS.has(event.type)) return;
      events.push(event);
      lines.push(tell(event));
    },
  };
  return {
    observer,
    lines,
    events,
    of<T extends ObserverEvent['type']>(type: T) {
      return events.filter((event): event is Extract<ObserverEvent, { type: T }> => event.type === type);
    },
  };
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

/** run registers the suite over the connections connect supplies. */
export function run(connect: Connect): void {
  /** A bound session: the wire its channels come from, the registry, the log, and the end the machine speaks on. */
  async function bound(maxFrameBytes = 1 << 20, before: (registry: Registry) => void = () => { /* A run that watches the session from before it is bound says so. */ }, observer?: Observer) {
    const wire = await connect(observer);
    // The registry is given the observer as well as the transport: a
    // transport whose connections observe through something of their own
    // seats it there, where a relay reads it first, and one whose
    // connections do not leaves this the only place it is.
    const registry = new Registry(observer ? { observer } : {});
    before(registry);
    const log = memoryLog(maxFrameBytes);
    const { near: machine, far: up } = await wire.open();
    registry.bind('s', up, governance, log);
    return { wire, registry, log, machine };
  }

  /**
   * A consumer attached to that session: the end it speaks on, what the
   * registry knows it by, what it is reading and who it was told holds
   * control. It listens before it attaches, because the attach sends who
   * holds control and then everything the consumer missed.
   */
  async function consumer(wire: Wire, registry: Registry, role: Role, origin: string, after = 0):
  Promise<{ near: FrameConnection; at: ReturnType<typeof listen>; attachment: Attachment; joined: string | null }> {
    const { near, far } = await wire.open(after);
    const at = listen(near);
    const attachment = registry.attach('s', far, role, origin, after);
    return { near, at, attachment, joined: await at.control() };
  }

  /** The machine of the suite: a peer of the profile serving the family's server side over the up connection. */
  async function serving(machine: FrameConnection): Promise<DuplexPeer> {
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
    assert.equal(await two.at.control(), 'one');
    const client = await Client.attach(one.near, {}, answering);
    assert.deepEqual(await client.echo(payload('value')), { text: 'machine:value', count: 1 });
    await peer.emit('changed', payload('moved', 2));
    // What the peer sends is the peer's — a trace context among it — so the
    // event is held to what it is, not to the members it arrives with.
    const event = await two.at.family();
    assert.equal(event.kind, 'event');
    assert.equal(event.event, 'changed');
    assert.deepEqual(event.data, { text: 'moved', count: 2 });
    await tick();
    // The observer saw the event and nothing of the exchange it was not part
    // of, beside the session's own vocabulary: who held control when it
    // joined, who holds it now, and where the event stood.
    assert.deepEqual(two.at.envelopes.map(envelope => envelope.event ?? envelope.kind),
      [CONTROL_EVENT, CONTROL_EVENT, 'changed', CURSOR_EVENT]);
    // A generated client of a family that declares none of this sees events
    // it has no listener for and drops them, as the profile says it does.
    assert.deepEqual(await client.noArgs(), 'ok');
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
    const atOne = one.at;
    const atTwo = two.at;
    assert.deepEqual(registry.attention(), []);
    registry.control('s', one.attachment);
    assert.deepEqual([await atOne.control(), await atTwo.control()], ['one', 'one']);
    const answer = peer.call('reverse', payload('deliver'));
    const asked = await atOne.family();
    assert.equal(asked.method, 'reverse');
    assert.deepEqual(registry.attention(), ['s']);
    // Control moves while the ask is open: every consumer is told, and the
    // ask is asked of the new holder under the id the machine gave it, while
    // the one it was asked of no longer answers it.
    registry.control('s', two.attachment);
    assert.deepEqual([await atOne.control(), await atTwo.control()], ['two', 'two']);
    // The frame the log already holds, handed again: no new place in the
    // order, and so no cursor of its own.
    const again = await atTwo.next();
    assert.deepEqual(again, asked);
    say(one.near, { version: 1, kind: 'response', id: asked.id, result: payload('too late') });
    await tick();
    assert.deepEqual(registry.attention(), ['s']);
    say(two.near, { version: 1, kind: 'response', id: again.id, result: payload('reveiled') });
    assert.deepEqual(await answer, { text: 'reveiled', count: 1 });
    assert.deepEqual(registry.attention(), []);
    // A consumer attaching afterwards is told who holds control before any
    // frame at all; an observer is never given it.
    const watching = await consumer(wire, registry, 'observer', 'watching');
    assert.equal(watching.joined, 'two');
    assert.throws(() => registry.control('s', watching.attachment), (error: unknown) => error instanceof DuplexError && error.code === 'not_controlling');
    wire.close();
  });

  test('two consumers that each mint c:1 are distinct on the session, and each response reaches the one that asked', async () => {
    const { wire, registry, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'participant', 'two');
    const atOne = one.at;
    const atTwo = two.at;
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'seen', params: { of: 'one' } });
    const first = await atMachine.next();
    say(two.near, { version: 1, kind: 'request', id: 'c:1', method: 'seen', params: { of: 'two' } });
    const second = await atMachine.next();
    assert.notEqual(first.id, second.id);
    assert.deepEqual([first.params, second.params], [{ of: 'one' }, { of: 'two' }]);
    // Answered in the other order, each answer still reaches the one that asked.
    say(machine, { version: 1, kind: 'response', id: second.id, result: 'for two' });
    say(machine, { version: 1, kind: 'response', id: first.id, result: 'for one' });
    assert.deepEqual(await atOne.family(), { version: 1, kind: 'response', id: 'c:1', result: 'for one' });
    assert.deepEqual(await atTwo.family(), { version: 1, kind: 'response', id: 'c:1', result: 'for two' });
    wire.close();
  });

  test('a frame carries the members the relay knows nothing of to the other side intact, in either direction', async () => {
    const { wire, registry, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const atOne = one.at;
    registry.control('s', one.attachment);
    await atOne.control();
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
    assert.deepEqual(await atOne.family(), emitted);
    wire.close();
  });

  test('a consumer attached after a sequence is given the log from it, before anything live, in one order', async () => {
    const { wire, registry, log, machine } = await bound();
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    registry.control('s', one.attachment);
    await one.at.control();
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
    const atTwo = two.at;
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('live') };
    say(machine, live);
    for (let i = 0; i < recorded.length + 1; i++) await atTwo.family();
    assert.deepEqual(conversation(atTwo.envelopes), [...recorded, live]);
    // The cursor it was told is the log's own sequence, which is where it
    // reattaches after and never a count of what arrived.
    assert.equal(atTwo.cursor, 7);
    assert.equal(two.attachment.sequence, 7);
    wire.close();
  });

  test('a session bound over a log that already holds frames is bound at its head', async () => {
    const wire = await connect();
    const registry = new Registry();
    // The log is filled through the interface a consumer's own durable one
    // implements, so that what is held here is what a session bound after a
    // restart is bound over, and the suite needs no log of its own.
    const log = memoryLog(1 << 20);
    const held = [1, 2, 3].map(count => ({ version: 1, kind: 'event', event: 'changed', data: payload('held', count) }));
    for (const message of held) await log.append({ sequence: 0, direction: 'down', origin: '', at: new Date(), message, truncated: false });
    const { near: machine, far: up } = await wire.open();
    registry.bind('s', up, governance, log);
    // A consumer resuming from nothing, before the machine has spoken at all,
    // is given every frame the log holds.
    const all = await consumer(wire, registry, 'observer', 'all');
    const atAll = all.at;
    for (const message of held) assert.deepEqual(await atAll.family(), message);
    // And one holding all but the last two takes exactly those two.
    const late = await consumer(wire, registry, 'observer', 'late', held.length - 2);
    const atLate = late.at;
    for (const message of held.slice(-2)) assert.deepEqual(await atLate.family(), message);
    await tick();
    assert.deepEqual([conversation(atAll.envelopes).length, conversation(atLate.envelopes).length], [held.length, 2]);
    // The session goes on from the log's end rather than from nothing: the
    // machine's next frame takes the sequence after the head, which is what a
    // consumer holding the whole log is replayed nothing before.
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('live', held.length + 1) };
    say(machine, live);
    assert.deepEqual(await atAll.family(), live);
    assert.deepEqual(await atLate.family(), live);
    const after = await consumer(wire, registry, 'observer', 'after', held.length);
    const atAfter = after.at;
    assert.deepEqual(await atAfter.family(), live);
    await tick();
    assert.deepEqual(conversation(atAfter.envelopes), [live]);
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
    const atTwo = two.at;
    registry.control('s', one.attachment);
    assert.equal(await atTwo.control(), 'one');
    one.near.close(1000, 'done with it');
    await tick();
    // The session stands: what the one that left held is released, which the
    // consumer still attached is told, and it is where it was.
    assert.equal(await atTwo.control(), null);
    assert.equal(two.attachment.holder, null);
    assert.throws(() => registry.control('s', one.attachment), (error: unknown) => error instanceof DuplexError && error.code === 'not_attached');
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('still here') };
    say(machine, live);
    assert.deepEqual(await atTwo.family(), live);
    machine.close(4001, 'the machine went away');
    await tick();
    assert.deepEqual(atTwo.closed, { code: 4001, reason: 'the machine went away' });
    assert.equal(two.near.state, 'closed');
    assert.throws(() => registry.attach('s', two.attachment.channel, 'participant', 'again', 0), (error: unknown) => error instanceof DuplexError && error.code === 'no_session');
    assert.deepEqual(registry.attention(), []);
    wire.close();
  });

  test('every domain change of a session reaches onChange and the observer the session was given, in the order the registry made them', async () => {
    let seen!: ReturnType<typeof watching>;
    const told = observing();
    const { wire, registry, machine } = await bound(1 << 20, registry => { seen = watching(registry); }, told.observer);
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
    // The same run, read off the observer the session was given:
    // the same changes, said the way the runtime says things, in one order.
    assert.deepEqual(told.lines, [
      'session.bound',
      'session.attached one participant #0',
      'session.attached two participant #0',
      'control.changed one',
      'frame.appended up one echo #1',
      'frame.appended down #2',
      'session.refused not_controlling echo two participant',
      'frame.appended down reverse #3',
      'ask.raised s:1 reverse asking',
      'ask.routed s:1 reverse one',
      'control.changed two',
      'ask.routed s:1 reverse two',
      'ask.answered s:1 reverse two',
      'frame.appended up two #4',
      'session.detached one participant',
      'session.unbound 4002 the machine went away',
    ]);
    // Every event is of the session it was made on, at the time it was made,
    // and one that concerns a frame carries that frame's trace.
    for (const event of told.events) {
      assert.equal((event as { session: string }).session, 's');
      assert.equal(event.at instanceof Date, true);
    }
    assert.deepEqual(told.of('ask.raised')[0]!.trace, { traceparent: asked });
    assert.deepEqual(told.of('ask.routed').map(event => event.trace?.traceparent), [asked, asked]);
    assert.deepEqual(told.of('frame.appended')[0]!.trace, { traceparent });
    assert.equal(told.of('frame.appended')[1]!.trace, undefined);
    assert.equal(told.of('session.bound')[0]!.at instanceof Date, true);
    // A frame's size is the frame the machine read, to the byte.
    assert.equal(told.of('frame.appended')[0]!.bytes, new TextEncoder().encode(JSON.stringify(request)).byteLength);
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

  test("a frame's meta reaches the machine and every consumer verbatim", async () => {
    // The relay forwards the profile's carriage as it forwards a member it does
    // not know: it is the consumer's and the machine's, and nothing between them
    // reads, rewrites or strips it.
    const { wire, registry, machine } = await bound(1 << 20);
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'two');
    registry.control('s', one.attachment);
    assert.deepEqual([await one.at.control(), await two.at.control()], ['one', 'one']);
    const carried = { tenant: 'acme', idempotency: 'k-1' };
    say(one.near, { version: 1, kind: 'request', id: 'c:9', method: 'echo', params: payload('t'), meta: carried });
    assert.deepEqual((await atMachine.next()).meta, carried);
    say(machine, { version: 1, kind: 'event', event: 'changed', data: payload('t'), meta: { cause: 'nightly' } });
    for (const at of [one.at, two.at]) {
      assert.deepEqual((await at.family()).meta, { cause: 'nightly' });
    }
    // The log keeps each message whole, so a consumer that was not there is
    // replayed the carriage with it — the request the relay recorded on its way
    // to the machine and then the event — which is why a meta that holds a
    // credential wants a Log that redacts, as docs/wire/session.md says.
    const late = await consumer(wire, registry, 'observer', 'late');
    assert.deepEqual((await late.at.family()).meta, carried);
    assert.deepEqual((await late.at.family()).meta, { cause: 'nightly' });
    wire.close();
  });

  test('a change and an event say what a frame was and never what it carried', async () => {
    const sentinel = 'squeamish-ossifrage';
    let seen!: ReturnType<typeof watching>;
    const told = observing();
    const { wire, registry, machine } = await bound(1 << 20, registry => { seen = watching(registry); }, told.observer);
    const atMachine = listen(machine);
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'two');
    registry.control('s', one.attachment);
    say(one.near, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: payload(sentinel), meta: { secret: sentinel } });
    const request = await atMachine.next();
    say(machine, { version: 1, kind: 'response', id: request.id, result: payload(sentinel) });
    say(machine, { version: 1, kind: 'event', event: 'changed', data: payload(sentinel), meta: { secret: sentinel } });
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
    assert.equal(told.events.length > 0, true);
    for (const event of told.events) {
      assert.equal(JSON.stringify(event).includes(sentinel), false, `a ${event.type} carried what a frame carried`);
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

  test('every consumer is told who holds control, and one attaching is told before anything else', async () => {
    const { wire, registry, machine } = await bound();
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'two');
    // A consumer that joins a session nobody holds is told exactly that,
    // which is what a holder absent means and what an origin alone could not
    // say of a consumer attached under no name.
    assert.deepEqual([one.joined, two.joined], [null, null]);
    registry.control('s', one.attachment);
    assert.deepEqual([await one.at.control(), await two.at.control()], ['one', 'one']);
    registry.control('s', null);
    assert.deepEqual([await one.at.control(), await two.at.control()], [null, null]);
    // Given again, so that a consumer attaching after it joins a session
    // somebody holds, and with one frame in the log so that its replay is
    // not empty: who holds control comes before that too.
    registry.control('s', one.attachment);
    await one.at.control();
    await two.at.control();
    const emitted = { version: 1, kind: 'event', event: 'changed', data: payload('t') };
    say(machine, emitted);
    await one.at.family();
    await two.at.family();
    const three = await consumer(wire, registry, 'observer', 'three');
    assert.equal(three.joined, 'one');
    assert.deepEqual(await three.at.family(), emitted);
    await tick();
    assert.deepEqual(conversation(three.at.envelopes), [emitted]);
    wire.close();
  });

  test('an attachment says what the relay told its consumer', async () => {
    const { wire, registry, machine } = await bound();
    const one = await consumer(wire, registry, 'participant', 'one');
    assert.equal(one.attachment.holder, null);
    assert.equal(one.attachment.sequence, 0);
    const moved: (string | null)[] = [];
    const stop = one.attachment.onControl(holder => { moved.push(holder); });
    registry.control('s', one.attachment);
    assert.equal(await one.at.control(), 'one');
    assert.deepEqual(moved, ['one']);
    assert.equal(one.attachment.holder, 'one');
    say(machine, { version: 1, kind: 'event', event: 'changed', data: payload('t') });
    await one.at.family();
    assert.deepEqual([one.attachment.sequence, one.at.cursor], [1, 1]);
    // A stopped registration hears nothing of what the consumer is still
    // told, and the state stands whether anything is registered at all.
    stop();
    registry.control('s', null);
    assert.equal(await one.at.control(), null);
    assert.equal(one.attachment.holder, null);
    assert.deepEqual(moved, ['one']);
    wire.close();
  });

  test('a consumer reattaches after the cursor it was told, which counting what arrived gets wrong', async () => {
    // A log bound with four frames in it, the second of them over its bound
    // and so kept cut: a cut message is no message for a channel that speaks
    // the family, so three arrive and the session stands at four. A consumer
    // counting what arrived would reattach at three and be given the fourth
    // a second time.
    const wire = await connect();
    const registry = new Registry();
    const log = memoryLog(64);
    const held: Envelope[] = [
      { version: 1, kind: 'event', event: 'changed', data: 1 },
      { version: 1, kind: 'event', event: 'changed', data: { text: 'well beyond the bound this log was given', count: 2 } },
      { version: 1, kind: 'event', event: 'changed', data: 3 },
      { version: 1, kind: 'event', event: 'changed', data: 4 },
    ];
    for (const message of held) await log.append({ sequence: 0, direction: 'down', origin: '', at: new Date(), message, truncated: false });
    const { near: machine, far: up } = await wire.open();
    registry.bind('s', up, governance, log);
    // A consumer holding the whole log, which is replayed nothing and is
    // where the suite reads that a live frame has been recorded.
    const watcher = await consumer(wire, registry, 'observer', 'watcher', held.length);
    const one = await consumer(wire, registry, 'observer', 'one');
    for (const message of [held[0], held[2], held[3]]) assert.deepEqual(await one.at.family(), message);
    await tick();
    assert.deepEqual([one.at.cursor, one.attachment.sequence], [4, 4]);
    assert.deepEqual(conversation(one.at.envelopes).length, 3);
    // Nothing of the session's own vocabulary is in the log, so a replay
    // never gives a stale holder or a cursor of its own: what a consumer is
    // told is the relay's, made where it is sent.
    const kept: Envelope[] = [];
    await log.replay(0, frame => { kept.push(frame.message as Envelope); return Promise.resolve(); });
    assert.deepEqual(conversation(kept).length, kept.length);

    one.attachment.detach();
    const live = { version: 1, kind: 'event', event: 'changed', data: 5 };
    say(machine, live);
    await watcher.at.family();
    const again = await consumer(wire, registry, 'observer', 'one', one.at.cursor);
    assert.deepEqual(await again.at.family(), live);
    await tick();
    assert.deepEqual(conversation(again.at.envelopes), [live]);
    // And the count a consumer would have kept itself is one short.
    const counted = await consumer(wire, registry, 'observer', 'counted', one.at.cursor - 1);
    assert.deepEqual(await counted.at.family(), held[3]);
    wire.close();
  });

  test("a machine that sends the session's own vocabulary ends the session", async () => {
    // The vocabulary is the relay's to produce: a machine speaking it speaks
    // for the layer above it, which is no frame of the family.
    const refused: Envelope[] = [
      { version: 1, kind: 'event', event: CONTROL_EVENT, data: { holder: 'one' } },
      { version: 1, kind: 'event', event: CURSOR_EVENT, data: { sequence: 9 } },
      { version: 1, kind: 'request', id: 's:1', method: 'session.subscribe', params: { events: [] } },
    ];
    for (const sent of refused) {
      const { wire, registry, machine } = await bound();
      const one = await consumer(wire, registry, 'participant', 'one');
      say(machine, sent);
      await tick();
      const named = (sent.event ?? sent.method) as string;
      // The consumer is ended with the session and was given nothing of what
      // the machine sent, which the log did not keep either.
      assert.deepEqual(one.at.closed, { code: 1002, reason: `a machine does not send ${named}` });
      assert.deepEqual(conversation(one.at.envelopes), []);
      wire.close();
    }
  });

  test("every refusal carries the code the session's vocabulary names", async () => {
    // What a call refuses with is the API a consumer's server is written
    // against, so it is a code a program branches on and not prose a program
    // would have to match.
    const refused = async (what: string, want: string, run: () => unknown) => {
      let caught: unknown;
      try {
        await run();
      } catch (error) {
        caught = error;
      }
      assert.ok(caught !== undefined, `${what} was not refused`);
      assert.ok(caught instanceof DuplexError, `${what} was refused with ${String(caught)}, which is no DuplexError`);
      assert.equal((caught as DuplexError).code, want, `${what} was refused with ${(caught as DuplexError).code}`);
      assert.notEqual((caught as DuplexError).message, '', `${what} was refused with a code and nothing for a person to read`);
    };

    const { wire, registry } = await bound();
    const spare = async () => (await wire.open()).far;

    // A session is bound under an id, and under one that is not already bound.
    await refused('a bind under no id', 'session_invalid', async () => registry.bind('', await spare(), governance, memoryLog(1 << 20)));
    await refused('a bind under an id already bound', 'session_exists', async () => registry.bind('s', await spare(), governance, memoryLog(1 << 20)));

    // No session under that id, whichever call names it.
    await refused('an attach to a session nothing bound', 'no_session', async () => registry.attach('nothing', await spare(), 'participant', 'one', 0));
    await refused('control of a session nothing bound', 'no_session', () => registry.control('nothing', null));

    // What a consumer attaches with: a role, an origin, a sequence.
    await refused('an attach in a role that is not one', 'role_invalid', async () => registry.attach('s', await spare(), 'holder' as Role, 'one', 0));
    await refused('an attach under an origin that is no text', 'origin_invalid', async () => registry.attach('s', await spare(), 'participant', 7 as unknown as string, 0));
    await refused('an attach after what is no sequence', 'sequence_invalid', async () => registry.attach('s', await spare(), 'participant', 'one', -1));

    // Who may be given control: a consumer of this session, and a participant.
    const one = await consumer(wire, registry, 'participant', 'one');
    const watcher = await consumer(wire, registry, 'observer', 'watcher');
    await refused('control given to an observer', 'not_controlling', () => registry.control('s', watcher.attachment));
    registry.bind('elsewhere', await spare(), governance, memoryLog(1 << 20));
    const stranger = registry.attach('elsewhere', await spare(), 'participant', 'stranger', 0);
    await refused('control given to a consumer of another session', 'not_attached', () => registry.control('s', stranger));
    one.attachment.detach();
    await tick();
    await refused('control given to a consumer that has left', 'not_attached', () => registry.control('s', one.attachment));

    // No room for another consumer.
    const full = new Registry({ maxAttachments: 1 });
    full.bind('s', await spare(), governance, memoryLog(1 << 20));
    full.attach('s', await spare(), 'participant', 'first', 0);
    await refused('an attach beyond what the session holds', 'too_many_attachments', async () => full.attach('s', await spare(), 'participant', 'second', 0));

    // And what a registry is made with.
    await refused('a limit that is no limit', 'invalid_options', () => new Registry({ maxAttachments: 0 }));

    wire.close();
  });

  test('a frame that is not text ends the connection as unsupported data', async () => {
    // A frame of the wrong kind carries no message of the profile at all,
    // which is unsupported data — 1003 — where text that is no message of
    // it is a fault of another kind and closes with another code. Either
    // side may send one and each is refused the same way.
    const said = 'a session speaks JSON text frames';
    const { wire, registry, machine } = await bound();
    const one = await consumer(wire, registry, 'participant', 'one');
    const two = await consumer(wire, registry, 'observer', 'watcher');

    one.near.send({ kind: 'binary', data: new Uint8Array([0, 1]) });
    await tick();
    assert.deepEqual(one.at.closed, { code: 1003, reason: said });
    // The session stands and goes on routing: only the consumer that sent
    // it is gone.
    assert.equal(two.at.closed, undefined);
    const live = { version: 1, kind: 'event', event: 'changed', data: payload('still here') };
    say(machine, live);
    assert.deepEqual(await two.at.family(), live);

    // The machine's own ends the session, and every consumer with it, under
    // the same code and the same reason.
    machine.send({ kind: 'binary', data: new Uint8Array([2]) });
    await tick();
    assert.deepEqual(two.at.closed, { code: 1003, reason: said });
    wire.close();
  });

  test('text that is no message of the profile ends the connection as a protocol error', async () => {
    // Text is where a message of the profile would be, so what is wrong is
    // the message and not the frame: a protocol error — 1002 — under a
    // reason naming the fault, where a frame of the wrong kind is
    // unsupported data. The reasons are the closed set the relay gives and
    // are the same in both languages, a consumer reading one off the close
    // being unable to ask which runtime wrote the relay — an object
    // malformed inside among them, where a parser's own words for a syntax
    // error would be one runtime's prose on the wire.
    const malformed = [
      { what: 'text that is no JSON at all', sent: 'not json', said: 'a session frame must be a JSON object' },
      { what: 'a JSON array', sent: '["version",1]', said: 'a session frame must be a JSON object' },
      { what: 'an object malformed inside', sent: '{"version":1,"kind":}', said: 'a session frame must be a JSON object' },
      { what: 'a member named twice', sent: '{"version":1,"kind":"request","id":"c:1","id":"c:2","method":"no_args","params":{}}', said: 'duplicate session frame member "id"' },
      { what: 'content after the object', sent: '{"version":1,"kind":"event","event":"changed","data":{"text":"x","count":1}} {}', said: 'invalid trailing session frame content' },
    ];
    for (const { what, sent, said } of malformed) {
      const { wire, registry, machine } = await bound();
      const one = await consumer(wire, registry, 'participant', 'one');
      const two = await consumer(wire, registry, 'observer', 'watcher');

      one.near.send({ kind: 'text', data: sent });
      await tick();
      assert.deepEqual(one.at.closed, { code: 1002, reason: said }, `a consumer sending ${what}`);
      // The session stands and goes on routing: only the consumer that sent
      // it is gone.
      assert.equal(two.at.closed, undefined, `a consumer sending ${what} ended another consumer`);
      const live = { version: 1, kind: 'event', event: 'changed', data: payload('still here') };
      say(machine, live);
      assert.deepEqual(await two.at.family(), live);

      // The machine's own ends the session, and every consumer with it,
      // under the same code and the same reason.
      machine.send({ kind: 'text', data: sent });
      await tick();
      assert.deepEqual(two.at.closed, { code: 1002, reason: said }, `the machine sending ${what}`);
      wire.close();
    }
  });
}
