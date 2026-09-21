import assert from 'node:assert/strict';
import test from 'node:test';
import {
  at,
  mount,
  pipe,
  type Wire,
  type Message,
  type Frame,
  type FrameConnection,
  type ReturnAddress,
  type ConnectionHandlers,
} from '@nightseam/duplex';
import { DuplexPeer, DuplexError, UnpublishedError } from './peer.ts';
import type { PeerOptions } from './peer.ts';
import { callWire, handleWire, emitWire, onWireEvent, forwardWire } from './wire.ts';
import { wirePair } from './wire-pair.ts';
import { defaultPropagator } from './trace.ts';
import type { ObserverEvent } from './observer.ts';

function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
async function paired(options: PeerOptions = {}) {
  const [left, right] = pipe();
  let writes = 0;
  const frames: { kind: string }[] = [];
  const client = new DuplexPeer(options),
    server = new DuplexPeer({ ...options, role: 'server' });
  await Promise.all([
    client.attach({
      get state() {
        return left.state;
      },
      get buffered() {
        return left.buffered;
      },
      send: (frame: Frame) => {
        writes++;
        if (frame.kind === 'text') frames.push(JSON.parse(frame.data));
        left.send(frame);
      },
      close: (code, reason) => left.close(code, reason),
      listen: (receiver) => left.listen(receiver),
    }),
    server.attach(right),
  ]);
  return {
    client,
    server,
    writes: () => writes,
    frames,
    close: () => {
      client.close();
      server.close();
    },
  };
}

test('mounted wire keeps independent c:1 origins and cancellation', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  const started = { first: deferred(), second: deferred() },
    cancelled = deferred<string>(),
    finish = deferred();
  handleWire(pair.server.wire(), ['worker', 'run'], async (params, context) => {
    const name = params as 'first' | 'second';
    started[name].resolve();
    const stopped = deferred();
    context.signal.addEventListener(
      'abort',
      () => {
        cancelled.resolve(name);
        stopped.resolve();
      },
      { once: true },
    );
    await Promise.race([finish.promise, stopped.promise]);
    return name;
  });
  const selected = at(mount(new Map([['service', pair.client.wire()]])), ['service', 'worker']);
  const sent: Message[] = [];
  const opaque: Wire = {
    send: (path, message) => {
      sent.push(message);
      selected.send(path, message);
    },
    receive: (path, receiver) => selected.receive(path, receiver),
    close: (code, reason) => selected.close(code, reason),
  };
  assert.equal(pair.client.wire(), pair.client.wire());
  const controller = new AbortController();
  const first = callWire(opaque, ['run'], 'first', { signal: controller.signal });
  const firstResult = first.catch((error: unknown) => error);
  const second = callWire(opaque, ['run'], 'second');
  await Promise.all([started.first.promise, started.second.promise]);
  assert.equal(sent[0]!.frame.kind, 'request');
  assert.equal(sent[1]!.frame.kind, 'request');
  assert.equal('id' in sent[0]!.frame && sent[0]!.frame.id, 'c:1');
  assert.equal('id' in sent[1]!.frame && sent[1]!.frame.id, 'c:1');
  assert.notEqual(sent[0]!.return, sent[1]!.return);
  controller.abort();
  assert.equal(((await firstResult) as DuplexError).code, 'cancelled');
  assert.equal(await cancelled.promise, 'first');
  handleWire(pair.server.wire(), ['worker', 'echo'], (value) => value);
  assert.equal(await callWire(opaque, ['echo'], 'still open'), 'still open');
  finish.resolve();
  assert.equal(await second, 'second');
});

test('wire paths preserve opaque scalar segments over the existing envelope', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  for (const [path, value] of [
    [['a.b'], 'one'],
    [['a', 'b'], 'two'],
    [[''], 'empty'],
    [['😀', '\ufeff'], 'unicode'],
  ] as const) {
    handleWire(pair.server.wire(), path, () => value);
  }
  for (const [path, value] of [
    [['a.b'], 'one'],
    [['a', 'b'], 'two'],
    [[''], 'empty'],
    [['😀', '\ufeff'], 'unicode'],
  ] as const) {
    assert.equal(await callWire(pair.client.wire(), path), value);
  }
});

test('call and event helpers accept an empty suffix at a selected leaf', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  handleWire(pair.server.wire(), ['a.b'], (value) => value);
  assert.equal(await callWire(at(pair.client.wire(), ['a.b']), [], 'selected'), 'selected');
  const arrived = deferred<unknown>();
  onWireEvent(pair.server.wire(), [''], (value) => {
    arrived.resolve(value);
  });
  emitWire(at(pair.client.wire(), ['']), [], 'event');
  assert.equal(await arrived.promise, 'event');
});

test('wire root queues asynchronously and preserves request/event send order', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  const seen: string[] = [],
    event = deferred();
  handleWire(pair.server.wire(), ['first'], () => {
    seen.push('request');
    return null;
  });
  onWireEvent(pair.server.wire(), ['second'], () => {
    seen.push('event');
    event.resolve();
  });
  const first = callWire(pair.client.wire(), ['first']);
  emitWire(pair.client.wire(), ['second']);
  assert.equal(pair.writes(), 0, 'wire send entered the physical writer inline');
  assert.deepEqual(seen, []);
  await Promise.all([first, event.promise]);
  assert.deepEqual(
    pair.frames.map((frame) => frame.kind),
    ['request', 'event'],
  );
  assert.deepEqual([...seen].sort(), ['event', 'request']);
});

test('wire carries same-call metadata but outgoing application calls copy it only explicitly', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  handleWire(pair.client.wire(), ['echoMeta'], (_value, context) => context.meta ?? null);
  handleWire(pair.server.wire(), ['relay'], async (_value, context) => ({
    received: context.meta,
    implicit: await callWire(context.wire, ['echoMeta'], null, { context }),
    explicit: await callWire(context.wire, ['echoMeta'], null, { context, meta: context.meta }),
  }));
  assert.deepEqual(await callWire(pair.client.wire(), ['relay'], null, { meta: { tenant: 'one' } }), {
    received: { tenant: 'one' },
    implicit: null,
    explicit: { tenant: 'one' },
  });
});

test('wire public refusals survive while dispatched nested publication proof does not', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  handleWire(pair.server.wire(), ['refuse'], () => {
    throw new UnpublishedError(new DuplexError('denied', 'No', { why: 7 }));
  });
  await assert.rejects(callWire(pair.client.wire(), ['refuse']), (error: unknown) => {
    assert.ok(error instanceof DuplexError);
    assert.equal(error instanceof UnpublishedError, false);
    assert.equal(error.code, 'denied');
    assert.deepEqual(error.data, { why: 7 });
    return true;
  });
  await assert.rejects(callWire(pair.client.wire(), ['missing']), { code: 'method_not_found' });
  pair.client.close();
  await assert.rejects(callWire(pair.client.wire(), ['refuse']), (error: unknown) => error instanceof UnpublishedError);
});

test('wire full admission ends the root immediately and settles queued calls', async (t) => {
  const pair = await paired({ queueCapacity: 1 });
  t.after(pair.close);
  const first = callWire(pair.client.wire(), ['first']).catch((error: unknown) => error);
  const second = callWire(pair.client.wire(), ['second']).catch((error: unknown) => error);
  assert.equal(pair.client.status, 'disconnected');
  assert.ok((await first) instanceof DuplexError);
  assert.equal((await first) instanceof UnpublishedError, false);
  assert.ok((await second) instanceof UnpublishedError);
});

test('wire close settles active calls and detaches registrations once', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  const started = deferred(),
    cancelled = deferred();
  handleWire(pair.server.wire(), ['held'], async (_value, context) => {
    context.signal.addEventListener('abort', () => cancelled.resolve(), { once: true });
    started.resolve();
    await cancelled.promise;
    return null;
  });
  const response = callWire(pair.client.wire(), ['held']).catch((error: unknown) => error);
  await started.promise;
  pair.client.wire().close(1000, 'done');
  assert.equal(((await response) as DuplexError).code, 'disconnected');
  await cancelled.promise;
  assert.throws(() => pair.client.wire().receive(['later'], {}));
});

test('mounted async event receivers keep serial order and peer backpressure accounting', async (t) => {
  const pair = await paired({ queueCapacity: 2, writeTimeoutMs: 20 });
  t.after(pair.close);
  const entered = deferred(),
    unblock = deferred(),
    ended = deferred<DuplexError>();
  t.after(() => unblock.resolve());
  pair.server.onClose((error) => ended.resolve(error));
  const view = at(mount(new Map([['outer', pair.server.wire()]])), ['outer']);
  const seen: number[] = [];
  onWireEvent(view, ['notice'], async (data) => {
    seen.push(data as number);
    entered.resolve();
    await unblock.promise;
  });
  emitWire(pair.client.wire(), ['notice'], 1);
  await entered.promise;
  emitWire(pair.client.wire(), ['notice'], 2);
  // Give the root admission queue its scheduled drain before the next frame.
  await Promise.resolve();
  emitWire(pair.client.wire(), ['notice'], 3);
  await ended.promise;
  assert.deepEqual(seen, [1]);
});

test('a full peer queue preserves unpublished proof only for the refused attempt', async (t) => {
  const [a, b] = pipe();
  let writes = 0,
    buffered = 0;
  const held: FrameConnection = {
    get state() {
      return a.state;
    },
    get buffered() {
      return buffered;
    },
    send: (frame) => {
      writes++;
      a.send(frame);
      buffered = 1;
    },
    close: (code, reason) => a.close(code, reason),
    listen: (receiver) => a.listen(receiver),
  };
  const peer = new DuplexPeer({ queueCapacity: 1, writeTimeoutMs: 5_000 });
  await peer.attach(held);
  t.after(() => {
    peer.close();
    b.close();
  });
  const accepted = peer.call('accepted').catch((error: unknown) => error);
  const rejected = await peer.call('rejected').catch((error: unknown) => error);
  const earlier = await accepted;
  assert.equal(writes, 1);
  assert.ok(!(earlier instanceof UnpublishedError), 'an accepted call inherited another attempt’s proof');
  assert.ok(rejected instanceof UnpublishedError, 'a rejected call lost its own pre-queue proof');
});

test('wire handlers inherit verified receive context and keep peer panic observations', async (t) => {
  const observed: ObserverEvent[] = [];
  const pair = await paired({
    observer: {
      observe: (event) => {
        observed.push(event);
      },
    },
    propagator: {
      extract: (context, trace) => {
        defaultPropagator.extract(context, trace);
        Object.defineProperty(context, 'verified', { value: 'authenticated', enumerable: false });
      },
      inject: (context) => defaultPropagator.inject(context),
    },
  });
  t.after(pair.close);
  let inherited: unknown;
  handleWire(pair.server.wire(), ['panic'], (_value, context) => {
    inherited = Reflect.get(context, 'verified');
    throw new Error('deliberate handler panic');
  });
  await assert.rejects(callWire(pair.client.wire(), ['panic']), { code: 'internal' });
  assert.equal(inherited, 'authenticated');
  assert.equal(observed.filter((event) => event.type === 'handler.panic').length, 1);
});

test('wire cancellation reserves bounded admission behind a full data queue', async (t) => {
  const pair = await paired({ queueCapacity: 1 });
  t.after(pair.close);
  const started = deferred(),
    cancelled = deferred();
  handleWire(pair.server.wire(), ['held'], async (_value, context) => {
    context.signal.addEventListener('abort', () => cancelled.resolve(), { once: true });
    started.resolve();
    await cancelled.promise;
    return null;
  });
  const controller = new AbortController();
  let request: Message | undefined;
  const root = pair.client.wire();
  const selected: Wire = {
    send: (path, message) => {
      if (message.frame.kind === 'request') request = message;
      root.send(path, message);
    },
    receive: (path, receiver) => root.receive(path, receiver),
    close: (code, reason) => root.close(code, reason),
  };
  const result = callWire(selected, ['held'], {}, { signal: controller.signal }).catch((error: unknown) => error);
  await started.promise;
  emitWire(root, ['fills-root'], null);
  controller.abort();
  assert.equal(pair.client.status, 'connected', 'cancel closed a full data queue');
  const cancel: Message = { frame: { version: 1, kind: 'cancel', id: 'c:1' }, return: request!.return };
  for (let i = 0; i < 20; i++) root.send(['held'], cancel);
  root.send(['unknown'], { ...cancel, return: { wire: root } });
  assert.equal(((await result) as DuplexError).code, 'cancelled');
  await cancelled.promise;
  assert.equal(pair.client.status, 'connected');
  assert.deepEqual(
    pair.frames.map((frame) => frame.kind),
    ['request', 'event', 'cancel'],
  );
});

test('completed calls retain their budget until a reserved cancellation is drained', async (t) => {
  const listeners = new Set<ConnectionHandlers>(),
    sent: { id: string; kind: string }[] = [];
  const connection: FrameConnection = {
    state: 'open',
    buffered: 0,
    send: (frame) => {
      if (frame.kind === 'text') sent.push(JSON.parse(frame.data));
    },
    close: () => {},
    listen: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
  const peer = new DuplexPeer({ queueCapacity: 1, maxPendingRequests: 1 });
  await peer.attach(connection);
  t.after(() => peer.close());
  const root = peer.wire(),
    second = deferred<Message>();
  const returning = (onResponse: (message: Message) => void): ReturnAddress => ({
    wire: {
      send: (_path, message) => onResponse(message),
      receive: () => () => {},
      close: () => {},
    },
  });
  const secondAddress = returning((message) => second.resolve(message));
  const firstAddress = returning(() => {
    // The response resolves before the root's queued cancellation runs. That
    // stale control still occupies its original request's bounded reservation.
    root.send(['second'], { frame: { version: 1, kind: 'request', id: 'c:1', params: null }, return: secondAddress });
  });
  root.send(['first'], { frame: { version: 1, kind: 'request', id: 'c:1', params: null }, return: firstAddress });
  await Promise.resolve();
  assert.equal(sent.length, 1);
  for (const listener of listeners)
    listener.frame?.({
      kind: 'text',
      data: JSON.stringify({ version: 1, kind: 'response', id: sent[0]!.id, result: null }),
    });
  root.send(['first'], { frame: { version: 1, kind: 'cancel', id: 'c:1' }, return: firstAddress });
  const refused = await second.promise;
  assert.equal(refused.frame.kind, 'response');
  assert.equal('error' in refused.frame && refused.frame.error?.code, 'busy');
  assert.equal(sent.length, 1, 'a stale cancellation or a call over budget reached the carrier');
  root.send(['third'], { frame: { version: 1, kind: 'request', id: 'c:1', params: null }, return: secondAddress });
  await Promise.resolve();
  assert.equal(sent.length, 2, 'drained control did not release its request reservation');
});

test('root wire validates structured profile frames before admission without ending its carrier', async (t) => {
  const pair = await paired({ queueCapacity: 1 });
  t.after(pair.close);
  let invoked = 0;
  handleWire(pair.server.wire(), ['checked'], (value) => {
    invoked++;
    return value;
  });
  const root = pair.client.wire();
  const request = { version: 1, kind: 'request', id: 'c:1', params: null };
  const invalid: [string, unknown][] = [
    ['version', { ...request, version: 2 }],
    ['kind', { ...request, kind: 'unknown' }],
    ['extra member', { ...request, extra: true }],
    ['duplicate method source', { ...request, method: 'other' }],
    ['missing params', { version: 1, kind: 'request', id: 'c:1' }],
    ['id prefix', { ...request, id: 'x:1' }],
    ['id zero', { ...request, id: 'c:0' }],
    ['trace', { ...request, traceparent: 'invalid' }],
    ['meta', { ...request, meta: { invalid: 1 } }],
    ['reserved meta', { ...request, meta: { 'nightseam.reserved': 'no' } }],
    ['missing event data', { version: 1, kind: 'event' }],
    ['duplicate event source', { version: 1, kind: 'event', event: 'other', data: null }],
    ['cancel extra member', { version: 1, kind: 'cancel', id: 'c:1', params: null }],
    ['null frame', null],
  ];
  for (const [name, frame] of invalid) {
    assert.throws(
      () => root.send(['checked'], { frame, return: { wire: root } } as Message),
      (error: unknown) => error instanceof DuplexError && error.code === 'invalid_message',
      name,
    );
    assert.equal(pair.client.status, 'connected', name);
  }
  assert.equal(pair.writes(), 0);
  assert.equal(invoked, 0);
  // Local origins may use either logical prefix, independently of the carrier role.
  for (const id of ['c:1', 's:1']) {
    const reply = deferred<Message>();
    const returning: Wire = {
      send: (_path, message) => reply.resolve(message),
      receive: () => () => {},
      close: () => {},
    };
    root.send(['checked'], { frame: { version: 1, kind: 'request', id, params: id }, return: { wire: returning } });
    const response = await reply.promise;
    assert.equal(response.frame.kind, 'response');
    assert.equal('result' in response.frame && response.frame.result, id);
  }
  assert.equal(invoked, 2);
});

test('root wire refuses oversized structured frames before accepting them', async (t) => {
  const pair = await paired({ maxFrameBytes: 512, queueCapacity: 1 });
  t.after(pair.close);
  const root = pair.client.wire();
  assert.throws(() => root.send(['large'], { frame: { version: 1, kind: 'event', data: '😀'.repeat(200) } }), {
    code: 'frame_too_large',
  });
  assert.equal(pair.client.status, 'connected');
  assert.equal(pair.writes(), 0);
  handleWire(pair.server.wire(), ['small'], () => 'ok');
  assert.equal(await callWire(root, ['small']), 'ok');
});

test('local wire return addresses validate responses before settling the call', async () => {
  let address!: ReturnAddress;
  const opaque: Wire = {
    send: (_path, message) => {
      address = message.return!;
    },
    receive: () => () => {},
    close: () => {},
  };
  const pending = callWire(opaque, ['test']);
  const response = { version: 1, kind: 'response', id: 'c:1', result: null };
  const invalid: unknown[] = [
    { ...response, version: 2 },
    { ...response, extra: true },
    { version: 1, kind: 'response', id: 'c:1' },
    { ...response, error: { code: 'bad', message: 'Bad' } },
    { ...response, traceparent: 'invalid' },
    { version: 1, kind: 'response', id: 'c:1', error: { code: '', message: 'Bad' } },
  ];
  try {
    for (const frame of invalid) {
      assert.throws(() => address.wire.send([], { frame } as Message), { code: 'invalid_message' });
    }
    address.wire.send([], { frame: { version: 1, kind: 'response', id: 'c:1', result: 'ok' } });
    assert.equal(await pending, 'ok');
  } finally {
    address.wire.close();
    await pending.catch(() => {});
  }
});

test('wire handlers sanitize empty public error fields without disconnecting', async (t) => {
  const pair = await paired();
  t.after(pair.close);
  for (const [name, code, message] of [
    ['code', '', 'No'],
    ['message', 'denied', ''],
  ] as const) {
    handleWire(pair.server.wire(), [name], () => {
      throw new DuplexError(code, message);
    });
    await assert.rejects(callWire(pair.client.wire(), [name]), { code: 'internal' });
    assert.equal(pair.client.status, 'connected');
  }
});

test('wire handler responses over the peer frame limit settle as internal errors', async (t) => {
  const pair = await paired({ maxFrameBytes: 512 });
  t.after(pair.close);
  handleWire(pair.server.wire(), ['large'], () => 'x'.repeat(1_024));
  await assert.rejects(callWire(pair.client.wire(), ['large'], {}, { timeoutMs: 100 }), { code: 'internal' });
  assert.equal(pair.client.status, 'connected');
});

test('configured outgoing traces retain private identity through a local Wire and physical peer', async (t) => {
  const trace = { traceparent: '00-11111111111111111111111111111111-2222222222222222-01', tracestate: 'vendor=kept' };
  const seen: ObserverEvent[] = [];
  const pair = await paired({ observer: { observe: (event) => seen.push(event) } });
  const [access, binding] = wirePair();
  t.after(() => {
    access.close();
    pair.close();
  });
  forwardWire(binding, pair.client.wire());
  handleWire(pair.server.wire(), ['call'], () => null);
  const delivered = deferred();
  onWireEvent(pair.server.wire(), ['event'], () => delivered.resolve());
  const propagator = { inject: () => trace, extract: () => {} };
  await callWire(access, ['call'], {}, { propagator });
  emitWire(access, ['event'], null, { propagator });
  await delivered.promise;
  const outgoing = seen.filter(
    (event) => (event.type === 'request.started' && !event.incoming) || event.type === 'event.emitted',
  );
  assert.equal(outgoing.length, 2);
  for (const event of outgoing) assert.equal('trace' in event && event.trace, trace);
  // A frame reconstructed at the receiving carrier retains public values only.
  const incoming = seen.find((event) => event.type === 'request.started' && event.incoming);
  assert.ok(incoming && 'trace' in incoming);
  assert.notEqual(incoming.trace, trace);
  assert.deepEqual(incoming.trace, trace);
});
