/**
 * The TypeScript side of the session component's cross-language gate,
 * invoked by session/go/interop_test.go against a real local WebSocket
 * server. Two sessions run over the one connection, one each way: first the
 * consumers of a session bound on the Go registry, then a session bound
 * here whose consumers are Go's. The machine of each speaks the registry's
 * own language over a pipe, because the boundary the gate is about is the
 * consumers'.
 *
 * Every frame it sends and every frame it holds the other side to is a
 * literal, the same literal session/go/interop_test.go carries: a relay
 * forwards what it does not know verbatim and mints the ids it sends on the
 * same way in either language, so the two sides meet byte for byte.
 */
import assert from 'node:assert/strict';
import { pipe } from '@nightseam/duplex';
import { DuplexPeer } from '@nightseam/runtime';
import { Tunnel, type Channel } from '@nightseam/tunnel';
import { asks, decides } from '../../../cmd/nightseam/testdata/golden/api/ts/probe-client/src/index.ts';
import { memoryLog, Registry, type Governance, type Role } from './index.ts';

/** The probe family's session tier, as its generated client states it. */
const governance: Governance = { decides: method => decides.has(method), asks: method => asks.has(method) };

/** The machine's first word, which is also the gate's go-ahead: the consumers are attached and the first of them holds control. */
const ready = '{"version":1,"kind":"event","event":"changed","tracestate":"nightseam=gate","data":{"text":"ready","count":0}}';
/** A consumer's deciding request, under an id of its own and carrying members no relay knows, and the same request as the machine sees it. */
const echo = '{"version":1,"kind":"request","id":"c:7","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","method":"echo","params":{"text":"gate","count":1},"tracestate":"nightseam=gate","baggage":{"tenant":"acme"}}';
const minted = '{"version":1,"kind":"request","id":"c:1","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","method":"echo","params":{"text":"gate","count":1},"tracestate":"nightseam=gate","baggage":{"tenant":"acme"}}';
/** The machine's answer, under the id the session asked with, and that answer as the one consumer that asked reads it. */
const answer = '{"version":1,"kind":"response","id":"c:1","result":{"text":"etag","count":1}}';
const answered = '{"version":1,"kind":"response","id":"c:7","result":{"text":"etag","count":1}}';
/** What the machine asks of whoever holds control, and the holder saying it has the ask — which is what moves control while the ask is open. */
const ask = '{"version":1,"kind":"request","id":"s:1","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b8-01","method":"reverse","params":{"text":"gate","count":1}}';
const noticed = '{"version":1,"kind":"event","event":"noticed","data":{"text":"asked","count":1}}';
/** The answer of the consumer control left, which no machine sees, and the answer of the one it moved to, which every machine does. */
const stale = '{"version":1,"kind":"response","id":"s:1","result":{"text":"stale","count":1}}';
const held = '{"version":1,"kind":"response","id":"s:1","result":{"text":"etag","count":1}}';
/** The one live frame after a replay, and the resuming consumer saying it read the log and the frame that followed it. */
const live = '{"version":1,"kind":"event","event":"changed","tracestate":"nightseam=late","data":{"text":"live","count":2}}';
const replayed = '{"version":1,"kind":"event","event":"noticed","data":{"text":"replayed","count":6}}';
/** The close the machine's channel ends with, and which every attached channel is ended with in turn. */
const gone = { code: 1008, reason: 'the machine went away' };

/** What a channel this side speaks over received, as the text it arrived as, and the close it ended with. */
function watch(channel: Channel, name: string) {
  const arrived: string[] = [];
  const waiting: ((frame: string) => void)[] = [];
  const ending: ((close: { code: number; reason: string }) => void)[] = [];
  let taken = 0;
  let closed: { code: number; reason: string } | undefined;
  channel.listen({
    frame: frame => {
      if (frame.kind !== 'text') return;
      arrived.push(frame.data);
      waiting.shift()?.(frame.data);
    },
    close: (code, reason) => {
      closed = { code, reason };
      for (const end of ending.splice(0)) end(closed);
    },
  });
  const next = (): Promise<string> => {
    if (taken < arrived.length) return Promise.resolve(arrived[taken++]!);
    taken++;
    return new Promise<string>(resolve => { waiting.push(resolve); });
  };
  return {
    arrived,
    next,
    /** takes holds the next frame to the one text it was sent as. */
    async takes(frame: string): Promise<void> {
      assert.equal(await next(), frame, `${name} received a frame other than the one that was sent`);
    },
    ended(): Promise<{ code: number; reason: string }> {
      if (closed) return Promise.resolve(closed);
      return new Promise(resolve => { ending.push(resolve); });
    },
  };
}

/** say writes one frame to a channel, as the text a peer of the family would have written. */
function say(channel: Channel, frame: string): void {
  channel.send({ kind: 'text', data: frame });
}

/** What to close once the gate has run, in the order it was opened. */
const closers: (() => void)[] = [];

/**
 * The gate one way: the session is bound on the Go registry and every
 * consumer of it is this side's, over channels this side opens in the order
 * the registry attaches them — the holder, the one control moves to, and
 * the consumer that resumes from the sequence its own open carries.
 */
async function consumers(carrier: Tunnel): Promise<void> {
  const one = await carrier.open('probe', 0);
  const two = await carrier.open('probe', 0);
  const atOne = watch(one, 'one');
  const atTwo = watch(two, 'two');
  await atOne.takes(ready);
  await atTwo.takes(ready);
  // A response reaches the one consumer that asked, under the id it asked with.
  say(one, echo);
  await atOne.takes(answered);
  // An ask reaches the holder, which says so; control moves while it is
  // open, and the ask follows it as the machine sent it.
  await atOne.takes(ask);
  say(one, noticed);
  await atTwo.takes(ask);
  say(one, stale);
  say(two, held);
  // A consumer resuming from the first frame reads the log from there, in
  // one order, before the frame the machine sends once it is attached.
  const late = await carrier.open('probe', 1);
  const atLate = watch(late, 'late');
  for (const frame of [minted, answer, ask, noticed, held, live]) await atLate.takes(frame);
  // That live frame reached every attached consumer, and those two read
  // nothing else: the exchange one was part of never reached two.
  await atOne.takes(live);
  await atTwo.takes(live);
  assert.deepEqual(atOne.arrived, [ready, answered, ask, live]);
  assert.deepEqual(atTwo.arrived, [ready, ask, live]);
  say(one, replayed);
  // The machine's channel closing ends every attached channel with its code
  // and its reason.
  for (const at of [atOne, atTwo, atLate]) assert.deepEqual(await at.ended(), gone);
}

/**
 * The gate the other way: the session is bound here and every consumer of it
 * is Go's, attached over a channel of the real connection in the order Go
 * opens them.
 */
async function relay(carrier: Tunnel): Promise<void> {
  const [here, there] = pipe();
  const near = new DuplexPeer({ role: 'client' });
  const far = new DuplexPeer({ role: 'server' });
  await Promise.all([near.attach(here), far.attach(there)]);
  closers.push(() => { near.close(); far.close(); });
  const accepted = new Tunnel(far).accept();
  const machine = await new Tunnel(near).open('probe', 0);
  const registry = new Registry();
  registry.bind('ts-relay', await accepted, governance, memoryLog(1 << 20));
  const atMachine = watch(machine, 'the machine');
  const holder = await joining(carrier, registry, 'participant', 'one');
  const next = await joining(carrier, registry, 'participant', 'two');
  registry.control('ts-relay', holder);
  say(machine, ready);
  // A consumer's request reaches the machine under an id of the session's
  // own, with every member the relay knows nothing of where it was.
  await atMachine.takes(minted);
  say(machine, answer);
  // What the machine asks reaches the holder; the holder says so, and
  // control moves while the ask is open.
  say(machine, ask);
  await atMachine.takes(noticed);
  assert.deepEqual(registry.attention(), ['ts-relay']);
  registry.control('ts-relay', next);
  // Only the consumer control moved to answers it: the other's answer is
  // dropped, so the machine reads one answer and it is the holder's.
  await atMachine.takes(held);
  assert.deepEqual(registry.attention(), []);
  // The consumer resuming from the first frame reads the rest of the log
  // before anything live; Go holds the replay to its one order.
  await joining(carrier, registry, 'observer', 'late');
  say(machine, live);
  await atMachine.takes(replayed);
  assert.deepEqual(atMachine.arrived, [minted, noticed, held, replayed]);
  // The machine's channel closing ends every attached channel with its own
  // close, which Go reads on each of them.
  machine.close(gone.code, gone.reason);
}

/** joining takes the next channel Go opened and adds it to the session as a consumer, resuming from the sequence the open itself carried: the tunnel carries after and never acts on it, and this is where it is acted on. */
async function joining(carrier: Tunnel, registry: Registry, role: Role, origin: string) {
  const channel = await carrier.accept();
  assert.equal(channel.family, 'probe', `Go opened a channel of another family for ${origin}`);
  return registry.attach('ts-relay', channel, role, origin, channel.after);
}

const peer = new DuplexPeer();
const ended = new Promise<void>(resolve => { peer.onClose(() => { resolve(); }); });
try {
  await peer.connect(process.argv[2]!);
  const carrier = new Tunnel(peer);
  await consumers(carrier);
  await relay(carrier);
  process.stdout.write(JSON.stringify({ ok: true, sessions: ['go-relay', 'ts-relay'] }) + '\n');
  // Go ends the connection once it has read the second session's closes, so
  // that what ends a channel here is a session and never the transport.
  await ended;
} finally {
  peer.close();
  for (const close of closers) close();
}
