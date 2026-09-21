import assert from 'node:assert/strict';
import { DuplexError, DuplexPeer } from '@nightseam/runtime';
import { MemoryWireLog, pipe } from '@nightseam/duplex';
import { liveOver, valueEnvironment, type LiveScope } from '@nightseam/live';
import * as binding from '@example/handles-binding';
import * as client from '@example/handles-client';
import type { Callback } from '@example/handles-client';

async function until(test: () => boolean) {
  const end = Date.now() + 5000;
  while (!test()) {
    assert.ok(Date.now() < end, 'delivery deadline');
    await new Promise((resolve) => setTimeout(resolve, 1));
  }
}
async function localScope() {
  const [left, right] = pipe();
  let scope!: LiveScope;
  const a = new DuplexPeer({
      prepare: (peer) => {
        scope = liveOver(peer);
      },
    }),
    b = new DuplexPeer({
      role: 'server',
      prepare: (peer) => {
        liveOver(peer);
      },
    });
  await Promise.all([a.attach(left), b.attach(right)]);
  return {
    scope,
    close() {
      a.close();
      b.close();
    },
  };
}
function target(scope: LiveScope) {
  const values: Callback[] = [];
  const wire = client.toWire(
    () => ({
      methods: {},
      events: {
        handle: (value) => {
          values.push(value);
        },
      },
    }),
    { valueEnvironment: valueEnvironment(scope) },
  );
  return { wire, values };
}
const local = await localScope(),
  other = await localScope();
const owner = local.scope.owner().child();
const primary = target(local.scope);
const history = new MemoryWireLog();
const recorded = await binding.record(primary.wire, history, {}, { valueEnvironment: valueEnvironment(local.scope) });
try {
  let effects = 0;
  await recorded.append(
    {
      name: 'handle',
      data: async (value) => {
        effects++;
        return value + 1;
      },
    },
    { valueContext: owner },
  );
  await until(() => primary.values.length === 1);
  assert.equal(await primary.values[0]!(3), 4);
  const repeated = target(local.scope);
  const sameFollower = await recorded.follow(0, repeated.wire);
  await until(() => repeated.values.length === 1);
  assert.equal(await repeated.values[0]!(4), 5);
  assert.equal(owner.counts().exports, 1);
  const elsewhere = target(other.scope);
  const foreignFollower = await recorded.follow(0, elsewhere.wire);
  await until(() => elsewhere.values.length === 1);
  await assert.rejects(
    Promise.resolve().then(() => elsewhere.values[0]!(5)),
    (error) => error instanceof DuplexError && error.code === 'reference_unknown',
  );
  assert.equal(effects, 2);
  owner.release();
  assert.equal(owner.counts().exports, 0);
  await assert.rejects(
    Promise.resolve().then(() => repeated.values[0]!(6)),
    (error) => error instanceof DuplexError && error.code === 'reference_released',
  );
  const entry = await history.read(1, new AbortController().signal);
  const frame = entry.message.frame;
  assert.equal(frame.kind, 'event');
  if (frame.kind === 'event')
    assert.throws(
      () => client.importCallback(local.scope.owner(), frame.data),
      (error) => error instanceof DuplexError && error.code === 'reference_released',
    );
  assert.equal(effects, 2);
  sameFollower.close();
  foreignFollower.close();
  await Promise.all([sameFollower.done, foreignFollower.done]);
} finally {
  recorded.close();
  local.close();
  other.close();
}
