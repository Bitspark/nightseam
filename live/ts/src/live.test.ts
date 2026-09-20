import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, UnpublishedError, type ObserverEvent } from '@nightseam/runtime';
import { cases, echo, handed, SINK, type Pair, type T } from './conformance.ts';
import {
  liveOver,
  scopeOf,
  CONTRACT_INVALID,
  REFERENCE_FOREIGN,
  REFERENCE_RELEASED,
  RELEASE_EVENT,
  REFERENCE_UNKNOWN,
  TOO_MANY_EXPORTS,
  TOO_MANY_IMPORTS,
  Reference,
  type LiveOptions,
  type Invoke,
  type LiveScope,
  type LiveOwner,
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
  const exported = first.a.owner().export(SINK, echo);
  const carried = JSON.parse(JSON.stringify(exported));
  first.close();

  const second = await over();
  try {
    const { imported: fresh } = handed(second.a, second.b, SINK, echo);
    // Decode accepts these bytes and import attaches; only invocation proves
    // that the new exporting scope has no binding with the old nonce.
    const arrived = second.b.decode(carried);
    const invoke = second.b.owner().import(arrived, SINK);
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
    const first = p.a.owner().export(SINK, echo);
    assert.throws(
      () => p.a.owner().export(SINK, echo),
      (error: DuplexError) => error.code === TOO_MANY_EXPORTS,
    );
    assert.equal(p.a.counts().exports, 1, 'a refused export left a binding behind');

    p.b.owner().import(p.b.decode(JSON.parse(JSON.stringify(first))), SINK);
    const second = p.b.decode({ binding: 'deadbeefdeadbeef.9', contract: SINK });
    assert.throws(
      () => p.b.owner().import(second, SINK),
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
      () => p.b.owner().import({ binding: 'a.1', contract: SINK } as unknown as Reference, SINK),
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
    const exported = p.a.owner().export(SINK, echo);
    assert.throws(() => p.a.owner().export('', echo));
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
    const exported = p.a.owner().export(SINK, echo);
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

test('exportValue tracks only its own allocations and completed views start fresh', async () => {
  const p = await over();
  try {
    let retained: Reference | undefined;
    let captured: LiveOwner | undefined;
    assert.throws(
      () =>
        p.a.owner().exportValue((scope) => {
          captured = scope;
          scope.export(SINK, echo);
          retained = p.a.owner().export(SINK, echo);
          scope.exportValue((nested) => nested.export(SINK, echo).toJSON());
          throw new Error('later conversion failed');
        }),
      /later conversion failed/,
    );
    assert.equal(p.a.counts().exports, 1);
    assert.ok(captured);
    assert.ok(retained);
    assert.equal(await captured.import(retained, SINK)(7), 7);
    const raw = captured.exportValue((next) => next.export(SINK, echo).toJSON());
    assert.equal(p.a.counts().exports, 2);
    captured.scope.release(p.a.decode(raw));
    assert.equal(p.a.counts().exports, 1);
    captured.scope.close();
    assert.equal(scopeOf(p.a.peer), undefined, 'closing a view must unregister the scope');
  } finally {
    p.close();
  }
});

test('exportValue unwinds serialization failures and throws before publication', async () => {
  const p = await over({ maxExports: 1 });
  try {
    const cycle: Record<string, unknown> = {};
    cycle.self = cycle;
    for (const value of [cycle, 1n, 'throw']) {
      for (let i = 0; i < 3; i++) {
        assert.throws(() =>
          p.a.owner().exportValue((scope) => {
            scope.export(SINK, echo);
            if (value === 'throw') throw new Error('conversion failed');
            return value;
          }),
        );
        assert.equal(p.a.counts().exports, 0);
      }
    }
    const source = { value: 1 };
    const wire = p.a.owner().exportValue(() => source);
    source.value = 2;
    assert.deepEqual(wire, { value: 1 }, 'the completed payload must be a serialized snapshot');
  } finally {
    p.close();
  }
});

test('importValue bound failure preserves prior attachments', async () => {
  const p = await over({ maxImports: 2 });
  try {
    const refs = Array.from({ length: 3 }, () =>
      p.b.decode(JSON.parse(JSON.stringify(p.a.owner().export(SINK, echo)))),
    );
    const retained = p.b.owner().import(refs[0]!, SINK);
    const owner = p.b.owner().child();
    let fresh!: Invoke;
    assert.throws(
      () =>
        owner.importValue((batch) => {
          fresh = batch.import(refs[1]!, SINK);
          batch.import(refs[0]!, SINK);
          batch.import(refs[2]!, SINK);
        }),
      { code: TOO_MANY_IMPORTS },
    );
    assert.deepEqual(owner.counts(), { exports: 0, imports: 0 });
    assert.deepEqual(p.b.counts(), { exports: 0, imports: 1 });
    await assert.rejects(() => fresh(null), { code: REFERENCE_RELEASED });
    assert.equal(await retained(7), 7);
    p.a.owner().release();
    p.b.owner().release();
  } finally {
    p.close();
  }
});

test('owner release revokes all aliases before observer reentry', async () => {
  let owner!: LiveOwner;
  const aliases: Invoke[] = [];
  const pending: Promise<void>[] = [];
  const seen: ObserverEvent[] = [];
  let reentries = 0;
  const p = await over(
    {},
    {
      observe(event) {
        seen.push(event);
        if (event.type !== 'live.released') return;
        reentries++;
        owner.release();
        for (const alias of aliases) pending.push(assert.rejects(() => alias(null), { code: REFERENCE_RELEASED }));
      },
    },
  );
  try {
    owner = p.a.owner().child();
    for (let i = 0; i < 3; i++) aliases.push(p.a.owner().import(owner.child().export(SINK, echo), SINK));
    owner.release();
    assert.equal(reentries, 3);
    await Promise.all(pending);
    assert.equal(seen.filter((event) => event.type === 'event.emitted' && event.name === RELEASE_EVENT).length, 3);
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  } finally {
    p.close();
  }
});

test('unpublished export rollback emits no release', async () => {
  const seen: ObserverEvent[] = [];
  const p = await over({}, { observe: (event) => void seen.push(event) });
  try {
    const owner = p.a.owner().child();
    assert.throws(
      () =>
        owner.exportValue((batch) => {
          batch.export(SINK, echo);
          throw new Error('unpublished');
        }),
      /unpublished/,
    );
    owner.release();
    assert.equal(seen.filter((event) => event.type === 'event.emitted' && event.name === RELEASE_EVENT).length, 0);
    assert.deepEqual(owner.counts(), { exports: 0, imports: 0 });
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  } finally {
    p.close();
  }
});

test('publishValue only unwinds its fresh unsent exports', async () => {
  const seen: ObserverEvent[] = [];
  const p = await over({ maxExports: 2 }, { observe: (event) => void seen.push(event) });
  try {
    const owner = p.a.owner().child();
    const prior = owner.export(SINK, echo);
    const controller = new AbortController();
    controller.abort();
    for (let i = 0; i < 12; i++) {
      await assert.rejects(
        owner.publishValue(
          (batch) => batch.export(SINK, echo).toJSON(),
          (raw) => p.a.peer.call('unsent', raw, { signal: controller.signal }),
        ),
        UnpublishedError,
      );
      assert.deepEqual(owner.counts(), { exports: 1, imports: 0 });
    }
    assert.equal(seen.filter((event) => event.type === 'event.emitted' && event.name === RELEASE_EVENT).length, 0);
    assert.equal(await owner.import(prior, SINK)(47), 47);
    owner.release();
  } finally {
    p.close();
  }
});

test('publishValue retains unknown publisher failures and throws', async () => {
  for (const throws of [false, true]) {
    const p = await over();
    try {
      const owner = p.a.owner().child();
      const cause = new Error('publisher outcome unknown');
      await assert.rejects(
        owner.publishValue(
          (batch) => batch.export(SINK, echo).toJSON(),
          () => {
            if (throws) throw cause;
            return Promise.reject(cause);
          },
        ),
        (error) => error === cause,
      );
      assert.deepEqual(owner.counts(), { exports: 1, imports: 0 });
      owner.release();
      assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
    } finally {
      p.close();
    }
  }
});

test('refused invocations unwind unpublished arguments without dispatch', async () => {
  for (const mode of ['local-released', 'remote-released', 'local-cancelled']) {
    const seen: ObserverEvent[] = [];
    const p = await over({ maxExports: 3 }, { observe: (event) => void seen.push(event) });
    try {
      const outgoing = p.a.owner().child();
      const holder = p.a.owner().child();
      const target = mode === 'remote-released' ? p.b.owner().child() : holder;
      let dispatched = 0;
      let ref = target.export('test/Target', async () => {
        dispatched++;
        return null;
      });
      if (mode === 'remote-released') ref = p.a.decode(ref.toJSON());
      const invoke = holder.import(ref, 'test/Target');
      const controller = new AbortController();
      if (mode === 'local-cancelled') controller.abort();
      else holder.release();
      const prior = outgoing.export(SINK, echo);
      for (let i = 0; i < 12; i++) {
        await assert.rejects(
          outgoing.publishValue(
            (batch) => batch.export(SINK, echo).toJSON(),
            (raw) => invoke(raw, { signal: controller.signal }),
          ),
          UnpublishedError,
        );
        assert.deepEqual(outgoing.counts(), { exports: 1, imports: 0 });
      }
      assert.equal(dispatched, 0);
      assert.equal(seen.filter((event) => event.type === 'frame.sent' && event.kind === 'request').length, 0);
      assert.equal(await outgoing.import(prior, SINK)(49), 49);
      outgoing.release();
      holder.release();
      target.release();
    } finally {
      p.close();
    }
  }
});

test('dispatched release refusals retain arguments', async () => {
  for (const remote of [false, true]) {
    const p = await over();
    try {
      const outgoing = p.a.owner().child();
      const holder = p.a.owner().child();
      const target = remote ? p.b.owner().child() : holder;
      let alias!: Invoke;
      let ref = target.export('test/Target', async (raw) => {
        alias = target.import(target.scope.decode(raw), SINK);
        throw new DuplexError(REFERENCE_RELEASED, 'implementation refused after retention');
      });
      if (remote) ref = p.a.decode(ref.toJSON());
      const invoke = holder.import(ref, 'test/Target');
      await assert.rejects(
        outgoing.publishValue(
          (batch) => batch.export(SINK, echo).toJSON(),
          (raw) => invoke(raw),
        ),
        (error) =>
          error instanceof DuplexError && !(error instanceof UnpublishedError) && error.code === REFERENCE_RELEASED,
      );
      assert.deepEqual(outgoing.counts(), { exports: 1, imports: 0 });
      assert.equal(await alias(51), 51);
      outgoing.release();
      holder.release();
      target.release();
    } finally {
      p.close();
    }
  }
});

test('empty owners and released allocations retain no parent bookkeeping', async () => {
  const p = await over();
  try {
    const root = p.a.owner();
    const parent = root.child();
    const borrowed = root.export(SINK, echo);
    for (let i = 0; i < 40; i++) {
      const empty = parent.child();
      empty.import(borrowed, SINK);
      assert.equal(root['state'].children?.size ?? 0, 0);
      const owned = empty.export(SINK, echo);
      assert.equal(root['state'].children?.size, 1);
      p.a.release(owned);
      assert.equal(root['state'].children?.size ?? 0, 0);
    }
    root.release();
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  } finally {
    p.close();
  }
});

test('Symbol.dispose releases an owner and leaves the scope usable', async () => {
  const p = await over();
  try {
    const owner = p.a.owner().child();
    owner.export(SINK, echo);
    owner[Symbol.dispose]();
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
    p.a.owner().export(SINK, echo);
    assert.deepEqual(p.a.counts(), { exports: 1, imports: 0 });
    p.a.owner().release();
  } finally {
    p.close();
  }
});

test('import rollback does not repeat an already released allocation', async () => {
  const seen: ObserverEvent[] = [];
  const p = await over({ maxImports: 1, maxExports: 1 }, { observe: (event) => void seen.push(event) });
  try {
    const owner = p.a.owner().child();
    const sibling = p.a.owner().child();
    let first!: Reference;
    let replacement!: Invoke;
    assert.throws(
      () =>
        owner.importValue((batch) => {
          for (let i = 0; i < 8; i++) {
            const ref = p.a.decode({ binding: `remote.${i}`, contract: SINK });
            if (i === 0) first = ref;
            batch.import(ref, SINK);
            p.a.release(ref);
          }
          replacement = sibling.import(first, SINK);
          throw new Error('later failure');
        }),
      /later failure/,
    );
    owner.release();
    await Promise.resolve();
    assert.equal(seen.filter((event) => event.type === 'event.emitted' && event.name === RELEASE_EVENT).length, 8);
    assert.deepEqual(owner.counts(), { exports: 0, imports: 0 });
    assert.deepEqual(sibling.counts(), { exports: 0, imports: 1 });
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 1 });
    await assert.rejects(() => replacement(null), { code: REFERENCE_UNKNOWN });
    sibling.release();
    assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
  } finally {
    p.close();
  }
});
