import assert from 'node:assert/strict';
import { MemoryWireLog, pipe } from '@nightseam/duplex';
import { DuplexPeer, jsonAdapter, type FamilyBinding } from '@nightseam/runtime';
import { liveOver, valueEnvironment, type LiveScope } from '@nightseam/live';
import * as handles from '@example/handles-client';
import * as binding from '@example/mixed-binding';
import * as client from '@example/mixed-client';

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
const owner = scope.owner().child(),
  context = { valueEnvironment: valueEnvironment(scope) };
const adapter = jsonAdapter<string>({ type: 'string', validate: client.validateWire });
const values: client.Payload<string, handles.Family>[] = [];
const target = () =>
  client.toWire<string, handles.Family>(
    () => ({
      methods: {},
      events: {
        changed: (value) => {
          values.push(value);
        },
      },
    }),
    context,
    adapter,
    handles.family,
  );
const primary = target(), subscriber = target(),
  log = new MemoryWireLog();
try {
  const missing = { ...handles.family, types: {} } as unknown as FamilyBinding<handles.Family, 'Box'>;
  await assert.rejects(binding.record<string, handles.Family>(primary, log, {}, context, adapter, missing));
  assert.equal(await log.head(new AbortController().signal), 0);
  assert.equal(values.length, 0);
  const recorder = await binding.record<string, handles.Family>(primary, log, {}, context, adapter, handles.family);
  try {
    await recorder.append(
      { name: 'changed', data: { item: 'native', callback: { invoke: async (value) => value + 10 } } },
      { valueContext: owner },
    );
    const wait = async (count: number) => {
      const end = Date.now() + 5000;
      while (values.length < count) {
        assert.ok(Date.now() < end);
        await new Promise((resolve) => setTimeout(resolve, 1));
      }
    };
    await wait(1);
    assert.equal(values[0]!.item, 'native');
    assert.equal(await values[0]!.callback.invoke(2), 12);
    const follower = await recorder.follow(0, subscriber);
    await wait(2);
    assert.equal(values[1]!.item, 'native');
    assert.equal(await values[1]!.callback.invoke(2), 12);
    assert.equal(owner.counts().exports, 1);
    follower.close();
    await follower.done;
  } finally {
    recorder.close();
  }
} finally {
  owner.release();
  primary.close();
  subscriber.close();
  a.close();
  b.close();
}
