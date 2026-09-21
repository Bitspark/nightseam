import assert from 'node:assert/strict';
import test from 'node:test';
import { at, mount, pipe, type Message, type Wire } from '@nightseam/duplex';
import { DuplexPeer } from './peer.ts';
import type { PeerOptions } from './peer.ts';
import { emitWire, forwardWire, registerWire, callWire, handleWire } from './wire.ts';
import type { WireEventContext } from './wire.ts';
import { wirePair } from './wire-pair.ts';
import { defaultPropagator } from './trace.ts';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
async function physical(serverOptions: PeerOptions = {}) {
  const [a, b] = pipe();
  const client = new DuplexPeer(),
    server = new DuplexPeer({ ...serverOptions, role: 'server' });
  await Promise.all([client.attach(a), server.attach(b)]);
  return {
    client,
    server,
    close: () => {
      client.close();
      server.close();
    },
  };
}

test('physical events keep verified context through forwarding local pair and mount without ambient outgoing metadata', async (t) => {
  const verified = Object.freeze({ source: 'trusted context' });
  const marker = Symbol('verified');
  const peers = await physical({
    propagator: {
      extract: (context, trace) => {
        defaultPropagator.extract(context, trace);
        Object.defineProperty(context, marker, { value: verified });
      },
      inject: defaultPropagator.inject,
    },
  });
  t.after(peers.close);
  const [access, binding] = wirePair({ maxPendingRequests: 1 });
  t.after(() => access.close());
  t.after(forwardWire(peers.server.wire(), at(mount(new Map([['local', access]])), ['local'])));
  const model = at(mount(new Map([['model', binding]])), ['model', 'events']);
  const observed = deferred<WireEventContext>();
  let effects = 0;
  registerWire(model, ['change'], {
    event: (_data, context) => {
      observed.resolve(context);
      if ((context as unknown as Record<symbol, unknown>)[marker] !== verified) throw new Error('Unverified event');
      effects++;
    },
  });
  const meta = { tenant: 'explicit', verified: 'cannot manufacture context' };
  emitWire(peers.client.wire(), ['events', 'change'], null, { meta });
  const context = await observed.promise;
  assert.equal((context as unknown as Record<symbol, unknown>)[marker], verified);
  assert.deepEqual(context.meta, meta);
  assert.equal((context as unknown as { peer: DuplexPeer }).peer, peers.server);
  handleWire(binding, ['barrier'], () => effects);
  assert.equal(await callWire(access, ['barrier']), 1);
  const received: WireEventContext[] = [];
  const arrived = deferred<void>();
  registerWire(peers.client.wire(), ['outgoing'], {
    event: (_value, context) => {
      received.push(context);
      if (received.length === 2) arrived.resolve();
    },
  });
  emitWire(peers.server.wire(), ['outgoing'], null, { context });
  emitWire(peers.server.wire(), ['outgoing'], null, { context, meta: context.meta });
  await arrived.promise;
  assert.equal((received[0] as unknown as Record<symbol, unknown>)[marker], undefined);
  assert.equal(received[0]!.meta, undefined);
  assert.deepEqual(received[1]!.meta, meta);
});

test('event metadata cannot create the private context required by an effect guard', async (t) => {
  const marker = Symbol('verified');
  const peers = await physical();
  t.after(peers.close);
  const [access, binding] = wirePair();
  t.after(() => access.close());
  t.after(forwardWire(peers.server.wire(), access));
  const denied = deferred<boolean>();
  registerWire(binding, ['guard'], {
    event: (_value, context) => {
      if (!(context as unknown as Record<symbol, unknown>)[marker]) {
        denied.resolve(true);
        throw new Error('Denied');
      }
      denied.resolve(false);
    },
  });
  emitWire(peers.client.wire(), ['guard'], null, { meta: { verified: 'yes' } });
  assert.equal(await denied.promise, true);
});

test('a forwarded event context ends at the next physical transport boundary', async (t) => {
  const marker = Symbol('verified');
  const source = await physical({
    propagator: {
      extract: (context, trace) => {
        defaultPropagator.extract(context, trace);
        Object.defineProperty(context, marker, { value: true });
      },
      inject: defaultPropagator.inject,
    },
  });
  const destination = await physical();
  t.after(source.close);
  t.after(destination.close);
  t.after(forwardWire(source.server.wire(), destination.client.wire()));
  const observed = deferred<WireEventContext>();
  registerWire(destination.server.wire(), ['event'], { event: (_value, context) => observed.resolve(context) });
  emitWire(source.client.wire(), ['event'], null, { meta: { explicit: 'yes' } });
  const context = await observed.promise;
  assert.equal((context as unknown as Record<symbol, unknown>)[marker], undefined);
  assert.deepEqual(context.meta, { explicit: 'yes' });
});

test('pure local events use the configured propagator', async (t) => {
  const marker = Symbol('local');
  const [a, b] = wirePair({
    propagator: {
      extract: (context, trace) => {
        defaultPropagator.extract(context, trace);
        Object.defineProperty(context, marker, { value: true });
      },
      inject: defaultPropagator.inject,
    },
  });
  t.after(() => a.close());
  const observed = deferred<WireEventContext>();
  registerWire(b, ['event'], { event: (_value, context) => observed.resolve(context) });
  emitWire(a, ['event']);
  assert.equal(((await observed.promise) as unknown as Record<symbol, unknown>)[marker], true);
});

test('a custom event propagator cannot rewrite the received frame when it is forwarded', async (t) => {
  const changed = { traceparent: '00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01' };
  const peers = await physical({
    propagator: {
      extract: (context) => {
        context.trace = changed;
      },
      inject: defaultPropagator.inject,
    },
  });
  t.after(peers.close);
  const [access, binding] = wirePair();
  t.after(() => access.close());
  const sent = deferred<Message>(),
    received = deferred<Message>();
  const source: Wire = {
    ...peers.client.wire(),
    send: (path, message) => {
      sent.resolve(message);
      peers.client.wire().send(path, message);
    },
  };
  const destination: Wire = {
    ...access,
    send: (path, message) => {
      received.resolve(message);
      access.send(path, message);
    },
  };
  t.after(forwardWire(peers.server.wire(), destination));
  const delivered = deferred<WireEventContext>();
  registerWire(binding, ['event'], { event: (_value, context) => delivered.resolve(context) });
  emitWire(source, ['event']);
  assert.equal((await received.promise).frame.traceparent, (await sent.promise).frame.traceparent);
  assert.equal((await delivered.promise).trace, changed);
});
