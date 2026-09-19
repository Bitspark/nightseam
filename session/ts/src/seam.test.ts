/**
 * What only a connection of the seam can be asked of a session, beside the
 * suite both transports run: the mixed case — an in-process machine bound
 * over a pipe and a consumer attached over a channel of a tunnel over a
 * WebSocket — and where a session tells what it does when what it is bound
 * over observes through nothing of its own.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe, webSocketConnection, type FrameConnection, type WebSocketLike } from '@nightseam/duplex';
import { DuplexPeer, type Observer, type ObserverEvent } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';
import { governance } from './conformance.ts';
import { CURSOR_EVENT, memoryLog, PREFIX, Registry, type Attachment } from './index.ts';

/** One envelope of the profile, as a raw end reads and writes it. */
type Envelope = Record<string, unknown>;

const tick = () =>
  new Promise((resolve) => {
    setTimeout(resolve, 5);
  });

/** say writes one envelope to a connection, as a peer of the family would. */
function say(connection: FrameConnection, envelope: Envelope): void {
  connection.send({ kind: 'text', data: JSON.stringify(envelope) });
}

/** The envelopes a connection receives, its close, and a promise of the next one not yet taken; a consumer's end also takes a frame of the family with the cursor the relay sends straight after it. */
function listen(connection: FrameConnection) {
  const envelopes: Envelope[] = [];
  const waiters: ((envelope: Envelope) => void)[] = [];
  let taken = 0;
  let closed: { code: number; reason: string } | undefined;
  connection.listen({
    frame: (frame) => {
      if (frame.kind !== 'text') return;
      const envelope = JSON.parse(frame.data) as Envelope;
      envelopes.push(envelope);
      waiters.shift()?.(envelope);
    },
    close: (code, reason) => {
      closed = { code, reason };
    },
  });
  function next(): Promise<Envelope> {
    if (taken < envelopes.length) return Promise.resolve(envelopes[taken++]!);
    taken++;
    return new Promise<Envelope>((resolve) => {
      waiters.push(resolve);
    });
  }
  return {
    envelopes,
    next,
    /** One frame of the family and the cursor that names its place, the session's own vocabulary before it passed over. */
    async family(): Promise<Envelope> {
      for (;;) {
        const frame = await next();
        if (typeof frame.event === 'string' && frame.event.startsWith(PREFIX) && frame.event !== CURSOR_EVENT) continue;
        assert.equal(frame.event === CURSOR_EVENT, false, 'a cursor arrived where no frame stood before it');
        assert.equal((await next()).event, CURSOR_EVENT, 'a frame of the family was not followed by its cursor');
        return frame;
      }
    },
    get closed() {
      return closed;
    },
  };
}

/**
 * drive takes one session of a registry from bound to unbound over pipes,
 * through every event a session has: bound, attached twice, control moved,
 * an ask raised, routed and answered, frames appended, a consumer refused,
 * one detached and the session unbound.
 */
async function drive(registry: Registry): Promise<void> {
  const [machine, up] = pipe();
  registry.bind('s', up, governance, memoryLog(1 << 20));
  const [holderEnd, near] = pipe();
  const holder = registry.attach('s', near, 'participant', 'one', 0);
  const [idleEnd, idle] = pipe();
  registry.attach('s', idle, 'participant', 'two', 0);
  registry.control('s', holder);
  const atHolder = listen(holderEnd);
  const atIdle = listen(idleEnd);
  say(machine, { version: 1, kind: 'request', id: 's:1', method: 'reverse', params: { text: 't', count: 1 } });
  const asked = await atHolder.family();
  say(holderEnd, { version: 1, kind: 'response', id: asked.id, result: { text: 't', count: 1 } });
  await tick();
  // A participant that does not hold control is refused in the machine's
  // place, which the machine never sees and an observer does.
  say(idleEnd, { version: 1, kind: 'request', id: 'c:1', method: 'echo', params: { text: 't', count: 1 } });
  await atIdle.next();
  await atIdle.next();
  await atIdle.next();
  holder.detach();
  machine.close(4002, 'the machine went away');
  await tick();
}

test('a session bound over a connection that observes through nothing of its own tells the registry observer', async () => {
  const told: string[] = [];
  const observer: Observer = {
    observe: (event: ObserverEvent) => {
      told.push(event.type);
    },
  };
  await drive(new Registry({ observer }));
  for (const type of [
    'session.bound',
    'session.attached',
    'control.changed',
    'ask.raised',
    'ask.routed',
    'ask.answered',
    'frame.appended',
    'session.refused',
    'session.detached',
    'session.unbound',
  ]) {
    assert.equal(
      told.includes(type),
      true,
      'the registry observer was never told ' + type + '; it heard ' + told.join(', '),
    );
  }
});

test('a session with neither a connection that observes nor a registry observer observes nothing and runs all the same', async () => {
  const registry = new Registry();
  await drive(registry);
  // The session ran to its end: it is gone from the registry, which is what
  // unbound says to an observer there is none of.
  assert.throws(
    () => registry.control('s', null),
    (error: unknown) => (error as { code?: string }).code === 'no_session',
  );
});

/**
 * A socket pair in memory, as faithful to a WebSocket as the seam's adapter
 * reads one: a message is delivered in a later turn, and a close ends both
 * ends with the code and the reason the closing side gave. It is how every
 * suite here holds the WebSocket surface, the runtime's and the seam's own
 * included.
 */
class Socket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  partner?: Socket;

  static pair(): [Socket, Socket] {
    const left = new Socket();
    const right = new Socket();
    left.partner = right;
    right.partner = left;
    return [left, right];
  }

  send(data: string): void {
    if (this.readyState !== 1) throw new Error('The socket is not open.');
    const partner = this.partner;
    if (!partner) return;
    queueMicrotask(() => {
      if (partner.readyState === 1) partner.dispatchEvent(new MessageEvent('message', { data }));
    });
  }

  close(code = 1000, reason = ''): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.dispatchEvent(Object.assign(new Event('close'), { code, reason }));
    this.partner?.close(code, reason);
  }
}

test('an in-process machine over a pipe and a consumer over a tunnel channel on a WebSocket are one session', async () => {
  const registry = new Registry();

  // The machine is this process: a peer of the family over a pipe, bound
  // with no tunnel and no socket between it and the relay.
  const [own, up] = pipe();
  const machine = new DuplexPeer({ role: 'server' });
  machine.handle('echo', (params) => {
    const said = params as { text: string; count: number };
    return { text: 'machine:' + said.text, count: said.count };
  });
  await machine.attach(own);
  registry.bind('s', up, governance, memoryLog(1 << 20));

  // The consumer is elsewhere: a channel of a tunnel over a WebSocket, which
  // the service accepts and attaches to the same session.
  const [here, there] = Socket.pair();
  const serving = new DuplexPeer({ role: 'server' });
  const dialling = new DuplexPeer({ role: 'client' });
  await Promise.all([serving.attach(webSocketConnection(here)), dialling.attach(webSocketConnection(there))]);
  const served = new Tunnel(serving);
  const dialled = new Tunnel(dialling);
  const accepted = served.accept();
  const consumer = await dialled.open('probe', 0);
  const attachment: Attachment = registry.attach('s', await accepted, 'participant', 'consumer', 0);
  registry.control('s', attachment);
  const atConsumer = listen(consumer);

  // A call: the consumer's request reaches the machine under an id of the
  // session's own and its answer comes back under the consumer's.
  say(consumer, {
    version: 1,
    kind: 'request',
    id: 'c:1',
    method: 'echo',
    params: { text: 'over the seam', count: 1 },
  });
  assert.deepEqual(await atConsumer.family(), {
    version: 1,
    kind: 'response',
    id: 'c:1',
    result: { text: 'machine:over the seam', count: 1 },
  });

  // An event: what the machine emits reaches every consumer attached.
  await machine.emit('changed', { text: 'moved', count: 2 });
  const event = await atConsumer.family();
  assert.equal(event.event, 'changed');
  assert.deepEqual(event.data, { text: 'moved', count: 2 });

  // An ask: what the machine asks is routed to the holder of control, and
  // its answer travels back over the pipe.
  const answer = machine.call('reverse', { text: 'deliver', count: 1 });
  const ask = await atConsumer.family();
  assert.equal(ask.method, 'reverse');
  say(consumer, { version: 1, kind: 'response', id: ask.id, result: { text: 'reveiled', count: 1 } });
  assert.deepEqual(await answer, { text: 'reveiled', count: 1 });

  // The close: the machine's connection ending ends the consumer's channel
  // with the same close, over a transport that carried none of it.
  own.close(4002, 'the machine went away');
  await tick();
  assert.deepEqual(atConsumer.closed, { code: 4002, reason: 'the machine went away' });
  dialling.close();
  serving.close();
});
