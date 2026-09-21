/** Recipes are made once, before any carrier, owner or model is constructed. */
import { jsonAdapter, type ValueAdapter } from '@nightseam/runtime';
import * as functions from './api/ts/functions-client/src/index.ts';
import * as numbers from './api/ts/numbers-client/src/index.ts';
import * as texts from './api/ts/texts-client/src/index.ts';
import * as holder from './api/ts/holder-client/src/index.ts';

interface CompositionSlot<T> {
  adapter: ValueAdapter<T>;
  retain: true;
  make(add: number): T;
  observe(value: T): Promise<unknown>;
}
const integer = jsonAdapter<number>({ type: 'integer', validate: functions.validateWire });
export const genericFunctionSlot: CompositionSlot<functions.Function<number, number>> = {
  adapter: functions.adapterFunction(integer, integer), retain: true,
  make: add => async value => value + add,
  observe: async value => value(5),
};
// This helper has its own specialized body; it does not delegate to adapterFunction.
export const integerFunctionSlot: CompositionSlot<functions.IntegerFunction> = {
  adapter: functions.adapterIntegerFunction(), retain: true,
  make: add => async value => value + add,
  observe: async value => value(5),
};
export const genericFactorySlot: CompositionSlot<functions.Factory> = {
  adapter: functions.adapterFactory(), retain: true,
  make: add => async callback => async value => (await callback(value)) + add,
  observe: async value => {
    let calls = 0;
    const returned = await value(async n => { calls++; return n + 3; });
    const answer = await returned(5);
    if (calls !== 1) throw new Error('generic higher-order callback did not run exactly once');
    return answer;
  },
};
export const holderNumbersSlot: CompositionSlot<holder.Batch<numbers.Family>> = {
  adapter: holder.adapterBatch(numbers.family), retain: true,
  make: add => [null, { kept: { kind: 'value', value: {
    job: { run: async n => n + add },
    progress: { label: 'kept', notify: { run: async n => n + 10 * add } },
  } }, empty: { kind: 'empty' } }],
  observe: async batch => {
    if (batch.length !== 2 || batch[0] !== null || batch[1]?.empty?.kind !== 'empty') throw new Error('Holder lost nullable, map or empty union structure');
    const choice = batch[1]?.kept;
    if (choice?.kind !== 'value' || choice.value.progress.label !== 'kept') throw new Error('Holder lost the value or its label');
    return { job: await choice.value.job.run(5), notify: await choice.value.progress.notify.run(5) };
  },
};
export const holderTextsSlot: CompositionSlot<holder.Batch<texts.Family>> = {
  adapter: holder.adapterBatch(texts.family), retain: true,
  make: add => [null, { kept: { kind: 'value', value: {
    job: { run: async value => value + String(add) },
    progress: { label: 'kept', notify: { run: async value => value + String(10 * add) } },
  } }, empty: { kind: 'empty' } }],
  observe: async batch => {
    if (batch.length !== 2 || batch[0] !== null || batch[1]?.empty?.kind !== 'empty') throw new Error('Holder lost nullable, map or empty union structure');
    const choice = batch[1]?.kept;
    if (choice?.kind !== 'value' || choice.value.progress.label !== 'kept') throw new Error('Holder lost the value or its label');
    return { job: await choice.value.job.run('v:'), notify: await choice.value.progress.notify.run('v:') };
  },
};
