import assert from 'node:assert/strict';
import test from 'node:test';
import { createValidator, jsonAdapter, type TypeBinding, type ValueContext, type ValueOptions } from './index.ts';

test('scalar adapters retain supplied declaration bindings and need no environment', () => {
  const count = createValidator({ types: { Count: { kind: 'alias', type: 'integer' } } }, '');
  const validate = createValidator({ parameters: [{ name: 'T' }], types: {} }, '');
  const binding: TypeBinding = { type: 'T', validate, slots: { T: { type: 'Count', validate: count } } };
  const adapter = jsonAdapter<number>(binding);
  assert.equal(adapter.binding, binding);
  assert.equal(adapter.needsContext, false);
  const untouched = new Proxy(
    {},
    {
      get() {
        throw new Error('scalar inspected a context');
      },
    },
  );
  for (const context of [undefined, untouched]) {
    assert.equal(adapter.import(context, adapter.export(context, 7)), 7);
    assert.throws(() => adapter.import(context, 'wrong'), /integer/);
    assert.throws(() => adapter.export(context, Number.NaN));
  }
});

// A scalar specialization keeps the ordinary operation shape, while a nested
// callable requires an opaque supplied context without importing its provider.
type Assert<T extends true> = T;
type Scalar = Assert<keyof ValueContext<string, { signal: AbortSignal }> extends 'signal' ? true : false>;
type Nested = ValueContext<{ callbacks: Array<() => void> }, { signal: AbortSignal }>;
type Required = Assert<{} extends Pick<Nested, 'valueContext'> ? false : true>;
type Optional = Assert<{} extends ValueOptions<() => void, {}> ? true : false>;
const typeChecks: [Scalar, Required, Optional] = [true, true, true];
assert.deepEqual(typeChecks, [true, true, true]);
