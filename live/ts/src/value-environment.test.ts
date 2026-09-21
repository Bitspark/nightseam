import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, UnpublishedError, createValidator, jsonAdapter } from '@nightseam/runtime';
import { LiveOwner, liveOver, valueEnvironment, type Invoke, type Reference } from './index.ts';

async function pair() {
  const [a, b] = pipe();
  const pa = new DuplexPeer({ role: 'client' });
  const pb = new DuplexPeer({ role: 'server' });
  const sa = liveOver(pa);
  const sb = liveOver(pb);
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

function owner(context: unknown): LiveOwner {
  assert.ok(context instanceof LiveOwner);
  return context;
}

test('environment selection preserves active batches and nested child rollback', async (t) => {
  const p = await pair();
  t.after(p.close);
  const env = valueEnvironment(p.a);
  const selected = p.a.owner().child();
  let callbacks = 0;
  const prior = selected.export('test/Call', async (raw) => {
    callbacks++;
    return raw;
  });
  const failure = new Error('later field failed');
  for (const boundary of ['export', 'import'] as const) {
    const aliases: Invoke[] = [];
    const acquire = (context: unknown) => {
      const reference = owner(context).export('test/Call', async (raw) => raw);
      aliases.push(selected.import(reference, 'test/Call'));
      return reference.toJSON();
    };
    assert.throws(
      () =>
        env[boundary](selected, (outer) => {
          assert.equal(env.select(outer), outer, 'selection lost the active batch view');
          env.import(outer, acquire);
          const child = env.child(outer);
          env.export(child, acquire);
          assert.deepEqual(p.a.counts(), { exports: 3, imports: 0 });
          throw failure;
        }),
      (error) => error === failure,
    );
    assert.deepEqual(p.a.counts(), { exports: 1, imports: 0 });
    for (const alias of aliases)
      await assert.rejects(
        alias(null),
        (error: unknown) => error instanceof DuplexError && error.code === 'reference_released',
      );
  }
  assert.equal(await selected.import(prior, 'test/Call')(7), 7);
  assert.equal(callbacks, 1);
  assert.deepEqual(
    env.export(selected, () => ({ value: 8 })),
    { value: 8 },
  );
  assert.throws(() => env.import(selected, async () => null), /synchronously/);
});

test('failed environmental import releases fresh attachments and preserves borrowed aliases', async (t) => {
  const p = await pair();
  t.after(p.close);
  let callbacks = 0;
  const prior = p.a.owner().export('test/Call', async (raw) => {
    callbacks++;
    return raw;
  });
  const fresh = p.a.owner().export('test/Call', async (raw) => raw);
  const priorAtB = p.b.decode(prior.toJSON());
  const freshAtB = p.b.decode(fresh.toJSON());
  const first = p.b.owner().child();
  const alias = first.import(priorAtB, 'test/Call');
  assert.equal(await alias(1), 1);
  const second = p.b.owner().child();
  const env = valueEnvironment(p.b);
  assert.throws(
    () =>
      env.import(second, (view) => {
        assert.equal(owner(view).import(priorAtB, 'test/Call'), alias);
        owner(view).import(freshAtB, 'test/Call');
        throw new Error('later conversion failed');
      }),
    /later conversion failed/,
  );
  assert.deepEqual(second.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(p.b.counts(), { exports: 0, imports: 1 });
  assert.equal(await alias(2), 2);
  assert.equal(callbacks, 2);
});

test('environment publication distinguishes unsent proof from uncertain cancellation', async (t) => {
  const p = await pair();
  t.after(p.close);
  const env = valueEnvironment(p.a);
  for (const unpublished of [true, false]) {
    const selected = p.a.owner().child();
    let reference!: Reference;
    let callbacks = 0;
    const cause = new DuplexError('cancelled', 'operation cancelled');
    const failure = unpublished ? new UnpublishedError(cause) : cause;
    await assert.rejects(
      env.publish(
        selected,
        (view) => {
          reference = owner(view).export('test/Call', async (raw) => {
            callbacks++;
            return raw;
          });
          return reference.toJSON();
        },
        async () => {
          throw failure;
        },
      ),
      (error) => error === failure,
    );
    assert.deepEqual(selected.counts(), { exports: unpublished ? 0 : 1, imports: 0 });
    if (!unpublished) {
      assert.equal(await selected.import(reference, 'test/Call')(3), 3);
      assert.equal(callbacks, 1);
    }
    selected.release();
  }
});

test('a publication batch ends before its publisher creates another allocation', async (t) => {
  const p = await pair();
  t.after(p.close);
  const env = valueEnvironment(p.a);
  const selected = p.a.owner().child();
  let captured: unknown;
  let later!: Reference;
  let callbacks = 0;
  await assert.rejects(
    env.publish(
      selected,
      (view) => {
        captured = view;
        return owner(view)
          .export('test/Call', async (raw) => raw)
          .toJSON();
      },
      async () => {
        later = owner(captured).export('test/Call', async (raw) => {
          callbacks++;
          return raw;
        });
        throw new UnpublishedError(new Error('not sent'));
      },
    ),
    UnpublishedError,
  );
  assert.deepEqual(selected.counts(), { exports: 1, imports: 0 });
  assert.equal(await selected.import(later, 'test/Call')(4), 4);
  assert.equal(callbacks, 1);
});

test('one environment keeps overlapping operation owners independent', async (t) => {
  const p = await pair();
  t.after(p.close);
  const env = valueEnvironment(p.a);
  const first = p.a.owner().child(),
    second = p.a.owner().child();
  let unblock!: () => void;
  const blocked = new Promise<void>((resolve) => {
    unblock = resolve;
  });
  let entered = false;
  const sending = env.publish(
    first,
    (view) =>
      owner(view)
        .export('test/Call', async (raw) => raw)
        .toJSON(),
    async () => {
      entered = true;
      await blocked;
      throw new UnpublishedError(new Error('not sent'));
    },
  );
  assert.equal(entered, true);
  let retained!: Reference;
  let callbacks = 0;
  assert.equal(
    await env.publish(
      second,
      (view) => {
        retained = owner(view).export('test/Call', async (raw) => {
          callbacks++;
          return raw;
        });
        return retained.toJSON();
      },
      async () => 'accepted',
    ),
    'accepted',
  );
  unblock();
  await assert.rejects(sending, UnpublishedError);
  assert.deepEqual(first.counts(), { exports: 0, imports: 0 });
  assert.deepEqual(second.counts(), { exports: 1, imports: 0 });
  assert.equal(await second.import(retained, 'test/Call')(5), 5);
  assert.equal(callbacks, 1);
});

test('released selected owners stay terminal while new operations select a fresh root', async (t) => {
  const p = await pair();
  t.after(p.close);
  const env = valueEnvironment(p.a);
  const first = owner(env.select(undefined));
  assert.equal(env.select(p.b.owner()), first);
  first.release();
  const next = owner(env.select(undefined));
  assert.notEqual(next, first);
  assert.equal(env.select(first), first);
  for (const context of [first, env.child(first)]) {
    assert.throws(
      () => env.export(context, () => ({})),
      (error: unknown) => error instanceof DuplexError && error.code === 'reference_released',
    );
    assert.throws(
      () => env.import(context, () => assert.fail('released import dispatched')),
      (error: unknown) => error instanceof DuplexError && error.code === 'reference_released',
    );
    await assert.rejects(
      env.publish(
        context,
        () => assert.fail('released conversion dispatched'),
        async () => assert.fail('released publication dispatched'),
      ),
      (error: unknown) => error instanceof DuplexError && error.code === 'reference_released',
    );
  }
  const scalar = jsonAdapter<string>({ type: 'string', validate: createValidator({ types: {} }) });
  assert.equal(scalar.import(first, scalar.export(first, 'still data')), 'still data');
  assert.deepEqual(p.a.counts(), { exports: 0, imports: 0 });
});
