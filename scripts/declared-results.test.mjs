import assert from 'node:assert/strict';
import { test } from 'node:test';
import { compareDeclared, declaredInputs } from './declared-results.mjs';

const fixture = { schemaVersion: 2, declarations: [{ id: 'root' }], cases: [
  { id: 'a', steps: [{ op: 'send' }], expected: { admitted: true } },
  { id: 'b', steps: [], expected: { refused: true } },
] };
const output = () => fixture.cases.map(c => ({ id: c.id, observations: structuredClone(c.expected) }));

test('declared driver inputs contain no oracle and do not alias its fixtures', () => {
  const inputs = declaredInputs(fixture);
  assert.equal('expected' in inputs.cases[0], false);
  inputs.declarations[0].id = 'changed';
  inputs.cases[0].steps.length = 0;
  assert.equal(fixture.declarations[0].id, 'root');
  assert.equal(fixture.cases[0].steps.length, 1);
});
test('declared oracle rejects missing, extra, duplicate, unknown and changed observations', () => {
  compareDeclared(fixture, output().reverse());
  assert.throws(() => compareDeclared(fixture, output().slice(1)));
  assert.throws(() => compareDeclared(fixture, [...output(), output()[0]]));
  assert.throws(() => compareDeclared(fixture, [output()[0], output()[0]]));
  const unknown = output(); unknown[0].id = 'unknown';
  assert.throws(() => compareDeclared(fixture, unknown));
  const changed = output(); changed[0].observations.admitted = false;
  assert.throws(() => compareDeclared(fixture, changed));
});
