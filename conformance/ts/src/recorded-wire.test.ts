import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { recordedWireWitness } from './recorded-wire.ts';

test('recorded wire head and order match the shared scenario', { timeout: 5000 }, async () => {
  const scenario = JSON.parse(
    readFileSync(new URL('../../scenarios/peer/recorded-wire-head-and-order.json', import.meta.url), 'utf8'),
  );
  assert.deepEqual(await recordedWireWitness(2000), scenario.steps[0].expect);
});
