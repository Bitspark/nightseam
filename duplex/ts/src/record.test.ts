import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { at, mount, type Message, type Path, type Wire } from './wire.ts';
import { record, MemoryWireLog, RecordError, type WireRecord } from './record.ts';

function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
class Target implements Wire {
  refuse: Error | undefined;
  values: WireRecord[] = [];
  closed = false;
  changed = deferred();
  send(path: Path, message: Message) {
    if (this.refuse) throw this.refuse;
    if (this.closed) throw new Error('closed');
    this.values.push({ sequence: 0, path, message });
    this.changed.resolve();
    this.changed = deferred();
  }
  receive() {
    return () => {};
  }
  close() {
    this.closed = true;
  }
  async take() {
    while (!this.values.length) await this.changed.promise;
    return this.values.shift()!;
  }
}
const message = (value: number): Message => ({ frame: { version: 1, kind: 'event', data: value } });
class HeldLog extends MemoryWireLog {
  entered = deferred();
  release = deferred();
  holdAppend = false;
  failure: Error | undefined;
  async hold(signal: AbortSignal) {
    this.entered.resolve();
    await Promise.race([
      this.release.promise,
      new Promise<never>((_, reject) => {
        if (signal.aborted) reject(signal.reason);
        else signal.addEventListener('abort', () => reject(signal.reason), { once: true });
      }),
    ]);
    if (this.failure) throw this.failure;
  }
  override async read(sequence: number, signal: AbortSignal) {
    if (!this.holdAppend) await this.hold(signal);
    return super.read(sequence, signal);
  }
  override async append(path: Path, m: Message, signal: AbortSignal) {
    if (this.holdAppend) await this.hold(signal);
    return super.append(path, m, signal);
  }
}
const table = JSON.parse(
  readFileSync(new URL('../../../conformance/tables/recorded-wire.json', import.meta.url), 'utf8'),
) as {
  cases: Array<{ name: string; before: number[]; during: number[]; after: number; head: number; expected: number[] }>;
};
for (const row of table.cases)
  test(`record/follow: ${row.name}`, async () => {
    const store = new HeldLog();
    const w = await record(new Target(), store, { maxQueuedMessages: 8 });
    try {
      const source = at(mount(new Map([['history', w]])), ['history']);
      for (const v of row.before) source.send(['tick'], message(v));
      const target = new Target();
      const f = await w.follow(row.after, target);
      assert.equal(f.head, row.head);
      await store.entered.promise;
      for (const v of row.during) source.send(['tick'], message(v));
      assert.equal(await w.head(), row.before.length + row.during.length);
      store.release.resolve();
      const got = [];
      for (const _ of row.expected) {
        const entry = await target.take();
        assert.deepEqual(entry.path, ['tick']);
        assert.equal(entry.message.frame.kind, 'event');
        if (entry.message.frame.kind === 'event') got.push(entry.message.frame.data);
      }
      assert.deepEqual(got, row.expected);
      w.send(['fence'], message(99));
      const fence = await target.take();
      assert.deepEqual(fence.path, ['fence']);
      f.close();
      await f.done;
    } finally {
      w.close();
    }
  });
class BrokenSequenceLog extends MemoryWireLog {
  appendGap = false;
  override async append(path: Path, m: Message, signal: AbortSignal) {
    const next = await super.append(path, m, signal);
    return next + (this.appendGap ? 1 : 0);
  }
  override async read(at: number, signal: AbortSignal) {
    return { ...(await super.read(at, signal)), sequence: at + 1 };
  }
}
for (const mode of ['append', 'sequence', 'target'])
  test(`recorder ${mode} failure ends only its carrier and permits reentrant close observation`, async () => {
    const sentinel = new Error('consumer failure');
    const target = new Target();
    let store: MemoryWireLog = new MemoryWireLog();
    if (mode === 'append') {
      const held = new HeldLog();
      held.holdAppend = true;
      held.failure = sentinel;
      held.release.resolve();
      store = held;
    }
    if (mode === 'sequence') {
      const broken = new BrokenSequenceLog();
      broken.appendGap = true;
      store = broken;
    }
    if (mode === 'target') target.refuse = sentinel;
    const ended = deferred<unknown>();
    const w = await record(target, store, {
      onClose: (error) => {
        w.close();
        void assert.rejects(w.head()).then(() => ended.resolve(error));
      },
    });
    w.send([], message(1));
    const error = await ended.promise;
    if (mode === 'sequence') assert.ok(error instanceof RecordError && error.code === 'sequence');
    else assert.equal(error, sentinel);
    assert.equal(target.closed, true);
  });
for (const mode of ['read', 'sequence', 'target'])
  test(`follower ${mode} failure leaves recorder usable`, async () => {
    const sentinel = new Error('replay failure');
    let store: MemoryWireLog = new MemoryWireLog();
    if (mode === 'read') {
      const held = new HeldLog();
      held.failure = sentinel;
      held.release.resolve();
      store = held;
    }
    if (mode === 'sequence') store = new BrokenSequenceLog();
    await store.append([], message(1), new AbortController().signal);
    const w = await record(new Target(), store);
    try {
      const target = new Target();
      if (mode === 'target') target.refuse = sentinel;
      const f = await w.follow(0, target);
      await f.done;
      if (mode === 'sequence') assert.ok(f.error instanceof RecordError && f.error.code === 'sequence');
      else assert.equal(f.error, sentinel);
      assert.equal(await w.head(), 1);
    } finally {
      w.close();
    }
  });
test('stalled follower closes its mount while producer and healthy follower continue', async () => {
  const store = new HeldLog();
  const w = await record(new Target(), store, { maxQueuedMessages: 2 });
  try {
    w.send(['tick'], message(1));
    const slowTarget = new Target();
    const slow = await w.follow(0, at(mount(new Map([['slow', slowTarget]])), ['slow']));
    await store.entered.promise;
    const target = new Target();
    const fast = await w.follow(1, target);
    for (let i = 2; i <= 4; i++) {
      w.send(['tick'], message(i));
      await target.take();
    }
    await slow.done;
    assert.ok(slow.error instanceof RecordError);
    assert.equal(slow.error.code, 'overflow');
    assert.equal(slowTarget.closed, false);
    slowTarget.send(['probe'], message(99));
    w.send(['tick'], message(5));
    await target.take();
    fast.close();
    await fast.done;
  } finally {
    w.close();
  }
});
test('blocked storage cannot pace send beyond its admission bound', async () => {
  const store = new HeldLog();
  store.holdAppend = true;
  const target = new Target();
  const ended = deferred<unknown>();
  const w = await record(target, store, { maxQueuedMessages: 2, onClose: (error) => ended.resolve(error) });
  w.send([], message(1));
  await store.entered.promise;
  w.send([], message(2));
  w.send([], message(3));
  assert.throws(
    () => w.send([], message(4)),
    (error) => error instanceof RecordError && error.code === 'overflow',
  );
  assert.ok((await ended.promise) instanceof RecordError);
  assert.equal(target.closed, true);
});
test('cursor validation, cancellation and local return identity', async () => {
  const target = new Target();
  const store = new MemoryWireLog();
  const w = await record(target, store);
  try {
    await assert.rejects(
      w.follow(1, new Target()),
      (error) => error instanceof RecordError && error.code === 'sequence',
    );
    const address = { wire: target };
    w.send(['', 'é'], { ...message(1), return: address });
    const destination = new Target();
    const controller = new AbortController();
    const f = await w.follow(0, destination, controller.signal);
    const entry = await destination.take();
    assert.equal(entry.message.return, address);
    controller.abort();
    await f.done;
    assert.equal(f.error, controller.signal.reason);
    f.close();
    f.close();
    await assert.rejects(store.read(0, new AbortController().signal), RecordError);
  } finally {
    w.close();
  }
});
