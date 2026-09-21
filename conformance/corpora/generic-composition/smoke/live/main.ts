import assert from 'node:assert/strict';
import * as cell from '@probe/compose-cell-binding';
import * as functions from '@probe/functions-client/types';
import * as holder from '@probe/holder-client/types';
import * as numbers from '@probe/numbers-client/types';
import * as texts from '@probe/texts-client/types';
import { DuplexPeer, type ValueAdapter } from '@nightseam/runtime';
import { liveOver, valueEnvironment } from '@nightseam/live';

const url = process.argv[2]!;
const selected = process.argv[3]!;
const empty = { exports: 0, imports: 0 };
let effects = 0;
const options = () => ({ signal: AbortSignal.timeout(8000) });

async function exercise<T>(adapter: ValueAdapter<T>, specimen: (seed: number) => T, observe: (value: T) => Promise<unknown>, expected: unknown[]) {
  const peer = new DuplexPeer();
  const scope = liveOver(peer);
  const prepared = cell.prepareFromWire(peer.wire(), { valueEnvironment: valueEnvironment(scope) }, adapter);
  const owner = scope.owner().child();
  try {
    await peer.connect(url);
    const model = await prepared.complete(options());
    const access = model({ methods: { mirror: input => input.value }, events: { changed() {} } });
    const call = () => ({ ...options(), valueContext: owner });
    assert.equal(await access.methods.put({ value: specimen(1) }, call()), 1);
    const saved = await access.methods.get({}, call());
    assert.equal(await access.methods.put({ value: specimen(2) }, call()), 2);
    const current = await access.methods.get({}, call());
    // All supplying RPCs returned, and replacement did not revoke saved.
    assert.deepEqual([await observe(saved), await observe(current), await observe(saved)], expected);
    assert.deepEqual(scope.owner().counts(), empty, 'generic recipe captured root lifetime');
    assert(owner.counts().exports > 0 && owner.counts().imports > 0, 'live values did not cross the socket');
    owner.release();
    assert.deepEqual(await peer.call('smoke.drop', {}, options()), empty);
    const deadline = Date.now() + 8000;
    while (scope.counts().exports || scope.counts().imports) {
      assert(Date.now() < deadline, 'bindings remained before teardown');
      await new Promise(resolve => setTimeout(resolve, 1));
    }
  } finally {
    owner.release();
    prepared.close();
    peer.close();
  }
}

switch (selected) {
  case 'function':
    // This source alias emits its own specialized body; Go supplies the
    // generic constructor. Nominal Function application identity must agree.
    await exercise(functions.adapterIntegerFunction(), seed => async n => { effects++; return seed + n; }, async value => value(5, options()), [6, 7, 6]);
    assert.equal(effects, 3);
    break;
  case 'numbers': {
    const specimen = (seed: number): holder.Batch<numbers.Family> => [null, {
      value: { kind: 'value', value: { job: { run: async n => { effects++; return seed + n; } }, progress: { label: 'numbers', notify: { run: async n => { effects++; return seed * 10 + n; } } } } },
      empty: { kind: 'empty' },
    }];
    await exercise(holder.adapterBatch(numbers.family), specimen, async value => {
      assert.equal(value[0], null); assert.equal(value[1]!.empty!.kind, 'empty');
      const choice = value[1]!.value!; assert.equal(choice.kind, 'value');
      if (choice.kind !== 'value') throw new Error('missing nested live draw');
      assert.equal(choice.value.progress.label, 'numbers');
      return [await choice.value.job.run(5, options()), await choice.value.progress.notify.run(5, options())];
    }, [[6, 15], [7, 25], [6, 15]]);
    assert.equal(effects, 6);
    break;
  }
  case 'texts': {
    const specimen = (seed: number): holder.Batch<texts.Family> => [null, {
      value: { kind: 'value', value: { job: { run: async n => { effects++; return `${n}:${seed}`; } }, progress: { label: 'texts', notify: { run: async n => { effects++; return `${n}:${seed * 10}`; } } } } },
      empty: { kind: 'empty' },
    }];
    await exercise(holder.adapterBatch(texts.family), specimen, async value => {
      assert.equal(value[0], null); assert.equal(value[1]!.empty!.kind, 'empty');
      const choice = value[1]!.value!; assert.equal(choice.kind, 'value');
      if (choice.kind !== 'value') throw new Error('missing nested live draw');
      assert.equal(choice.value.progress.label, 'texts');
      return [await choice.value.job.run('v', options()), await choice.value.progress.notify.run('v', options())];
    }, [['v:1', 'v:10'], ['v:2', 'v:20'], ['v:1', 'v:10']]);
    assert.equal(effects, 6);
    break;
  }
  default: throw new Error(`unexercised generic recipe ${selected}`);
}
console.log(`packed generic ${selected}: retained values, ${effects} callback effects, and zero bindings before teardown`);
