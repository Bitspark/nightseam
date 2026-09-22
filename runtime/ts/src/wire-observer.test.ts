import assert from 'node:assert/strict';
import test from 'node:test';
import { encodePath } from '@nightseam/duplex';
import type { Endpoint, Wire } from '@bitspark/bitwire';
import { createDispatcher } from './dispatcher.ts';
import { DuplexError } from './error.ts';
import type { Observer, ObserverEvent } from './observer.ts';
import { callWire, emitWire, registerWire } from './wire.ts';
import { wirePair } from './wire-pair.ts';

const opaque = (wire: Endpoint): Endpoint => ({ send: wire.send, receive: wire.receive, close: wire.close });
const deferred = () => {
  let resolve!: () => void;
  return { promise: new Promise<void>((done) => (resolve = done)), resolve: () => resolve() };
};

test('wire helper observations label opaque model operations without adding transport observations', async () => {
  const carrier: ObserverEvent[] = [],
    events: ObserverEvent[] = [];
  const observer: Observer = { observe: (event) => events.push(event) };
  const [left, right] = wirePair({ observer: { observe: (event) => carrier.push(event) } });
  const dispatcher = createDispatcher(opaque(right));
  try {
    const delivered = deferred(),
      ended = deferred();
    const named: Observer = {
      observe: (event) => {
        observer.observe(event);
        if (event.type === 'request.ended' && event.incoming) ended.resolve();
      },
    };
    registerWire(dispatcher, ['member.with.dot'], {
      observer: named,
      family: 'probe',
      request: (params) => params,
      event: (data) => {
        assert.equal(data, 'secret😀');
        delivered.resolve();
      },
    });
    assert.equal(
      await callWire(opaque(left), ['member.with.dot'], 'secret😀', { observer, family: 'probe' }),
      'secret😀',
    );
    await ended.promise;
    emitWire(opaque(left), ['member.with.dot'], 'secret😀', { observer, family: 'probe' });
    await delivered.promise;
    const starts = events.filter((event) => event.type === 'request.started');
    const ends = events.filter((event) => event.type === 'request.ended');
    assert.equal(starts.length, 2);
    assert.deepEqual(starts.map((event) => event.incoming).sort(), [false, true]);
    assert.equal(ends.length, 2);
    assert.ok(ends.every((event) => event.outcome === 'ok' && event.durationMs >= 0));
    assert.deepEqual(
      events.filter((event) => event.type.startsWith('event.')).map((event) => event.type),
      ['event.emitted', 'event.delivered'],
    );
    for (const event of events) {
      assert.equal('family' in event && event.family, 'probe');
      assert.equal(
        'method' in event ? event.method : 'name' in event ? event.name : undefined,
        encodePath(['member.with.dot']),
      );
      if (event.type === 'event.emitted' || event.type === 'event.delivered')
        assert.equal(event.bytes, Buffer.byteLength(JSON.stringify('secret😀')));
    }
    assert.ok(!JSON.stringify(events).includes('secret'));
    assert.deepEqual(carrier, []);
  } finally {
    dispatcher.close();
    left.close();
  }
});

test('wire helper completion distinguishes a local cancellation from a same-code refusal exactly once', async () => {
  const [left, right] = wirePair();
  const dispatcher = createDispatcher(right);
  const events: ObserverEvent[] = [],
    started = deferred(),
    ended = deferred();
  const observer: Observer = {
    observe: (event) => {
      events.push(event);
      if (event.type === 'request.ended' && event.incoming) ended.resolve();
    },
  };
  try {
    registerWire(dispatcher, ['wait'], {
      observer,
      family: 'probe',
      request: (_params, context) =>
        new Promise((resolve) => {
          context.signal.addEventListener('abort', () => resolve(null), { once: true });
          started.resolve();
        }),
    });
    const controller = new AbortController();
    let endedBeforeCancel = false;
    const selected: Wire = {
      ...opaque(left),
      send: (path, message) => {
        if (message.frame.kind === 'cancel')
          endedBeforeCancel = events.some((event) => event.type === 'request.ended' && !event.incoming);
        left.send(path, message);
      },
    };
    const pending = callWire(selected, ['wait'], null, { signal: controller.signal, observer, family: 'probe' });
    await started.promise;
    controller.abort();
    await assert.rejects(pending, { code: 'cancelled' });
    await ended.promise;
    assert.equal(endedBeforeCancel, true);
    const localEnds = events.filter((event) => event.type === 'request.ended');
    assert.equal(localEnds.length, 2);
    assert.ok(localEnds.every((event) => event.outcome === 'cancelled' && event.errorCode === 'cancelled'));
    registerWire(dispatcher, ['refuse'], {
      request: () => {
        throw new DuplexError('cancelled', 'Application refusal.');
      },
    });
    await assert.rejects(callWire(left, ['refuse'], null, { observer, family: 'probe' }), { code: 'cancelled' });
    const last = events.filter((event) => event.type === 'request.ended').at(-1)!;
    assert.equal(last.outcome, 'error');
    assert.equal(last.errorCode, 'cancelled');
  } finally {
    dispatcher.close();
    left.close();
  }
});

test('wire helper observers cannot break traffic or duplicate the root panic report', async () => {
  const transport: ObserverEvent[] = [],
    model: ObserverEvent[] = [];
  const [left, right] = wirePair({ observer: { observe: (event) => transport.push(event) } });
  const dispatcher = createDispatcher(right);
  const observer: Observer = {
    observe: (event) => {
      model.push(event);
      throw new Error('broken diagnostic');
    },
  };
  try {
    registerWire(dispatcher, ['panic'], {
      observer,
      family: 'probe',
      request: () => {
        throw new Error('application panic');
      },
    });
    await assert.rejects(callWire(left, ['panic'], null, { observer, family: 'probe' }), { code: 'internal' });
    assert.equal(transport.filter((event) => event.type === 'handler.panic').length, 1);
    assert.equal(model.filter((event) => event.type === 'handler.panic').length, 0);
    assert.equal(model.filter((event) => event.type === 'request.started').length, 2);
    assert.equal(model.filter((event) => event.type === 'request.ended').length, 2);
  } finally {
    dispatcher.close();
    left.close();
  }
});

test('wire handler refusal remains an error when cancellation raced its return', async () => {
  const [left, right] = wirePair();
  const dispatcher = createDispatcher(right);
  const events: ObserverEvent[] = [],
    started = deferred(),
    ended = deferred();
  const observer: Observer = {
    observe: (event) => {
      events.push(event);
      if (event.type === 'request.ended' && event.incoming) ended.resolve();
    },
  };
  try {
    registerWire(dispatcher, ['refuse'], {
      observer,
      request: (_params, context) =>
        new Promise((_resolve, reject) => {
          context.signal.addEventListener('abort', () => reject(new DuplexError('cancelled', 'Application refusal.')), {
            once: true,
          });
          started.resolve();
        }),
    });
    const controller = new AbortController();
    const pending = callWire(left, ['refuse'], null, { signal: controller.signal, observer });
    await started.promise;
    controller.abort();
    await assert.rejects(pending, { code: 'cancelled' });
    await ended.promise;
    const incoming = events.find((event) => event.type === 'request.ended' && event.incoming);
    assert.ok(incoming?.type === 'request.ended');
    assert.equal(incoming.outcome, 'error');
    assert.equal(incoming.errorCode, 'cancelled');
  } finally {
    dispatcher.close();
    left.close();
  }
});

test('concurrent model lifetimes have distinct observer identities without changing frame request IDs', async () => {
  const [left, right] = wirePair();
  const dispatcher = createDispatcher(right);
  const first = deferred(),
    second = deferred(),
    arrived = deferred();
  const active = new Map<string, boolean>();
  const ids: string[] = [],
    frameIds: string[] = [];
  let collision = false,
    unmatched = false;
  const observer: Observer = {
    observe: (event) => {
      if (event.type === 'request.started') {
        collision ||= active.has(event.id);
        active.set(event.id, event.incoming);
        ids.push(event.id);
      } else if (event.type === 'request.ended') {
        unmatched ||= !active.delete(event.id);
      }
    },
  };
  try {
    registerWire(dispatcher, ['hold'], {
      observer,
      request: async (value, context) => {
        frameIds.push(context.requestId);
        if (frameIds.length === 2) arrived.resolve();
        await (value === 1 ? first.promise : second.promise);
        return value;
      },
    });
    const a = callWire(left, ['hold'], 1, { observer });
    const b = callWire(left, ['hold'], 2, { observer });
    await arrived.promise;
    const count = active.size;
    second.resolve();
    assert.equal(await b, 2);
    first.resolve();
    assert.equal(await a, 1);
    assert.equal(collision, false);
    assert.equal(unmatched, false);
    assert.equal(count, 4);
    assert.equal(active.size, 0);
    assert.equal(new Set(ids).size, 4);
    assert.ok(ids.every((id) => id.startsWith('wire:')));
    assert.deepEqual(frameIds, ['c:1', 'c:1']);
  } finally {
    first.resolve();
    second.resolve();
    dispatcher.close();
    left.close();
  }
});

test('incoming model observation reports the bounded refusal selected for an unencodable response', async (t) => {
  for (const kind of ['oversized result', 'oversized public refusal', 'unencodable result']) {
    await t.test(kind, async () => {
      const [left, right] = wirePair({ maxFrameBytes: 512 });
      const dispatcher = createDispatcher(right);
      const ended = deferred();
      let incoming: ObserverEvent | undefined;
      const observer: Observer = {
        observe: (event) => {
          if (event.type === 'request.ended' && event.incoming) {
            incoming = event;
            ended.resolve();
          }
        },
      };
      try {
        registerWire(dispatcher, ['response'], {
          observer,
          family: 'probe',
          request: () => {
            if (kind === 'oversized public refusal') throw new DuplexError('denied', 'Refused.', 'x'.repeat(2048));
            return kind === 'oversized result' ? 'x'.repeat(2048) : () => {};
          },
        });
        await assert.rejects(callWire(left, ['response']), { code: 'internal' });
        await ended.promise;
        assert.ok(incoming?.type === 'request.ended');
        assert.equal(incoming.outcome, 'error');
        assert.equal(incoming.errorCode, 'internal');
      } finally {
        dispatcher.close();
        left.close();
      }
    });
  }
});
