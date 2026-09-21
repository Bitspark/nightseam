import assert from 'node:assert/strict';
import * as cell from '@probe/compose-cell-binding';
import { jsonAdapter } from '@nightseam/runtime';

const adapter = jsonAdapter<number>({ type: 'integer', validate: cell.validateWire });
let value = 0;
const wire = cell.toWire(() => ({ methods: { put(input) { value = input.value; return 1; }, get() { return value; } }, events: { noted() {} } }), {}, adapter);
try {
  const model = await cell.fromWire(wire, {}, adapter);
  const access = model({ methods: { mirror: input => input.value }, events: { changed() {} } });
  assert.equal(await access.methods.put({ value: 42 }), 1);
  assert.equal(await access.methods.get({}), 42);
  console.log('packed generic TypeScript scalar Cell: 42 without a live environment');
} finally {
  wire.close();
}
