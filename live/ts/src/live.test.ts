import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, type ObserverEvent } from '@nightseam/runtime';
import { cases, echo, handed, SINK, type Pair, type T } from './conformance.ts';
import {
  liveOver,
  scopeOf,
  CONTRACT_INVALID,
  REFERENCE_FOREIGN,
  REFERENCE_UNKNOWN,
  TOO_MANY_EXPORTS,
  TOO_MANY_IMPORTS,
  Reference,
  type LiveOptions,
  type LiveScope,
} from './index.ts';

/** Two scopes over a pipe: the pair every shared case runs on. */
async function over(options: LiveOptions = {}, observer?: { observe(event: ObserverEvent): void }): Promise<Pair> {
  const [a, b] = pipe();
  const pa = new DuplexPeer(observer ? { role: 'client', observer } : { role: 'client' });
  const pb = new DuplexPeer({ role: 'server' });
  // Handlers first, frames second: the scope is made before the peer is given
  // a connection, which is the order tunnel.New needs for the same reason.
  const sa = liveOver(pa, options);
  const sb = liveOver(pb, options);
  await Promise.all([pa.attach(a), pb.attach(b)]);
  return {
    a: sa,
    b: sb,
    close: () => {
      pa.close();
      pb.close();
    },
  };
}

/** node:test's `t`, as the shared suite reports through. */
function reporter(): T {
  return { fail: (message: string) => assert.fail(message) };
}

// The shared suite, which is what holds this runtime and its Go twin to one
// behavior. The names are the Go suite's names, in the Go suite's order.
for (const one of cases()) {
  test(one.name, async () => {
    const p = await over();
    try {
      await one.run(reporter(), p);
    } finally {
      p.close();
    }
  });
}

async function serializedReferenceNewConnection(): Promise<void> {
  const first = await over();
  const exported = first.a.export(SINK, echo);
  const carried = JSON.parse(JSON.stringify(exported));
  first.close();

  const second = await over();
  try {
    const { imported: fresh } = handed(second.a, second.b, SINK, echo);
    // Decode accepts these bytes and import attaches; only invocation proves
    // that the new exporting scope has no binding with the old nonce.
    const arrived = second.b.decode(carried);
    const invoke = second.b.import(arrived, SINK);
    assert.deepEqual(second.a.counts(), { exports: 1, imports: 0 });
    assert.deepEqual(second.b.counts(), { exports: 0, imports: 2 });
    await assert.rejects(
      () => invoke(1),
      (error: DuplexError) => error.code === REFERENCE_UNKNOWN,
    );
    assert.equal(await fresh('fresh'), 'fresh');
    second.a.peer.handle('ordinary', async (request) => request);
    assert.equal(await second.b.peer.call('ordinary', 'alive'), 'alive');
    assert.deepEqual(second.a.counts(), { exports: 1, imports: 0 });
    assert.deepEqual(second.b.counts(), { exports: 0, imports: 2 });
  } finally {
    second.close();
  }
}

test('serializedReferenceNewConnection', serializedReferenceNewConnection);

test('pre-cancelled invocations retain no scope bookkeeping', async () => {
  const p = await over();
  try {
    const { imported } = handed(p.a, p.b, SINK, echo);
    const cancelled = new AbortController();
    cancelled.abort();
    await assert.rejects(() => imported(null, { signal: cancelled.signal }), { code: 'cancelled' });
    assert.equal(p.b['inflight'].size, 0);
    assert.equal(p.a['inflight'].size, 0);
    assert.equal(await imported('again'), 'again');
    assert.equal(p.b['inflight'].size, 0);
  } finally {
    p.close();
  }
});

test('the scope over a peer is found on it', async () => {
  const p = await over();
  try {
    assert.equal(scopeOf(p.a.peer), p.a);
    assert.equal(scopeOf(new DuplexPeer()), undefined);
  } finally {
    p.close();
  }
});

test('the bounds refuse and leave nothing', async () => {
  const p = await over({ maxExports: 1, maxImports: 1 });
  try {
    const first = p.a.export(SINK, echo);
    assert.throws(
      () => p.a.export(SINK, echo),
      (error: DuplexError) => error.code === TOO_MANY_EXPORTS,
    );
    assert.equal(p.a.counts().exports, 1, 'a refused export left a binding behind');

    p.b.import(p.b.decode(JSON.parse(JSON.stringify(first))), SINK);
    const second = p.b.decode({ binding: 'deadbeefdeadbeef.9', contract: SINK });
    assert.throws(
      () => p.b.import(second, SINK),
      (error: DuplexError) => error.code === TOO_MANY_IMPORTS,
    );
    assert.equal(p.b.counts().imports, 1, 'a refused import left an attachment behind');
  } finally {
    p.close();
  }
});

test('a negative bound is refused', async () => {
  const peer = new DuplexPeer();
  assert.throws(
    () => liveOver(peer, { maxExports: -1 }),
    (error: DuplexError) => error.code === CONTRACT_INVALID,
  );
  assert.throws(
    () => liveOver(peer, { maxImports: 0 }),
    (error: DuplexError) => error.code === CONTRACT_INVALID,
  );
});

// Valid native references come from export or decode. Their associated scope
// is distinct from serialized bytes, which decode accepts from the caller.
test('a reference is not a constructible value', async () => {
  const p = await over();
  try {
    assert.throws(
      () => new Reference('a.1', SINK, p.b, Symbol('not the one') as never),
      (error: DuplexError) => error.code === CONTRACT_INVALID,
    );
    assert.throws(
      () => p.b.decode({ binding: '', contract: SINK }),
      (e: DuplexError) => e.code === CONTRACT_INVALID,
    );
    assert.throws(
      () => p.b.decode({ binding: 'a.1' }),
      (e: DuplexError) => e.code === CONTRACT_INVALID,
    );
    assert.throws(
      () => p.b.decode(7),
      (e: DuplexError) => e.code === CONTRACT_INVALID,
    );
    assert.throws(
      () => p.b.import({ binding: 'a.1', contract: SINK } as unknown as Reference, SINK),
      (error: DuplexError) => error.code === REFERENCE_FOREIGN,
    );
  } finally {
    p.close();
  }
});

test('the observer is told and sees no payload', async () => {
  const seen: ObserverEvent[] = [];
  const p = await over({}, { observe: (event) => void seen.push(event) });
  try {
    const exported = p.a.export(SINK, echo);
    assert.throws(() => p.a.export('', echo));
    handed(p.a, p.b, SINK, echo);
    p.a.release(exported);

    const kinds: string[] = seen.map((event) => event.type);
    for (const want of ['live.exported', 'live.refused', 'live.released']) {
      assert.ok(kinds.includes(want), `the observer was never told ${want}; it saw ${kinds.join(' ')}`);
    }
    for (const event of seen) {
      assert.ok(!JSON.stringify(event).includes('request'), 'an observer event carried a payload');
    }
  } finally {
    p.close();
  }
});

test('a reference marshals as a binding and a contract, and nothing else', async () => {
  const p = await over();
  try {
    const exported = p.a.export(SINK, echo);
    const wire = JSON.parse(JSON.stringify(exported)) as Record<string, unknown>;
    assert.deepEqual(Object.keys(wire).sort(), ['binding', 'contract']);
    assert.equal(wire.contract, SINK);
    assert.equal(typeof wire.binding, 'string');
    assert.equal(exported.contract, SINK);
  } finally {
    p.close();
  }
});

/** The scope is one connection's, and the peer ending is the scope ending. */
test('a scope ends with the connection carrying it', async () => {
  const p = await over();
  const scope: LiveScope = p.b;
  p.close();
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.deepEqual(scope.counts(), { exports: 0, imports: 0 });
});
