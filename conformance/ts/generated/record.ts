import { at, mount, MemoryWireLog, type Wire } from '@nightseam/duplex';
import { forwardWire, jsonAdapter, wirePair } from '@nightseam/runtime';
import * as client from './api/ts/cell-client/src/index.ts';
import * as binding from './api/ts/cell-binding/src/index.ts';
import { Inbox } from './server.ts';

const adapter = () => jsonAdapter<string>({ type: 'string', validate: client.validateWire });
function destination(values: Inbox<string>): Wire {
  return binding.toWire(
    () => ({
      methods: { put: () => 0, get: () => '', roundTrip: () => '' },
      events: { noted: (value) => values.put(value.value) },
    }),
    {},
    adapter(),
  );
}
export async function recordRead(values: Inbox<unknown>, within: number): Promise<unknown[]> {
  const result = [];
  for (let i = 0; i < 2; i++) {
    const value = await values.take(within);
    if (value === undefined) throw new Error('recorded event delivery deadline');
    result.push(value);
  }
  return result;
}
export async function recordExercise(target: Wire, within: number) {
  const originals = new Inbox<string>();
  const primary = destination(originals);
  const recorded = await client.record(primary, new MemoryWireLog(), {}, {}, adapter());
  try {
    await recorded.append({ name: 'noted', data: { value: 'first' } });
    await recorded.head();
    const follower = await recorded.follow(0, target);
    await recorded.append({ name: 'noted', data: { value: 'second' } });
    const head = await recorded.head();
    const original = await recordRead(originals, within);
    return { result: { follower_head: follower.head, head, original }, close: () => recorded.close() };
  } catch (error) {
    recorded.close();
    throw error;
  }
}
export async function recordLocal(presentation: string, within: number) {
  const values = new Inbox<string>();
  const root = destination(values);
  let target = root;
  const cleanup: Array<() => void> = [];
  if (presentation !== 'local') {
    const tree = mount(new Map([['outer', mount(new Map([['leaf', root]]))]]));
    target = at(tree, ['outer', 'leaf']);
    cleanup.push(() => tree.close());
  }
  if (presentation === 'forwarded') {
    const [left, right] = wirePair();
    cleanup.push(
      forwardWire(left, target),
      () => left.close(),
      () => right.close(),
    );
    target = right;
  }
  try {
    const exercise = await recordExercise(target, within);
    cleanup.push(exercise.close);
    return { ...exercise.result, replayed: await recordRead(values, within) };
  } finally {
    for (const close of cleanup.reverse()) close();
    root.close();
  }
}
